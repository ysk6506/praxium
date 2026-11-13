package contract

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// ValidateResponse checks one real response against the contract: the status
// must be one the operation declares, the media type and the JSON body must
// fit the declared schema, and every header declared required must be there.
// path is the template as written in the contract (/v1/object-types/{type_id}),
// headers has lower-case names. The problems are returned as one error.
func (v *Validator) ValidateResponse(method, path string, status int, contentType string, headers map[string]string, body []byte) error {
	op, ok := v.doc.operation(method, path)
	if !ok {
		return kernel.Errorf(kernel.CodeNotFound, "the contract has no operation %s %s", method, path)
	}
	responses, _ := op["responses"].(map[string]any)
	raw, ok := responses[strconv.Itoa(status)]
	if !ok {
		raw, ok = responses["default"]
	}
	if !ok {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s %s does not declare the status %d", method, path, status)
	}
	response, err := v.deref(raw)
	if err != nil {
		return err
	}
	var problems []string
	problems = append(problems, v.checkHeaders(response, headers)...)
	problems = append(problems, v.checkBody(response, contentType, body)...)
	if len(problems) > 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s %s %d: %s", method, path, status, strings.Join(problems, "; "))
	}
	return nil
}

func (v *Validator) deref(raw any) (map[string]any, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a response is not an object")
	}
	if ref, ok := obj["$ref"].(string); ok {
		target, err := v.doc.Resolve(ref)
		if err != nil {
			return nil, err
		}
		return v.deref(target)
	}
	return obj, nil
}

func (v *Validator) checkHeaders(response map[string]any, headers map[string]string) []string {
	declared, _ := response["headers"].(map[string]any)
	var problems []string
	for name, raw := range declared {
		h, err := v.deref(raw)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if required, _ := h["required"].(bool); required {
			if _, present := headers[strings.ToLower(name)]; !present {
				problems = append(problems, fmt.Sprintf("the header %s is required", name))
			}
		}
	}
	return problems
}

func (v *Validator) checkBody(response map[string]any, contentType string, body []byte) []string {
	content, _ := response["content"].(map[string]any)
	if len(content) == 0 {
		if len(body) > 0 {
			return []string{"the response declares no body but has one"}
		}
		return nil
	}
	media := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	entry, ok := content[media].(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("the media type %q is not declared", media)}
	}
	schema, ok := entry["schema"]
	if !ok {
		return nil
	}
	return v.ValidateJSON(schema, body)
}
