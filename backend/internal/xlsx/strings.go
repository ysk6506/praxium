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
		text, err := richText(dec, "si")
		if err != nil {
			return nil, err
		}
		if len(text) > maxChars && utf8.RuneCountInString(text) > maxChars {
			return nil, overLimit("max_cell_chars", maxChars, fmt.Sprintf("by shared string %d", len(out)))
		}
		out = append(out, text)
	}
}

// richText reads the text of an element that holds <t> and runs (<r><t>) up to the end of the element named end.
func richText(dec *xml.Decoder, end string) (string, error) {
	var b strings.Builder
	depthPhonetic, inText := 0, false
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "rPh":
				depthPhonetic++
			case "t":
				inText = depthPhonetic == 0
			}
		case xml.CharData:
			if inText {
				b.Write(t)
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
