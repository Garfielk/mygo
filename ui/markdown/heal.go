package markdown

import "strings"

// Healing completes what the end of a streaming document leaves open in
// the inline content of its last block, as Vercel's remend does to the
// text, but from the parser's own delimiters, and only for that block:
// the source never changes. It returns the text to parse and the length
// of what came from the source, past which the text was added.

// heal returns s completed, and whether the tree the parser holds is
// already that of the text returned.
func (ip *inlineParser) heal(s string) (string, int, bool) {
	s = trimTrailingMarkers(s)
	from := len(s)
	// A link whose destination is being written: [text](http…
	if k := pendingLink(s); k >= 0 {
		s += ")"
	}
	// A code span not closed.
	if ticks := openCodeSpan(s); ticks > 0 {
		run := 0
		for run < len(s) && s[len(s)-1-run] == '`' {
			run++
		}
		if run >= ticks || run == 0 {
			run = 0
		}
		s += strings.Repeat("`", ticks-run)
		return s, from, false
	}
	// Math between $$ not closed.
	if ip.ext&ExtMath != 0 && strings.Count(s, "$$")%2 == 1 && !strings.HasSuffix(s, "$$") {
		s += "$$"
	}
	// Emphasis and strikethrough not closed: the openers the parser
	// leaves, innermost first.
	ip.run(s)
	built := true
	if len(ip.leftover) > 0 {
		var b strings.Builder
		b.WriteString(s)
		n := 0
		for _, d := range ip.leftover {
			if d.c == '*' && wordInside(s, d.at, d.orig) {
				continue
			}
			b.WriteString(strings.Repeat(string(d.c), d.n))
			n++
		}
		if n > 0 {
			s = b.String()
			built = false
		}
	}
	return s, from, built
}

// wordInside reports whether a run of n delimiters at i has letters or
// digits on both sides, as 2*3, which is not emphasis being written.
func wordInside(s string, i, n int) bool {
	if i == 0 || i+n >= len(s) {
		return false
	}
	before, after := rune(s[i-1]), rune(s[i+n])
	return isAlnumByte(before) && isAlnumByte(after)
}

func isAlnumByte(r rune) bool {
	return r < 0x80 && (isASCIILetter(byte(r)) || isASCIIDigit(byte(r))) || r >= 0x80
}

// trimTrailingMarkers drops what the end of s may be starting, with
// nothing after it yet: a run of delimiters opening emphasis, a code span
// or math, a bracket, or a backslash.
func trimTrailingMarkers(s string) string {
	for {
		t := strings.TrimRight(s, " \t")
		if t == "" {
			return t
		}
		c := t[len(t)-1]
		k := len(t)
		for k > 0 && t[k-1] == c {
			k--
		}
		run := len(t) - k
		opening := k == 0 || t[k-1] == ' ' || t[k-1] == '\t' || t[k-1] == '\n'
		switch {
		case strings.HasSuffix(t, "!["):
			t = t[:len(t)-2]
		case c == '[':
			t = t[:k]
		case (c == '*' || c == '_' || c == '~' || c == '$') && opening:
			t = t[:k]
		case c == '`' && !closesSpan(t[:k], run):
			t = t[:k]
		case c == '\\' && run%2 == 1:
			t = t[:len(t)-1]
		default:
			return t
		}
		s = t
	}
}

// closesSpan reports whether a run of n backticks after s closes a code
// span s opens.
func closesSpan(s string, n int) bool { return openCodeSpan(s) == n }

// pendingLink returns where a link's destination being written starts in
// s, after ](, or -1.
func pendingLink(s string) int {
	k := strings.LastIndex(s, "](")
	if k < 0 || !strings.Contains(s[:k], "[") {
		return -1
	}
	rest := s[k+2:]
	if strings.ContainsAny(rest, ") \t\n") {
		return -1
	}
	// Not inside a code span.
	if strings.Count(s[:k], "`")%2 == 1 {
		return -1
	}
	return k + 2
}

// openCodeSpan returns the length of the run of backticks opening a code
// span that s leaves open, or 0.
func openCodeSpan(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '`':
			j := i
			for j < len(s) && s[j] == '`' {
				j++
			}
			n := j - i
			// Its closer: the next run of as many.
			k := j
			found := false
			for k < len(s) {
				m := strings.IndexByte(s[k:], '`')
				if m < 0 {
					break
				}
				k += m
				e := k
				for e < len(s) && s[e] == '`' {
					e++
				}
				if e-k == n {
					found = true
					i = e - 1
					break
				}
				k = e
			}
			if !found {
				if j == len(s) {
					return 0 // a run at the very end opens nothing yet
				}
				return n
			}
		}
	}
	return 0
}
