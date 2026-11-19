package httpsource

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/url"
	"strings"
	"time"
)

// reply is an answer of the source that was a success: its status and headers, and its body, which is open until it has
// been read or closed. The answer to a request for data is read as a stream (stream.go); the answer to a request for a
// token is small and is read whole (smallBody).
type reply struct {
	status int
	header map[string][]string
	url    *url.URL
	rc     io.ReadCloser
	// started is when the request was sent, for the time the answer took.
	started time.Time
}

// closeBody lets go of the body, once.
func (r *reply) closeBody() {
	if r != nil && r.rc != nil {
		_ = r.rc.Close()
		r.rc = nil
	}
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
