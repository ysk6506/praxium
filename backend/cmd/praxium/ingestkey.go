package main

import (
	"bytes"
	"io"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/xlsx"
)

// maxKeyHintBytes bounds the file read to find the candidates of a natural key: a file over it is not read for this.
const maxKeyHintBytes = 64 << 20

// withKeyCandidates adds to the error of a draft whose natural key was never filled in the columns of the file given
// for the dataset that are filled in every row and different in each, the same ones `ingest inspect -emit-source`
// told when it made the draft. They come before the list of every column. Any other error is returned as it is.
func withKeyCandidates(err error, source []byte, f ingestFlags, w wiring) error {
	text := errText(err)
	if !strings.Contains(text, ingest.NaturalKeyPlaceholder) {
		return err
	}
	src, perr := ingest.ParseSourceLoose(bytes.NewReader(source))
	if perr != nil {
		return err
	}
	for _, ds := range src.Datasets {
		if !ds.HasKeyPlaceholder() || !strings.Contains(text, "dataset "+ds.ID+":") {
			continue
		}
		file, ok := f.inputs[ds.ID]
		if !ok {
			return err
		}
		data, rerr := readInputFile(w, file)
		if rerr != nil {
			return err
		}
		cands, cerr := ingest.NaturalKeyCandidates(ds, data, xlsx.Reader{})
		if cerr != nil {
			return err
		}
		note := ingest.KeyCandidatesNote(ds.ID, cands)
		const marker = "The columns are:"
		if i := strings.Index(text, marker); i >= 0 {
			text = text[:i] + note + " All the columns of the dataset: " + text[i+len(marker):]
		} else {
			text += ". " + note
		}
		return kernel.Errorf(kernel.CodeOf(err), "%s", text)
	}
	return err
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
