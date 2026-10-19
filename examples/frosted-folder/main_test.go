package main

import (
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The folder plays the video's loop: closed, it opens at openAt, its
// glass clearing after it, and closes at closeAt, frosting over.
func TestLoop(t *testing.T) {
	f := newFolder()
	tt := ui.NewTester(f.view, windowSize, windowSize)
	if f.open != 0 || f.clear != 0 {
		t.Fatalf("open %.2f, clear %.2f at first, want closed", f.open, f.clear)
	}
	tt.Advance(openAt + 200*time.Millisecond)
	if f.open < 0.4 || f.clear != 0 {
		t.Errorf("open %.2f, clear %.2f 200 ms into opening, want halfway open, still frosted", f.open, f.clear)
	}
	tt.Advance(closeAt - openAt - 200*time.Millisecond)
	if f.open != 1 || f.clear != 1 {
		t.Errorf("open %.2f, clear %.2f as it closes, want open and clear", f.open, f.clear)
	}
	tt.Advance(closing.Duration)
	if f.open != 0 || f.clear != 0 {
		t.Errorf("open %.2f, clear %.2f after closing, want closed", f.open, f.clear)
	}
	tt.Advance(period - closeAt - closing.Duration + openAt + time.Second)
	if f.open < 0.99 {
		t.Errorf("open %.2f a second into the next loop, want open", f.open)
	}
}

// The pointer over the folder holds it open; let go, it closes and stays
// closed as long as the loop does after closing.
func TestHold(t *testing.T) {
	f := newFolder()
	tt := ui.NewTester(f.view, windowSize, windowSize)
	tt.Move(windowSize/2, windowSize/2)
	tt.Advance(period)
	if f.open != 1 || f.clear != 1 {
		t.Fatalf("open %.2f, clear %.2f held a whole loop, want open and clear", f.open, f.clear)
	}
	tt.Move(10, 10)
	tt.Advance(period - closeAt + openAt - 10*time.Millisecond)
	if f.open != 0 || f.clear != 0 {
		t.Errorf("open %.2f, clear %.2f let go until the loop opens it, want closed", f.open, f.clear)
	}
	tt.Advance(time.Second)
	if f.open < 0.99 {
		t.Errorf("open %.2f after, want the loop to open it", f.open)
	}
}

// The pane's top comes down open of the way, in perspective, as the
// angle grows.
func TestAngle(t *testing.T) {
	top := func(a float64) float64 {
		return paneHeight * (1 - math.Cos(a)) * camera / (camera - paneHeight*math.Sin(a))
	}
	last := -1.0
	for i := range 11 {
		open := float32(i) / 10
		a := angle(open)
		if got := top(a); math.Abs(got-drop*float64(open)) > 1e-6 {
			t.Errorf("angle(%.1f) brings the top %.4f down, want %.4f", open, got, drop*float64(open))
		}
		if a <= last {
			t.Errorf("angle(%.1f) = %.4f, not more than %.4f", open, a, last)
		}
		last = a
	}
	if deg := angle(1) * 180 / math.Pi; deg < 35 || deg > 38 {
		t.Errorf("open, the pane tips %.1f°, want about 36", deg)
	}
}

// The shader draws the folder: the back's tab blue above the pane, the
// pane frosted over it; open, the pane comes down and shows the back
// through it, darker beside the sheet.
func TestDraw(t *testing.T) {
	shot := func(open, clear float32) func(x, y int) color.RGBA {
		f := newFolder()
		f.open, f.clear = open, clear
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Draw(f.drawGlass)
		}, windowSize, windowSize)
		img := tt.Image()
		// In the video's pixels, two a DIP.
		return func(x, y int) color.RGBA { return img.RGBAAt(x/2, y/2) }
	}
	blue := func(c color.RGBA) bool { return c.B > 240 && c.R < 180 && c.G < 215 }
	near := func(c color.RGBA, r, g uint8) bool {
		return math.Abs(float64(c.R)-float64(r)) < 8 && math.Abs(float64(c.G)-float64(g)) < 8
	}

	closed := shot(0, 0)
	if c := closed(400, 380); !blue(c) {
		t.Errorf("the tab is %v closed, want blue", c)
	}
	if c := closed(540, 540); !near(c, 188, 210) {
		t.Errorf("the glass is %v closed, want frosted (188, 210)", c)
	}
	if c := closed(40, 40); !near(c, 247, 248) {
		t.Errorf("the page is %v, want (247, 248)", c)
	}

	open := shot(1, 1)
	if c := open(540, 440); !blue(c) {
		t.Errorf("the back is %v above the open pane, want blue", c)
	}
	if band, sheet := open(344, 540), open(540, 540); !near(band, 163, 184) || !near(sheet, 194, 215) {
		t.Errorf("open, the glass is %v over the back and %v over the sheet, want (163, 184) and (194, 215)", band, sheet)
	}
}

// While the desktop asks for less motion, the loop stops, and the
// pointer opens the folder at once.
func TestReduceMotion(t *testing.T) {
	f := newFolder()
	tt := ui.NewTester(f.view, windowSize, windowSize)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Advance(period)
	if f.open != 0 {
		t.Errorf("open %.2f a loop later, want closed", f.open)
	}
	tt.Move(windowSize/2, windowSize/2)
	if f.open != 1 || f.clear != 1 {
		t.Errorf("open %.2f, clear %.2f under the pointer, want open and clear at once", f.open, f.clear)
	}
}
