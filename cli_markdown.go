package main

import (
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Small in-house renderer for winget release-notes markdown — no heavy
// dependency. It handles the shapes winget manifests actually use: headings,
// bullet/numbered lists, paragraphs, links, and basic inline emphasis, themed
// to the active palette and word-wrapped to the terminal.
//
// It deliberately does NOT syntax-highlight code blocks (winget notes are
// prose, not code). Pulling in glamour+chroma for that costs ~8 MB of binary
// and the chroma/goldmark/bluemonday/net dependency tree — see the roadmap
// tech-debt note. Fenced code is shown verbatim, indented.

var (
	mdHeadingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	mdBulletRe  = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	mdNumberRe  = regexp.MustCompile(`^\s*(\d+[.)])\s+(.*)$`)
	mdLinkRe    = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
)

func renderReleaseNotesMarkdown(notes string) string {
	width := notesWidth()
	var b strings.Builder
	inCode := false

	for _, raw := range strings.Split(notes, "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			b.WriteString("    " + line + "\n")
			continue
		}
		if trimmed == "" {
			b.WriteByte('\n')
			continue
		}

		switch {
		case mdHeadingRe.MatchString(trimmed):
			// Headings are short; style the whole line in accent, no wrap.
			m := mdHeadingRe.FindStringSubmatch(trimmed)
			b.WriteString(styleNotesHeader(renderInlineMarkdown(m[2])) + "\n")

		case mdBulletRe.MatchString(line):
			m := mdBulletRe.FindStringSubmatch(line)
			// Accent the bullet glyph; visible prefix "  • " is 4 cols wide.
			writeWrappedNote(&b, "  "+cliAccent("•")+" ", 4, "    ", renderInlineMarkdown(m[1]), width)

		case mdNumberRe.MatchString(line):
			m := mdNumberRe.FindStringSubmatch(line)
			prefix := "  " + m[1] + " "
			pw := runewidth.StringWidth(prefix)
			writeWrappedNote(&b, prefix, pw, strings.Repeat(" ", pw), renderInlineMarkdown(m[2]), width)

		default:
			writeWrappedNote(&b, "", 0, "", renderInlineMarkdown(trimmed), width)
		}
	}
	return b.String()
}

// renderInlineMarkdown flattens the common inline markdown winget notes use:
// links become "text (url)", and inline-code / bold markers are stripped.
// Single * / _ (italic) are left alone to avoid mangling mid-word punctuation.
func renderInlineMarkdown(s string) string {
	s = mdLinkRe.ReplaceAllString(s, "$1 ($2)")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

// writeWrappedNote word-wraps text to width with a (possibly ANSI-styled) prefix
// on the first line and a plain hanging indent on continuations. prefixWidth is
// the prefix's VISIBLE column width (the prefix string may contain zero-width
// ANSI escapes), so wrapping math stays correct.
func writeWrappedNote(b *strings.Builder, prefix string, prefixWidth int, hang, text string, width int) {
	words := strings.Fields(text)
	if len(words) == 0 {
		b.WriteString(prefix + "\n")
		return
	}
	hangWidth := runewidth.StringWidth(hang)
	cur := prefix
	curWidth := prefixWidth
	atLineStart := true
	for _, w := range words {
		ww := runewidth.StringWidth(w)
		switch {
		case atLineStart:
			cur += w
			curWidth += ww
			atLineStart = false
		case curWidth+1+ww <= width:
			cur += " " + w
			curWidth += 1 + ww
		default:
			b.WriteString(cur + "\n")
			cur = hang + w
			curWidth = hangWidth + ww
		}
	}
	b.WriteString(cur + "\n")
}
