package richtext

import (
	"strconv"
	"strings"
	"unicode"
)

// Markdown returns t as CommonMark with GitHub's extensions: headings,
// lists and task lists, quotations, fenced blocks of code, **bold**,
// *italic*, ~~strikethrough~~, `code`, links and images; underlines are
// HTML's <u>, which Markdown keeps. Body paragraphs are apart by a blank
// line, so that they stay paragraphs; alignment, fonts and colors have no
// Markdown and are left out.
func (t Text) Markdown() string {
	var b strings.Builder
	var paras []Paragraph
	for _, p := range t.Paragraphs() {
		paras = append(paras, p)
	}
	// numbers counts the items of the numbered lists open, by level.
	var numbers []int
	prevList := false
	for i := 0; i < len(paras); i++ {
		p := paras[i]
		st := p.Style
		if i > 0 && !(st.List != NoList && prevList) {
			b.WriteString("\n\n")
		} else if i > 0 {
			b.WriteByte('\n')
		}
		prevList = st.List != NoList
		if st.List == NoList {
			numbers = numbers[:0]
		}
		quote := ""
		if st.Quote {
			quote = "> "
		}
		switch {
		case st.CodeBlock:
			fence := "```"
			for k := i; k < len(paras) && paras[k].Style.CodeBlock; k++ {
				for strings.Contains(plainOf(paras[k]), fence) {
					fence += "`"
				}
			}
			b.WriteString(quote + fence + "\n")
			for ; i < len(paras) && paras[i].Style.CodeBlock; i++ {
				b.WriteString(quote + plainOf(paras[i]) + "\n")
			}
			i--
			b.WriteString(quote + fence)
			continue
		case st.Heading > 0:
			b.WriteString(quote + strings.Repeat("#", min(st.Heading, 6)) + " ")
		case st.List != NoList:
			level := st.Indent
			for len(numbers) <= level {
				numbers = append(numbers, 0)
			}
			numbers = numbers[:level+1]
			b.WriteString(quote + strings.Repeat("    ", level))
			switch st.List {
			case Numbers:
				numbers[level]++
				b.WriteString(strconv.Itoa(numbers[level]) + ". ")
			case Tasks:
				if st.Checked {
					b.WriteString("- [x] ")
				} else {
					b.WriteString("- [ ] ")
				}
				numbers[level] = 0
			default:
				b.WriteString("- ")
				numbers[level] = 0
			}
		default:
			b.WriteString(quote)
		}
		writeMarkdownRuns(&b, p.Runs, st.List == NoList && st.Heading == 0)
	}
	return b.String()
}

// plainOf returns the text of a paragraph.
func plainOf(p Paragraph) string {
	var b strings.Builder
	for _, r := range p.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// mark is a Markdown mark around runs: a link (by its URL), bold, italic,
// strikethrough, underline or code, from the outside in.
type mark struct {
	kind byte // 'a', 'b', 'i', 's', 'u', '`'
	url  string
}

func marksOf(a Attributes) []mark {
	var m []mark
	if a.Link != "" {
		m = append(m, mark{kind: 'a', url: a.Link})
	}
	if a.Bold() {
		m = append(m, mark{kind: 'b'})
	}
	if a.Italic {
		m = append(m, mark{kind: 'i'})
	}
	if a.Strikethrough {
		m = append(m, mark{kind: 's'})
	}
	if a.Underline {
		m = append(m, mark{kind: 'u'})
	}
	if a.Code {
		m = append(m, mark{kind: '`'})
	}
	return m
}

func (m mark) open() string {
	switch m.kind {
	case 'a':
		return "["
	case 'b':
		return "**"
	case 'i':
		return "*"
	case 's':
		return "~~"
	case 'u':
		return "<u>"
	}
	return "`"
}

func (m mark) close() string {
	switch m.kind {
	case 'a':
		return "](" + markdownURL(m.url) + ")"
	case 'u':
		return "</u>"
	}
	return m.open()
}

// writeMarkdownRuns writes runs with their marks, opening and closing
// them as they change, whitespace outside them so that they flank words
// as CommonMark wants. At the start of a body paragraph, what would start
// a block is escaped.
func writeMarkdownRuns(b *strings.Builder, runs []Run, body bool) {
	var open []mark
	pending := ""
	first := true
	closeTo := func(n int) {
		for len(open) > n {
			b.WriteString(open[len(open)-1].close())
			open = open[:len(open)-1]
		}
	}
	for _, r := range runs {
		if at := r.Attributes.Attachment; at != nil {
			closeTo(0)
			b.WriteString(pending)
			pending = ""
			b.WriteString("![" + escapeMarkdown(at.Name) + "](" + markdownURL(at.URL) + ")")
			first = false
			continue
		}
		core := strings.TrimFunc(r.Text, unicode.IsSpace)
		if core == "" {
			pending += r.Text
			continue
		}
		lead := r.Text[:strings.Index(r.Text, core)]
		trail := r.Text[len(lead)+len(core):]
		want := marksOf(r.Attributes)
		k := 0
		for k < len(open) && k < len(want) && open[k] == want[k] {
			k++
		}
		closeTo(k)
		b.WriteString(pending + lead)
		for _, m := range want[k:] {
			b.WriteString(m.open())
			open = append(open, m)
		}
		if r.Attributes.Code {
			b.WriteString(codeSpan(core))
		} else {
			s := escapeMarkdown(core)
			if first && body {
				s = escapeBlockStart(s)
			}
			b.WriteString(s)
		}
		first = false
		pending = trail
	}
	closeTo(0)
	b.WriteString(pending)
}

// codeSpan returns s as the inside of a code span whose marks the caller
// writes: backticks inside it take a longer fence.
func codeSpan(s string) string {
	if !strings.Contains(s, "`") {
		return s
	}
	return "` " + s + " `"
}

// escapeMarkdown escapes what Markdown would read as marks.
func escapeMarkdown(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '*', '_', '`', '[', ']', '<', '~':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeBlockStart escapes the start of a body paragraph that Markdown
// would read as a heading, a list, a quotation or a rule.
func escapeBlockStart(s string) string {
	switch {
	case strings.HasPrefix(s, "#"), strings.HasPrefix(s, ">"), strings.HasPrefix(s, "- "), strings.HasPrefix(s, "+ "):
		return "\\" + s
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && (s[i] == '.' || s[i] == ')') {
		return s[:i] + "\\" + s[i:]
	}
	return s
}

// markdownURL returns a URL as the destination of a link.
func markdownURL(u string) string {
	if strings.ContainsAny(u, " ()<>") {
		return "<" + strings.NewReplacer("<", "%3C", ">", "%3E").Replace(u) + ">"
	}
	return u
}
