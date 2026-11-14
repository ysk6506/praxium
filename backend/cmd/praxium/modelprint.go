package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/model"
)

// printCheck writes the report of "model check" as text: the size of the file, the errors, the warnings and then what
// would change, grouped by type, and the verdict.
func printCheck(out io.Writer, r checkReport) {
	against := "the standard model alone"
	if r.StoredSet > 0 {
		against = fmt.Sprintf("the stored definitions D-%d", r.StoredSet)
	}
	fmt.Fprintf(out, "%s: %d overlays, %d properties, compared with %s\n", r.File, r.Overlays, r.Properties, against)
	for _, e := range r.Errors {
		fmt.Fprintf(out, "error    %s\n", e.Message)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(out, "warning  %s\n", w)
	}
	printChanges(out, r.Changes)
	if r.OK {
		fmt.Fprintf(out, "ok: apply would store %d properties added and %d changed\n", r.Added, r.Modified)
		return
	}
	fmt.Fprintf(out, "not ok: %d errors, %d breaking changes; apply would refuse the file\n", len(r.Errors), r.Breaking)
}

// printChanges lists the changes under the type they belong to.
func printChanges(out io.Writer, changes []planChange) {
	byTarget := map[string][]planChange{}
	for _, c := range changes {
		byTarget[c.Target] = append(byTarget[c.Target], c)
	}
	targets := make([]string, 0, len(byTarget))
	for t := range byTarget {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, t := range targets {
		fmt.Fprintf(out, "%s\n", t)
		for _, c := range byTarget[t] {
			fmt.Fprintf(out, "  %-10s %s\n", strings.ToUpper(string(c.Kind[:1]))+string(c.Kind[1:]), changeText(c))
		}
	}
}

// changeText is the name of the property and what changed; an addition needs no more than the name.
func changeText(c planChange) string {
	if c.Kind == model.ChangeAdded {
		return c.Name
	}
	return c.Name + ": " + c.Detail
}

// printShow writes the stored organization properties as text.
func printShow(out io.Writer, rs readSet) {
	if !rs.hasSet || len(rs.overlay) == 0 {
		fmt.Fprintln(out, "no organization properties are stored: the model is the standard model alone")
		return
	}
	fmt.Fprintf(out, "organization properties of D-%d (%s)\n", rs.set.No, short(rs.set.Hash))
	for _, o := range rs.overlay {
		fmt.Fprintf(out, "%s\n", targetNameOf(rs.st.base, o.Target))
		for _, p := range o.Properties {
			fmt.Fprintf(out, "  %-34s %-9s %-10s attr_no %d%s\n", p.Name, strings.TrimPrefix(p.ValueType.String(), "vt."), statusText(p), p.AttrNo, labelText(p))
		}
	}
}

func statusText(p model.Property) string {
	if p.Status == "" {
		return string(model.StatusActive)
	}
	return string(p.Status)
}

func labelText(p model.Property) string {
	if len(p.DisplayNames) == 0 {
		return ""
	}
	langs := make([]string, 0, len(p.DisplayNames))
	for l := range p.DisplayNames {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	parts := make([]string, len(langs))
	for i, l := range langs {
		parts[i] = l + "=" + p.DisplayNames[l]
	}
	return "  " + strings.Join(parts, " ")
}

// printHistory writes the definition sets as text, newest first.
func printHistory(out io.Writer, rows []historyRow) {
	if len(rows) == 0 {
		fmt.Fprintln(out, "no definitions are stored yet")
		return
	}
	for _, r := range rows {
		fmt.Fprintf(out, "D-%-4d %s  %s  +%d added, %d changed  %s\n", r.Set, r.CreatedAt, short(r.Hash), r.Added, r.Changed, r.Message)
	}
}
