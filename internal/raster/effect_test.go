package raster

import (
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

// Effects for tests: lensEffect shows its backdrop, sampled further in
// near its edge by Params[0][0] pixels a pixel, mixed with the color
// Params[1] by its alpha; tintEffect paints Params[1] alone.
var (
	lensEffect = &scene.Effect{Name: "lens", Backdrop: true, Pixels: func() scene.EffectPixels { return &testPixels{} }}
	tintEffect = &scene.Effect{Name: "tint", Pixels: func() scene.EffectPixels { return &testPixels{} }}
)

type testPixels struct {
	op    *scene.EffectOp
	rect  scene.Rect
	radii [4]float32
}

func (t *testPixels) Begin(op *scene.EffectOp, rect scene.Rect, radii [4]float32) {
	t.op, t.rect, t.radii = op, rect, radii
}

func (t *testPixels) Color(x, y float32, b *scene.BackdropImage) [4]float32 {
	tint := t.op.Params[1]
	if b == nil {
		return [4]float32{tint[0] * tint[3], tint[1] * tint[3], tint[2] * tint[3], tint[3]}
	}
	d := max(8+scene.SDRoundRect(t.rect, t.radii, x, y), 0) * t.op.Params[0][0]
	c := b.Sample(x+d, y)
	for i := range c {
		c[i] += (tint[i] - c[i]) * tint[3]
	}
	return [4]float32{c[0], c[1], c[2], 1}
}

// TestSharedBackdrop checks that an effect sharing the backdrop of the
// one before it (scene.EffectOp.Shared) reads what was under both, not
// what the first drew, and one that does not reads that.
func TestSharedBackdrop(t *testing.T) {
	for _, shared := range []bool{false, true} {
		s := &scene.Scene{Width: 40, Height: 20}
		s.Ops = []scene.Op{
			{Kind: scene.OpFill, Rect: scene.Rect{W: 40, H: 20}, Color: red},
			{Kind: scene.OpEffect, Rect: scene.Rect{W: 20, H: 20}, Start: 0},
			{Kind: scene.OpEffect, Rect: scene.Rect{X: 10, W: 30, H: 20}, Start: 1},
		}
		s.Effects = []scene.EffectOp{
			{Effect: lensEffect, Params: [5][4]float32{1: {0, 0, 1, 1}}},
			{Effect: lensEffect, Shared: shared},
		}
		img := NewImage(s.Width, s.Height)
		Render(img, s)
		want := [4]byte{0, 0, 255, 255}
		if shared {
			want = [4]byte{255, 0, 0, 255}
		}
		if got := at(img, 15, 10); got != want {
			t.Errorf("shared %v: %v under both, want %v", shared, got, want)
		}
		if got := at(img, 5, 10); got != [4]byte{0, 0, 255, 255} {
			t.Errorf("shared %v: %v under the first alone", shared, got)
		}
	}
}
