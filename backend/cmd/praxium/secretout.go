package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// What the secret commands print. A Meta has no value to print; the key id is the id of the master key that sealed
// the secret (it is no secret either).

// secretView is a secret as shown: its meta, the id of the key that sealed it and, when this command made the master
// key file, where it is.
type secretView struct {
	secrets.Meta
	KeyID       string `json:"key_id,omitempty"`
	KeyFileMade string `json:"master_key_file_made,omitempty"`
}

func (s *secretStore) viewOf(ctx context.Context, meta secrets.Meta) secretView {
	v := secretView{Meta: meta}
	if rec, ok, err := s.store.Get(ctx, meta.ID); err == nil && ok {
		v.KeyID = rec.Binding.KeyID
	}
	if s.keys != nil {
		v.KeyFileMade = s.keys.made
	}
	return v
}

func joinOr(list []string, none string) string {
	if len(list) == 0 {
		return none
	}
	return strings.Join(list, ", ")
}

func addressTexts(list []secrets.Address) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		host := a.Host
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		out = append(out, host+":"+strconv.Itoa(a.Port))
	}
	return out
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func versionSummary(m secrets.Meta) string {
	retired := 0
	for _, v := range m.Versions {
		if v.RetiredAt != nil {
			retired++
		}
	}
	if retired == 0 {
		return strconv.Itoa(len(m.Versions))
	}
	return fmt.Sprintf("%d (%d retired)", len(m.Versions), retired)
}

func printSecretTable(out io.Writer, metas []secrets.Meta) {
	if len(metas) == 0 {
		fmt.Fprintln(out, "no secrets")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tKIND\tIN USE\tVERSIONS\tSCOPE\tADDRESSES")
	for _, m := range metas {
		inUse := "none (all retired)"
		if m.Version > 0 {
			inUse = "v" + strconv.Itoa(m.Version)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", m.ID, m.Kind, inUse, versionSummary(m), joinOr(m.Scope, "-"), joinOr(addressTexts(m.AllowedHosts), "-"))
	}
	_ = tw.Flush()
}

func printSecretDetail(out io.Writer, v secretView) {
	fmt.Fprintf(out, "id:        %s\nkind:      %s\n", v.ID, v.Kind)
	fmt.Fprintf(out, "fields:    %s (the values are never shown)\n", strings.Join(v.FieldNames, ", "))
	if v.Version > 0 {
		fmt.Fprintf(out, "in use:    version %d\n", v.Version)
	} else {
		fmt.Fprintln(out, "in use:    none: every version is retired, the secret cannot be opened")
	}
	fmt.Fprintln(out, "versions:")
	for _, ver := range v.Versions {
		line := fmt.Sprintf("  %d  created %s", ver.Version, stamp(ver.CreatedAt))
		if ver.RetiredAt != nil {
			line += "  retired " + stamp(*ver.RetiredAt)
		}
		fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "scope:     %s\n", joinOr(v.Scope, "(no source may use it)"))
	fmt.Fprintf(out, "addresses: %s\n", joinOr(addressTexts(v.AllowedHosts), "(no address allowed)"))
	fmt.Fprintf(out, "key id:    %s\ncreated:   %s\n", v.KeyID, stamp(v.CreatedAt))
}

func putSentence(m secrets.Meta) string {
	s := "put the secret " + m.ID + " in (" + string(m.Kind) + "): version 1; fields " + strings.Join(m.FieldNames, ", ") + " (the values are never shown)"
	if len(m.Scope) == 0 || len(m.AllowedHosts) == 0 {
		s += "\nnobody may use it yet: say which sources and which addresses with praxium secret set-scope and set-hosts"
	}
	return s
}

func rotateSentence(m secrets.Meta) string {
	return fmt.Sprintf("added version %d to the secret %s: it is the one that is used from now on; the older versions stay until you retire them (praxium secret retire %s -version N)", m.Version, m.ID, m.ID)
}

func retireSentence(f secretFlags, m secrets.Meta) string {
	s := fmt.Sprintf("retired version %d of the secret %s: its encrypted value is deleted", f.version, m.ID)
	if m.Version == 0 {
		return s + "; no version is left, the secret cannot be opened"
	}
	return fmt.Sprintf("%s; version %d is in use", s, m.Version)
}

func printSecretChange(ctx context.Context, out io.Writer, st *secretStore, f secretFlags, meta secrets.Meta, sentence string) error {
	view := st.viewOf(ctx, meta)
	if f.asJSON {
		return printJSON(out, view)
	}
	fmt.Fprintln(out, sentence)
	if view.KeyFileMade != "" {
		fmt.Fprintf(out, "made the master key file %s (key id %s): keep a copy of it somewhere safe; the secrets cannot be opened without it\n", view.KeyFileMade, view.KeyID)
	}
	return nil
}

func printSecretDone(out io.Writer, asJSON bool, v any, sentence string) error {
	if asJSON {
		return printJSON(out, v)
	}
	_, err := fmt.Fprintln(out, sentence)
	return err
}
