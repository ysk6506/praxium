package xlsx

import (
	"encoding/xml"
	"fmt"
	"strings"
	"unicode/utf8"
)

// readSharedStrings reads the table of shared strings. An item is the text of its runs, so a string with several
// fonts is one string; the phonetic hints of East Asian text (rPh) are no part of it.
func readSharedStrings(dec *xml.Decoder, maxStrings, maxChars int) ([]string, error) {
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			return out, eofOrError(err)
		}
		e, ok := tok.(xml.StartElement)
		if !ok || e.Name.Local != "si" {
			continue
		}
		if len(out) >= maxStrings {
			return nil, overLimit("max_shared_strings", maxStrings, "in the table of shared strings")
		}
		text, err := richText(dec, "si", maxChars)
		if err != nil {
			return nil, err
		}
		if len(text) > maxChars && utf8.RuneCountInString(text) > maxChars {
			return nil, overLimit("max_cell_chars", maxChars, fmt.Sprintf("by shared string %d", len(out)))
		}
		out = append(out, text)
	}
}

// maxTextBytes is how many bytes the text of a cell may have before it is unescaped, for a limit of characters. A character is at most
// four bytes, and "_x0041_" writes one in seven, and a pair of surrogates in fourteen; so the bound is far over any text that is
// within the limit. It stops a text that is gathered from many small pieces from growing before the limit of characters is looked at.
func maxTextBytes(maxChars int) int { return 16 * maxChars }

// appendBounded adds characters to the text of a cell and refuses it when it is over what a text within the limit can be.
func appendBounded(b *strings.Builder, t xml.CharData, maxChars int) error {
	b.Write(t)
	if b.Len() > maxTextBytes(maxChars) {
		return overLimit("max_cell_chars", maxChars, "by a text of the workbook")
	}
	return nil
}

// startOfText follows the start of an element of a rich text: whether the characters that come are text, and how deep in a phonetic hint.
func startOfText(name string, inText bool, depthPhonetic int) (bool, int) {
	switch name {
	case "rPh":
		return inText, depthPhonetic + 1
	case "t":
		return depthPhonetic == 0, depthPhonetic
	}
	return inText, depthPhonetic
}

// richText reads the text of an element that holds <t> and runs (<r><t>) up to the end of the element named end.
func richText(dec *xml.Decoder, end string, maxChars int) (string, error) {
	var b strings.Builder
	depthPhonetic, inText := 0, false
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			inText, depthPhonetic = startOfText(t.Name.Local, inText, depthPhonetic)
		case xml.CharData:
			if inText {
				if err := appendBounded(&b, t, maxChars); err != nil {
					return "", err
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "rPh":
				depthPhonetic--
			case end:
				return unescapeXString(b.String()), nil
			}
		}
	}
}
