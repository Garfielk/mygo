package markdown

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func llm(b *testing.B) string {
	data, err := os.ReadFile("testdata/llm.md")
	if err != nil {
		b.Fatal(err)
	}
	return string(data)
}

// inlinesAll parses the inlines of every block, as showing them does.
func inlinesAll(d *Document) {
	for i := range d.Len() {
		d.Inlines(i)
	}
}

// BenchmarkParse parses the 50 KB answer whole, inlines included.
func BenchmarkParse(b *testing.B) {
	src := llm(b)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		inlinesAll(Parse(src))
	}
}

// benchStream streams the answer in chunks of n bytes, with healing, the
// inlines of the tail parsed after each, as a frame shows them; it
// reports the time per chunk.
func benchStream(b *testing.B, n int) {
	src := llm(b)
	b.ReportAllocs()
	chunks := 0
	for b.Loop() {
		d := New(Default)
		d.SetStreaming(true)
		for i := 0; i < len(src); i += n {
			d.Append(src[i:min(i+n, len(src))])
			inlinesAll(d)
			chunks++
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(chunks), "ns/chunk")
}

func BenchmarkStream4(b *testing.B)  { benchStream(b, 4) }
func BenchmarkStream64(b *testing.B) { benchStream(b, 64) }

// BenchmarkEdit types a character in the middle of the answer and takes
// it back, as an editor's preview.
func BenchmarkEdit(b *testing.B) {
	src := llm(b)
	d := Parse(src)
	inlinesAll(d)
	mid := len(src) / 2
	for mid < len(src) && src[mid] != ' ' {
		mid++
	}
	b.ReportAllocs()
	for b.Loop() {
		d.Edit(mid, mid, "x")
		inlinesAll(d)
		d.Edit(mid, mid+1, "")
		inlinesAll(d)
	}
}

// BenchmarkViewSteady draws frames of the 50 KB answer shown whole, in a
// window of 800 by 1000, as nothing changes.
func BenchmarkViewSteady(b *testing.B) {
	src := llm(b)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Fill().Children(func() { View(c, src, nil) })
	}, 800, 1000)
	b.ReportAllocs()
	for b.Loop() {
		tt.Frame()
	}
}

// BenchmarkViewStream draws the frames of the 50 KB answer streaming in,
// 16 bytes a frame, as a chat follows a reply's end; it reports the time
// of a frame with its token.
func BenchmarkViewStream(b *testing.B) {
	src := llm(b)
	o := &Options{Reveal: &Reveal{Unit: AtOnce}}
	n := 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Fill().Children(func() { Stream(c, src[:n], o) })
	}, 800, 1000)
	b.ReportAllocs()
	for b.Loop() {
		n += 16
		if n > len(src) {
			n = 16
		}
		tt.Frame()
	}
}
