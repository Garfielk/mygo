//go:build darwin

package darwin

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/watchdriver"
)

// Watches use FSEvents: one stream per directory target, covering the whole
// tree beneath it, so a watch costs no descriptor per file and a large
// folder is fine. The stream reports what changed (with
// kFSEventStreamCreateFlagFileEvents) rather than only that something did,
// but the notices stay hints: the core compares snapshots, and a notice
// that changes may have been dropped (MustScanSubDirs, a user or kernel
// drop) makes it look at everything again, as an overflow does elsewhere.

// FSEventStreamCreateFlags.
const (
	fseWatchRoot  = 0x00000004 // kFSEventStreamCreateFlagWatchRoot
	fseFileEvents = 0x00000010 // kFSEventStreamCreateFlagFileEvents
)

// FSEventStreamEventFlags.
const (
	fseMustScanSubDirs = 0x00000001
	fseUserDropped     = 0x00000002
	fseKernelDropped   = 0x00000004
	fseEventIdsWrapped = 0x00000008
	fseHistoryDone     = 0x00000010
	fseRootChanged     = 0x00000020
	fseMount           = 0x00000040
	fseUnmount         = 0x00000080
	fseItemCreated     = 0x00000100
	fseItemRemoved     = 0x00000200
	fseItemInodeMeta   = 0x00000400
	fseItemRenamed     = 0x00000800
	fseItemModified    = 0x00001000
	fseItemFinderInfo  = 0x00002000
	fseItemChangeOwner = 0x00004000
	fseItemXattr       = 0x00008000
	fseItemIsFile      = 0x00010000
	fseItemIsDir       = 0x00020000
	fseItemIsSymlink   = 0x00040000
)

const (
	// fseLatency is how long FSEvents may coalesce events: 10 ms, well
	// under the core's 50 ms Debounce, so the core's trailing debounce
	// does the coalescing an app sees and the stream only batches bursts.
	fseLatency = 0.01
	// fseQueueSize is how many notices a watch buffers before it asks the
	// core to look at everything again.
	fseQueueSize = 4096
	// fseBatch is how many notices Wait returns at a time.
	fseBatch = 128
	// fseUTF8 is kCFStringEncodingUTF8.
	fseUTF8 = 0x08000100
	// fseFramework is where the FSEvents C API lives.
	fseFramework = "/System/Library/Frameworks/CoreServices.framework/Frameworks/FSEvents.framework/FSEvents"
)

// The FSEvents API, bound once.
var (
	fseStreamCreate     func(alloc, callback uintptr, context *fseContext, paths uintptr, sinceWhen uint64, latency float64, flags uint32) uintptr
	fseStreamStart      func(stream uintptr) uint8
	fseStreamStop       func(stream uintptr)
	fseStreamInvalidate func(stream uintptr)
	fseStreamRelease    func(stream uintptr)
	fseStreamSetQueue   func(stream, queue uintptr)

	cfStringCreateWithCString func(alloc uintptr, s *byte, encoding uint32) uintptr
	cfArrayCreate             func(alloc uintptr, values *uintptr, n int, callbacks uintptr) uintptr
	kCFTypeArrayCallBacks     uintptr

	watchQueueCreate func(label *byte, attr uintptr) uintptr
	dispatchSyncF    func(queue, context, work uintptr)

	// fseQueue is the one serial queue every stream delivers on, off the
	// main thread, so stopping a stream can wait for a callback already
	// running with dispatch_sync_f.
	fseQueue uintptr
	fseOnce  sync.Once
)

// loadFSEvents binds FSEvents, CoreFoundation's string and array
// constructors and libdispatch, once, whoever asks first (NewFileWatch can
// run before the app does, and tests make a Backend directly).
func loadFSEvents() {
	fseOnce.Do(func() {
		load()
		lib := libCoreServices
		// CoreServices re-exports FSEvents; the subframework, when it is
		// there, pins the symbols to it.
		if h, err := purego.Dlopen(fseFramework, purego.RTLD_GLOBAL|purego.RTLD_NOW); err == nil {
			lib = h
		}
		purego.RegisterLibFunc(&fseStreamCreate, lib, "FSEventStreamCreate")
		purego.RegisterLibFunc(&fseStreamStart, lib, "FSEventStreamStart")
		purego.RegisterLibFunc(&fseStreamStop, lib, "FSEventStreamStop")
		purego.RegisterLibFunc(&fseStreamInvalidate, lib, "FSEventStreamInvalidate")
		purego.RegisterLibFunc(&fseStreamRelease, lib, "FSEventStreamRelease")
		purego.RegisterLibFunc(&fseStreamSetQueue, lib, "FSEventStreamSetDispatchQueue")
		purego.RegisterLibFunc(&cfStringCreateWithCString, libCF, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfArrayCreate, libCF, "CFArrayCreate")
		kCFTypeArrayCallBacks = mustDlsym(libCF, "kCFTypeArrayCallBacks")
		purego.RegisterLibFunc(&watchQueueCreate, purego.RTLD_DEFAULT, "dispatch_queue_create")
		purego.RegisterLibFunc(&dispatchSyncF, purego.RTLD_DEFAULT, "dispatch_sync_f")
		label := []byte("mygo.watch\x00")
		fseQueue = watchQueueCreate(&label[0], 0)
	})
}

// fseContext mirrors FSEventStreamContext. FSEvents copies it, and hands
// info back to the callback, which is how the one callback finds its stream.
type fseContext struct {
	version         uintptr
	info            uintptr
	retain          uintptr
	release         uintptr
	copyDescription uintptr
}

// fseStream is one FSEventStream watching one directory target.
type fseStream struct {
	id      uint64
	path    string
	subtree bool
	driver  *fseWatch
	stream  uintptr
	started bool
	context fseContext
}

// fseStreams routes the one FSEventStreamCallback of the process to the
// stream of the event, by the context info FSEvents hands back.
var fseStreams = struct {
	sync.Mutex
	m map[uintptr]*fseStream
}{m: map[uintptr]*fseStream{}}

// fseCallback is the process's one FSEventStreamCallback, created at
// startup and never freed. Without kFSEventStreamCreateFlagUseCFTypes the
// paths are a C array of C strings.
var fseCallback = purego.NewCallback(func(stream, info, numEvents, paths, flags, ids uintptr) uintptr {
	if info == 0 {
		return 0
	}
	fseStreams.Lock()
	s := fseStreams.m[info]
	fseStreams.Unlock()
	if s == nil {
		return 0
	}
	// The addresses come from FSEvents, not from Go: keep them out of
	// uintptr conversions the compiler and the GC don't understand.
	eventPaths, eventFlags := ptrOf(paths), ptrOf(flags)
	for i := uintptr(0); i < numEvents; i++ {
		p := *(*uintptr)(unsafe.Add(eventPaths, i*unsafe.Sizeof(uintptr(0))))
		if p == 0 {
			continue
		}
		path := cString(ptrOf(p))
		if n, ok := s.event(path, *(*uint32)(unsafe.Add(eventFlags, i*4))); ok {
			s.deliver(n)
		}
	}
	return 0
})

// fseFlush is what dispatch_sync_f runs to wait for the callbacks already
// queued: our callback never blocks, so this cannot deadlock.
var fseFlush = purego.NewCallback(func() {})

// ptrOf returns the pointer at the native address p.
func ptrOf(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// cString copies the NUL-terminated string at p.
func cString(p unsafe.Pointer) string {
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}

// event translates one FSEvents event of the stream into a notice: pure,
// so the mapping can be tested without the framework.
func (s *fseStream) event(path string, flags uint32) (platform.FileWatchNotice, bool) {
	prefix := s.path
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	rel, ok := strings.CutPrefix(path, prefix)
	if !ok {
		if path != s.path {
			return platform.FileWatchNotice{}, false
		}
		rel = ""
	}
	if rel != "" && !s.subtree {
		// FSEvents is recursive, but the core rejects a name with a slash
		// for a target without Subtree: name the direct child, whose stat
		// is what a non-recursive watch is about.
		rel, _, _ = strings.Cut(rel, "/")
	}
	n := platform.FileWatchNotice{Target: s.id, Name: rel}
	switch {
	case flags&(fseMustScanSubDirs|fseUserDropped|fseKernelDropped|fseEventIdsWrapped) != 0:
		// Changes were dropped: the core lists everything again.
		return platform.FileWatchNotice{Target: s.id, Err: platform.ErrFileWatchOverflow}, true
	case flags&fseHistoryDone != 0:
		return platform.FileWatchNotice{}, false
	case flags&fseRootChanged != 0:
		// The watched directory was moved, renamed or replaced: the core
		// stats it and ends the watch when it is gone.
		return n, true
	case flags&fseUnmount != 0:
		n.Kind = platform.FileWatchLost
	case flags&fseMount != 0:
		// A volume appeared in the tree.
	case flags&(fseItemRemoved|fseItemRenamed|fseItemCreated) != 0:
		// Dirty: the core looks. A write on APFS sets ItemCreated too (an
		// O_TRUNC write reports the file as created), and a metadata
		// change can carry ItemModified, so the structural flags must win:
		// only a plain content write is a Write hint, and the core's
		// snapshot comparison reports the writes that are dirty instead.
	case flags&fseItemModified != 0 && flags&fseItemIsFile != 0:
		n.Kind = platform.FileWatchWrite
	}
	return n, true
}

// deliver hands a notice to Wait, or, with the queue full, asks the core to
// look at everything again. It never blocks: it runs on the dispatch queue,
// which a stream's teardown waits on.
func (s *fseStream) deliver(n platform.FileWatchNotice) {
	select {
	case s.driver.notices <- n:
	default:
		s.driver.overflow.Store(true)
	}
}

// stop stops and releases the stream. Only the goroutine of the watch calls
// it, but a callback may be running: dispatch_sync_f waits for the queue,
// after which nothing touches s.
func (s *fseStream) stop() {
	if s.stream == 0 {
		return
	}
	if s.started {
		fseStreamStop(s.stream)
		s.started = false
	}
	fseStreamInvalidate(s.stream)
	dispatchSyncF(fseQueue, 0, fseFlush)
	fseStreams.Lock()
	delete(fseStreams.m, s.context.info)
	fseStreams.Unlock()
	fseStreamRelease(s.stream)
	s.stream = 0
}

// fseWatch is the FSEvents implementation of watchdriver.Driver: one stream
// per directory target, all delivering on fseQueue.
type fseWatch struct {
	// notices is filled by the callback, on the dispatch queue, and read
	// by Wait on the goroutine of the watch.
	notices chan platform.FileWatchNotice
	// wake interrupts Wait, buffered so Wake never waits.
	wake chan struct{}
	// overflow says notices was full, so the core must look at everything.
	overflow atomic.Bool
	// streams are the running streams by target ID; only the goroutine of
	// the watch (Add, Remove, Close) uses it.
	streams map[uint64]*fseStream
}

func (*Backend) NewFileWatch(notify func(platform.FileWatchNotice)) (platform.FileWatch, error) {
	loadFSEvents()
	if fseQueue == 0 {
		return nil, errors.New("cannot create the watch dispatch queue")
	}
	d := &fseWatch{
		notices: make(chan platform.FileWatchNotice, fseQueueSize),
		wake:    make(chan struct{}, 1),
		streams: map[uint64]*fseStream{},
	}
	return watchdriver.New(d, notify), nil
}

// covered reports whether a whole tree above path is already watched: a
// target the core enrolls below it needs no stream of its own. Only the
// goroutine of the watch calls it, so streams needs no lock.
func (d *fseWatch) covered(path string) bool {
	for _, s := range d.streams {
		prefix := s.path
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		if path == s.path || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (d *fseWatch) Add(t platform.FileWatchTarget) error {
	if !t.Directory {
		// A file target is covered by the stream of the directory that
		// holds it, which the core enrolls for a file watch (watchParent),
		// and FSEvents watches trees, not single files.
		return nil
	}
	if d.covered(t.Path) {
		// A stream above t already watches its whole tree: FSEvents is
		// recursive, so a target beneath it needs no stream of its own.
		// The core enrolls children of a directory it doesn't watch
		// recursively, and below their stream's path events still arrive,
		// named by the direct child of the target that has it.
		return nil
	}
	info, err := os.Lstat(t.Path)
	if err != nil {
		return err
	}
	if !os.SameFile(info, t.Info) {
		return errors.New("watch target identity changed")
	}
	path := append([]byte(t.Path), 0)
	cfPath := cfStringCreateWithCString(0, &path[0], fseUTF8)
	runtime.KeepAlive(path)
	if cfPath == 0 {
		return errors.New("cannot make the watch path a CFString")
	}
	values := []uintptr{cfPath}
	cfPaths := cfArrayCreate(0, &values[0], 1, kCFTypeArrayCallBacks)
	runtime.KeepAlive(values)
	if cfPaths == 0 {
		cfRelease(cfPath)
		return errors.New("cannot make the watch paths a CFArray")
	}
	s := &fseStream{id: t.ID, path: t.Path, subtree: t.Subtree, driver: d}
	s.context.info = uintptr(unsafe.Pointer(s))
	// kFSEventStreamEventIdSinceNow: the core's snapshot is taken already.
	stream := fseStreamCreate(0, fseCallback, &s.context, cfPaths, ^uint64(0), fseLatency, fseFileEvents|fseWatchRoot)
	cfRelease(cfPaths)
	cfRelease(cfPath)
	if stream == 0 {
		return errors.New("cannot create an FSEventStream")
	}
	s.stream = stream
	fseStreams.Lock()
	fseStreams.m[s.context.info] = s
	fseStreams.Unlock()
	fseStreamSetQueue(stream, fseQueue)
	if fseStreamStart(stream) == 0 {
		s.stop()
		return errors.New("cannot start an FSEventStream")
	}
	s.started = true
	d.streams[t.ID] = s
	return nil
}

func (d *fseWatch) Remove(id uint64) {
	if s := d.streams[id]; s != nil {
		s.stop()
		delete(d.streams, id)
	}
}

// Wait returns the notices that came in, or, after an overflow, one notice
// that makes the core look at everything again. Wake makes it return with
// none, so that the command Wake announced is taken. Channels are never
// closed: Watch wakes the driver before it closes it, and a callback may
// still be delivering.
func (d *fseWatch) Wait() ([]platform.FileWatchNotice, error) {
	if d.overflow.Swap(false) {
		// The queued notices are covered by the core's full scan.
		for len(d.notices) > 0 {
			<-d.notices
		}
		return []platform.FileWatchNotice{{Err: platform.ErrFileWatchOverflow}}, nil
	}
	select {
	case n := <-d.notices:
		notices := []platform.FileWatchNotice{n}
		for len(notices) < fseBatch {
			select {
			case n := <-d.notices:
				notices = append(notices, n)
			default:
				return notices, nil
			}
		}
		return notices, nil
	case <-d.wake:
		return nil, nil
	}
}

func (d *fseWatch) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *fseWatch) Close() {
	for id, s := range d.streams {
		s.stop()
		delete(d.streams, id)
	}
}
