package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// [REQ-474]
func TestSecretAssemblyGivesTheStoreTheRandomnessOfTheSystem(t *testing.T) {
	e := newSecEnv(t)
	for _, access := range []secretAccess{secretCreate, secretChange, secretDelete} {
		st, err := openSecretStore(e.w, secretLocation{dataDir: e.dataDir}, access)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := st.bytes.(kernel.SystemBytes); !ok {
			t.Errorf("the store of access %d has the randomness %T, not the system's", access, st.bytes)
		}
		_ = st.Close()
	}
	if _, ok := productBytes().(kernel.SystemBytes); !ok {
		t.Errorf("productBytes is %T", productBytes())
	}
	// two keys made one after the other are not the same: the bytes are not a fixed sequence
	a, _, _ := createMasterKey(filepath.Join(t.TempDir(), "a.key"), productBytes())
	b, _, _ := createMasterKey(filepath.Join(t.TempDir(), "b.key"), productBytes())
	if a.ID() == b.ID() {
		t.Errorf("two new keys have the same id")
	}
}

// [REQ-474]
func TestSecretAssemblyNoProductCodeOfTheRootUsesTheDeterministicRandomness(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no sources: %v", err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && (id.Name == "NewSeededBytes" || id.Name == "SeededBytes") {
				t.Errorf("%s: %s is for tests of the units and must not be in the program", fset.Position(id.Pos()), id.Name)
			}
			return true
		})
	}
	if checked < 10 {
		t.Errorf("only %d sources were read", checked)
	}
}

// [REQ-474]
func TestSecretAssemblyAReadOnlyStoreHasNoKeyAndHoldsNoLock(t *testing.T) {
	e := newSecEnv(t)
	e.secPut("db_main")
	st, err := openSecretStore(e.w, secretLocation{dataDir: e.dataDir}, secretRead)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if st.unlock != nil || st.keys != nil {
		t.Errorf("a read holds a lock or a key provider")
	}
	if _, err := st.svc.Writer().AddVersion(context.Background(), "db_main", secrets.NewFields(map[string]string{"username": "u", "password": "p"})); kernel.CodeOf(err) != secrets.CodeMasterKeyMissing {
		t.Errorf("a store opened to read can change a secret: %v", err)
	}
	if _, err := st.svc.Describer().List(context.Background()); err != nil {
		t.Errorf("a store opened to read cannot list: %v", err)
	}
	// the data folder is not made by a read
	if _, err := openSecretStore(e.w, secretLocation{dataDir: filepath.Join(e.dataDir, "no")}, secretRead); err == nil {
		t.Errorf("a read of a folder that is not there is not refused")
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "no")); err == nil {
		t.Errorf("a read made the folder")
	}
}
