package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ANSI styling for text written into gocui views (the fork parses SGR codes).
func sgr(code, s string) string { return "\x1b[" + code + "m" + s + "\x1b[0m" }

func bold(s string) string   { return sgr("1", s) }
func dim(s string) string    { return sgr("2", s) }
func red(s string) string    { return sgr("31", s) }
func green(s string) string  { return sgr("32", s) }
func yellow(s string) string { return sgr("33", s) }
func blue(s string) string   { return sgr("34", s) }
func cyan(s string) string   { return sgr("36", s) }

// fit pads s with spaces, or truncates it with an ellipsis, to exactly w
// columns. Text is assumed to be single-width (module and device names).
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	n := utf8.RuneCountInString(s)
	if n <= w {
		return s + strings.Repeat(" ", w-n)
	}
	if w == 1 {
		return "…"
	}
	r := []rune(s)
	return string(r[:w-1]) + "…"
}

// truncate shortens s to at most w columns, without padding.
func truncate(s string, w int) string {
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	return fit(s, w)
}

// padRight left-aligns styled text s in w columns, never truncating it.
func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(w-visibleLen(s), 0))
}

// padLeft right-aligns s in w columns.
func padLeft(s string, w int) string {
	if n := utf8.RuneCountInString(s); n < w {
		return strings.Repeat(" ", w-n) + s
	}
	return s
}

// shortSize formats a byte count compactly for list columns: 12K, 20.6M.
func shortSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%dK", b/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(b)/(1024*1024))
	}
}

// longSize formats a byte count for the details view: 20.6 MiB (21585920 bytes).
func longSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d bytes", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KiB (%d bytes)", float64(b)/1024, b)
	default:
		return fmt.Sprintf("%.1f MiB (%d bytes)", float64(b)/(1024*1024), b)
	}
}

// joinLimit joins items with ", ", keeping at most max and noting the rest.
func joinLimit(items []string, max int) string {
	if len(items) == 0 {
		return dim("—")
	}
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:max], ", ") + dim(fmt.Sprintf(" … (+%d more)", len(items)-max))
}

// shellQuote quotes s for a POSIX shell when it contains special characters.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n*?[]$`\"'\\|&;<>(){}#~!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// segment is a run of text in one SGR style ("" for none).
type segment struct{ style, text string }

// word is a space-free run of text, possibly in several styles.
type word []segment

func (w word) len() int {
	n := 0
	for _, s := range w {
		n += utf8.RuneCountInString(s.text)
	}
	return n
}

func (w word) String() string {
	var b strings.Builder
	for _, s := range w {
		if s.style == "" {
			b.WriteString(s.text)
		} else {
			b.WriteString(s.style + s.text + "\x1b[0m")
		}
	}
	return b.String()
}

// cut splits w after n visible characters.
func (w word) cut(n int) (word, word) {
	var head, tail word
	for _, s := range w {
		r := []rune(s.text)
		switch {
		case n <= 0:
			tail = append(tail, s)
		case len(r) <= n:
			head = append(head, s)
		default:
			head = append(head, segment{s.style, string(r[:n])})
			tail = append(tail, segment{s.style, string(r[n:])})
		}
		n -= len(r)
	}
	return head, tail
}

// words splits styled text at spaces, tracking the active SGR style (our
// sgr helper always closes a style with a full reset).
func words(s string) []word {
	var out []word
	var cur word
	style := ""
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			if j := strings.IndexByte(s[i:], 'm'); j > 0 {
				if code := s[i : i+j+1]; code == "\x1b[0m" {
					style = ""
				} else {
					style = code
				}
				i += j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == ' ' {
			if len(cur) > 0 {
				out = append(out, cur)
				cur = nil
			}
			continue
		}
		if n := len(cur); n > 0 && cur[n-1].style == style {
			cur[n-1].text += string(r)
		} else {
			cur = append(cur, segment{style, string(r)})
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// visibleLen is the width of s without SGR escape sequences.
func visibleLen(s string) int {
	return utf8.RuneCountInString(stripSGR(s))
}

func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			if j := strings.IndexByte(s[i:], 'm'); j > 0 {
				i += j
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// wrapText word-wraps styled text: the first line holds at most first
// columns, later ones at most rest. Words longer than a line are split.
func wrapText(s string, first, rest int) []string {
	var lines []string
	var cur []string
	curLen, limit := 0, max(first, 1)
	emit := func() {
		lines = append(lines, strings.Join(cur, " "))
		cur, curLen, limit = nil, 0, max(rest, 1)
	}
	for _, w := range words(s) {
		for {
			need := w.len()
			if curLen > 0 {
				need++
			}
			if curLen+need <= limit {
				cur = append(cur, w.String())
				curLen += need
				break
			}
			if curLen > 0 {
				emit()
				continue
			}
			n := breakPoint(w, limit)
			head, tail := w.cut(n)
			cur = append(cur, head.String())
			curLen = n
			emit()
			w = tail
		}
	}
	if len(cur) > 0 || len(lines) == 0 {
		lines = append(lines, strings.Join(cur, " "))
	}
	return lines
}

// breakPoint picks where to split a word that is longer than limit: after
// the last '/', '-', ',' or ':' in the second half of the line if there is
// one (paths, module lists), else at limit.
func breakPoint(w word, limit int) int {
	var r []rune
	for _, s := range w {
		r = append(r, []rune(s.text)...)
	}
	for i := min(limit, len(r)); i > limit/2; i-- {
		if strings.ContainsRune("/-,:", r[i-1]) {
			return i
		}
	}
	return limit
}

// indentWrap fits a line into width columns. Lines that fit are returned
// unchanged; longer ones keep their leading indentation and continue with
// extra columns of indentation.
func indentWrap(s string, width, extra int) string {
	if width <= 0 || visibleLen(s) <= width {
		return s
	}
	plain := stripSGR(s)
	lead := len(plain) - len(strings.TrimLeft(plain, " "))
	pad := strings.Repeat(" ", lead)
	lines := wrapText(s, width-lead, width-lead-extra)
	for i := range lines {
		if i == 0 {
			lines[i] = pad + lines[i]
		} else {
			lines[i] = pad + strings.Repeat(" ", extra) + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
