package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/filejournal"
	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// `praxium ingest import` (ADR-0132): a source definition and its mapping definition go into the definition store of
// the data folder, one document to one item (REQ-438), after the whole set they would make is compiled.

const ingestImportUsage = "usage: praxium ingest import -data-dir <folder> -source <source.json> -mapping <mapping.json> [-message <text>]"

const ingestImportBody = `Stores a source definition and its mapping definition in the definition store of the data folder (the store of
"praxium model apply"): the source as an item of the kind source named by its id, the mapping as an item of the kind
mapping named by its id. A mapping without an id is given the id of the source, so importing the same files again
changes the same two items. The source may be of any adapter (file, http, postgres).

Before anything is stored, every source and mapping of the store, with the two files in place of the items they
replace, is compiled together. A mapping is compiled with the source of its id or, else, with the one source that
defines every dataset it reads; the ids of feeds, refinements and link definitions are unique among the mappings.
When anything is wrong, every mistake is listed (document, id, place in the document, what is wrong) and nothing is
stored. A value of a secret written in a definition (password=..., token=..., api_key=..., secret=..., a field named
password, token, api_key, secret or authorization with a text, user:password@ in an address, a private key) is refused
without repeating it: refer to a secret ({"secret_id": "..."}) and put the value in with praxium secret put. A secret
that is referred to but not put in yet does not stop the import.

Importing what is stored already writes nothing and says so. Organization properties (praxium model apply) and the
definitions of ingestion share the store and do not touch each other.

ingest import only stores the definitions: praxium ingest run still reads its -source and -mapping files. The folder
must not be open in another praxium (a server, local or an ingest run): stop it, import, and start it again.
Exit codes: 0 stored, or nothing to store; 1 refused or failed; 2 the command line is wrong.

example:
  praxium ingest import -data-dir data -source example/csv/source.json -mapping example/csv/mapping.json -message "first sources"
`

// importFlags is what the command line of an import says.
type importFlags struct{ dataDir, source, mapping, message string }

func newIngestImportFlags(f *importFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("ingest import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&f.dataDir, "data-dir", "", "the data folder whose definition store gets the two documents")
	fs.StringVar(&f.source, "source", "", "the source definition (JSON)")
	fs.StringVar(&f.mapping, "mapping", "", "the mapping definition (JSON) that reads the datasets of that source")
	fs.StringVar(&f.message, "message", "", "what the change is, kept in the history of the definitions")
	fs.StringVar(&f.message, "m", "", "the same as -message")
	return fs
}

func importHelpEntry(out map[string]helpEntry) {
	out["ingest import"] = helpEntry{usage: ingestImportUsage, body: ingestImportBody, flags: func() *flag.FlagSet { return newIngestImportFlags(&importFlags{}) }}
}

func parseIngestImport(args []string) (importFlags, error) {
	var f importFlags
	fs := newIngestImportFlags(&f)
	if err := fs.Parse(args); err != nil {
		return f, flagProblem(err)
	}
	if fs.NArg() > 0 {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "unexpected argument %q", fs.Arg(0))
	}
	if f.dataDir == "" || f.source == "" || f.mapping == "" {
		return f, kernel.Errorf(kernel.CodeInvalidValue, "ingest import needs -data-dir, -source and -mapping")
	}
	return f, nil
}

// runIngestImport is `praxium ingest import`.
func runIngestImport(args []string, stdout, stderr io.Writer, w wiring) int {
	f, err := parseIngestImport(args)
	if err != nil {
		return usageFail(stderr, err, "ingest import")
	}
	if err := ingestImport(f, stdout, w); err != nil {
		printImportFailure(stderr, err, f)
		return exitFailure
	}
	return exitOK
}

// importDoc is one document of the files: its kind, its id and its body in the normal form of the store.
type importDoc struct {
	kind, id string
	body     json.RawMessage
}

// ingestImport checks the two files, then the set they make with what is stored, and stores them in one write.
func ingestImport(f importFlags, out io.Writer, w wiring) error {
	src, mp, docs, err := readImportFiles(f, w)
	if err != nil {
		return err
	}
	disk, err := openDefinitionsDisk(f.dataDir, false)
	if err != nil {
		return err
	}
	unlock, err := disk.Lock()
	if err != nil {
		return importLocked(f.dataDir, err)
	}
	defer func() { _ = unlock() }()
	st, err := openModelState(f.dataDir, false, w.deps)
	if err != nil {
		return err
	}
	reg, errs := st.registry()
	if len(errs) > 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "the organization properties stored in %s do not fit the model (%s); mend them with praxium model apply first",
			f.dataDir, errText(joinErrors(errs)))
	}
	stored, err := readStoredIngestion(context.Background(), st)
	if err != nil {
		return err
	}
	if _, err := ingest.CompileSet(reg, stored.with(src, mp)); err != nil {
		return err
	}
	return writeImport(f, out, st, stored, docs, w)
}

// readImportFiles reads the two files: first what is certainly a secret's value (so that no other sentence can repeat
// one), then the documents. A mapping without an id is given the id of the source.
func readImportFiles(f importFlags, w wiring) (ingest.Source, ingest.Mapping, []importDoc, error) {
	srcText, err := readImportFile(w, f.source)
	if err != nil {
		return ingest.Source{}, ingest.Mapping{}, nil, err
	}
	mapText, err := readImportFile(w, f.mapping)
	if err != nil {
		return ingest.Source{}, ingest.Mapping{}, nil, err
	}
	if err := ingest.JoinDefinitionErrors(ingest.SecretValues(ingest.DocumentSource, srcText), ingest.SecretValues(ingest.DocumentMapping, mapText)); err != nil {
		return ingest.Source{}, ingest.Mapping{}, nil, err
	}
	src, srcErr := ingest.ParseAdapterSource(bytes.NewReader(srcText))
	mp, mapErr := ingest.ParseMapping(bytes.NewReader(mapText))
	if err := ingest.JoinDefinitionErrors(srcErr, mapErr); err != nil {
		return ingest.Source{}, ingest.Mapping{}, nil, err
	}
	if mp.ID == "" {
		mp.ID, mapText = src.ID, withID(mapText, src.ID)
	}
	docs := []importDoc{{ingest.ItemKindSource, src.ID, srcText}, {ingest.ItemKindMapping, mp.ID, mapText}}
	for i, d := range docs {
		// the normal form of the store refuses what the parse lets by: a key twice, too deep a nesting
		if docs[i].body, err = definitions.NormalizeBody(d.body); err != nil {
			return ingest.Source{}, ingest.Mapping{}, nil, kernel.Wrap(kernel.CodeInvalidValue, err, "the "+d.kind+" definition cannot be stored")
		}
	}
	return src, mp, docs, nil
}

// readImportFile reads a definition file whole, at most as large as an item of the store may be.
func readImportFile(w wiring, path string) ([]byte, error) {
	in, err := readDefinition(w, path)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	text, err := io.ReadAll(io.LimitReader(in, definitions.DefaultMaxBodyBytes+1))
	if err != nil {
		return nil, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read "+path)
	}
	if len(text) > definitions.DefaultMaxBodyBytes {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "%s is larger than %d bytes, the most a definition may be", path, definitions.DefaultMaxBodyBytes)
	}
	return text, nil
}

// withID puts the id in a mapping document that has none. The document was read as a mapping, so it is a JSON object;
// a text that is not one is given back as it is (its item is still named by the id).
func withID(text []byte, id string) []byte {
	var fields map[string]json.RawMessage
	if json.Unmarshal(text, &fields) != nil || fields == nil {
		return text
	}
	fields["id"], _ = json.Marshal(id)
	out, _ := json.Marshal(fields)
	return out
}

// importLocked says that a data folder another process holds is not changed, and what to do.
func importLocked(dir string, err error) error {
	if kernel.CodeOf(err) != filejournal.CodeLocked {
		return err
	}
	return kernel.Errorf(filejournal.CodeLocked, "the data folder %s is in use by another praxium (a server such as praxium local or api serve, or an ingest run). "+
		"Stop it, run praxium ingest import again, and start it again", dir)
}

// writeImport stores the documents that are new or changed in one write, or says that nothing changed.
func writeImport(f importFlags, out io.Writer, st *modelState, stored *storedIngestion, docs []importDoc, w wiring) error {
	in := definitions.ApplyInput{ExpectHead: st.head.No, Origin: definitions.OriginImport, Actor: definitions.Actor{ID: applyActor(w)}, Message: f.message}
	var said []string
	for _, d := range docs {
		ch := definitions.Change{Kind: d.kind, Name: d.id, SchemaVersion: ingest.ItemSchemaVersion, Body: d.body}
		word := "added"
		if it, ok := stored.items[d.kind][d.id]; ok {
			ch.ItemID, word = it.ItemID, "changed"
			if bytes.Equal(it.Body, d.body) {
				word = "unchanged"
			}
		}
		in.Changes = append(in.Changes, ch)
		said = append(said, d.kind+" "+d.id+" "+word)
	}
	res, err := st.svc.Apply(context.Background(), in)
	if err != nil {
		return err
	}
	if !res.Changed {
		fmt.Fprintf(out, "nothing changed: the stored definitions (D-%d) already hold %s as they are\n", res.Set.No, strings.Join(docIDs(docs), " and "))
		return nil
	}
	fmt.Fprintf(out, "stored D-%d (%s): %s\n", res.Set.No, short(res.Set.Hash), strings.Join(said, ", "))
	fmt.Fprintln(out, "ingest import only stores the definitions; praxium ingest run still reads its -source and -mapping files.")
	return nil
}

func docIDs(docs []importDoc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.kind + " " + d.id
	}
	return out
}

// printImportFailure writes why nothing was stored. The errors of the definitions come one to a line: those of the
// files with the file, those of the set with the document and its id; the last line counts them.
func printImportFailure(stderr io.Writer, err error, f importFlags) {
	var lines []string
	omitted := 0
	if se, ok := ingest.SetErrorsOf(err); ok {
		for _, e := range se.List {
			lines = append(lines, e.Line())
		}
		omitted = se.Omitted
	} else if de, ok := ingest.DefinitionErrorsOf(err); ok {
		for _, e := range de.List {
			file := f.source
			if e.Document == ingest.DocumentMapping {
				file = f.mapping
			}
			where := ""
			if e.Pointer != "" {
				where = " at " + e.Pointer
			}
			lines = append(lines, e.Document+" definition "+file+where+": "+e.Message)
		}
		omitted = de.Omitted
	} else {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		return
	}
	for _, l := range lines {
		fmt.Fprintf(stderr, "praxium: %s\n", l)
	}
	fmt.Fprintf(stderr, "praxium: nothing was stored: %d problems found in the definitions\n", len(lines)+omitted)
}

// storedIngestion is what the head of the store holds of ingestion: the items by kind and name, and the documents.
type storedIngestion struct {
	items    map[string]map[string]definitions.ResolvedItem
	sources  map[string]ingest.Source
	mappings map[string]ingest.Mapping
}

// readStoredIngestion reads the sources and mappings of the head. Items of another kind are not looked at. A kind with
// two items of one name, an item whose document has another id, and a document this build cannot read are refused
// with the item they are in: the store is not changed on top of them.
func readStoredIngestion(ctx context.Context, st *modelState) (*storedIngestion, error) {
	out := &storedIngestion{items: map[string]map[string]definitions.ResolvedItem{ingest.ItemKindSource: {}, ingest.ItemKindMapping: {}},
		sources: map[string]ingest.Source{}, mappings: map[string]ingest.Mapping{}}
	if !st.hasHead {
		return out, nil
	}
	items, err := st.svc.SetItems(ctx, st.head.No)
	if err != nil {
		return nil, err
	}
	var bad []error
	for _, it := range items {
		byName, ours := out.items[it.Kind]
		if !ours {
			continue
		}
		if other, dup := byName[it.Name]; dup {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "the definition store holds two %s items named %s (%s and %s); a name is used once in a kind", it.Kind, it.Name, other.ItemID, it.ItemID)
		}
		byName[it.Name] = it
		bad = append(bad, out.read(it))
	}
	if err := joinStoredErrors(bad); err != nil {
		return nil, err
	}
	return out, nil
}

// read reads the document of one stored item.
func (s *storedIngestion) read(it definitions.ResolvedItem) error {
	id := ""
	var err error
	if it.Kind == ingest.ItemKindSource {
		var src ingest.Source
		src, err = ingest.ParseAdapterSource(bytes.NewReader(it.Body))
		s.sources[it.Name], id = src, src.ID
	} else {
		var mp ingest.Mapping
		mp, err = ingest.ParseMapping(bytes.NewReader(it.Body))
		if mp.ID == "" {
			mp.ID = it.Name
		}
		s.mappings[it.Name], id = mp, mp.ID
	}
	if err != nil {
		return namedErrors(it.Name, err)
	}
	if id != it.Name {
		return kernel.Errorf(kernel.CodeInvalidValue, "the %s item %s of the definition store holds the document of %s", it.Kind, it.Name, id)
	}
	return nil
}

// namedErrors are the errors of the document of one stored item, named by the item.
func namedErrors(name string, err error) error {
	de, ok := ingest.DefinitionErrorsOf(err)
	if !ok {
		return err
	}
	se := &ingest.SetErrors{Omitted: de.Omitted}
	for _, e := range de.List {
		se.List = append(se.List, ingest.SetError{Name: name, DefinitionError: e})
	}
	return se
}

// joinStoredErrors makes one error of what the stored documents have: the errors of the documents together, or the
// first other error.
func joinStoredErrors(errs []error) error {
	all := &ingest.SetErrors{}
	for _, err := range errs {
		if err == nil {
			continue
		}
		se, ok := ingest.SetErrorsOf(err)
		if !ok {
			return err
		}
		all.List = append(all.List, se.List...)
		all.Omitted += se.Omitted
	}
	if len(all.List) == 0 {
		return nil
	}
	return all
}

// with is the set of every stored document, with the two of the files in place of the items of their names.
func (s *storedIngestion) with(src ingest.Source, mp ingest.Mapping) ingest.DefinitionSet {
	sources := map[string]ingest.Source{src.ID: src}
	for name, d := range s.sources {
		if name != src.ID {
			sources[name] = d
		}
	}
	mappings := map[string]ingest.Mapping{mp.ID: mp}
	for name, d := range s.mappings {
		if name != mp.ID {
			mappings[name] = d
		}
	}
	var set ingest.DefinitionSet
	for _, name := range sortedKeys(sources) {
		set.Sources = append(set.Sources, sources[name])
	}
	for _, name := range sortedKeys(mappings) {
		set.Mappings = append(set.Mappings, mappings[name])
	}
	return set
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
