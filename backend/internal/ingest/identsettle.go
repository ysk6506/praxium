package ingest

import (
	"context"
	"sort"
)

// settlePut is how many settled pairs a unit that writes them takes.
const settlePut = 5000

// settleGroups ends the search for the groups of a round that its turns did not end (ADR-0108): it reads the pairs of the groups that
// are not whole, finds the groups by their keys with a union of the pairs that share one, and writes the label of each (the smallest
// pair of the group). A run that was stopped goes on from the pairs not written yet: what is read is the same, so the labels are.
// More pairs than the limit are not held: nothing is written then, and the groups are left.
func settleGroups(ctx context.Context, d Identifier, lease RunLease, limit int) error {
	if limit == 0 {
		limit = DefaultSettlePairs
	}
	var pairs []SettlePair
	var written, after int64
	for {
		page, err := d.SettleRead(ctx, lease, after)
		if err != nil {
			return err
		}
		written = page.Written
		pairs = append(pairs, page.Pairs...)
		if len(pairs) > limit {
			return d.PutSettled(ctx, lease, nil, SettleEnd{Last: true, Skipped: true})
		}
		if page.Done {
			break
		}
		after = page.Next
	}
	rows := settleLabels(pairs)
	i := sort.Search(len(rows), func(i int) bool { return rows[i].Pid > written })
	rows = rows[i:]
	for {
		n := min(settlePut, len(rows))
		last := n == len(rows)
		if err := d.PutSettled(ctx, lease, rows[:n], SettleEnd{Last: last}); err != nil {
			return err
		}
		if last {
			return nil
		}
		rows = rows[n:]
	}
}

// settleLabels gives each pair the smallest pair of its group: the pairs that have a key in common are one group. The rows are in the
// order of the pairs.
func settleLabels(pairs []SettlePair) []SettledPair {
	index := make(map[int64]int, len(pairs))
	parent := make([]int, len(pairs))
	for i, p := range pairs {
		index[p.Pid], parent[i] = i, i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	owner := map[string]int{}
	for i, p := range pairs {
		for _, k := range p.Keys {
			if o, ok := owner[k]; ok {
				a, b := find(i), find(o)
				// the root is the smaller pair, so the root of a group is its label
				if pairs[a].Pid < pairs[b].Pid {
					parent[b] = a
				} else if a != b {
					parent[a] = b
				}
			} else {
				owner[k] = i
			}
		}
	}
	out := make([]SettledPair, len(pairs))
	for i, p := range pairs {
		out[i] = SettledPair{Pid: p.Pid, Label: pairs[find(i)].Pid}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pid < out[j].Pid })
	return out
}
