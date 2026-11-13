package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// SchemaKeywords is the subset of JSON Schema the contract may use and the
// validator understands. The structure check rejects any other keyword in the
// contract, and NewValidator rejects it too, so the two cannot drift apart
// without a test failing. Keys starting with x- are extensions and are ignored.
func SchemaKeywords() []string {
	return []string{
		"$ref", "type", "properties", "required", "additionalProperties", "items", "enum", "const",
		"pattern", "minLength", "maxLength", "minimum", "maximum", "minItems", "maxItems",
		"format", "description", "title", "example", "default", "deprecated",
	}
}

// Validator checks JSON values against the schemas of a contract.
type Validator struct {
	doc *Document
	// Strict treats an object schema that lists properties and says nothing
	// about additionalProperties as closed, so a response that carries a field
	// the contract does not name fails. Clients must still ignore unknown
	// fields (rest-api.md 5-4); the contract tests are stricter than clients.
	Strict bool
	known  map[string]bool
}

// NewValidator builds a validator and checks every schema of the document for
// keywords outside the subset and for references that do not resolve.
func NewValidator(doc *Document, strict bool) (*Validator, error) {
	v := &Validator{doc: doc, Strict: strict, known: map[string]bool{}}
	for _, k := range SchemaKeywords() {
		v.known[k] = true
	}
	for _, name := range doc.Schemas() {
		s, _ := doc.Schema(name)
		if err := v.compile(s, "#/components/schemas/"+name, 0); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func (v *Validator) compile(schema any, at string, depth int) error {
	obj, ok := schema.(map[string]any)
	if !ok {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s is not a schema object", at)
	}
	if depth > 40 {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s is nested too deeply", at)
	}
	for key, value := range obj {
		if strings.HasPrefix(key, "x-") {
			continue
		}
		if !v.known[key] {
			return kernel.Errorf(kernel.CodeInvalidValue, "%s uses the keyword %q, which is outside the contract's schema subset", at, key)
		}
		if err := v.compileChild(key, value, at, depth); err != nil {
			return err
		}
	}
	return nil
}

func (v *Validator) compileChild(key string, value any, at string, depth int) error {
	switch key {
	case "$ref":
		ref, _ := value.(string)
		if _, err := v.doc.Resolve(ref); err != nil {
			return kernel.Errorf(kernel.CodeInvalidValue, "%s: %v", at, err)
		}
	case "properties":
		props, _ := value.(map[string]any)
		for name, p := range props {
			if err := v.compile(p, at+"/properties/"+name, depth+1); err != nil {
				return err
			}
		}
	case "items":
		return v.compile(value, at+"/items", depth+1)
	case "additionalProperties":
		if _, isSchema := value.(map[string]any); isSchema {
			return v.compile(value, at+"/additionalProperties", depth+1)
		}
	}
	return nil
}

// Validate checks a JSON value against a schema (an object of the document, or
// a reference). It returns every problem found, each with the JSON pointer of
// the value, and nil when the value fits.
func (v *Validator) Validate(schema any, value any) []string {
	var errs []string
	v.check(schema, value, "", &errs, 0)
	return errs
}

// ValidateJSON decodes JSON text and validates it.
func (v *Validator) ValidateJSON(schema any, data []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return []string{"the body is not JSON: " + err.Error()}
	}
	if _, err := dec.Token(); err != io.EOF {
		return []string{"the body has text after the JSON value"}
	}
	return v.Validate(schema, value)
}

func at(ptr string) string {
	if ptr == "" {
		return "/"
	}
	return ptr
}

func (v *Validator) check(schema any, value any, ptr string, errs *[]string, depth int) {
	obj, ok := schema.(map[string]any)
	if !ok || depth > 60 {
		*errs = append(*errs, at(ptr)+": the schema is not usable")
		return
	}
	if ref, ok := obj["$ref"].(string); ok {
		target, err := v.doc.Resolve(ref)
		if err != nil {
			*errs = append(*errs, at(ptr)+": "+err.Error())
			return
		}
		v.check(target, value, ptr, errs, depth+1)
		return
	}
	if !v.checkType(obj, value, ptr, errs) {
		return
	}
	v.checkEnum(obj, value, ptr, errs)
	switch x := value.(type) {
	case map[string]any:
		v.checkObject(obj, x, ptr, errs, depth)
	case []any:
		v.checkArray(obj, x, ptr, errs, depth)
	case string:
		checkString(obj, x, ptr, errs)
	case json.Number:
		checkNumber(obj, x, ptr, errs)
	}
}

func typeOf(value any) []string {
	switch x := value.(type) {
	case nil:
		return []string{"null"}
	case bool:
		return []string{"boolean"}
	case string:
		return []string{"string"}
	case map[string]any:
		return []string{"object"}
	case []any:
		return []string{"array"}
	case json.Number:
		if f, err := strconv.ParseFloat(string(x), 64); err == nil && f == math.Trunc(f) && !math.IsInf(f, 0) {
			return []string{"integer", "number"}
		}
		return []string{"number"}
	}
	return nil
}

func (v *Validator) checkType(obj map[string]any, value any, ptr string, errs *[]string) bool {
	var want []string
	switch t := obj["type"].(type) {
	case string:
		want = []string{t}
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok {
				want = append(want, s)
			}
		}
	default:
		return true
	}
	have := typeOf(value)
	for _, w := range want {
		for _, h := range have {
			if w == h {
				return true
			}
		}
	}
	*errs = append(*errs, fmt.Sprintf("%s: want %s, have %s", at(ptr), strings.Join(want, " or "), have[0]))
	return false
}

func (v *Validator) checkEnum(obj map[string]any, value any, ptr string, errs *[]string) {
	if list, ok := obj["enum"].([]any); ok {
		found := false
		for _, item := range list {
			if equalJSON(item, value) {
				found = true
				break
			}
		}
		if !found {
			*errs = append(*errs, fmt.Sprintf("%s: %s is not one of the allowed values", at(ptr), show(value)))
		}
	}
	if c, ok := obj["const"]; ok && !equalJSON(c, value) {
		*errs = append(*errs, fmt.Sprintf("%s: want the constant %s, have %s", at(ptr), show(c), show(value)))
	}
}

func show(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	return string(data)
}

func equalJSON(a, b any) bool { return show(a) == show(b) }

func (v *Validator) checkObject(obj map[string]any, value map[string]any, ptr string, errs *[]string, depth int) {
	props, _ := obj["properties"].(map[string]any)
	if required, ok := obj["required"].([]any); ok {
		for _, r := range required {
			if name, _ := r.(string); name != "" {
				if _, present := value[name]; !present {
					*errs = append(*errs, fmt.Sprintf("%s: the property %q is required", at(ptr), name))
				}
			}
		}
	}
	names := make([]string, 0, len(value))
	for name := range value {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := ptr + "/" + name
		if sub, ok := props[name]; ok {
			v.check(sub, value[name], child, errs, depth+1)
			continue
		}
		v.checkExtra(obj, props != nil, value[name], child, errs, depth)
	}
}

func (v *Validator) checkExtra(obj map[string]any, hasProps bool, value any, ptr string, errs *[]string, depth int) {
	switch extra := obj["additionalProperties"].(type) {
	case bool:
		if !extra {
			*errs = append(*errs, at(ptr)+": the property is not in the contract")
		}
	case map[string]any:
		v.check(extra, value, ptr, errs, depth+1)
	case nil:
		if v.Strict && hasProps {
			*errs = append(*errs, at(ptr)+": the property is not in the contract")
		}
	}
}

func (v *Validator) checkArray(obj map[string]any, value []any, ptr string, errs *[]string, depth int) {
	if n, ok := intKeyword(obj, "minItems"); ok && len(value) < n {
		*errs = append(*errs, fmt.Sprintf("%s: has %d items, want at least %d", at(ptr), len(value), n))
	}
	if n, ok := intKeyword(obj, "maxItems"); ok && len(value) > n {
		*errs = append(*errs, fmt.Sprintf("%s: has %d items, want at most %d", at(ptr), len(value), n))
	}
	if items, ok := obj["items"]; ok {
		for i, item := range value {
			v.check(items, item, ptr+"/"+strconv.Itoa(i), errs, depth+1)
		}
	}
}

func intKeyword(obj map[string]any, key string) (int, bool) {
	n, ok := obj[key].(json.Number)
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(string(n))
	return i, err == nil
}

func checkString(obj map[string]any, value string, ptr string, errs *[]string) {
	length := utf8.RuneCountInString(value)
	if n, ok := intKeyword(obj, "minLength"); ok && length < n {
		*errs = append(*errs, fmt.Sprintf("%s: is %d characters, want at least %d", at(ptr), length, n))
	}
	if n, ok := intKeyword(obj, "maxLength"); ok && length > n {
		*errs = append(*errs, fmt.Sprintf("%s: is %d characters, want at most %d", at(ptr), length, n))
	}
	if pattern, ok := obj["pattern"].(string); ok {
		re, err := regexp.Compile(pattern)
		if err != nil {
			*errs = append(*errs, fmt.Sprintf("%s: the pattern %q is not a valid expression", at(ptr), pattern))
		} else if !re.MatchString(value) {
			*errs = append(*errs, fmt.Sprintf("%s: %q does not match %s", at(ptr), value, pattern))
		}
	}
	if obj["format"] == "date-time" {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			*errs = append(*errs, fmt.Sprintf("%s: %q is not a date-time (RFC 3339)", at(ptr), value))
		}
	}
}

func checkNumber(obj map[string]any, value json.Number, ptr string, errs *[]string) {
	f, err := strconv.ParseFloat(string(value), 64)
	if err != nil {
		return
	}
	if min, ok := obj["minimum"].(json.Number); ok {
		if m, err := strconv.ParseFloat(string(min), 64); err == nil && f < m {
			*errs = append(*errs, fmt.Sprintf("%s: %s is below the minimum %s", at(ptr), value, min))
		}
	}
	if max, ok := obj["maximum"].(json.Number); ok {
		if m, err := strconv.ParseFloat(string(max), 64); err == nil && f > m {
			*errs = append(*errs, fmt.Sprintf("%s: %s is above the maximum %s", at(ptr), value, max))
		}
	}
}
