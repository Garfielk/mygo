package markdown

import (
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// extras exercises what the spec examples do not: footnotes, math,
// definitions after their links, task lists, tables streaming.
const extras = `# Notes on streaming

Text with a footnote[^1] and another[^note], math $$e^{i\pi}+1=0$$ and \(a^2\),
and a [link defined later][later] with *emphasis*, **strong**, ~~struck~~ and ` + "`code`" + `.

$$
\int_0^1 x^2\,dx = \frac{1}{3}
$$

\[
a + b
\]

- [ ] a task
- [x] a done task
  continued lazily

| a | b |
|---|:-:|
| 1 | 2 |
| 3 | 4 |

> A quote
with a lazy line
> - and a list
>   inside

1. one
2. two

   still two

3) three
10. ten

` + "```go" + `
func main() {
	fmt.Println("hi")
}
` + "```" + `

    indented code

<div>
raw <b>HTML</b>
</div>

Setext heading
--------------

Visit www.example.com, https://example.com/a_(b) or mail someone@example.com.

[later]: https://example.com/later "Later"
[^1]: The footnote.
[^note]: A footnote
    of two lines.
`

func corpus(t testing.TB) []string {
	llm, err := os.ReadFile("testdata/llm.md")
	if err != nil {
		t.Fatal(err)
	}
	return []string{string(llm), extras}
}

// TestAppendMatchesWhole appends documents a character at a time and
// checks each prefix parses as when parsed whole.
func TestAppendMatchesWhole(t *testing.T) {
	examples := loadSpec(t, "commonmark.json")
	for _, ex := range loadSpec(t, "gfm.json") {
		if strings.Contains(ex.Section, "(extension)") {
			examples = append(examples, ex)
		}
	}
	for _, ex := range examples {
		for _, ext := range []Extensions{0, Default} {
			d := New(ext)
			src := ex.Markdown
			for i := 0; i < len(src); {
				_, n := utf8.DecodeRuneInString(src[i:])
				d.Append(src[i : i+n])
				i += n
				if want := render(src[:i], ext); d.HTML() != want {
					t.Fatalf("example %d, ext %b, after %q:\ngot  %q\nwant %q", ex.Example, ext, src[:i], d.HTML(), want)
				}
			}
		}
	}
}

func TestAppendCorpus(t *testing.T) {
	for _, src := range corpus(t) {
		d := New(Default)
		for i := 0; i < len(src); {
			_, n := utf8.DecodeRuneInString(src[i:])
			d.Append(src[i : i+n])
			i += n
			if i%997 == 0 || i == len(src) {
				if want := render(src[:i], Default); d.HTML() != want {
					t.Fatalf("after %d bytes (%q):\ngot  %q\nwant %q", i, src[max(0, i-40):i], d.HTML(), want)
				}
			}
		}
	}
}

// TestAppendChunks appends in random chunks, a stream's tokens.
func TestAppendChunks(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, src := range corpus(t) {
		for range 20 {
			d := New(Default)
			for i := 0; i < len(src); {
				n := min(1+r.IntN(12), len(src)-i)
				d.Append(src[i : i+n])
				i += n
			}
			if want := render(src, Default); d.HTML() != want {
				t.Fatalf("got  %q\nwant %q", d.HTML(), want)
			}
		}
	}
}

// TestStreamingEnds checks that a document streamed with healing parses
// exactly once streaming is turned off.
func TestStreamingEnds(t *testing.T) {
	for _, src := range corpus(t) {
		d := New(Default)
		d.SetStreaming(true)
		for i := 0; i < len(src); {
			_, n := utf8.DecodeRuneInString(src[i:])
			d.Append(src[i : i+n])
			i += n
			d.HTML() // inlines of the tail, healed
		}
		d.SetStreaming(false)
		if want := render(src, Default); d.HTML() != want {
			t.Fatalf("got  %q\nwant %q", d.HTML(), want)
		}
	}
}

// TestEditsMatchWhole edits documents at random places and checks they
// parse as when parsed whole.
func TestEditsMatchWhole(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	pieces := []string{"", "\n", "\n\n", "- ", "> ", "# ", "```", "**", "*", "_", "[a]", "[a]: /u\n", "|", "| a | b |\n|---|---|\n", "    ", "1. ", "---\n", "text", "`", "<div>", "$$", "[^1]"}
	for _, src := range corpus(t) {
		d := New(Default)
		d.Set(src)
		for range 400 {
			cur := d.Source()
			from := r.IntN(len(cur) + 1)
			to := min(len(cur), from+r.IntN(30))
			if r.IntN(3) == 0 {
				to = from
			}
			text := pieces[r.IntN(len(pieces))]
			next := cur[:from] + text + cur[to:]
			if !utf8.ValidString(next) {
				continue
			}
			d.Edit(from, to, text)
			if d.Source() != next {
				t.Fatalf("source %q, want %q", d.Source(), next)
			}
			if want := render(next, Default); d.HTML() != want {
				t.Fatalf("edit [%d, %d) to %q:\ngot  %q\nwant %q", from, to, text, d.HTML(), want)
			}
		}
	}
}

// TestDefinitionLater checks that a definition appended after its link
// makes the link a link, and that removing it unmakes it.
func TestDefinitionLater(t *testing.T) {
	d := New(Default)
	d.Append("See [docs].\n\nMore text.\n\n")
	if got := d.HTML(); strings.Contains(got, "<a ") {
		t.Fatalf("link before its definition: %q", got)
	}
	d.Append("[docs]: https://example.com\n")
	if got := d.HTML(); !strings.Contains(got, `<a href="https://example.com">docs</a>`) {
		t.Fatalf("no link after the definition: %q", got)
	}
	if d.Block(1).Revision() == 0 {
		t.Error("the paragraph's revision did not change")
	}
	d.Set("See [docs].\n\nMore text.\n\n")
	if got := d.HTML(); strings.Contains(got, "<a ") {
		t.Fatalf("link after its definition went: %q", got)
	}
}
