package kgread

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CleanText makes a text that came from the caller or from the KG fit to be shown to a model and to a log (ADR-0062,
// kg-mcp.md 4.3): it takes out control characters, characters that do not show (zero-width and the other format
// characters), the controls of text direction and the tag characters, turns the white space controls into a plain
// space, and cuts the result to maxRunes characters. truncated says whether it was cut. Invalid UTF-8 becomes the
// replacement character. There is no complete defence against a text that reads as an instruction; this takes away
// the ways to hide one.
func CleanText(s string, maxRunes int) (cleaned string, truncated bool) {
	var b strings.Builder
	n := 0
	for _, r := range s {
		r = cleanRune(r)
		if r < 0 {
			continue
		}
		if n >= maxRunes {
			return b.String(), true
		}
		b.WriteRune(r)
		n++
	}
	return b.String(), false
}

// cleanRune gives the rune that stands for r in a cleaned text, or -1 for a rune that is taken out.
func cleanRune(r rune) rune {
	switch {
	case r == utf8.RuneError:
		return 0xFFFD
	case r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == 0x85 || r == 0x2028 || r == 0x2029:
		return ' '
	case unicode.IsControl(r), unicode.Is(unicode.Cf, r), r >= 0xE0000 && r <= 0xE007F, r == 0x180E:
		return -1
	}
	return r
}
