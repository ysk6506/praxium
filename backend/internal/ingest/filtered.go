package ingest

import (
	"sort"
	"time"
)

// A row that a rule of the mapping filters out is treated as a row that is gone from the file (ADR-0052): the object
// it made is not closed at once, a typo in a filter would close it. It is closed when the same number of runs in a
// row and the same time have passed as for a row that vanished (rawrec.go), and a dataset that never retires a row
// that is gone (deletion none) never retires a row that is filtered out. Until then the row is fed as if no rule had
// filtered it, but only when its object exists: a row that was never an object does not become one by waiting.

// releasedAfterWaiting tells whether a row that is filtered out has been so long enough for its object to be closed.
func releasedAfterWaiting(u Unseen, deletion string, now time.Time) bool {
	if deletion != DeletionAbsentInFull {
		return false
	}
	first, _ := time.Parse(time.RFC3339, u.First)
	return u.Runs >= UnseenRuns && now.Sub(first) >= UnseenSpan
}

// settleFiltered counts this run for the rows the rules filtered out and drops from d.held the rows whose time is
// up. A dataset that was not read in this run counts nothing: nothing was seen, so nothing has passed.
func (d *derivation) settleFiltered() {
	deletion := map[string]string{}
	for _, ds := range d.p.Source.Datasets {
		deletion[ds.ID] = ds.Deletion
	}
	filtered := map[string]map[string]bool{}
	for _, r := range d.p.refs {
		root := d.p.roots[r.ID]
		if filtered[root] == nil {
			filtered[root] = map[string]bool{}
		}
		for _, row := range d.held[r.ID] {
			filtered[root][row.Key] = true
		}
	}
	roots := make([]string, 0, len(filtered))
	for root := range filtered {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, root := range roots {
		d.countFiltered(root, filtered[root])
	}
	for _, r := range d.p.refs {
		root := d.p.roots[r.ID]
		st := d.st.dataset(root)
		kept := d.held[r.ID][:0:0]
		for _, row := range d.held[r.ID] {
			if !releasedAfterWaiting(st.Filtered[row.Key], deletion[root], d.now) {
				kept = append(kept, row)
			}
		}
		d.held[r.ID] = kept
	}
}

func (d *derivation) countFiltered(root string, keys map[string]bool) {
	st := d.st.dataset(root)
	if !d.rep.dataset(root).Applied {
		return
	}
	for k := range st.Filtered {
		if !keys[k] {
			delete(st.Filtered, k)
		}
	}
	if st.Filtered == nil {
		st.Filtered = map[string]Unseen{}
	}
	for k := range keys {
		u := st.Filtered[k]
		if u.Runs == 0 {
			u.First = d.now.UTC().Format(time.RFC3339)
		}
		u.Runs++
		st.Filtered[k] = u
	}
}

// feedHeld feeds a row that a rule filtered out and whose time is not up. It does what feedRow does for an object
// that is already there and nothing else: a row with no identity, an identity no object has, or a value that is
// wrong is no problem to report, since the rule has set the row aside.
func (d *derivation) feedHeld(fp *feedPlan, row Row) error {
	root := d.p.roots[fp.feed.Dataset]
	formed, _ := formKeys(fp, d.p.reg, row)
	if !d.hasLiveObject(formed) {
		return nil
	}
	res, found, err := identify(fp, d.st, formed)
	if err != nil || !found {
		return err
	}
	acc := d.objects[res.id.String()]
	if acc == nil {
		if !fp.feed.CreatesObjects {
			return nil
		}
		acc = d.newObject(fp, res, row, root)
	}
	contribs, _ := d.values(fp, row, root)
	d.attach(fp, acc, formed, contribs, row)
	d.rowObj[fp.feed.ID][row.Key] = res.id.String()
	return nil
}

// hasLiveObject tells whether one of the keys is of an object this source made and has not closed.
func (d *derivation) hasLiveObject(formed []formedKey) bool {
	for _, fk := range formed {
		if id, ok := d.st.Identity[fk.sig]; ok {
			if _, live := d.st.Objects[id]; live {
				return true
			}
		}
	}
	return false
}

// waitingObjects counts the objects that wait to be closed: a feed that retires them made them and no row that is
// shown, not filtered out and not on its way out is left to say they are there.
func (d *derivation) waitingObjects() int {
	n := 0
	for _, acc := range d.objects {
		if acc.creates && acc.retires && !acc.live {
			n++
		}
	}
	return n
}
