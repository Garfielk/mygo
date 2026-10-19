package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// The window shows the video's frame at half its size: the scene is laid
// out in the video's pixels, units, two a DIP, from the frame's middle,
// y down.
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

// The scene, in units.
const (
	// The camera is camera units from the hinge, its eye level with the
	// pane's top, which comes down drop units as the folder opens.
	camera = 2074
	drop   = 57.6

	// The back: its sides, the top of its tab and of its body, its bottom,
	// and the radius of its corners, top and bottom. The tab's top slopes
	// down to the body's, through (slopeX, slopeY).
	backHalf    = 230
	tabTop      = -189
	bodyTop     = -148.5
	backBottom  = 146
	backRadius  = 44
	bottomRound = 120
	slopeX      = -25.5
	slopeY      = -169
	slope       = 0.6

	// The sheet in it.
	sheetHalf, sheetTop, sheetBottom, sheetRound = 177, -70, 88, 50

	// The pane: half its width, its top, its height above the hinge and
	// below it, which shows taller as the glass comes into focus, and its
	// corners' radius.
	paneHalf   = 246
	paneTop    = -135
	paneHeight = 275
	belowFrom  = 65
	belowTo    = 100
	paneRound  = 30

	// The progressive blur over the pane's lower part: from focusFrom
	// below its top, closed, or focusTo, open, more and more, to blurMost
	// blurReach further down.
	focusFrom = 45
	focusTo   = 235
	blurMost  = 50
	blurReach = 300

	// How much the glass blurs what is behind it, frosted and clear.
	frostedBlur = 90
	clearBlur   = 12
)

// The scene's colors.
var (
	// The page: a little bluer in the middle.
	pageMiddle, pageEdge = ui.RGB(237, 239, 253), ui.RGB(247, 248, 254)
	// The back: light at the top of the tab, deeper down, indigo at the
	// bottom; its top's rim shaded at the edge and lit inside it.
	backStops = []ui.GradientStop{
		{Color: ui.RGB(170, 210, 253), At: down(-178)},
		{Color: ui.RGB(85, 128, 251), At: down(-84)},
		{Color: ui.RGB(64, 77, 245), At: down(40)},
	}
	rimShade, rimLight = ui.RGBA(40, 70, 190, 0.25), ui.RGBA(205, 238, 255, 0.55)
	// The sheet in the back: a little grayer than the page.
	sheetColor = ui.RGB(214, 219, 251)
	// The glass's milk, frosted and clear, and its rim: a dark hairline at
	// the edge, a bright one inside.
	milkFrosted, milkClear = [3]float32{0.75, 0.84, 0.988}, [3]float32{0.742, 0.842, 0.992}
	hairline, innerLine    = ui.RGBA(140, 158, 230, 0.45), ui.RGBA(217, 230, 255, 0.5)
)

// down returns where y lies down the back, from 0 at its top to 1 at its
// bottom.
func down(y float32) float32 { return (y - tabTop) / (backBottom - tabTop) }

// folder is the window's state.
type folder struct {
	// start is when the loop started, last after the pointer left, and
	// held tells that the pointer holds the folder open.
	start time.Time
	held  bool
	// open is how far it is open, from 0 to 1, and clear how clear its
	// glass is.
	open, clear float32
	back        ui.Path
}

func newFolder() *folder {
	return &folder{}
}

func (f *folder) view(c *ui.Context) {
	now := c.Now()
	if f.start.IsZero() {
		f.start = now
	}
	// The stage is the video's frame, in the middle of the window, whose
	// page goes on around it.
	ui.Column(c).Fill().Center().Background(pageEdge).Children(func() {
		stage := ui.Box(c).Size(windowSize, windowSize).RadialGradient(ui.RadialGradient{
			Stops:  []ui.GradientStop{{Color: pageMiddle, At: 0.45}, {Color: pageEdge, At: 1}},
			Radius: ui.Point{X: 280.0 / windowSize, Y: 280.0 / windowSize},
		})
		stage.Children(func() { f.scene(c, now) })
	})
}

// scene builds the folder on the stage, at now.
func (f *folder) scene(c *ui.Context, now time.Time) {
	// The folder, which the pointer holds open.
	box := place(ui.Box(c), -250, -200, 500, 370).Label("Folder")
	held := box.Hovered()
	if f.held && !held {
		// Let go, it closes and stays closed a while, as in the loop.
		f.start = now.Add(-closeAt)
	}
	f.held = held
	// The loop stops while the desktop asks for less motion, which would
	// only flash it open and shut.
	open := false
	if !held && !c.Preferences().ReduceMotion {
		var wait time.Duration
		open, wait = f.looping(now)
		c.After(wait)
	}
	target, move, frost := float32(0), closing, frosting
	if held || open {
		target, move, frost = 1, opening, clearing
	}
	f.open = box.AnimateTo("open", target, move)
	f.clear = box.AnimateTo("clear", target, frost)
	focus := min((120*f.open+70*f.clear)/190, 1)

	// The back, and the sheet in it.
	place(ui.Box(c), -backHalf, tabTop, 2*backHalf, backBottom-tabTop).PassThrough().Draw(f.drawBack)
	place(ui.Box(c), -sheetHalf, sheetTop, 2*sheetHalf, sheetBottom-sheetTop).PassThrough().
		Radius(sheetRound / unit).Background(sheetColor)

	// The pane, turned on its hinge toward the camera, frosting what is
	// behind it under its milk, lighter at the top.
	below := belowFrom + (belowTo-belowFrom)*focus
	hinge := float32(paneHeight) / unit
	a := float32(angle(f.open))
	turn := ui.Translation(0, -hinge).Then(ui.RotationX(-a * 180 / math.Pi)).
		Then(ui.Translation(0, hinge)).Then(ui.Perspective(camera / unit))
	milk := func(light float32) ui.Color {
		var rgb [3]uint8
		for i := range rgb {
			v := mix(milkFrosted[i], milkClear[i], f.clear) + light*[3]float32{0.035, 0.025, 0}[i]
			rgb[i] = uint8(min(max(v, 0), 1)*255 + 0.5)
		}
		return ui.RGBA(rgb[0], rgb[1], rgb[2], mix(0.92, 0.77, f.clear))
	}
	pane := place(ui.Box(c), -paneHalf, paneTop, 2*paneHalf, paneHeight+below).PassThrough().
		TransformOrigin(0.5, 0).Transform(turn).Radius(paneRound / unit).
		Material(glass.Blur{Radius: mix(frostedBlur, clearBlur, f.clear) / unit})
	pane.Children(func() {
		ui.Box(c).Fill().PassThrough().Radius(paneRound/unit).
			LinearGradient(ui.LinearGradient{From: milk(0.5), To: milk(0.5 - (paneHeight+below)/paneHeight), Angle: 180}).
			Border(0.6, hairline).InsetShadow(0, 0, 1, 1.4, innerLine)
	})

	// The progressive blur over the pane's lower part and what is around
	// it, from where it starts down the pane, which comes down as it
	// opens.
	// It reaches as far below the pane as its blur spreads, and no
	// further: it blurs what is under it every frame the pane moves.
	cos := float32(math.Cos(float64(a)))
	top := paneTop + drop*f.open + cos*mix(focusFrom, focusTo, focus)
	h := max(paneTop+(paneHeight+below)*cos+drop*f.open+2.5*blurMost-top, 1)
	place(ui.Box(c), -320, top, 640, h).PassThrough().Material(glass.Blur{Radius: blurMost / unit, Mask: &ui.LinearGradient{
		From: ui.Transparent, To: ui.RGBA(0, 0, 0, min(h/blurReach, 1)), Angle: 180, End: min(blurReach/h, 1),
	}})
}

// place puts e on the stage at (x, y), w×h, in units.
func place(e ui.Element, x, y, w, h float32) ui.Element {
	return e.Absolute().Left(windowSize/2+x/unit).Top(windowSize/2+y/unit).Size(w/unit, h/unit)
}

func mix(a, b, t float32) float32 { return a + (b-a)*t }

// drawBack paints the back in r: its outline, the tab's top sloping down
// to the body's, in its blues.
func (f *folder) drawBack(p *ui.Painter, r ui.Rect) {
	line, curve, top := f.outline(r)
	path := &f.back
	path.Reset()
	path.MoveTo(f.at(r, -backHalf, backBottom-bottomRound))
	top(0, false)
	line(backHalf, backBottom-bottomRound)
	curve(backHalf, backBottom, backHalf-bottomRound, backBottom)
	line(-backHalf+bottomRound, backBottom)
	curve(-backHalf, backBottom, -backHalf, backBottom-bottomRound)
	path.Close()
	p.FillPathGradient(path, ui.LinearGradient{Angle: 180, Stops: backStops})

	// The rim along the top: a shade at its edge, and light a few units
	// inside it, as on a bevel lit from above, each soft as strokes of a
	// few widths over each other.
	for _, rim := range []struct {
		inset, width float32
		c            ui.Color
	}{{2.5, 2, rimShade}, {6.5, 3.2, rimLight}} {
		path.Reset()
		top(rim.inset, true)
		for _, s := range [...]struct{ wider, alpha float32 }{{2.4, 0.3}, {1.2, 0.35}, {0, 0.45}} {
			p.StrokePath(path, (rim.width+s.wider)/unit, rim.c.Alpha(s.alpha))
		}
	}
}

// at maps units to the window, in the back's box r.
func (f *folder) at(r ui.Rect, x, y float32) (float32, float32) {
	return r.X + (x+backHalf)/unit, r.Y + (y-tabTop)/unit
}

// outline returns functions adding to f.back, in the back's box r, in
// units: a line, a curve, and the top's line from the left side to the
// right, d units inside it: round at the corners, the slope meeting the
// tab's top at x0 and the body's at x1, rounded into them; a rim's goes
// from halfway round the corners, starting a path of its own.
func (f *folder) outline(r ui.Rect) (line func(x, y float32), curve func(cx, cy, x, y float32), top func(d float32, rim bool)) {
	path := &f.back
	line = func(x, y float32) { path.LineTo(f.at(r, x, y)) }
	curve = func(cx, cy, x, y float32) {
		ax, ay := f.at(r, cx, cy)
		bx, by := f.at(r, x, y)
		path.QuadTo(ax, ay, bx, by)
	}
	top = func(d float32, rim bool) {
		x0, x1 := float32(slopeX+(tabTop-slopeY)/slope), float32(slopeX+(bodyTop-slopeY)/slope)
		if rim {
			// From halfway round the corner: the second half of its curve.
			path.MoveTo(f.at(r, -backHalf+0.75*d+backRadius/4, tabTop+backRadius/4+0.75*d))
			curve(-backHalf+(d+backRadius)/2, tabTop+d, -backHalf+backRadius, tabTop+d)
		} else {
			line(-backHalf+d, tabTop+backRadius)
			curve(-backHalf+d, tabTop+d, -backHalf+backRadius, tabTop+d)
		}
		line(x0-12, tabTop+d)
		curve(x0+d/2, tabTop+d, x0+12, tabTop+d+12*slope)
		line(x1-12, bodyTop+d-12*slope)
		curve(x1+d/2, bodyTop+d, x1+12, bodyTop+d)
		line(backHalf-backRadius, bodyTop+d)
		if rim {
			// To halfway round the corner: the first half of its curve.
			curve(backHalf-(d+backRadius)/2, bodyTop+d, backHalf-0.75*d-backRadius/4, bodyTop+backRadius/4+0.75*d)
		} else {
			curve(backHalf-d, bodyTop+d, backHalf-d, bodyTop+backRadius)
		}
	}
	return line, curve, top
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
