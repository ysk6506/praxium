package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// printJSON writes a value as indented JSON.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// scalar writes a value as one table cell.
func scalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "-"
	case string:
		return x
	case float64:
		return fmt.Sprintf("%g", x)
	case bool:
		return fmt.Sprintf("%t", x)
	case int:
		return fmt.Sprintf("%d", x)
	case map[string]any:
		return mapText(x)
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				parts = append(parts, "("+mapText(m)+")")
				continue
			}
			parts = append(parts, scalar(item))
		}
		return strings.Join(parts, ",")
	}
	return "{...}"
}

// printTable writes rows as aligned columns. columns lists the wanted columns
// in order; without it the scalar fields of the first row are used.
func printTable(w io.Writer, rows []map[string]any, columns []string) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "(none)")
		return err
	}
	if len(columns) == 0 {
		for name, v := range rows[0] {
			switch v.(type) {
			case map[string]any:
			default:
				columns = append(columns, name)
			}
		}
		sort.Strings(columns)
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.ToUpper(strings.Join(columns, "\t")))
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, c := range columns {
			cells[i] = scalar(row[c])
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// printFields writes one object as name and value lines.
func printFields(w io.Writer, obj map[string]any) error {
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, name := range names {
		for i, line := range fieldLines(name, obj[name]) {
			label := name
			if i > 0 {
				label = ""
			}
			fmt.Fprintf(tw, "%s\t%s\n", label, line)
		}
	}
	return tw.Flush()
}
