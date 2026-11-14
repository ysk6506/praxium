package filejournal

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"strings"
	"testing"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// closedDisk plays the big plan on a folder, closes the journal and returns the folder and what was written.
func closedDisk(t *testing.T) (*fakeDisk, state) {
	t.Helper()
	d := newFakeDisk()
	p := bigPlan()
	j := openEmpty(t, d)
	if acked := p.run(j); acked != len(p.items) {
		t.Fatalf("acked %d", acked)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	return d, p.items
}

func clone(d *fakeDisk) *fakeDisk {
	out := newFakeDisk()
	for n, f := range d.files {
		c := &fakeFile{data: append([]byte(nil), f.data...)}
		c.synced = len(c.data)
		out.files[n], out.durable[n] = c, c
	}
	return out
}

func namesOf(d *fakeDisk, prefix string) []string {
	var out []string
	names, _ := d.List()
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}

// A snapshot that is damaged anywhere, or cut at any length, is passed over for the one before it and the log: every
// record is still there.
// [REQ-272]
func TestADamagedNewestSnapshotIsReplacedByTheOneBeforeIt(t *testing.T) {
	base, want := closedDisk(t)
	snaps := namesOf(base, "snap-")
	if len(snaps) != 2 {
		t.Fatalf("the journal keeps %d snapshots, want the two newest: %v", len(snaps), snaps)
	}
	newest := snaps[len(snaps)-1]
	size := len(base.files[newest].data)
	for pos := 0; pos < size; pos++ {
		d := clone(base)
		d.files[newest].data[pos] ^= 0x40
		_, got, rep := mustRecover(t, d)
		if !got.equal(want) || len(rep.SkippedSnapshots) != 1 {
			t.Fatalf("a flipped bit at %d of the newest snapshot: %d items, report %+v", pos, len(got), rep)
		}
	}
	for n := 0; n < size; n++ {
		d := clone(base)
		d.files[newest].data = d.files[newest].data[:n]
		_, got, _ := mustRecover(t, d)
		if !got.equal(want) {
			t.Fatalf("the newest snapshot cut at %d: %d items", n, len(got))
		}
	}
}

// Both snapshots damaged leaves a log that does not reach back to the start: that is an error, not a guess.
// [REQ-272]
func TestWithNoWholeSnapshotAndAGapInTheLogRecoveryStops(t *testing.T) {
	base, _ := closedDisk(t)
	for _, n := range namesOf(base, "snap-") {
		base.files[n].data[len(base.files[n].data)/2] ^= 1
	}
	_, _, _, err := recoverState(base)
	wantCode(t, err, CodeCorrupt)
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("the error does not say what is missing: %v", err)
	}
}

// Damage inside the log, with whole records after it, is never skipped.
// [REQ-272]
func TestDamageInTheMiddleOfTheLogStopsRecovery(t *testing.T) {
	d := newFakeDisk()
	j := openEmpty(t, d)
	p := plan{items: payloads(8, 3)}
	p.run(j)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	seg := namesOf(d, "log-")[0]
	data := d.files[seg].data
	mid := headerLen + frameLen + len(p.items[0]) + frameLen + 1 // inside the payload of the second record
	data[mid] ^= 0xff
	_, _, _, err := recoverState(d)
	wantCode(t, err, CodeCorrupt)
}

// A whole record with the wrong number, and a segment that is missing, are damage too.
// [REQ-272]
func TestARecordOutOfPlaceAndAMissingSegmentAreDamage(t *testing.T) {
	d := newFakeDisk()
	j := openEmpty(t, d)
	if _, err := j.Append([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append([]byte("two")); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	seg := namesOf(d, "log-")[0]
	data := d.files[seg].data
	dup := append(append([]byte(nil), data...), appendFrame(nil, 2, []byte("again"))...)
	d.files[seg].data = dup
	_, _, _, err := recoverState(d)
	wantCode(t, err, CodeCorrupt)

	base, _ := closedDisk(t)
	snaps := namesOf(base, "snap-")
	base.files[snaps[len(snaps)-1]].data[headerLen+2] ^= 1
	segs := namesOf(base, "log-")
	delete(base.files, segs[0])
	_, _, _, err = recoverState(base)
	wantCode(t, err, CodeCorrupt)
}

// A file of a format version this build does not know is not opened, and the message says so.
// [REQ-273]
func TestAFileOfAnUnknownVersionIsNotOpened(t *testing.T) {
	for _, prefix := range []string{"log-", "snap-"} {
		base, _ := closedDisk(t)
		names := namesOf(base, prefix)
		h := base.files[names[len(names)-1]].data
		copy(h, putHeader(map[string]string{"log-": segMagic, "snap-": snapMagic}[prefix], 1))
		h[7] = 9 // the version is bytes 6 and 7
		binary.BigEndian.PutUint32(h[16:], crc32.Checksum(h[:16], crcTable))
		_, _, _, err := recoverState(base)
		wantCode(t, err, CodeUnsupportedFormat)
		if !strings.Contains(err.Error(), "version 9") {
			t.Fatalf("the error does not name the version: %v", err)
		}
	}
}

// Two processes cannot open one folder, and a folder is free again once the first lets go.
// [REQ-273]
func TestTheFolderOpensInOneJournalAtATime(t *testing.T) {
	d := newFakeDisk()
	j := openEmpty(t, d)
	_, err := Open(d)
	wantCode(t, err, CodeLocked)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("a second close: %v", err)
	}
	j2, err := Open(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := j2.Close(); err != nil {
		t.Fatal(err)
	}
}

// The calls of a journal in the wrong order or with a wrong argument are refused.
// [REQ-273]
func TestTheJournalRefusesCallsOutOfOrder(t *testing.T) {
	d := newFakeDisk()
	j, err := Open(d)
	if err != nil {
		t.Fatal(err)
	}
	_, err = j.Append([]byte("x"))
	wantCode(t, err, CodeIO)
	wantCode(t, j.Checkpoint(nil), CodeIO)
	if err := j.Recover(func(r_ io.Reader) error { return nil }, func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	wantCode(t, j.Recover(nil, nil), CodeIO)
	if _, err := j.Append(make([]byte, maxRecord+1)); kernel.CodeOf(err) != kernel.CodeInvalidValue {
		t.Fatalf("an oversized record: %v", err)
	}
	if _, err := j.Append([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := j.Checkpoint(func(w io.Writer) error { return errCrash }); err == nil {
		t.Fatal("a failing snapshot writer must fail the checkpoint")
	}
	if names := namesOf(d, "snap-"); len(names) != 0 {
		t.Fatalf("a failed checkpoint left %v", names)
	}
	if _, err := j.Append([]byte("y")); err != nil {
		t.Fatalf("the journal is not usable after a failed checkpoint: %v", err)
	}
	if err := j.Checkpoint(func(w io.Writer) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := j.Checkpoint(func(w io.Writer) error { return errCrash }); err != nil {
		t.Fatalf("a checkpoint with nothing new is a no-op: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
}

// Leftovers of a snapshot that never became one are removed, and the codes have meanings.
// [REQ-273]
func TestLeftoversAreRemovedAndCodesHaveMeanings(t *testing.T) {
	d := newFakeDisk()
	d.files[tmpName(5)] = &fakeFile{data: []byte("half")}
	d.files["notes.txt"] = &fakeFile{data: []byte("not ours")}
	j := openEmpty(t, d)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.files[tmpName(5)]; ok {
		t.Fatal("the temporary snapshot stayed")
	}
	if _, ok := d.files["notes.txt"]; !ok {
		t.Fatal("a file that is not the journal's was touched")
	}
	for _, c := range Codes() {
		if m, ok := CodeMeaning(c); !ok || m == "" {
			t.Fatalf("code %s has no meaning", c)
		}
	}
	if _, ok := CodeMeaning("nope"); ok {
		t.Fatal("an unknown code has a meaning")
	}
}

// Noise after the last record of the log is a cut write, and the records before it are kept; bytes of a record in the
// middle are not.
// [REQ-272]
func TestNoiseAfterTheLastRecordIsDroppedAndTheLogGoesOn(t *testing.T) {
	d := newFakeDisk()
	j := openEmpty(t, d)
	p := plan{items: payloads(5, 9)}
	p.run(j)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	seg := namesOf(d, "log-")[0]
	d.files[seg].data = append(d.files[seg].data, bytes.Repeat([]byte{0xAB}, 300)...)
	j2, got, rep := mustRecover(t, d)
	if !got.equal(p.items) || !rep.DroppedTail {
		t.Fatalf("recovered %d items, report %+v", len(got), rep)
	}
	if _, err := j2.Append([]byte("after")); err != nil {
		t.Fatal(err)
	}
	if err := j2.Close(); err != nil {
		t.Fatal(err)
	}
	_, got, rep = mustRecover(t, d)
	if !got.equal(append(append(state(nil), p.items...), []byte("after"))) || rep.DroppedTail != true {
		t.Fatalf("after the log went on: %d items, report %+v", len(got), rep)
	}
}
