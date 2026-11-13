package model

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// standardFiles holds the standard model: JSON files in the standard folder,
// the source of truth of the model that ships with the product (ADR-0020).
//
//	standard/meta.json          model_version and the reserved numbers
//	standard/value_types.json   the named value types
//	standard/traits.json        the traits
//	standard/objects/<layer>.json   {"layer": ..., "types": [...]}: the object types of one layer
//	standard/links.json         the link types with their bindings
//	standard/identifier_schemes.json  the identifier schemes
//	standard/actions.json       the action types
//
// References between definitions are by id. In the standard files an object
// type may leave out its namespace (std), its layer (the layer of its file)
// and its status (active), and a link type or an action type its namespace
// (std) and status (active).
//
//go:embed standard
var standardFiles embed.FS

// StandardDefinitions reads the standard model from the embedded files. It
// does not check the rules: LoadStandard does.
func StandardDefinitions() (Definitions, error) {
	sub, err := fs.Sub(standardFiles, "standard")
	if err != nil {
		return Definitions{}, kernel.Wrap(kernel.CodeInternal, err, "the embedded standard model is missing")
	}
	return ReadDefinitions(sub)
}

// LoadStandard reads, checks and indexes the standard model. Every call reads
// the files again and gives an equal registry.
func LoadStandard() (*Registry, error) {
	defs, err := StandardDefinitions()
	if err != nil {
		return nil, err
	}
	if err := requireStandard(defs); err != nil {
		return nil, err
	}
	return NewRegistry(defs)
}

// requireStandard rejects a definition outside std in the standard files:
// org and ext definitions come from the definitions of a deployment and sys
// ones from the product's own parts, not from the standard model (ADR-0007 16).
func requireStandard(defs Definitions) error {
	for _, t := range defs.ObjectTypes {
		if t.Namespace != NamespaceStd {
			return kernel.Errorf(CodeNotStandard, "object type %s is in namespace %s, not std", t.ID, t.Namespace)
		}
	}
	for _, l := range defs.LinkTypes {
		if l.Namespace != NamespaceStd {
			return kernel.Errorf(CodeNotStandard, "link type %s is in namespace %s, not std", l.ID, l.Namespace)
		}
	}
	for _, s := range defs.Schemes {
		if s.Namespace != NamespaceStd {
			return kernel.Errorf(CodeNotStandard, "identifier scheme %s is in namespace %s, not std", s.ID, s.Namespace)
		}
	}
	for _, a := range defs.ActionTypes {
		if a.Namespace != NamespaceStd {
			return kernel.Errorf(CodeNotStandard, "action type %s is in namespace %s, not std", a.ID, a.Namespace)
		}
	}
	return nil
}

// objectsFile is the shape of one file under objects/.
type objectsFile struct {
	Layer Layer        `json:"layer"`
	Types []ObjectType `json:"types"`
}

type metaFile struct {
	ModelVersion int      `json:"model_version"`
	Reserved     Reserved `json:"reserved"`
}

// ReadDefinitions reads a standard-model-shaped folder of files. Unknown
// fields and trailing text are errors, so a misspelt field is not ignored.
func ReadDefinitions(fsys fs.FS) (Definitions, error) {
	var defs Definitions
	var meta metaFile
	if err := readJSON(fsys, "meta.json", &meta); err != nil {
		return defs, err
	}
	defs.ModelVersion, defs.Reserved = meta.ModelVersion, meta.Reserved
	files := []struct {
		name   string
		target any
	}{
		{"value_types.json", &defs.ValueTypes},
		{"identifier_schemes.json", &defs.Schemes},
		{"traits.json", &defs.Traits},
		{"links.json", &defs.LinkTypes},
		{"actions.json", &defs.ActionTypes},
	}
	for _, file := range files {
		if err := readJSON(fsys, file.name, file.target); err != nil {
			return defs, err
		}
	}
	var err error
	if defs.ObjectTypes, err = readObjectFiles(fsys); err != nil {
		return defs, err
	}
	applyDefaults(&defs)
	return defs, nil
}

func readJSON(fsys fs.FS, name string, target any) error {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return kernel.Wrap(CodeDataInvalid, err, "cannot read "+name)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return kernel.Wrap(CodeDataInvalid, err, name+" is not valid")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return kernel.Errorf(CodeDataInvalid, "%s has text after the JSON value", name)
	}
	return nil
}

// readObjectFiles reads objects/<layer>.json for every file there, in the
// order of the file names. The file name must be the layer it holds.
func readObjectFiles(fsys fs.FS) ([]ObjectType, error) {
	entries, err := fs.ReadDir(fsys, "objects")
	if err != nil {
		return nil, kernel.Wrap(CodeDataInvalid, err, "cannot read the objects folder")
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	var out []ObjectType
	for _, name := range names {
		var file objectsFile
		full := path.Join("objects", name)
		if err := readJSON(fsys, full, &file); err != nil {
			return nil, err
		}
		if string(file.Layer)+".json" != name {
			return nil, kernel.Errorf(CodeDataInvalid, "%s holds the layer %q: the file name must be the layer", full, file.Layer)
		}
		for i := range file.Types {
			if file.Types[i].Layer == "" {
				file.Types[i].Layer = file.Layer
			}
		}
		out = append(out, file.Types...)
	}
	return out, nil
}

// applyDefaults fills what the standard files leave out: namespace std and status active.
func applyDefaults(defs *Definitions) {
	for i := range defs.ObjectTypes {
		t := &defs.ObjectTypes[i]
		t.Namespace, t.Status = orDefault(t.Namespace, NamespaceStd), orDefault(t.Status, StatusActive)
	}
	for i := range defs.LinkTypes {
		l := &defs.LinkTypes[i]
		l.Namespace, l.Status = orDefault(l.Namespace, NamespaceStd), orDefault(l.Status, StatusActive)
	}
	for i := range defs.Schemes {
		s := &defs.Schemes[i]
		s.Namespace = orDefault(s.Namespace, NamespaceStd)
	}
	for i := range defs.ActionTypes {
		a := &defs.ActionTypes[i]
		a.Namespace, a.Status = orDefault(a.Namespace, NamespaceStd), orDefault(a.Status, StatusActive)
	}
}

func orDefault[T ~string](value, fallback T) T {
	if strings.TrimSpace(string(value)) == "" {
		return fallback
	}
	return value
}
