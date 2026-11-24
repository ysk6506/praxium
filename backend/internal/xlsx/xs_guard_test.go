package xlsx

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// xsTokens reads every token of an XML text through the guard of a part and gives the error it ended with, or nil at the end.
func xsTokens(text string) error {
	dec := xmlDecoder("xl/p.xml", strings.NewReader(text))
	for {
		if _, err := dec.Token(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func xsNested(n int) string { return strings.Repeat("<a>", n) + strings.Repeat("</a>", n) }

// [REQ-807]
func TestXsXMLOfTheRightShapeIsReadThroughTheGuard(t *testing.T) {
	big := strings.Repeat("a", maxXMLToken)
	for name, text := range map[string]string{
		"a declaration":              `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><a/>`,
		"a declaration at the end":   `<?xml?><a/>`,
		"a comment":                  `<a><!-- a > comment <b> --></a>`,
		"an empty comment":           `<a><!----></a>`,
		"a CDATA section":            `<a><![CDATA[ <b> ]]> </a>`,
		"an empty CDATA section":     `<a><![CDATA[]]></a>`,
		"a value with a > in quotes": `<a b="x>y" c='1>2'/>`,
		"a self-closing element":     `<a><b/><c x="1"/></a>`,
		"the deepest nesting":        xsNested(maxXMLDepth),
		"a text of the largest size": `<a>` + big + `</a>`,
		"a comment of the largest":   `<a><!--` + big[:maxXMLToken-7] + `--></a>`,
		"a CDATA of the largest":     `<a><![CDATA[` + big[:maxXMLToken-12] + `]]></a>`,
	} {
		if err := xsTokens(text); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// [REQ-807]
func TestXsXMLThatTheDecoderWouldGatherWithoutABoundIsRefused(t *testing.T) {
	big := strings.Repeat("a", maxXMLToken+1)
	for _, c := range []struct{ name, text, want string }{
		{"a tag", `<a b="` + big + `"/>`, "a tag is larger than 1048576 bytes"},
		{"an end tag", `<a></a` + big + `>`, "a tag is larger"},
		{"a text", `<a>` + big + `</a>`, "a stretch of text is larger"},
		{"a comment", `<a><!--` + big + `--></a>`, "a comment is larger"},
		{"a CDATA section", `<a><![CDATA[` + big + `]]></a>`, "a CDATA section is larger"},
		{"a processing instruction", `<?xml ` + big + `?><a/>`, "a processing instruction is larger"},
		{"nesting that is too deep", xsNested(maxXMLDepth + 1), "nested deeper than 64 levels"},
		{"a document type", `<!DOCTYPE a [<!ENTITY x "y">]><a/>`, "a document type declaration"},
		{"another declaration", `<a><!ELEMENT b ANY></a>`, "a document type declaration"},
		{"a processing instruction that is not the declaration", `<a><?php x ?></a>`, "a processing instruction"},
		{"a stylesheet instruction", `<?xml-stylesheet href="x"?><a/>`, "a processing instruction"},
		{"a short instruction", `<?x?><a/>`, "a processing instruction"},
		{"text that ends in the middle of the size", `<a>` + big, "a stretch of text is larger"},
		// "<!-->" is no comment that ends, so what follows is still in it; a guard that took it as ended would let the rest through
		{"a comment that starts like one that ends", `<a><!-->` + strings.Repeat("<b>", maxXMLToken/3+1), "a comment is larger"},
		{"a CDATA section that ends at once", `<a><![CDATA[]]>` + big + `</a>`, "a stretch of text is larger"},
	} {
		err := xsTokens(c.text)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "the workbook is damaged: in the part xl/p.xml") {
			t.Errorf("%s: got %v, want one that says %q", c.name, err, c.want)
		}
	}
}

// [REQ-807]
func TestXsGuardKeepsWhatItSaidWhenReadAgainAndGivesNothingAfter(t *testing.T) {
	g := newXMLGuard("xl/p.xml", strings.NewReader(`<!DOCTYPE a>rest`))
	buf := make([]byte, 64)
	n, first := g.Read(buf)
	if first == nil || n != 0 {
		t.Fatalf("the first read gave %d bytes and %v", n, first)
	}
	if n, again := g.Read(buf); n != 0 || again != first {
		t.Fatalf("the next read gave %d bytes and %v, want the same error", n, again)
	}
}
