package markdown

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// A paragraph is the spans of its text, cut into pieces at its links,
// which are elements of their own inside it (ui.Link), with where each
// span's text is in the source, for copying.

// piece is a run of spans: a link's, when link >= 0 (an index into the
// block's inlines), else plain text.
type piece struct {
	link  int
	spans []ui.Span
}

// runMark notes that the rune at rune of a paragraph's text is source
// byte src, and, when exact, that the span's text is the source's bytes
// from there.
type runMark struct {
	rune, src int
	exact     bool
}

// para is what a paragraph, heading or cell shows.
type para struct {
	pieces []piece
	marks  []runMark
	runes  int
	fading bool // its spans fade in: build them anew next frame
	// width is the width of its text on one line, once measured.
	width    float32
	measured bool
}

// spanStyle is the style inlines set as the builder descends.
type spanStyle struct {
	weight              int
	italic, strike      bool
	underline           bool
	code                bool
	sub                 bool
	mark                bool
	color               Color
	size                float32
	font                string
	inLink, pendingLink bool
}

type spanBuilder struct {
	d     *Document
	th    *Theme
	block int
	in    []Inline
	p     *para
	cur   *piece
	fade  func(src int) float32 // the opacity of text at a source offset, while revealing
	style spanStyle
	// html is the stack of inline HTML tags open, as <b>.
	html []string
	// notes numbers footnotes by their definitions' order.
	notes func(label string) int
	// linkIndex is the inline of the link being built.
	linkIndex int
}

// build makes the paragraph of block i, of base style s.
func (sb *spanBuilder) build(p *para, base spanStyle) {
	*p = para{pieces: p.pieces[:0], marks: p.marks[:0]}
	sb.p = p
	sb.in = sb.d.Inlines(sb.block)
	sb.html = sb.html[:0]
	sb.style = base
	sb.cur = nil
	sb.inlines(0, len(sb.in), base)
	// A paragraph's pieces keep their own slices: they outlive the build.
	for i := range p.pieces {
		p.pieces[i].spans = append([]ui.Span(nil), p.pieces[i].spans...)
	}
}

func (sb *spanBuilder) piece(link int) *piece {
	if sb.cur == nil || sb.cur.link != link {
		sb.p.pieces = append(sb.p.pieces, piece{link: link})
		sb.cur = &sb.p.pieces[len(sb.p.pieces)-1]
	}
	return sb.cur
}

// add adds text, which starts at source offset src (exact when the text
// is the source's bytes there), in style s.
func (sb *spanBuilder) add(text string, src int, exact bool, s spanStyle, link int) {
	if text == "" {
		return
	}
	th := sb.th
	if sb.fade != nil && exact {
		// Text revealed at different times fades in on its own: cut it
		// where its opacity changes.
		for len(text) > 0 {
			a := sb.fade(src)
			n := len(text)
			for k := 1; k < len(text); k++ {
				if sb.fade(src+k) != a {
					n = k
					break
				}
			}
			for n < len(text) && !utf8.RuneStart(text[n]) {
				n++
			}
			sb.addSpan(text[:n], src, exact, s, link, a)
			text, src = text[n:], src+n
		}
		return
	}
	a := float32(1)
	if sb.fade != nil {
		a = sb.fade(src)
	}
	_ = th
	sb.addSpan(text, src, exact, s, link, a)
}

func (sb *spanBuilder) addSpan(text string, src int, exact bool, s spanStyle, link int, alpha float32) {
	th := sb.th
	sp := ui.Span{Text: text, Font: th.Font, Size: th.FontSize, Weight: s.weight, Italic: s.italic, Strikethrough: s.strike, Underline: s.underline}
	if s.size != 0 {
		sp.Size = s.size
	}
	if s.font != "" {
		sp.Font = s.font
	}
	if s.code {
		sp.Font = th.CodeFont
		sp.Size = th.em(th.CodeSize)
		if s.size != 0 {
			sp.Size = s.size * th.CodeSize
		}
		sp.Background = th.CodeBackground
		sp.Radius = max(3, sp.Size/4)
		if !s.inLink {
			sp.Color = th.Code
		}
	}
	if s.sub {
		sp.Size *= 0.75
	}
	if s.mark {
		sp.Background = th.Mark
	}
	if s.color.A > 0 {
		sp.Color = s.color
	}
	if alpha < 1 {
		// Not quite transparent: a color of alpha 0 is none, the text's.
		alpha = max(alpha, 0.02)
		if sp.Color.A == 0 {
			sp.Color = th.Text
			if s.inLink {
				sp.Color = th.Link
			}
		}
		sp.Color = sp.Color.Alpha(alpha)
		sp.Background = sp.Background.Alpha(alpha)
	}
	if sb.fade != nil {
		sb.p.fading = sb.p.fading || alpha < 1
	}
	pc := sb.piece(link)
	pc.spans = append(pc.spans, sp)
	sb.p.marks = append(sb.p.marks, runMark{rune: sb.p.runes, src: src, exact: exact})
	sb.p.runes += utf8.RuneCountInString(text)
}

func (sb *spanBuilder) inlines(from, to int, s spanStyle) {
	in := sb.in
	link := -1
	if s.inLink {
		link = sb.linkIndex
	}
	for k := from; k < to; k = in[k].Next {
		n := &in[k]
		switch n.Kind {
		case Text:
			exact := n.End-n.Start == len(n.Text) && sb.d.src[n.Start:n.End] == n.Text
			sb.add(n.Text, n.Start, exact, sb.htmlStyle(s), link)
		case SoftBreak:
			sb.add(" ", n.Start, false, s, link)
		case LineBreak:
			sb.add("\n", n.Start, false, s, link)
		case Code:
			cs := sb.htmlStyle(s)
			cs.code = true
			sb.add(n.Text, codeStart(sb.d.src, n), false, cs, link)
		case Emphasis:
			cs := s
			cs.italic = true
			sb.inlines(k+1, n.Next, cs)
		case Strong:
			cs := s
			cs.weight = sb.th.StrongWeight
			if cs.color.A == 0 && !cs.inLink && sb.th.Strong != sb.th.Text {
				cs.color = sb.th.Strong
			}
			sb.inlines(k+1, n.Next, cs)
		case Strikethrough:
			cs := s
			cs.strike = true
			sb.inlines(k+1, n.Next, cs)
		case Link:
			if s.inLink {
				sb.inlines(k+1, n.Next, s)
				continue
			}
			cs := s
			cs.inLink, cs.pendingLink = true, n.Pending
			cs.color = Color{} // the link's
			save := sb.linkIndex
			sb.linkIndex = k
			sb.inlines(k+1, n.Next, cs)
			sb.linkIndex = save
		case Image:
			// Images within text show their description; a paragraph of
			// images alone shows the images (see imagesOnly).
			cs := s
			cs.color = sb.th.Muted
			alt := PlainText(in[k+1 : n.Next])
			if alt == "" {
				alt = "image"
			}
			sb.add("["+alt+"]", n.Start, false, cs, link)
		case HTML:
			if !sb.inlineHTML(n.Text) {
				sb.add(n.Text, n.Start, true, s, link)
			}
		case Math:
			cs := s
			cs.font = sb.th.CodeFont
			cs.color = sb.th.Muted
			if !cs.italic {
				cs.size = sb.th.em(sb.th.CodeSize)
			}
			sb.add(n.Text, mathStart(sb.d.src, n), false, cs, link)
		case FootnoteRef:
			cs := s
			cs.sub = true
			cs.color = sb.th.Link
			num := 0
			if sb.notes != nil {
				num = sb.notes(n.Text)
			}
			label := n.Text
			if num > 0 {
				label = itoa(num)
			}
			sb.add("["+label+"]", n.Start, false, cs, -2-k)
		}
	}
}

// codeStart returns where a code span's text starts in the source.
func codeStart(src string, n *Inline) int {
	i := n.Start
	for i < n.End && src[i] == '`' {
		i++
	}
	return i
}

func mathStart(src string, n *Inline) int {
	i := n.Start
	for i < n.End && (src[i] == '$' || src[i] == '\\' || src[i] == '(' || src[i] == '[') && i-n.Start < 2 {
		i++
	}
	return i
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// inlineHTML applies the inline HTML tags of formatting language models
// write, <b>, <i>, <code>, <br> and the like, and reports whether tag was
// one: others show as text.
func (sb *spanBuilder) inlineHTML(tag string) bool {
	t := strings.ToLower(strings.Trim(tag, "<>/ \t\n"))
	if i := strings.IndexAny(t, " \t\n"); i >= 0 {
		t = t[:i]
	}
	closing := strings.HasPrefix(tag, "</")
	switch t {
	case "br":
		sb.add("\n", 0, false, sb.style, -1)
		return true
	case "b", "strong", "i", "em", "u", "ins", "s", "del", "strike", "code", "kbd", "samp", "mark", "sub", "sup", "small":
	default:
		return false
	}
	if closing {
		for i := len(sb.html) - 1; i >= 0; i-- {
			if sb.html[i] == t {
				sb.html = append(sb.html[:i], sb.html[i+1:]...)
				break
			}
		}
	} else if !strings.HasSuffix(tag, "/>") {
		sb.html = append(sb.html, t)
	}
	return true
}

// htmlStyle returns s with the inline HTML tags open applied.
func (sb *spanBuilder) htmlStyle(s spanStyle) spanStyle {
	for _, t := range sb.html {
		switch t {
		case "b", "strong":
			s.weight = sb.th.StrongWeight
		case "i", "em":
			s.italic = true
		case "u", "ins":
			s.underline = true
		case "s", "del", "strike":
			s.strike = true
		case "code", "kbd", "samp":
			s.code = true
		case "mark":
			s.mark = true
		case "sub", "sup", "small":
			s.sub = true
		}
	}
	return s
}

// imagesOnly reports whether the inlines are images, and the spaces
// between them, alone: a paragraph showing them as images.
func imagesOnly(in []Inline) bool {
	n := 0
	for k := 0; k < len(in); k = in[k].Next {
		switch in[k].Kind {
		case Image:
			n++
		case SoftBreak, LineBreak:
		case Text:
			if strings.TrimSpace(in[k].Text) != "" {
				return false
			}
		default:
			return false
		}
	}
	return n > 0
}

// sourceAt returns the source offset of rune r of the paragraph's text.
func (p *para) sourceAt(src string, r int) int {
	if len(p.marks) == 0 {
		return 0
	}
	lo, hi := 0, len(p.marks)
	for lo < hi {
		m := (lo + hi) / 2
		if p.marks[m].rune <= r {
			lo = m + 1
		} else {
			hi = m
		}
	}
	m := p.marks[max(lo-1, 0)]
	if !m.exact || r <= m.rune {
		if r > m.rune && lo < len(p.marks) {
			return p.marks[lo].src
		}
		return m.src
	}
	// Count runes into the span's source text.
	off := m.src
	for k := m.rune; k < r && off < len(src); k++ {
		_, n := utf8.DecodeRuneInString(src[off:])
		off += n
	}
	return off
}
