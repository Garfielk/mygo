package glass

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/ui"
)

// Blur is a backdrop blur, a ui.Material: what was painted under the
// element shows through it blurred, with no tint, rim or shadow. With a
// Mask, the blur fades along a gradient, a progressive blur, as under a
// bar that content scrolls under (ScrollEdge is macOS's own effect there):
//
//	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(56).Material(glass.Blur{
//		Radius: 12,
//		Mask:   &ui.LinearGradient{From: ui.RGB(0, 0, 0), To: ui.Transparent, Angle: 180},
//	})
type Blur struct {
	// Radius is how much what shows through blurs, in DIPs: the standard
	// deviation of a Gaussian, as CSS's blur() takes.
	Radius float32
	// Mask, if set, varies the blur along a gradient, by the alpha of its
	// colors, as CSS's mask-image does: what shows through blurs by Radius
	// where the gradient is opaque, by less where it is translucent, and
	// not at all where it is transparent. Its colors do not show.
	Mask *ui.LinearGradient
}

// PaintMaterial paints the blur over box, rounded by radii.
func (b Blur) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	paintBlur(p, box, radii, b)
}

// String describes the blur, as the inspector shows it.
func (b Blur) String() string {
	s := fmt.Sprintf("blur(%g)", b.Radius)
	if m := b.Mask; m != nil {
		s += fmt.Sprintf(" mask(%g%%→%g%% %g°)", math.Round(float64(m.From.A)/2.55), math.Round(float64(m.To.A)/2.55), m.Angle)
	}
	return s
}

// blurLevel is a step of a blur that varies across its element: what was
// painted under the element, blurred by blur pixels, where the blur the
// element wants is more than lo pixels and at most hi, the level's band,
// in which it mixes into the blur of the level before it by how far the
// blur wanted is toward hi; blur is hi. The last level shows wherever the
// blur wanted is more than its lo, and a first level of the least blur
// wanted (lo -1) where it is that least, mixing into none.
//
// The levels' his double from one level to the next, from at most
// finestLevel pixels to the blur's most, and a pixel between two shows the
// blurs of both, mixed, which the eye takes for a blur between them, as
// Core Animation's variable blur mixes the levels of its pyramid. Each
// level is drawn only over its band, reading what is under it there, and
// all read the one backdrop the first read (scene.EffectOp.Shared): the
// frame read once, each level's blurs of it, its own and its second
// backdrop (EffectOp.Backdrop2) with the level before's, in texels of its
// own blur's size, over only its band. A blur that does not vary is
// one level, which shows everywhere.
type blurLevel struct{ blur, lo, hi float32 }

// finestLevel is the most the first level of a varying blur blurs, in
// pixels: it shows mixed with what is not blurred, which at 2 pixels looks
// as a blur between them. Against a blur varying per pixel, a first level
// of 1 pixel measured no closer, and levels √2 apart rather than 2 closer
// within the element but further along its edges, where each level reads
// what is around the element unblurred.
const finestLevel = 2

// blurLevels returns the levels of a blur varying from least to most
// pixels.
func blurLevels(least, most float32) []blurLevel {
	least = max(least, 0)
	if most <= least {
		if least <= 0 {
			return nil
		}
		return []blurLevel{{blur: least, lo: -1, hi: least}}
	}
	// The levels' his, from the most down, halving.
	his := []float32{most}
	for h := most; h > finestLevel && h/2 > least; {
		h /= 2
		his = append(his, h)
	}
	var levels []blurLevel
	if least > 0 {
		levels = append(levels, blurLevel{blur: least, lo: -1, hi: least})
	}
	lo := least
	for i := len(his) - 1; i >= 0; i-- {
		hi := his[i]
		levels = append(levels, blurLevel{blur: hi, lo: lo, hi: hi})
		lo = hi
	}
	return levels
}

// paintBlur paints the blur b over box, rounded by radii.
func paintBlur(p *ui.Painter, box ui.Rect, radii [4]float32, b Blur) {
	s := p.Scale()
	if box.W <= 0 || box.H <= 0 {
		return
	}
	// The rectangle of the element, in pixels, as the painter snaps it.
	px := scene.Rect{X: round(box.X * s), Y: round(box.Y * s)}
	px.W, px.H = round((box.X+box.W)*s)-px.X, round((box.Y+box.H)*s)-px.Y
	blurOps(px, radii == [4]float32{}, b.Radius*s, b.Mask, func(r scene.Rect, fx scene.EffectOp) {
		p.EffectOp(ui.Rect{X: r.X / s, Y: r.Y / s, W: r.W / s, H: r.H / s}, radii, fx)
	})
}

// blurOps calls op with the rectangle and the effect of each level
// (blurLevels) of a blur of most pixels over r, in pixels, along mask: a
// level after another, the levels after the first sharing its backdrop,
// each over its band when r is square, which it then reads only what is
// under.
func blurOps(r scene.Rect, square bool, most float32, mask *ui.LinearGradient, op func(r scene.Rect, fx scene.EffectOp)) {
	if r.W <= 0 || r.H <= 0 || most <= 0 {
		return
	}
	// The blur at the mask's start and end, along its line.
	var line [4]float32
	at0, at1 := most, most
	if mask != nil {
		line = maskLine(r, *mask)
		at0, at1 = most*float32(mask.From.A)/255, most*float32(mask.To.A)/255
	}
	levels := blurLevels(min(at0, at1), max(at0, at1))
	shared := false
	for k, l := range levels {
		last := k == len(levels)-1
		fx := scene.EffectOp{Effect: blurEffect, Blur: l.blur, Params: [5][4]float32{line, {at0, at1, l.lo, l.hi}}, Shared: shared}
		if !last {
			fx.Params[2][3] = 1
		}
		if l.lo >= 0 {
			// It mixes into the level before's blur, or what is under it
			// unblurred for the first.
			fx.Backdrop2, fx.Params[3][3] = true, 1
			if k > 0 {
				fx.Blur2 = levels[k-1].blur
			}
		}
		lr := r
		if square {
			lo, hi := l.lo, l.hi
			if lo < 0 {
				lo = float32(math.Inf(-1))
			}
			if last {
				hi = float32(math.Inf(1))
			}
			var ok bool
			if lr, ok = blurWanted(r, line, at0, at1, lo, hi); !ok {
				continue
			}
		}
		op(lr, fx)
		shared = true
	}
}

// maskLine returns where the mask's From and To are, in pixels, as package
// ui places a gradient's across the rectangle r (Element.Gradient): along
// a line through its middle at the gradient's angle, as long as the
// rectangle's corners are apart along it, as CSS's linear-gradient.
func maskLine(r scene.Rect, g ui.LinearGradient) [4]float32 {
	a := float64(g.Angle) * math.Pi / 180
	dx, dy := float32(math.Sin(a)), float32(-math.Cos(a))
	half := (abs(r.W*dx) + abs(r.H*dy)) / 2
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	x0, y0, x1, y1 := cx-dx*half, cy-dy*half, cx+dx*half, cy+dy*half
	start, end := g.Start, g.End
	if start == 0 && end == 0 {
		end = 1
	}
	sx, sy := x0+(x1-x0)*start, y0+(y1-y0)*start
	ex, ey := x0+(x1-x0)*end, y0+(y1-y0)*end
	if end-start < 0.5/max(2*half, 1) {
		// Stops at one place: a hard edge, half a pixel wide.
		ex, ey = sx+dx*0.5, sy+dy*0.5
	}
	return [4]float32{sx, sy, ex, ey}
}

// blurWanted returns the bounds, in whole pixels, of the part of r where
// the blur along line, from at0 at its start to at1 at its end, is more
// than lo and at most hi, and whether there is one.
func blurWanted(r scene.Rect, line [4]float32, at0, at1, lo, hi float32) (scene.Rect, bool) {
	if at0 == at1 {
		return r, at0 > lo && at0 <= hi
	}
	// Along the line, the blur grows by k a pixel, from at0 at its start:
	// it is more than lo, and at most hi, in half-planes, which cut the
	// rectangle's corners down to a polygon.
	dx, dy := line[2]-line[0], line[3]-line[1]
	k := (at1 - at0) / (dx*dx + dy*dy)
	blur := func(p [2]float32) float32 { return at0 + k*((p[0]-line[0])*dx+(p[1]-line[1])*dy) }
	poly := [][2]float32{{r.X, r.Y}, {r.X + r.W, r.Y}, {r.X + r.W, r.Y + r.H}, {r.X, r.Y + r.H}}
	if !math.IsInf(float64(lo), -1) {
		poly = cut(poly, func(p [2]float32) float32 { return blur(p) - lo })
	}
	if !math.IsInf(float64(hi), 1) {
		poly = cut(poly, func(p [2]float32) float32 { return hi - blur(p) })
	}
	if len(poly) == 0 {
		return scene.Rect{}, false
	}
	x0, y0 := float32(math.Inf(1)), float32(math.Inf(1))
	x1, y1 := float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, p := range poly {
		x0, y0, x1, y1 = min(x0, p[0]), min(y0, p[1]), max(x1, p[0]), max(y1, p[1])
	}
	x0, y0 = max(floor(x0), r.X), max(floor(y0), r.Y)
	x1, y1 = min(ceil(x1), r.X+r.W), min(ceil(y1), r.Y+r.H)
	if x1 <= x0 || y1 <= y0 {
		return scene.Rect{}, false
	}
	return scene.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

// cut returns the part of the convex polygon poly where f, linear, is
// more than 0.
func cut(poly [][2]float32, f func(p [2]float32) float32) [][2]float32 {
	var out [][2]float32
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		fa, fb := f(a), f(b)
		if fa > 0 {
			out = append(out, a)
		}
		if (fa > 0) != (fb > 0) {
			t := fa / (fa - fb)
			out = append(out, [2]float32{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t})
		}
	}
	return out
}

// blurTone is what a level does to the colors it shows, which a blur's
// levels leave as they are: saturation more saturation (0 for as is),
// offset added to each channel, and mix of the opaque color over it, in
// its parameters p2 and p3 (but for p2.w and p3.w, which say how the level
// shows), as a scroll edge's hard style frosts.
type blurTone struct {
	saturation, offset, mix float32
	color                   [3]float32
}

// params returns the tone's parameters, p2 and p3.
func (t blurTone) params() (p2, p3 [4]float32) {
	return [4]float32{t.saturation, t.offset, t.mix}, [4]float32{t.color[0], t.color[1], t.color[2]}
}

// blurPixels draws a level of a blur on the CPU, as its shaders do on the
// GPU.
type blurPixels struct {
	line, band, tone, color [4]float32
}

// Begin reads the level's mask, band and tone.
func (px *blurPixels) Begin(op *scene.EffectOp, _ scene.Rect, _ [4]float32) {
	px.line, px.band, px.tone, px.color = op.Params[0], op.Params[1], op.Params[2], op.Params[3]
}

// Color returns the color of the level at the pixel center (x, y), as the
// shaders' effect does: none outside its band, and in it the backdrop,
// mixed into the second by how far the blur is into the band, or by how
// much it shows there over what is under it, toned.
func (px *blurPixels) Color(x, y float32, b *scene.BackdropImage) [4]float32 {
	band := &px.band
	blur := px.blur(x, y)
	if blur <= band[2] || px.tone[3] > 0.5 && blur > band[3] {
		return [4]float32{}
	}
	w := min(max((blur-band[2])/(band[3]-band[2]), 0), 1)
	c := sampleRGBA(b, x, y)
	if px.color[3] > 0.5 {
		c2 := sampleRGBA(b.Second, x, y)
		for i := range c {
			c[i] = c2[i] + (c[i]-c2[i])*w
		}
		w = 1
	}
	tn := &px.tone
	lum := 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
	for i := range 3 {
		c[i] = min(max(lum+(c[i]-lum)*(1+tn[0])+tn[1]*c[3], 0), c[3])
	}
	color := [4]float32{px.color[0], px.color[1], px.color[2], 1}
	for i := range c {
		c[i] += (color[i] - c[i]) * tn[2]
	}
	return [4]float32{c[0] * w, c[1] * w, c[2] * w, c[3] * w}
}

// blur returns the blur wanted at (x, y), along the mask.
func (px *blurPixels) blur(x, y float32) float32 {
	l, band := &px.line, &px.band
	dx, dy := l[2]-l[0], l[3]-l[1]
	t := float32(0)
	if d := dx*dx + dy*dy; d > 0 {
		t = min(max(((x-l[0])*dx+(y-l[1])*dy)/max(d, 0.0001), 0), 1)
	}
	return band[0]*(1-t) + band[1]*t
}

// sampleRGBA returns the backdrop at (x, y) with its alpha, premultiplied,
// as BackdropImage.Sample does its color, and the shaders' blurSample.
func sampleRGBA(b *scene.BackdropImage, x, y float32) [4]float32 {
	x, y = b.Transform.Point(x, y)
	k := float32(b.Down)
	u := (x-float32(b.Area.Min.X))/k - 0.5
	v := (y-float32(b.Area.Min.Y))/k - 0.5
	fu, fv := floor(u), floor(v)
	tx, ty := u-fu, v-fv
	x0, y0 := min(max(int(fu), 0), b.W-1), min(max(int(fv), 0), b.H-1)
	x1, y1 := min(max(int(fu)+1, 0), b.W-1), min(max(int(fv)+1, 0), b.H-1)
	p00, p10 := b.Pix[4*(y0*b.W+x0):][:4], b.Pix[4*(y0*b.W+x1):][:4]
	p01, p11 := b.Pix[4*(y1*b.W+x0):][:4], b.Pix[4*(y1*b.W+x1):][:4]
	var c [4]float32
	for i := range c {
		top := p00[i]*(1-tx) + p10[i]*tx
		bot := p01[i]*(1-tx) + p11[i]*tx
		c[i] = top*(1-ty) + bot*ty
	}
	return c
}

func round(v float32) float32 { return float32(math.Round(float64(v))) }
func floor(v float32) float32 { return float32(math.Floor(float64(v))) }
func ceil(v float32) float32  { return float32(math.Ceil(float64(v))) }
