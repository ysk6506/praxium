package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
)

// askImpact is "praxium query impact": what a failure reaches, or what a target depends on.
func askImpact(ctx context.Context, c *Client, opts options, w io.Writer) error {
	fs, rf := newReadFlags("query impact")
	upstream := fs.String("upstream", "", "")
	mode := fs.String("mode", "", "")
	maxHops, maxItems := fs.Int("max-hops", 0, ""), fs.Int("max-items", 0, "")
	budget := fs.String("time-budget", "", "")
	var dead, deadLinks, sites multiFlag
	fs.Var(&dead, "dead", "")
	fs.Var(&deadLinks, "dead-link", "")
	fs.Var(&sites, "site-failure", "")
	positional, err := parseMixed(fs, opts.rest[2:])
	if err != nil {
		return err
	}
	dead = append(dead, positional...)
	if (*upstream != "") == (len(dead)+len(deadLinks)+len(sites) > 0) {
		return usageError{msg: "query impact needs failed objects (ids or --dead, --dead-link, --site-failure), or --upstream <id>"}
	}
	body := rf.body()
	if *upstream != "" {
		body["direction"], body["target_object_id"] = "upstream", *upstream
	}
	for name, list := range map[string]multiFlag{"dead": dead, "dead_links": deadLinks, "site_failure": sites} {
		if len(list) > 0 {
			body[name] = []string(list)
		}
	}
	for name, value := range map[string]string{"mode": *mode, "time_budget": *budget} {
		if value != "" {
			body[name] = value
		}
	}
	for name, value := range map[string]int{"max_hops": *maxHops, "max_items": *maxItems} {
		if value > 0 {
			body[name] = value
		}
	}
	env, err := c.Post(ctx, kgPath(rf.kg)+"/queries:impact", body)
	if err != nil {
		return err
	}
	return printEnvelope(w, env, opts.asJSON, printImpactData)
}

// printImpactData prints an answer: the summary first, then the objects by kind of impact, each with its path.
func printImpactData(w io.Writer, data map[string]any) error {
	if t, ok := data["target"].(map[string]any); ok {
		fmt.Fprintf(w, "depends on, for %s  %s\n", scalar(t["object_id"]), scalar(t["display_name"]))
	}
	for _, s := range rowsOf(data["seeds"]) {
		fmt.Fprintf(w, "failed  %s  %s\n", scalar(s["object_id"]), scalar(s["display_name"]))
	}
	if up, ok := data["upstream"].(map[string]any); ok {
		printUpstream(w, up)
	}
	for _, key := range []string{"facts_only", "with_assumptions"} {
		if sec, ok := data[key].(map[string]any); ok {
			printImpactSection(w, strings.ReplaceAll(key, "_", " "), sec)
		}
	}
	if comp, ok := data["computation"].(map[string]any); ok {
		printComputation(w, comp)
	}
	return nil
}

var impactOrder = []string{"down", "interrupted", "unknown", "exposed_direct", "exposed_inherited", "degraded", "unmanageable"}

func printImpactSection(w io.Writer, title string, sec map[string]any) {
	counts, _ := sec["counts_by_result"].(map[string]any)
	parts := []string{}
	for _, k := range impactOrder {
		if n, ok := counts[k].(float64); ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%s %g", k, n))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "nothing changes")
	}
	fmt.Fprintf(w, "\n== %s: %s\n", title, strings.Join(parts, ", "))
	if n, _ := sec["unfilled_channel_objects"].(float64); n > 0 {
		fmt.Fprintf(w, "channel-empty objects %g (a needed channel has no supplier link; not counted as unknown)\n", n)
	}
	if rows := rowsOf(sec["counts_by_type"]); len(rows) > 0 {
		fmt.Fprintln(w, "\nby type")
		_ = printTable(w, rows, []string{"object_type", "count"})
	}
	if rows := rowsOf(sec["groups_touched"]); len(rows) > 0 {
		fmt.Fprintln(w, "\nredundancy groups touched")
		_ = printTable(w, rows, []string{"display_name", "group_type", "mode", "value", "alive", "unknown", "n", "min_alive", "slack"})
	}
	items := rowsOf(sec["items"])
	for _, kind := range impactOrder {
		printImpactGroup(w, kind, items)
	}
}

func printImpactGroup(w io.Writer, kind string, items []map[string]any) {
	var rows []map[string]any
	for _, it := range items {
		if it["result"] == kind {
			rows = append(rows, it)
		}
	}
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s (%d shown)\n", kind, len(rows))
	for _, it := range rows {
		extra := ""
		if ch := sliceOf(it["lost_channels"]); len(ch) > 0 {
			extra += "  lost " + scalar(ch)
		}
		if ch := sliceOf(it["unfilled_channels"]); len(ch) > 0 {
			extra += "  unfilled " + scalar(ch)
		}
		if it["assumption_based"] == true {
			extra += "  assumption-based"
		}
		fmt.Fprintf(w, "  %s  %s  %s  hops %s%s\n", scalar(it["object_id"]), scalar(it["object_type"]), scalar(it["display_name"]), scalar(it["hops"]), extra)
		if path := pathText(it["path"]); path != "" {
			fmt.Fprintf(w, "    path: %s\n", path)
		}
	}
}

// pathText writes a path as names joined by the link types that carried the failure.
func pathText(v any) string {
	var b strings.Builder
	for _, step := range rowsOf(v) {
		name := scalar(step["display_name"])
		if step["hidden"] == true {
			name = "(hidden)"
		}
		if link, ok := step["link"].(map[string]any); ok {
			fmt.Fprintf(&b, " -[%s]-> ", scalar(link["link_type"]))
		}
		b.WriteString(name)
	}
	return b.String()
}

func printUpstream(w io.Writer, up map[string]any) {
	fmt.Fprintf(w, "\n== upstream: %s objects, %s single points of failure\n", scalar(up["total"]), scalar(up["single_points_of_failure"]))
	if rows := rowsOf(up["counts_by_type"]); len(rows) > 0 {
		fmt.Fprintln(w, "\nby type")
		_ = printTable(w, rows, []string{"object_type", "count"})
	}
	for _, it := range rowsOf(up["items"]) {
		spof := ""
		if it["single_point_of_failure"] == true {
			spof = "  SINGLE POINT OF FAILURE"
		}
		effects, _ := it["effects"].(map[string]any)
		fmt.Fprintf(w, "\n  %s  %s  %s  hops %s%s\n    alone it causes: %s\n", scalar(it["object_id"]), scalar(it["object_type"]),
			scalar(it["display_name"]), scalar(it["hops"]), spof, effectsText(effects))
		if path := pathText(it["path"]); path != "" {
			fmt.Fprintf(w, "    path: %s\n", path)
		}
	}
}

func effectsText(effects map[string]any) string {
	parts := make([]string, 0, len(effects))
	for k, v := range effects {
		parts = append(parts, k+"="+scalar(v))
	}
	if len(parts) == 0 {
		return "-"
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func printComputation(w io.Writer, comp map[string]any) {
	state := "complete"
	if comp["complete"] != true {
		state = "NOT complete, stopped by " + scalar(comp["stopped_by"])
	}
	fmt.Fprintf(w, "\ncomputation: %s; rounds %s; weak links %s; stale inputs %s\n", state, scalar(comp["rounds"]), scalar(comp["weak_links"]), scalar(comp["stale_inputs"]))
	if comp["items_over_cap"] == true {
		fmt.Fprintf(w, "items over the cap of %s; raise --max-items or narrow the question\n", scalar(comp["max_items"]))
	}
}
