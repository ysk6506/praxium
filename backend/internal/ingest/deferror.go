package ingest

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// The two documents a definition error can be in.
const (
	DocumentSource  = "source"
	DocumentMapping = "mapping"
)

// MaxDefinitionErrors is the most errors one check lists. A definition with more has the first of them and the count
// of those left out (ADR-0070).
const MaxDefinitionErrors = 100

// Params are the values a sentence of an error is made of (the value given, the candidates, the kind expected). They
// hold only what JSON can carry.
type Params map[string]any

// DefinitionError is one thing wrong in a source definition or a mapping definition (ADR-0070): where it is (a
// JSON pointer, RFC 6901, into that document; a list is told by its place, from 0), a stable code, the values the
// sentence is made of and the sentence itself.
type DefinitionError struct {
	Document string `json:"document"`
	Pointer  string `json:"pointer"`
	Code     string `json:"code"`
	Params   Params `json:"params,omitempty"`
	Message  string `json:"message"`

	kernel kernel.Code
}

// KernelCode is the kernel code the error has always had: invalid_value, or unsupported for what this build lacks.
func (e DefinitionError) KernelCode() kernel.Code { return e.kernel }

// DefinitionErrors is the error the checks of a definition return: the errors in the order of the document, and
// how many more there were beyond MaxDefinitionErrors. It satisfies error. Its kernel code is that of the first
// error, so the code a caller reads is what it was when the first error was all there was.
type DefinitionErrors struct {
	List    []DefinitionError `json:"errors"`
	Omitted int               `json:"omitted,omitempty"`
}

// Error is "code: sentence" for one error, as it has always been, and one such line for each when there are more.
func (e *DefinitionErrors) Error() string {
	lines := make([]string, 0, len(e.List)+1)
	for _, d := range e.List {
		lines = append(lines, string(d.kernel)+": "+d.Message)
	}
	if e.Omitted > 0 {
		lines = append(lines, fmt.Sprintf("%d more problems are left out (a check lists at most %d)", e.Omitted, MaxDefinitionErrors))
	}
	return strings.Join(lines, "\n")
}

// Unwrap gives the first error as a kernel error, so that kernel.CodeOf and errors.As work as they did.
func (e *DefinitionErrors) Unwrap() error {
	if len(e.List) == 0 {
		return nil
	}
	return kernel.Errorf(e.List[0].kernel, "%s", e.List[0].Message)
}

// DefinitionErrorsOf takes the list out of an error a check returned (wrapped or not).
func DefinitionErrorsOf(err error) (*DefinitionErrors, bool) {
	var de *DefinitionErrors
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

// collector gathers the errors of one document, in the order they are found.
type collector struct {
	doc     string
	list    []DefinitionError
	omitted int
}

func newCollector(doc string) *collector { return &collector{doc: doc} }

// add records one error. The sentence is the one the check always had.
func (c *collector) add(ec *errCode, pointer string, params Params, format string, args ...any) {
	if len(c.list) >= MaxDefinitionErrors {
		c.omitted++
		return
	}
	c.list = append(c.list, DefinitionError{Document: c.doc, Pointer: pointer, Code: ec.code, Params: params, Message: fmt.Sprintf(format, args...), kernel: ec.kernel})
}

func (c *collector) any() bool { return len(c.list) > 0 }

// err is nil when nothing was found.
func (c *collector) err() error {
	if !c.any() {
		return nil
	}
	return &DefinitionErrors{List: c.list, Omitted: c.omitted}
}

// ptr builds a JSON pointer from its steps: a name, or a place in a list.
func ptr(steps ...any) string {
	var b strings.Builder
	for _, s := range steps {
		b.WriteByte('/')
		switch v := s.(type) {
		case int:
			b.WriteString(strconv.Itoa(v))
		default:
			t := strings.ReplaceAll(fmt.Sprint(v), "~", "~0")
			b.WriteString(strings.ReplaceAll(t, "/", "~1"))
		}
	}
	return b.String()
}

// pathPointer turns the dotted path the decoder reports (datasets.0.name) into a JSON pointer.
func pathPointer(dotted string) string {
	if dotted == "" {
		return ""
	}
	steps := strings.Split(dotted, ".")
	out := make([]any, len(steps))
	for i, s := range steps {
		out[i] = s
	}
	return ptr(out...)
}

// JoinDefinitionErrors puts the errors of several documents in one, in the order given, within the limit. An error
// that is no DefinitionErrors is kept as the one error of its kind (first in the list). It is nil when every one is nil.
func JoinDefinitionErrors(errs ...error) error {
	out := &DefinitionErrors{}
	for _, err := range errs {
		if err == nil {
			continue
		}
		de, ok := DefinitionErrorsOf(err)
		if !ok {
			return err
		}
		for _, e := range de.List {
			if len(out.List) >= MaxDefinitionErrors {
				out.Omitted++
				continue
			}
			out.List = append(out.List, e)
		}
		out.Omitted += de.Omitted
	}
	if len(out.List) == 0 {
		return nil
	}
	return out
}
