package definitionsfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/ysk6506/praxium/backend/internal/definitions"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// wireEntry is the entry of a set as the file has it: [item ID, revision number]. The content hash of an entry is the
// one of the revision, and the file need not say it twice.
type wireEntry struct {
	ItemID string
	RevNo  int64
}

func (e wireEntry) MarshalJSON() ([]byte, error) { return json.Marshal([]any{e.ItemID, e.RevNo}) }

func (e *wireEntry) UnmarshalJSON(b []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(b, &pair); err != nil || len(pair) != 2 {
		return fmt.Errorf("an entry of a set is [item ID, revision number]")
	}
	if err := json.Unmarshal(pair[0], &e.ItemID); err != nil {
		return err
	}
	return json.Unmarshal(pair[1], &e.RevNo)
}

type wireSet struct {
	No         int64              `json:"set_no"`
	Hash       string             `json:"content_hash"`
	ParentNo   int64              `json:"parent_set_no"`
	Origin     definitions.Origin `json:"origin"`
	Baseline   string             `json:"baseline"`
	Entries    []wireEntry        `json:"entries"`
	ProposalID string             `json:"proposal_id,omitempty"`
	CreatedBy  string             `json:"created_by"`
	Message    string             `json:"message,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
}

// wireDoc is what the file holds. The lists are in a fixed order, so the same content is the same file.
type wireDoc struct {
	Format    string                 `json:"format"`
	Version   int                    `json:"version"`
	Head      int64                  `json:"head"`
	Counters  map[string]int64       `json:"counters"`
	Items     []definitions.Item     `json:"items"`
	Revisions []definitions.Revision `json:"revisions"`
	Sets      []wireSet              `json:"sets"`
	Branches  []definitions.Branch   `json:"branches"`
	Proposals []definitions.Proposal `json:"proposals"`
}

// encode writes the data as one JSON object with a record to a line.
func encode(d definitions.Data) ([]byte, error) {
	var out bytes.Buffer
	counters, err := json.Marshal(nonNil(d.Counters))
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&out, "{\"format\":%q,\"version\":%d,\"head\":%d,\n\"counters\":%s,\n", format, version, d.Head, counters)
	sets := make([]wireSet, len(d.Sets))
	for i, s := range d.Sets {
		sets[i] = toWire(s)
	}
	for _, sec := range []struct {
		name string
		list any
		last bool
	}{{"items", d.Items, false}, {"revisions", d.Revisions, false}, {"sets", sets, false}, {"branches", d.Branches, false}, {"proposals", d.Proposals, true}} {
		lines, err := lines(sec.list)
		if err != nil {
			return nil, err
		}
		section(&out, sec.name, lines, sec.last)
	}
	out.WriteString("}\n")
	return out.Bytes(), nil
}

func nonNil(m map[string]int64) map[string]int64 {
	if m == nil {
		return map[string]int64{}
	}
	return m
}

// lines marshals every element of a list (a slice) on its own.
func lines(list any) ([]json.RawMessage, error) {
	v := reflect.ValueOf(list)
	out := make([]json.RawMessage, v.Len())
	for i := range out {
		el, err := json.Marshal(v.Index(i).Interface())
		if err != nil {
			return nil, err
		}
		out[i] = el
	}
	return out, nil
}

func toWire(s definitions.Set) wireSet {
	entries := make([]wireEntry, len(s.Entries))
	for i, e := range s.Entries {
		entries[i] = wireEntry{ItemID: e.ItemID, RevNo: e.RevNo}
	}
	return wireSet{No: s.No, Hash: s.Hash, ParentNo: s.ParentNo, Origin: s.Origin, Baseline: s.Baseline, Entries: entries,
		ProposalID: s.ProposalID, CreatedBy: s.CreatedBy, Message: s.Message, CreatedAt: s.CreatedAt}
}

// decode reads the file back into data. The bodies are put in normal form again, so a file that was reformatted by hand
// is still read; whether the content is the same content is for the hashes to say when the store opens.
func decode(raw []byte) (definitions.Data, error) {
	var doc wireDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return definitions.Data{}, kernel.Errorf(kernel.CodeInvalidValue, "%s is not valid: %v", FileName, err)
	}
	d := definitions.Data{Head: doc.Head, Counters: doc.Counters, Items: doc.Items, Revisions: doc.Revisions,
		Branches: doc.Branches, Proposals: doc.Proposals}
	hashes := map[string]string{}
	for i, r := range d.Revisions {
		body, err := definitions.NormalizeBody(r.Body)
		if err != nil {
			return definitions.Data{}, kernel.Errorf(kernel.CodeInvalidValue, "%s: the body of revision %d of %q is not valid: %v", FileName, r.RevNo, r.ItemID, err)
		}
		d.Revisions[i].Body = body
		hashes[fmt.Sprintf("%s\x00%d", r.ItemID, r.RevNo)] = r.ContentHash
	}
	for i, b := range d.Branches {
		for j, e := range b.Entries {
			if e.Op != definitions.OpRemove {
				body, err := definitions.NormalizeBody(e.Body)
				if err != nil {
					return definitions.Data{}, kernel.Errorf(kernel.CodeInvalidValue, "%s: a body in the branch %q is not valid: %v", FileName, b.ID, err)
				}
				d.Branches[i].Entries[j].Body = body
			}
		}
	}
	for _, s := range doc.Sets {
		set, err := fromWire(s, hashes)
		if err != nil {
			return definitions.Data{}, err
		}
		d.Sets = append(d.Sets, set)
	}
	return d, nil
}

func fromWire(s wireSet, hashes map[string]string) (definitions.Set, error) {
	entries := make([]definitions.SetEntry, len(s.Entries))
	for i, e := range s.Entries {
		h, ok := hashes[fmt.Sprintf("%s\x00%d", e.ItemID, e.RevNo)]
		if !ok {
			return definitions.Set{}, kernel.Errorf(kernel.CodeInvalidValue, "%s: D-%d holds revision %d of %q, which is not in the file", FileName, s.No, e.RevNo, e.ItemID)
		}
		entries[i] = definitions.SetEntry{ItemID: e.ItemID, RevNo: e.RevNo, ContentHash: h}
	}
	return definitions.Set{No: s.No, Hash: s.Hash, ParentNo: s.ParentNo, Origin: s.Origin, Baseline: s.Baseline, Entries: entries,
		ProposalID: s.ProposalID, CreatedBy: s.CreatedBy, Message: s.Message, CreatedAt: s.CreatedAt}, nil
}
