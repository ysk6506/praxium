package worldgen

import (
	"bytes"
	"io"
	"sort"
	"strings"
)

// Store creates the output files of a run. The command gives it files on
// disk; the tests give it memory. The package itself knows no file system.
type Store interface {
	Create(name string) (io.WriteCloser, error)
}

// MemStore keeps files in memory.
type MemStore struct{ Files map[string]*bytes.Buffer }

// NewMemStore makes an empty store.
func NewMemStore() *MemStore { return &MemStore{Files: map[string]*bytes.Buffer{}} }

type memFile struct{ *bytes.Buffer }

func (memFile) Close() error { return nil }

// Create starts (or restarts) a file.
func (m *MemStore) Create(name string) (io.WriteCloser, error) {
	b := &bytes.Buffer{}
	m.Files[name] = b
	return memFile{b}, nil
}

// Names lists the files in order.
func (m *MemStore) Names() []string {
	var out []string
	for n := range m.Files {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Text returns a file as a string.
func (m *MemStore) Text(name string) string {
	if b, ok := m.Files[name]; ok {
		return b.String()
	}
	return ""
}

// discardStore throws everything away (a run that only counts).
type discardStore struct{}

type nopFile struct{}

func (nopFile) Write(p []byte) (int, error) { return len(p), nil }
func (nopFile) Close() error                { return nil }

func (discardStore) Create(string) (io.WriteCloser, error) { return nopFile{}, nil }

// Source names. Each source is an invented system with its own identifiers.
const (
	SrcRegister = "asset-register" // CSV tables
	SrcVirt     = "virt-inventory" // JSON API pages
	SrcNetconf  = "netconf"        // device configuration text
	SrcShow     = "show-tables"    // command output tables
	SrcDeclared = "declared-lists" // declared ECMP lists
	SrcFeed     = "change-feed"    // change stream
)

// srcID is the identifier a source gives to an object of the truth. It is a
// function of the source and the truth id, so any part can compute it alone.
func srcID(source, prefix, id string) string {
	return prefix + "-" + base32(mix(hash64(source), hash64(id)), 10)
}

// parentID cuts the last "/" part off an id: the object it is part of.
func parentID(id string) string {
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		return id[:i]
	}
	return ""
}

// deviceOfIface is the device id of an interface id made by iface or ifaceFor.
func deviceOfIface(id string) string {
	if i := strings.Index(id, "/if"); i >= 0 {
		return id[:i]
	}
	return ""
}

// Imperfection is one flaw put into a source view, with what a reader is
// expected to do with it.
type Imperfection struct {
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	File     string `json:"file"`
	Target   string `json:"target"`
	Expected string `json:"expected"`
}

// ImperfectionLog counts every flaw and keeps the first few of each kind.
type ImperfectionLog struct {
	Counts  map[string]int64
	ByFile  map[string]int64
	Samples []Imperfection
	perKind map[string]int
}

const samplesPerKind = 25

func newImperfectionLog() *ImperfectionLog {
	return &ImperfectionLog{Counts: map[string]int64{}, ByFile: map[string]int64{}, perKind: map[string]int{}}
}

func (l *ImperfectionLog) add(im Imperfection) {
	l.Counts[im.Kind]++
	l.ByFile[im.Kind+" "+im.File]++
	if l.perKind[im.Kind] < samplesPerKind {
		l.perKind[im.Kind]++
		l.Samples = append(l.Samples, im)
	}
}
