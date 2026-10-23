package ui

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

// A bordered box under a fractional translation sits on whole device
// pixels: the painter snaps the box where the translation lands it, as
// browsers pixel-snap static borders, instead of spreading one border
// over a partial row inside the stroke (egoist/mygo#531). Under a
// transform that scales, the box keeps its subpixel edges.
func TestPainterBorderSnapsUnderTranslation(t *testing.T) {
	black, white := RGB(0, 0, 0), RGB(255, 255, 255)
	tt := coreNewTester(func(c *context) {
		coreBox(c).Size(100, 100).Draw(func(p *Painter, r Rect) {
			// p.Stroke goes through fill's border path.
			p.Transform(Translation(0.325, 0.1875), 0, 0, func() {
				p.Stroke(Rect{X: r.X + 10, Y: r.Y + 10, W: 40, H: 30}, black, 3, 1)
			})
			// background's border path, as BorderWidth paints it.
			p.Transform(Translation(0.325, 0.1875), 0, 0, func() {
				e := &node{fill: fillColor, bg: white, border: [4]float32{1, 1, 1, 1}, borderC: black}
				p.background(e, Rect{X: r.X + 60, Y: r.Y + 10, W: 30, H: 20}, true)
			})
			// A scaled box keeps today's snap in its own coordinates.
			p.Transform(Scaling(2, 2), 0, 0, func() {
				p.Stroke(Rect{X: r.X + 10, Y: r.Y + 50, W: 40, H: 30}, black, 3, 1)
			})
		})
	}, 100, 100)
	tt.SetScale(2)

	whole := func(r scene.Rect) bool {
		return r.X == round(r.X) && r.Y == round(r.Y) &&
			r.X+r.W == round(r.X+r.W) && r.Y+r.H == round(r.Y+r.H)
	}
	identity := func(m scene.Affine) bool {
		return !m.Set || (m.A == 1 && m.B == 0 && m.C == 0 && m.D == 1 && m.X == 0 && m.Y == 0 && m.G == 0 && m.H == 0)
	}
	var stroke, bg, scaled *scene.Op
	for i := range tt.h.last.Ops {
		op := &tt.h.last.Ops[i]
		if op.Border[0] == 0 {
			continue
		}
		switch {
		case op.Rect.Y >= 100:
			scaled = op
		case op.Rect.X >= 100:
			bg = op
		default:
			stroke = op
		}
	}
	if stroke == nil {
		t.Fatal("no stroked border op")
	}
	if bg == nil {
		t.Fatal("no background border op")
	}
	if scaled == nil {
		t.Fatal("no scaled border op")
	}
	if !whole(stroke.Rect) || !identity(stroke.Transform) {
		t.Errorf("stroke under a fractional translation: rect %+v, transform %+v; want whole pixels, no transform", stroke.Rect, stroke.Transform)
	}
	if !whole(bg.Rect) || !identity(bg.Transform) {
		t.Errorf("bordered background under a fractional translation: rect %+v, transform %+v; want whole pixels, no transform", bg.Rect, bg.Transform)
	}
	if !scaled.Transform.Set || scaled.Transform.A != 2 {
		t.Errorf("scaled stroke lost its scale: %+v", scaled.Transform)
	}

	// The row just inside the background's stroke is pure fill: no
	// second, partial line of border color, the artifact of #531.
	img := tt.Image()
	if c := img.RGBAAt(150, 22); c != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("row inside the stroke = %v, want pure fill", c)
	}
}

// Where the fill and the border of one box go on separate ops — a
// clipping box painting its border over its children, or a gradient
// border — they must land on the same whole pixels under a fractional
// translation: a fill snapping one way and its border the other would
// show the fill's edge beside the border.
func TestPainterSplitBorderAlignsUnderTranslation(t *testing.T) {
	black, white := RGB(0, 0, 0), RGB(255, 255, 255)
	tt := coreNewTester(func(c *context) {
		// Clipping with children paints the fill behind them and the
		// border on top.
		coreBox(c).Absolute().Left(0).Top(0).Size(60, 40).Translate(0.325, 0.1875).
			Clip().Background(white).BorderWidth(1).BorderColor(black).Children(func() {
			coreBox(c).Size(10, 10).Background(black)
		})
		// A gradient border always goes on an op of its own.
		coreBox(c).Absolute().Left(0).Top(60).Size(50, 30).Translate(0.325, 0.1875).
			Background(white).BorderWidth(1).BorderLinearGradient(LinearGradient{From: black, To: black})
	}, 100, 100)
	tt.SetScale(2)

	// Each box's translation (0.65, 0.375) device pixels folds into the
	// snap: the split box lands at (1, 0), the gradient one at (1, 120).
	whole := func(r scene.Rect) bool {
		return r.X == round(r.X) && r.Y == round(r.Y) &&
			r.X+r.W == round(r.X+r.W) && r.Y+r.H == round(r.Y+r.H)
	}
	identity := func(m scene.Affine) bool {
		return !m.Set || (m.A == 1 && m.B == 0 && m.C == 0 && m.D == 1 && m.X == 0 && m.Y == 0 && m.G == 0 && m.H == 0)
	}
	find := func(y float32, w float32, bordered bool) *scene.Op {
		for i := range tt.h.last.Ops {
			op := &tt.h.last.Ops[i]
			if op.Rect.Y == y && op.Rect.W == w && (op.Border[0] > 0) == bordered {
				return op
			}
		}
		return nil
	}
	splitFill, splitBorder := find(0, 120, false), find(0, 120, true)
	gradFill, gradBorder := find(120, 100, false), find(120, 100, true)
	if splitFill == nil || splitBorder == nil {
		t.Fatal("the clipping box's fill or border op is missing")
	}
	if gradFill == nil || gradBorder == nil {
		t.Fatal("the gradient border box's fill or border op is missing")
	}
	check := func(name string, fill, border *scene.Op) {
		t.Helper()
		if fill.Rect != border.Rect {
			t.Errorf("%s: fill at %+v, border at %+v; want the same box", name, fill.Rect, border.Rect)
		}
		if !whole(fill.Rect) || !identity(fill.Transform) {
			t.Errorf("%s: fill rect %+v, transform %+v; want whole pixels, no transform", name, fill.Rect, fill.Transform)
		}
		if !whole(border.Rect) || !identity(border.Transform) {
			t.Errorf("%s: border rect %+v, transform %+v; want whole pixels, no transform", name, border.Rect, border.Transform)
		}
	}
	check("clipping box", splitFill, splitBorder)
	check("gradient box", gradFill, gradBorder)
}
