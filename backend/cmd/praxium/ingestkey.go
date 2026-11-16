package main

import (
	"bytes"
	"io"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/xlsx"
)

// maxKeyHintBytes bounds the file read to find the candidates of a natural key: a file over it is not read for this.
const maxKeyHintBytes = 64 << 20

// withKeyCandidates adds to the error of a draft whose natural key was never filled in the columns of the file given
// for the dataset that are filled in every row and different in each, the same ones `ingest inspect -emit-source`
// told when it made the draft. They come before the list of every column. Each error of the list is looked at on its
// own and the others stay as they are; an error that is no list of definition errors is returned as it is.
func withKeyCandidates(err error, source []byte, f ingestFlags, w wiring) error {
	de, isList := ingest.DefinitionErrorsOf(err)
	if !isList {
		return err
	}
	out := &ingest.DefinitionErrors{List: append([]ingest.DefinitionError{}, de.List...), Omitted: de.Omitted}
	for i, e := range out.List {
		if text, changed := keyCandidatesText(e.Message, source, f, w); changed {
			out.List[i].Message = text
		}
	}
	return out
}

// keyCandidatesText is the sentence with the candidates put in, and whether it was changed.
func keyCandidatesText(text string, source []byte, f ingestFlags, w wiring) (string, bool) {
	if !strings.Contains(text, ingest.NaturalKeyPlaceholder) {
		return text, false
	}
	src, perr := ingest.ParseSourceLoose(bytes.NewReader(source))
	if perr != nil {
		return text, false
	}
	for _, ds := range src.Datasets {
		if !ds.HasKeyPlaceholder() || !strings.Contains(text, "dataset "+ds.ID+":") {
			continue
		}
		file, ok := f.inputs[ds.ID]
		if !ok {
			return text, false
		}
		data, rerr := readInputFile(w, file)
		if rerr != nil {
			return text, false
		}
		cands, cerr := ingest.NaturalKeyCandidates(ds, data, xlsx.Reader{})
		if cerr != nil {
			return text, false
		}
		note := ingest.KeyCandidatesNote(ds.ID, cands)
		const marker = "The columns are:"
		if i := strings.Index(text, marker); i >= 0 {
			return text[:i] + note + " All the columns of the dataset: " + text[i+len(marker):], true
		}
		return text + ". " + note, true
	}
	return text, false
}

// readInputFile reads an input file whole, up to a bound.
func readInputFile(w wiring, path string) ([]byte, error) {
	in, err := openTruth(w, path)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	return io.ReadAll(io.LimitReader(in, maxKeyHintBytes))
}
