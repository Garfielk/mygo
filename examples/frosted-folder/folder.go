package main

import (
	_ "embed"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// The window shows the video's frame at half its size: a DIP is two of
// its pixels, the shader's units.
const (
	windowSize = 540
	unit       = 2
)

// The loop, as the video plays it: closed, the folder opens at openAt
// and closes at closeAt, every period.
const (
	period  = 3600 * time.Millisecond
	openAt  = 400 * time.Millisecond
	closeAt = 2380 * time.Millisecond
)

// How the folder moves, measured from the video's frames. Open is how far
// the pane's top has come down, from 0 to 1: it opens on a spring
// without bounce, and closes along a curve. The glass clears after it
// has mostly opened and frosts over as soon as it closes.
var (
	opening  = ui.Animation{Spring: ui.SpringOptions{Duration: 750 * time.Millisecond}}
	closing  = ui.Animation{Duration: 950 * time.Millisecond, Ease: ui.CubicBezier(0.55, 0, 0.4, 1)}
	clearing = ui.Animation{Duration: 750 * time.Millisecond, Delay: 270 * time.Millisecond, Ease: ui.CubicBezier(0.3, 0, 0.7, 1)}
	frosting = ui.Animation{Duration: 470 * time.Millisecond, Ease: ui.CubicBezier(0.3, 0, 0.7, 1)}
)

// The scene's geometry, in the shader's units (see folder.kage): the
// camera's distance, the pane's half width, its height above the hinge,
// where the hinge is, and how far the pane's top comes down as it opens.
const (
	camera     = 2074
	paneHalf   = 246
	paneHeight = 275
	hinge      = 140
	drop       = 57.6
)

var (
	//go:embed folder.kage
	folderSource string
	folderShader = shader.MustCompile(folderSource)

	angleVar = folderShader.Uniform("Angle")
	clearVar = folderShader.Uniform("Clear")
	focusVar = folderShader.Uniform("Focus")
	unitVar  = folderShader.Uniform("Unit")
)

// The page, and the folder's colors where the window draws without a GPU.
var (
	pageColor  = ui.Hex("#f7f8fe")
	backTop    = ui.Hex("#aad2fd")
	backBottom = ui.Hex("#5580fb")
	paneColor  = ui.RGB(190, 210, 252)
)

// folder is the window's state.
type folder struct {
	// start is when the loop started, last after the pointer left, and
	// held tells that the pointer holds the folder open.
	start time.Time
	held  bool
	// open is how far it is open, from 0 to 1, and clear how clear its
	// glass is.
	open, clear float32
	values      *shader.Values
}

func newFolder() *folder {
	return &folder{values: folderShader.NewValues()}
}

func (f *folder) view(c *ui.Context) {
	now := c.Now()
	if f.start.IsZero() {
		f.start = now
	}
	root := ui.Column(c).Fill().Center().Background(pageColor)
	root.Draw(f.draw)
	root.Children(func() {
		// The folder, which the pointer holds open; the drawing is the
		// window's.
		box := ui.Box(c).Size(250, 185).Margin(0, 0, 15, 0).Label("Folder")
		held := box.Hovered()
		if f.held && !held {
			// Let go, it closes and stays closed a while, as in the loop.
			f.start = now.Add(-closeAt)
		}
		f.held = held
		// The loop stops while the desktop asks for less motion, which
		// would only flash it open and shut.
		open := false
		if !held && !c.Preferences().ReduceMotion {
			var wait time.Duration
			open, wait = f.looping(now)
			c.After(wait)
		}
		target, move, glass := float32(0), closing, frosting
		if held || open {
			target, move, glass = 1, opening, clearing
		}
		f.open = box.AnimateTo("open", target, move)
		f.clear = box.AnimateTo("clear", target, glass)
	})
}

// looping tells whether the loop has the folder open at now, and how long
// it stays so.
func (f *folder) looping(now time.Time) (open bool, wait time.Duration) {
	at := now.Sub(f.start) % period
	switch {
	case at < openAt:
		return false, openAt - at
	case at < closeAt:
		return true, closeAt - at
	}
	return false, period - at + openAt
}

// angle returns how far the pane has tipped, in radians, for its top to
// have come open of the way down, in perspective: the camera's eye is
// level with the top of the closed pane.
func angle(open float32) float64 {
	want := drop * float64(open)
	top := func(a float64) float64 {
		return paneHeight * (1 - math.Cos(a)) * camera / (camera - paneHeight*math.Sin(a))
	}
	lo, hi := 0.0, 1.2
	for range 40 {
		mid := (lo + hi) / 2
		if top(mid) < want {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// focus returns how far down the pane the progressive blur has moved:
// with the pane, and further as the glass clears.
func focus(open, clear float32) float32 {
	return min((120*open+70*clear)/190, 1)
}

// draw paints the folder in the middle of r.
func (f *folder) draw(p *ui.Painter, r ui.Rect) {
	if !p.Accelerated() {
		f.drawFlat(p, r)
		return
	}
	f.drawGlass(p, r)
}

// drawGlass paints the folder with the shader.
func (f *folder) drawGlass(p *ui.Painter, r ui.Rect) {
	f.values.Float(angleVar, float32(angle(f.open))).
		Float(clearVar, f.clear).
		Float(focusVar, focus(f.open, f.clear)).
		Float(unitVar, unit)
	folderShader.Draw(p, r, shader.Options{Values: f.values})
}

// drawFlat paints the folder without the shader, which would run on the
// CPU: its back, and its pane foreshortened as it tips, without blur.
func (f *folder) drawFlat(p *ui.Painter, r ui.Rect) {
	// pt maps the shader's units, from the middle of r, to the window.
	x0, y0 := r.X+r.W/2, r.Y+r.H/2
	pt := func(x, y float32) ui.Point { return ui.Point{X: x0 + x/unit, Y: y0 + y/unit} }
	var back ui.Path
	move := func(x, y float32) { q := pt(x, y); back.MoveTo(q.X, q.Y) }
	line := func(x, y float32) { q := pt(x, y); back.LineTo(q.X, q.Y) }
	// A corner of radius 44, from the edge it ends to the next.
	corner := func(x1, y1, x2, y2, x, y float32) {
		a, b, c := pt(x1, y1), pt(x2, y2), pt(x, y)
		back.CubeTo(a.X, a.Y, b.X, b.Y, c.X, c.Y)
	}
	move(-230, 100)
	line(-230, -145)
	corner(-230, -169, -210, -189, -186, -189)
	line(-58.8, -189)
	line(8.6, -148.5)
	line(186, -148.5)
	corner(210, -148.5, 230, -128.5, 230, -104.5)
	line(230, 100)
	back.Close()
	p.FillPathGradient(&back, ui.LinearGradient{From: backTop, To: backBottom, Angle: 180})

	// The pane, as wide as it shows halfway up.
	a := angle(f.open)
	h := float32(paneHeight * math.Cos(a))
	w := float32(paneHalf * camera / (camera - paneHeight*math.Sin(a)/2))
	top := pt(-w, hinge-h)
	p.Fill(ui.Rect{X: top.X, Y: top.Y, W: 2 * w / unit, H: h / unit}, paneColor, 15)
}
