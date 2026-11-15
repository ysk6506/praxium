package kgread

import (
	"strings"
	"testing"
)

// [REQ-510]
func TestCleanTextTakesOutWhatHidesAnInstruction(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain text stays", "hello world", "hello world"},
		{"korean stays", "서버 점검", "서버 점검"},
		{"controls go", "a\x00b\x07c\x1bd", "abcd"},
		{"white space controls become a space", "a\tb\nc\rd", "a b c d"},
		{"zero width characters go", "a\U0000200Bb\U0000200Dc\U0000FEFFd", "abcd"},
		{"direction controls go", "a\U0000202Eb\U00002066c\U0000200Fd", "abcd"},
		{"tag characters go", "a\U000E0041\U000E0042b", "ab"},
		{"invalid utf-8 becomes the replacement character", "a\xffb", "a�b"},
	}
	for _, c := range cases {
		got, cut := CleanText(c.in, 100)
		if got != c.want || cut {
			t.Errorf("%s: CleanText(%q) = %q, %v; want %q, false", c.name, c.in, got, cut, c.want)
		}
	}
}

// [REQ-510]
func TestCleanTextCutsAndSaysSo(t *testing.T) {
	got, cut := CleanText(strings.Repeat("가", 10), 4)
	if got != "가가가가" || !cut {
		t.Fatalf("CleanText cut = %q, %v; want four characters and true", got, cut)
	}
	if got, cut := CleanText("abcd", 4); got != "abcd" || cut {
		t.Fatalf("a text exactly at the limit must not be reported as cut: %q, %v", got, cut)
	}
}
