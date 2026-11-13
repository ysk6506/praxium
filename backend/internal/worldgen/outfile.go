package worldgen

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"sort"
	"strings"
)

// FileInfo describes one output file of a run.
type FileInfo struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Format string `json:"format"`
	Lines  int64  `json:"lines"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type outFile struct {
	info FileInfo
	c    io.Closer
	bw   *bufio.Writer
	h    hash.Hash
	err  error
}

func (o *outFile) write(b []byte) {
	if o.err != nil {
		return
	}
	n, err := o.bw.Write(b)
	o.err = err
	o.info.Bytes += int64(n)
	o.h.Write(b[:n])
	o.info.Lines += int64(countNewlines(b))
}

func countNewlines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

func (o *outFile) close() FileInfo {
	if o.err == nil {
		o.err = o.bw.Flush()
	}
	if err := o.c.Close(); o.err == nil {
		o.err = err
	}
	o.info.SHA256 = hex.EncodeToString(o.h.Sum(nil))
	return o.info
}

// fileSet opens output files on demand and closes them all at the end.
type fileSet struct {
	st  Store
	by  map[string]*outFile
	err error
}

func newFileSet(st Store) *fileSet { return &fileSet{st: st, by: map[string]*outFile{}} }

func (f *fileSet) open(name, source, format string) *outFile {
	if o, ok := f.by[name]; ok {
		return o
	}
	w, err := f.st.Create(name)
	o := &outFile{info: FileInfo{Name: name, Source: source, Format: format}, h: sha256.New(), err: err}
	if err == nil {
		o.c = w
		o.bw = bufio.NewWriterSize(w, 1<<16)
	} else {
		o.c, o.bw = nopFile{}, bufio.NewWriter(io.Discard)
		if f.err == nil {
			f.err = err
		}
	}
	f.by[name] = o
	return o
}

// closeAll closes the files and lists them by name.
func (f *fileSet) closeAll() ([]FileInfo, error) {
	var out []FileInfo
	for _, o := range f.by {
		out = append(out, o.close())
		if o.err != nil && f.err == nil {
			f.err = o.err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, f.err
}

// appendCSV writes one CSV record (RFC 4180 quoting) with a line feed.
func appendCSV(b []byte, fields ...string) []byte {
	for i, f := range fields {
		if i > 0 {
			b = append(b, ',')
		}
		if strings.ContainsAny(f, ",\"\n\r") {
			b = append(b, '"')
			b = append(b, strings.ReplaceAll(f, `"`, `""`)...)
			b = append(b, '"')
		} else {
			b = append(b, f...)
		}
	}
	return append(b, '\n')
}
