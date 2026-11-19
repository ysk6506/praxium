package ingest

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// The small checks the definitions of an HTTP source and of a PostgreSQL source share.

var placeholderRe = regexp.MustCompile(`^\{\{credential\.([a-z][a-z_]*)\}\}$`)

// hasBraceProblem tells whether a text has a { or a } that is not written doubled. The doubled ones stand for the brace
// itself (UnescapeBraces); a single one would be a variable, and no variable is defined yet.
func hasBraceProblem(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' && s[i] != '}' {
			continue
		}
		if i+1 < len(s) && s[i+1] == s[i] {
			i++
			continue
		}
		return true
	}
	return false
}

// checkText records the error of a text of a request or a statement that has a single brace, or that has a placeholder
// for a secret where none may be.
func (c *collector) checkText(source, pointer, field, s string) bool {
	switch {
	case strings.Contains(s, "{{credential."):
		c.add(ecAdElsewhere, pointer, Params{"source": source, "field": field},
			"source %s: %s has a {{credential.…}}, which only a value of the body of the token request of the authentication may have", source, field)
	case hasBraceProblem(s):
		c.add(ecAdBraces, pointer, Params{"source": source, "field": field},
			"source %s: %s has a { or a } that is not doubled; write {{ or }} for the brace itself (a variable is not defined yet)", source, field)
	default:
		return true
	}
	return false
}

// checkRange records an error of the code ec when a number that is not zero (zero is "not given") is outside lo..hi. params
// are the places the code names (the source, the dataset); the field, the value and the range are added. It tells whether
// the number is fine.
func (c *collector) checkRange(ec *errCode, params Params, field, pointer string, v float64, r numBounds) bool {
	lo, hi := r.lo, r.hi
	if v == 0 || (v >= lo && v <= hi) {
		return true
	}
	params["field"], params["value"], params["min"], params["max"] = field, v, lo, hi
	c.add(ec, pointer, params, "%s: %s is %s, and it has to be from %s to %s", where(params), field, num(v), num(lo), num(hi))
	return false
}

// numBounds is the range a number may have, both ends in.
type numBounds struct{ lo, hi float64 }

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// where is the start of a sentence about a place: the source, and the dataset when there is one.
func where(params Params) string {
	if d, ok := params["dataset"]; ok {
		return fmt.Sprintf("source %v, dataset %v", params["source"], d)
	}
	return fmt.Sprintf("source %v", params["source"])
}

// checkPath records the error of a path into a record or a response that the grammar does not read.
func (c *collector) checkPath(source, dataset, pointer, field, text string) bool {
	_, err := ParsePath(text)
	var pe *PathError
	if !errors.As(err, &pe) {
		return true
	}
	params := Params{"source": source, "field": field, "path": clip(text, idClipAt)}
	if dataset != "" {
		params["dataset"] = dataset
	}
	if pe.Unsupported {
		params["notation"] = pe.Notation
		c.add(ecAdPathUnsup, pointer, params, "source %s: %s %q %s", source, field, clip(text, idClipAt), pe.Reason)
		return false
	}
	params["reason"] = pe.Reason
	c.add(ecAdPathBad, pointer, params, "source %s: %s %q %s", source, field, clip(text, idClipAt), pe.Reason)
	return false
}

// hasControl tells whether a text has a control character or a space.
func hasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0
}

// isToken tells whether a text is a name of the kind an HTTP header or a query parameter has: letters, digits and a few
// marks, no space.
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}

// A header the product sets itself; the definition may not (sources-and-adapters.md 8).
var forbiddenHeaders = []string{"host", "cookie", "accept-encoding", "content-length", "transfer-encoding", "connection"}

// headerProblem says why a header may not be named in a definition, or "".
func headerProblem(name string, allowAuthorization bool) string {
	lower := strings.ToLower(name)
	switch {
	case !isToken(name):
		return "is not a header name"
	case containsString(forbiddenHeaders, lower), strings.HasPrefix(lower, "proxy-"):
		return "is a header the product sets itself or that moves the request, and the definition may not write it"
	case lower == "authorization" && !allowAuthorization:
		return "is the header of the authentication; write the authentication in auth"
	}
	return ""
}

// valueProblem says why the value of a header or of a prefix is no value, or "".
func valueProblem(v string) string {
	if strings.ContainsFunc(v, unicode.IsControl) {
		return "has a control character"
	}
	if len(v) > 4096 {
		return "is longer than 4096 bytes"
	}
	return ""
}
