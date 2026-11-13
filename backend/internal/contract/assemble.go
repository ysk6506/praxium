package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// OpenAPIVersion is the OpenAPI version of the document (rest-api.md 5-1).
const OpenAPIVersion = "3.1.0"

// Assemble merges the JSON files of a folder into one OpenAPI document and
// writes it in a fixed form: object keys sorted, two-space indent, no escaping
// of <, > and &, one newline at the end. The same files always give the same
// bytes.
//
// Every file is a JSON object holding any of the top-level fields of an
// OpenAPI document. Objects merge by key, in the order of the file names; a
// key that two files both define is an error (a path, a schema or a parameter
// has one owner), and so is an array or a value that two files both give. The
// result must declare openapi 3.1.0.
func Assemble(parts fs.FS, dir string) ([]byte, error) {
	entries, err := fs.ReadDir(parts, dir)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the contract parts")
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the folder %s has no contract part", dir)
	}
	merged := map[string]any{}
	for _, name := range names {
		part, err := readPart(parts, dir+"/"+name)
		if err != nil {
			return nil, err
		}
		if err := mergeInto(merged, part, "", name); err != nil {
			return nil, err
		}
	}
	if merged["openapi"] != OpenAPIVersion {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the contract must declare openapi %s", OpenAPIVersion)
	}
	return encodeCanonical(merged)
}

func readPart(parts fs.FS, name string) (map[string]any, error) {
	data, err := fs.ReadFile(parts, name)
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read "+name)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, name+" is not a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s has text after the JSON value", name)
	}
	return out, nil
}

// mergeInto adds src to dst. Objects merge; any other value present in both is a conflict.
func mergeInto(dst, src map[string]any, at, file string) error {
	for key, value := range src {
		here := at + "/" + key
		existing, found := dst[key]
		if !found {
			dst[key] = value
			continue
		}
		have, haveMap := existing.(map[string]any)
		add, addMap := value.(map[string]any)
		if !haveMap || !addMap {
			return kernel.Errorf(kernel.CodeConflict, "%s defines %s, which another part already defines", file, here)
		}
		if err := mergeInto(have, add, here, file); err != nil {
			return err
		}
	}
	return nil
}

func encodeCanonical(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, kernel.Wrap(kernel.CodeInternal, err, "cannot write the contract")
	}
	return buf.Bytes(), nil
}

// Describe returns a one-line summary of an assembled document, for logs and commands.
func Describe(doc []byte) string {
	d, err := ParseDocument(doc)
	if err != nil {
		return "unreadable contract"
	}
	return fmt.Sprintf("%s %s: %d operations", d.Title, d.Version, len(d.Operations()))
}
