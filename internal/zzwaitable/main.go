// Command zzwaitable measures #517 on Windows: whether presenting through
// the swap chain's frame latency object keeps the frame rate of blocking
// in Present while freeing the main thread. SCRATCH: removed before
// merging, with the ZZ_WAITABLE switch in internal/gpu/d3d11.
//
//	go run ./internal/zzwaitable [-rounds 6] [-secs 6]
//
// Each round runs, as processes of their own and in alternating order, an
// animated native UI window with the waitable object off (blocking
// Present, as main does) and on, for each of two scenes: one box moving
// (anim) and 30 boxes casting wide shadows (heavy). Each logs every frame
// with MYGO_FRAME_STATS=all; the medians over the rounds print: frames a
// second, the mean present phase of a frame (where blocking Present
// waits), and the share of frames whose present phase took over 4 ms.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	rounds := flag.Int("rounds", 6, "rounds, each running every scene both ways")
	secs := flag.Float64("secs", 6, "seconds each window animates")
	child := flag.String("child", "", "run one window of this scene (anim or heavy)")
	flag.Parse()
	if *child != "" {
		window(*child, *secs)
		return
	}
	self, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	type key struct {
		scene    string
		waitable bool
	}
	results := map[key][]figures{}
	for r := range *rounds {
		order := []bool{false, true}
		if r%2 == 1 {
			order = []bool{true, false}
		}
		for _, scene := range []string{"anim", "heavy"} {
			for _, waitable := range order {
				cmd := exec.Command(self, "-child", scene, "-secs", fmt.Sprint(*secs))
				cmd.Env = append(os.Environ(), "MYGO_FRAME_STATS=all", "ZZ_WAITABLE="+map[bool]string{false: "0", true: "1"}[waitable])
				var logs bytes.Buffer
				cmd.Stderr = &logs
				if err := cmd.Run(); err != nil {
					log.Fatalf("%s, waitable %v: %v\n%s", scene, waitable, err, logs.String())
				}
				f := parse(&logs)
				fmt.Printf("round %d  %-5s  waitable %-5v  %s\n", r+1, scene, waitable, f)
				results[key{scene, waitable}] = append(results[key{scene, waitable}], f)
			}
		}
	}
	fmt.Println()
	fmt.Printf("medians of %d rounds:\n", *rounds)
	for _, scene := range []string{"anim", "heavy"} {
		for _, waitable := range []bool{false, true} {
			name := "blocking Present (main)"
			if waitable {
				name = "frame latency object (#517)"
			}
			fmt.Printf("  %-5s  %-28s  %s\n", scene, name, median(results[key{scene, waitable}]))
		}
	}
	fmt.Println()
	fmt.Println("OK to merge when, for both scenes, frames a second with the latency object are at least")
	fmt.Println("those of blocking Present (the display's rate when the GPU keeps up), and its present")
	fmt.Println("phase is near 0 with almost no frames over 4 ms.")
}

type figures struct {
	fps, present, over4 float64 // frames a second, ms, percent
}

func (f figures) String() string {
	return fmt.Sprintf("%6.1f frames/s   present %6.2f ms   %5.1f%% over 4 ms", f.fps, f.present, f.over4)
}

func median(fs []figures) figures {
	pick := func(get func(figures) float64) float64 {
		v := make([]float64, len(fs))
		for i, f := range fs {
			v[i] = get(f)
		}
		slices.Sort(v)
		if n := len(v); n%2 == 0 {
			return (v[n/2-1] + v[n/2]) / 2
		}
		return v[len(v)/2]
	}
	return figures{
		fps:     pick(func(f figures) float64 { return f.fps }),
		present: pick(func(f figures) float64 { return f.present }),
		over4:   pick(func(f figures) float64 { return f.over4 }),
	}
}

// A frame's log: "15:04:05.000000 mygo: frame 12 took 1.2 ms: ..., present 0.4 (...".
var frameLine = regexp.MustCompile(`^(\d\d):(\d\d):(\d\d\.\d+) mygo: frame \d+ took [0-9.]+ ms: .*?, present ([0-9.]+) \(`)

// parse reads a window's frame logs, leaving out its first second, as it
// shows and warms up.
func parse(logs *bytes.Buffer) figures {
	var at, present []float64
	sc := bufio.NewScanner(logs)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		m := frameLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		h, _ := strconv.ParseFloat(m[1], 64)
		mi, _ := strconv.ParseFloat(m[2], 64)
		s, _ := strconv.ParseFloat(m[3], 64)
		p, _ := strconv.ParseFloat(m[4], 64)
		at = append(at, h*3600+mi*60+s)
		present = append(present, p)
	}
	var f figures
	first := 0
	for first < len(at) && at[first]-at[0] < 1 {
		first++
	}
	at, present = at[first:], present[first:]
	if len(at) < 2 {
		return f
	}
	f.fps = float64(len(at)-1) / (at[len(at)-1] - at[0])
	for _, p := range present {
		f.present += p / float64(len(present))
		if p > 4 {
			f.over4 += 100 / float64(len(present))
		}
	}
	return f
}

// window animates a window for secs seconds: one box crossing it every
// second, or 30 casting wide shadows, a frame each refresh.
func window(scene string, secs float64) {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "zzwaitable " + scene, Width: 800, Height: 600, Content: ui.View(func(c *ui.Context) {
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				t := float32(p.Now().UnixNano()%1e9) / 1e9
				n := 1
				if scene == "heavy" {
					n = 30
				}
				for i := range n {
					x := float32(i%20)*36 + t*(r.W-200)
					y := float32(i/20)*50 + 20
					if scene == "heavy" {
						p.Shadow(ui.Rect{X: r.X + x, Y: r.Y + y, W: 160, H: 120}, 16, 0, 8, 48, 0, ui.RGBA(0, 0, 0, 0.3))
					}
					p.Fill(ui.Rect{X: r.X + x, Y: r.Y + y, W: 160, H: 120}, c.Theme().Accent, 16)
				}
				p.AnimationFrame()
			})
		})})
		time.AfterFunc(time.Duration(secs*float64(time.Second)), mygo.App.Quit)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
