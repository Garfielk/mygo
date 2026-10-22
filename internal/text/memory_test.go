package text

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unsafe"
	"weak"
)

// Scrolling must not retain hundreds of frames of glyph arrays that
// cannot expire once the window stops drawing.
func TestLayoutCacheBoundsScrollingMemory(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	const rows = 40
	for frame := range 300 {
		for row := range rows {
			s.Layout(Params{Text: fmt.Sprintf("line %d: %s", frame*rows+row, strings.Repeat("code ", 20))})
		}
		s.EndFrame()
		if s.layoutBytes > maxLayoutBytes || len(s.layouts) > maxLayouts {
			t.Fatalf("frame %d retained %d bytes in %d layouts", frame, s.layoutBytes, len(s.layouts))
		}
	}
}

func TestLayoutCacheReusesDisplayedText(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p := Params{Text: "unchanged visible text"}
	l := s.Layout(p)
	for range 10 {
		s.EndFrame()
		if got := s.Layout(p); got != l {
			t.Fatal("a new frame reshaped unchanged visible text")
		}
		if got := s.Layout(p); got != l {
			t.Fatal("repeated measurement in one frame reshaped the text")
		}
	}
	if n := s.LayoutsMade(); n != 1 {
		t.Fatalf("made %d layouts for unchanged text", n)
	}
}

func TestLayoutCacheReleasesEvictedText(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	old := weak.Make(s.Layout(Params{Text: "the first viewport"}))
	for i := range maxLayouts + 1 {
		s.Layout(Params{Text: fmt.Sprint(i)})
	}
	runtime.GC()
	if old.Value() != nil {
		t.Fatal("evicted text is retained while the window is idle")
	}
	runtime.KeepAlive(s)
}

func TestLayoutCacheKeepsFrequentlyUsedText(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p := Params{Text: "a frequently used label"}
	l := s.Layout(p)
	for i := range maxLayouts + 1 {
		s.Layout(Params{Text: fmt.Sprint(i)})
		if got := s.Layout(p); got != l {
			t.Fatal("cache pressure evicted a frequently used label")
		}
	}
}

// fillToLivePressure lays fresh paragraphs until the live cache has less
// than the ordinary budget's room left, as a busy window's texts would.
func fillToLivePressure(s *System) {
	for i := 0; s.layoutBytes < maxLiveLayoutBytes-maxLayoutBytes; i++ {
		s.Layout(Params{Text: fmt.Sprintf("paragraph %04d: %s", i, strings.Repeat("word ", 200))})
	}
}

// The live cache may hold an oversized paragraph beside ordinary labels,
// but never by evicting a label a recent frame used: under pressure the
// paragraph stays uncached, and reshaping it is cheaper than losing them.
func TestLayoutCacheOversizedParagraphKeepsLabels(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	fillToLivePressure(s)
	labels, keep := len(s.layouts), s.Layout(Params{Text: "a small label"})
	p := Params{Text: strings.Repeat("x", maxLayoutBytes/64)}
	made := s.LayoutsMade()
	if large := s.Layout(p); len(large.Runes) == 0 || len(large.Lines) == 0 {
		t.Fatal("oversized text did not lay out")
	}
	if s.Layout(p); s.LayoutsMade()-made != 2 || len(s.layouts) != labels+1 {
		t.Fatal("an oversized paragraph displaced ordinary labels")
	}
	if s.Layout(Params{Text: "a small label"}) != keep {
		t.Fatal("an oversized paragraph evicted a label")
	}
}

func TestLayoutCacheReusesOversizedParagraph(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p := Params{Text: strings.Repeat("x", maxLayoutBytes/32)}
	first := s.Layout(p)
	if n := layoutBytes(&cached{layout: *first}); n <= maxLayoutBytes {
		t.Fatalf("fixture is not oversized: %d bytes", n)
	}
	s.EndFrame()
	if s.Layout(p) != first {
		t.Fatal("an unchanged oversized paragraph was reshaped")
	}
}

// Several oversized paragraphs a frame keeps drawing share the live room,
// and leave with the rest when the fonts change.
func TestLayoutCacheKeepsSeveralOversizedParagraphs(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p0 := Params{Text: strings.Repeat("x", maxLayoutBytes/64)}
	p1 := Params{Text: strings.Repeat("y", maxLayoutBytes/64)}
	a, b := s.Layout(p0), s.Layout(p1)
	if n := layoutBytes(&cached{layout: *a}); n <= maxLayoutBytes {
		t.Fatalf("fixture is not oversized: %d bytes", n)
	}
	s.EndFrame()
	if s.Layout(p0) != a || s.Layout(p1) != b {
		t.Fatal("the live cache kept only one oversized paragraph")
	}
	s.clearLayouts()
	if len(s.layouts) != 0 || s.layoutBytes != 0 {
		t.Fatal("clearing the cache kept an oversized paragraph")
	}
	if s.Layout(p0) == a {
		t.Fatal("a cleared cache served its old layout")
	}
}

// A dragged split pane lays the same oversized paragraph out at width
// after width: every width keeps its own layout, within the budget.
func TestLayoutCacheKeepsOversizedParagraphAcrossWidths(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p0 := Params{Text: strings.Repeat("x", maxLayoutBytes/64), Width: 700}
	p1 := p0
	p1.Width = 701
	a, b := s.Layout(p0), s.Layout(p1)
	if a == b {
		t.Fatal("two widths shared one layout")
	}
	if n := layoutBytes(&cached{layout: *a}); n <= maxLayoutBytes {
		t.Fatalf("fixture is not oversized: %d bytes", n)
	}
	s.EndFrame()
	if s.Layout(p0) != a || s.Layout(p1) != b {
		t.Fatal("an oversized paragraph was reshaped at a width it kept")
	}
	if s.layoutBytes > maxLiveLayoutBytes {
		t.Fatal("one paragraph at two widths exceeded the live budget")
	}
}

// Once no recent frame uses the labels, the next oversized paragraph takes
// their room: it is admitted exactly then, not before.
func TestLayoutCacheAdmitsOversizedOnStaleRoom(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	fillToLivePressure(s)
	p := Params{Text: strings.Repeat("x", maxLayoutBytes/64)}
	s.Layout(p)
	made := s.LayoutsMade()
	if s.Layout(p); s.LayoutsMade()-made != 1 {
		t.Fatal("a fresh cache admitted an oversized paragraph")
	}
	for range liveLayoutFrames {
		s.EndFrame()
	}
	first := s.Layout(p)
	if s.Layout(p) != first || s.LayoutsMade()-made != 2 {
		t.Fatal("an oversized paragraph did not take stale room")
	}
}

// An oversized paragraph unused for a few frames leaves the cache, as the
// ordinary document does, and lays out anew.
func TestLayoutCacheForgetsUnusedOversizedParagraph(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p := Params{Text: strings.Repeat("x", maxLayoutBytes/64)}
	s.Layout(p)
	if s.layoutBytes <= maxLayoutBytes {
		t.Fatal("fixture is not oversized")
	}
	for range liveLayoutFrames {
		s.EndFrame()
	}
	if s.layoutBytes != 0 || len(s.layouts) != 0 {
		t.Fatal("an unused oversized paragraph stayed cached")
	}
	made := s.LayoutsMade()
	s.Layout(p)
	if s.LayoutsMade()-made != 1 {
		t.Fatal("a forgotten oversized paragraph was not reshaped")
	}
}

// A paragraph beyond the whole live budget never enters the cache: holding
// it would take evicting every layout of every window, every frame.
func TestLayoutCacheSkipsParagraphBeyondLiveBudget(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p := Params{Text: strings.Repeat("x", maxLayoutBytes/16)}
	l := s.Layout(p)
	if n := layoutBytes(&cached{layout: *l}); n <= maxLiveLayoutBytes {
		t.Fatalf("fixture takes %d bytes, within the live budget", n)
	}
	made := s.LayoutsMade()
	s.Layout(p)
	if s.LayoutsMade()-made != 1 || len(s.layouts) != 0 {
		t.Fatal("a paragraph beyond the live budget was cached")
	}
}

func TestRetainedLayoutDoesNotRetainClearedCache(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	kept := s.Layout(Params{Text: "kept by an element"})
	other := weak.Make(s.Layout(Params{Text: "no longer used"}))
	s.clearLayouts()
	runtime.GC()
	if other.Value() != nil || s.layoutBytes != 0 {
		t.Fatal("a retained layout keeps the cleared cache alive")
	}
	runtime.KeepAlive(kept)
	runtime.KeepAlive(s)
}

func cacheDocumentExcerpt(s *System) weak.Pointer[byte] {
	document := strings.Repeat("x", 1<<20)
	old := weak.Make(unsafe.StringData(document))
	s.Layout(Params{Text: document[100:110]})
	return old
}

func TestLayoutCacheDoesNotRetainExcerptSource(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	old := cacheDocumentExcerpt(s)
	runtime.GC()
	if old.Value() != nil {
		t.Fatal("a cached ten-character excerpt retains its entire source document")
	}
	if s.Layout(Params{Text: strings.Repeat("x", 10)}).Params.Text != strings.Repeat("x", 10) {
		t.Fatal("the cache changed the excerpt's text")
	}
	runtime.KeepAlive(s)
}

// documentRow is a paragraph of a long document, and document the
// paragraphs whose layouts take a little more than the cache's budget,
// however large the layouts of this engine are.
func documentRow(i int) Params {
	return Params{Text: fmt.Sprintf("paragraph %04d: %s", i, strings.Repeat("word ", 60))}
}

func document(s *System) []Params {
	s.Layout(documentRow(0))
	ps := make([]Params, maxLayoutBytes*9/8/s.layoutBytes+1)
	for i := range ps {
		ps[i] = documentRow(i)
	}
	return ps
}

// A window drawing a document a little over the budget must not evict
// each paragraph just before its next frame lays it out again.
func TestLayoutCacheKeepsDisplayedDocument(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	ps := document(s)
	for _, p := range ps {
		s.Layout(p)
	}
	if s.layoutBytes <= maxLayoutBytes {
		t.Fatalf("the document takes %d bytes, within the budget", s.layoutBytes)
	}
	s.EndFrame()
	made := s.LayoutsMade()
	for range 20 {
		for _, p := range ps {
			s.Layout(p)
		}
		s.EndFrame()
	}
	if n := s.LayoutsMade() - made; n != 0 {
		t.Fatalf("made %d layouts for the unchanged document", n)
	}
}

// Once no frame draws the document, the cache goes back to its budget,
// whichever window's frames go by, and later forgets the rest.
func TestLayoutCacheTrimsUnusedDocument(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	for _, p := range document(s) {
		s.Layout(p)
	}
	for range liveLayoutFrames - 1 {
		s.EndFrame()
		if s.layoutBytes <= maxLayoutBytes {
			t.Fatal("the document was trimmed while still recent")
		}
	}
	s.Layout(Params{Text: "another window"})
	s.EndFrame()
	if s.layoutBytes > maxLayoutBytes {
		t.Fatalf("kept %d bytes of unused layouts", s.layoutBytes)
	}
	for range 241 {
		s.EndFrame()
	}
	if len(s.layouts) != 0 || s.layoutBytes != 0 {
		t.Fatal("old layouts survived expiry")
	}
}

// Windows take turns: another window's frames between two of the
// document's keep its layouts.
func TestLayoutCacheKeepsDocumentAcrossWindows(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	ps := document(s)
	first := s.Layout(ps[0])
	for _, p := range ps[1:] {
		s.Layout(p)
	}
	s.EndFrame()
	for range liveLayoutFrames - 2 {
		s.Layout(Params{Text: "another window"})
		s.EndFrame()
	}
	if s.Layout(ps[0]) != first {
		t.Fatal("another window's frames evicted the document")
	}
}

// Recent layouts still stay within a hard limit.
func TestLayoutCacheCapsRecentBytes(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	first := s.Layout(Params{Text: "first"})
	for i := range 100 {
		s.Layout(Params{Text: fmt.Sprintf("%d: %s", i, strings.Repeat("word ", 2000))})
		if s.layoutBytes > maxLiveLayoutBytes {
			t.Fatalf("kept %d bytes", s.layoutBytes)
		}
	}
	if s.Layout(Params{Text: "first"}) == first {
		t.Fatal("the hard limit did not evict the oldest layout")
	}
}
