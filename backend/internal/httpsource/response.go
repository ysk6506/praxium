package httpsource

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/url"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// reply is an answer of the source that was a success, read whole: its body is at most as big as the limit of the
// connection allows, after it was unpacked.
type reply struct {
	status int
	header map[string][]string
	body   []byte
	url    *url.URL
}

// readBody reads at most limit bytes of a body, and refuses a body that has more. It closes the body.
func readBody(rc io.ReadCloser, limit int) ([]byte, error) {
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

func headerValue(h map[string][]string, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// jsonType tells whether a Content-Type is JSON. A response that names no type is read as JSON and fails there if it is
// not; one that names another type is not read.
func jsonType(h map[string][]string) bool {
	v := headerValue(h, "Content-Type")
	if v == "" {
		return true
	}
	media, _, err := mime.ParseMediaType(v)
	if err != nil {
		return false
	}
	return media == "application/json" || strings.HasSuffix(media, "+json")
}

// decodeJSON reads one JSON value, with the numbers kept as the text the source wrote them in.
func decodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("more than one value")
	}
	return doc, nil
}

// parsed is a response that was read as JSON: the document and the records it holds.
type parsed struct {
	reply   *reply
	doc     any
	records []any
}

// parse reads the body of a successful answer as JSON and finds the records in it. A 204 is an answer with no records.
func (rn *run) parse(rep *reply, recordsPath string) (*parsed, error) {
	if !jsonType(rep.header) {
		return nil, rn.fail(ingest.SrcResponseFormat, "the source answered with something that is not JSON", nil)
	}
	if rep.status == 204 {
		rn.prog.responded, rn.prog.recordsFound = true, true
		return &parsed{reply: rep}, nil
	}
	doc, err := decodeJSON(rep.body)
	if err != nil {
		return nil, rn.fail(ingest.SrcResponseFormat, "the body of the answer is not one JSON value", nil)
	}
	rn.prog.responded = true
	list, err := rn.recordsOf(doc, recordsPath)
	if err != nil {
		return nil, err
	}
	rn.prog.recordsFound = true
	return &parsed{reply: rep, doc: doc, records: list}, nil
}

// recordsOf finds the array of records: records_path says where in the document it is, and a path that is not given means
// that the document is the array. A null there is a list with no records.
func (rn *run) recordsOf(doc any, recordsPath string) ([]any, error) {
	at := doc
	if recordsPath != "" {
		path, err := ingest.ParsePath(recordsPath)
		if err != nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "records_path is not a path")
		}
		found, ok := path.Lookup(doc)
		if !ok {
			return nil, rn.fail(ingest.SrcResponseFormat, "records_path leads to nothing in the answer", nil)
		}
		at = found
	}
	switch list := at.(type) {
	case []any:
		return list, nil
	case nil:
		if recordsPath != "" {
			return nil, nil
		}
	}
	return nil, rn.fail(ingest.SrcResponseFormat, "the records are not an array; records_path may be missing or wrong", nil)
}
