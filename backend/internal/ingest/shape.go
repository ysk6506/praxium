package ingest

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The delimiters of a CSV file, by name. A definition may give the name or the character.
var delimiterNames = map[string]rune{"comma": ',', "tab": '\t', "semicolon": ';', "pipe": '|'}

// normalizedDelimiter checks the delimiter of a dataset and gives it as the character ("" is the comma).
func normalizedDelimiter(v, parser, where string) (string, error) {
	if v == "" {
		return "", nil
	}
	if parser != ParserCSV {
		return "", kernel.Errorf(kernel.CodeInvalidValue, "%s: delimiter belongs to the csv parser", where)
	}
	if r, ok := delimiterNames[strings.ToLower(v)]; ok {
		return string(r), nil
	}
	for _, r := range delimiterNames {
		if v == string(r) {
			return v, nil
		}
	}
	return "", kernel.Errorf(kernel.CodeInvalidValue, "%s: delimiter %q is none of comma, tab, semicolon, pipe (or the character , \\t ; |)", where, v)
}

// CheckDelimiter says whether a delimiter is one a CSV dataset can have: a comma, tab, semicolon or pipe, by name or as
// the character. The empty one is the comma.
func CheckDelimiter(v string) error {
	_, err := normalizedDelimiter(v, ParserCSV, "the delimiter")
	return err
}

// comma is the delimiter a csv dataset reads with.
func (d Dataset) comma() rune {
	if d.Delimiter == "" {
		return ','
	}
	r, _ := utf8.DecodeRuneInString(d.Delimiter)
	return r
}

// delimiterHint tells when a header that came out as one column probably is a row of a file with another delimiter.
func delimiterHint(header []string, comma rune) string {
	if len(header) != 1 {
		return ""
	}
	for _, name := range []string{"tab", "semicolon", "pipe"} {
		r := delimiterNames[name]
		if r != comma && strings.ContainsRune(header[0], r) {
			return fmt.Sprintf("the header came out as one column whose name holds %ss, so the file may be separated by %ss: set \"delimiter\": %q on the dataset", name, name, name)
		}
	}
	return ""
}

// nonBlank is the names of a header that are not empty, normalized.
func nonBlank(header []string) []string {
	var out []string
	for _, h := range header {
		// A title in a merged cell is every column's header; it is listed once.
		if n := NormalizeHeader(h); n != "" && (len(out) == 0 || out[len(out)-1] != n) {
			out = append(out, n)
		}
	}
	return out
}

// maxListed bounds the names a message lists.
const maxListed = 30

// columnList writes names for a message: quoted, in order, and cut after maxListed.
func columnList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	out := make([]string, 0, maxListed+1)
	for i, n := range names {
		if i == maxListed {
			out = append(out, fmt.Sprintf("... (%d more)", len(names)-i))
			break
		}
		out = append(out, fmt.Sprintf("%q", n))
	}
	return strings.Join(out, ", ")
}

// shapeReason says why a table cannot be used: the sheet is not there, or columns are missing. For missing columns
// it lists the header the parser read, so a wrong header row or delimiter shows at once.
func shapeReason(t Table) string {
	if t.Reason != "" {
		return t.Reason
	}
	out := fmt.Sprintf("the file lacks the columns %v", t.Missing)
	if t.HeaderRow > 0 {
		out += fmt.Sprintf("; the header it read (row %d) has the columns: %s; if the header is in another row, set header_row", t.HeaderRow, columnList(t.Header))
	} else if len(t.Header) > 0 {
		out += "; the header it read (line 1) has the columns: " + columnList(t.Header)
	}
	if t.Hint != "" {
		out += "; " + t.Hint
	}
	return out
}

// CheckEncoding refuses a file that is not UTF-8 text, which is the only encoding this build reads: a file in a
// local code page (EUC-KR, CP949, Windows-1252) would be read as broken characters and no column would match, and a
// UTF-16 file (a byte order mark at its start) would not be text at all. The error says what the file is and what to
// do. A UTF-8 byte order mark is fine.
func CheckEncoding(data []byte, parser string) error {
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF}) {
		return kernel.Errorf(kernel.CodeInvalidValue, "the file starts with a UTF-16 byte order mark: it is UTF-16 text (Excel's \"Unicode Text\"), which this build does not read. %s", encodingFix(parser))
	}
	if i := firstInvalidUTF8(data); i >= 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the file is not UTF-8 text (the byte at offset %d does not begin a valid character): it is probably in a local code page such as EUC-KR or CP949, which this build does not decode. %s", i, encodingFix(parser))
	}
	return nil
}

func encodingFix(parser string) string {
	if parser == ParserJSONLines {
		return "Save the file again as UTF-8."
	}
	return "In Excel save it again as \"CSV UTF-8 (Comma delimited)\" (a UTF-8 byte order mark is fine), or give the .xlsx workbook itself as the input."
}

// firstInvalidUTF8 is the offset of the first byte that is not part of valid UTF-8, or -1.
func firstInvalidUTF8(data []byte) int {
	if utf8.Valid(data) {
		return -1
	}
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			return i
		}
		i += size
	}
	return -1
}

// unreadableReason is the reason of an unreadable line as the report says it: the code, and for a natural key that
// is empty the column that is.
func unreadableReason(u Unreadable) string {
	if u.Column == "" {
		return u.Reason
	}
	return fmt.Sprintf("%s: the natural key column %q is empty", u.Reason, u.Column)
}

// delimiterNotes tells, for a CSV file read with the comma that has come out as one column, that the values hold
// tabs, semicolons or vertical bars that repeat: the file probably uses that delimiter.
func delimiterNotes(t TableProfile, o InspectOptions) []string {
	comma := Dataset{Delimiter: o.Delimiter}.comma()
	if len(t.Columns) != 1 {
		return nil
	}
	text := []string{t.Columns[0].Name}
	for _, r := range t.SampleRows {
		text = append(text, r...)
	}
	for _, name := range []string{"tab", "semicolon", "pipe"} {
		r := delimiterNames[name]
		if r == comma {
			continue
		}
		n := 0
		for _, v := range text {
			if strings.ContainsRune(v, r) {
				n++
			}
		}
		if n > 0 && n >= len(text)/2 {
			return []string{fmt.Sprintf("the file came out as one column and its values hold %ss, so it is probably separated by %ss: look at it again with -delimiter %s (and set \"delimiter\": %q in the dataset)", name, name, name, name)}
		}
	}
	return nil
}

// Similar gives up to three of the names that are close to a word: the same without the case and the underscores, or
// a few letters away. It is for a message about a name that is wrong.
func Similar(word string, names []string) []string {
	flat := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "")) }
	w := flat(word)
	var same, near []string
	for _, n := range names {
		switch f := flat(n); {
		case f == w:
			same = append(same, n)
		case len(w) >= 4 && editDistance(w, f) <= 2:
			near = append(near, n)
		}
	}
	if len(same) > 0 {
		return same
	}
	return near[:min(len(near), 3)]
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// chainText is an error in the words of a person: the messages of the chain without the codes in front of them, so a
// wrapped error does not say "invalid_value:" twice.
func chainText(err error) string {
	var ke *kernel.Error
	if !errors.As(err, &ke) {
		return err.Error()
	}
	if ke.Cause == nil {
		return ke.Message
	}
	return ke.Message + ": " + chainText(ke.Cause)
}
