package xlsx

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/ingest"
	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// attr is the value of an attribute by its local name, whatever namespace prefix it has: the same workbook is written
// with different prefixes by different tools.
func attr(e xml.StartElement, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// eofOrError is nil at the end of a part.
func eofOrError(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// xmlDecoder reads the XML of a part through the guard that bounds its tokens and its depth (xmlguard.go).
func xmlDecoder(part string, r io.Reader) *xml.Decoder { return xml.NewDecoder(newXMLGuard(part, r)) }

// parseRels reads a relationships part.
func parseRels(part string, r io.Reader) (map[string]relationship, error) {
	dec := xmlDecoder(part, r)
	rels := map[string]relationship{}
	seen := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return rels, eofOrError(err)
		}
		e, ok := tok.(xml.StartElement)
		if !ok || e.Name.Local != "Relationship" {
			continue
		}
		if seen++; seen > maxRelationships {
			return nil, fixedLimit("max_relationships", maxRelationships, "by the part "+part)
		}
		rel := relationship{target: attr(e, "Target"), typ: attr(e, "Type"), external: strings.EqualFold(attr(e, "TargetMode"), "External")}
		id := attr(e, "Id")
		if err := firstError(
			checkLen("max_relationship_id_bytes", maxRelIDBytes, id, "a relationship of the part "+part),
			checkLen("max_relationship_type_bytes", maxRelTypeBytes, rel.typ, "a relationship of the part "+part),
			checkLen("max_relationship_target_bytes", maxRelTargetBytes, rel.target, "a relationship of the part "+part)); err != nil {
			return nil, err
		}
		rels[id] = rel
	}
}

// firstError is the first of the errors that is not nil.
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// sheetEntry is a sheet as the workbook part lists it.
type sheetEntry struct {
	name  string
	state string
	rid   string
}

// parseWorkbookPart reads the list of sheets and the date system of the workbook part.
func parseWorkbookPart(part string, r io.Reader) (sheets []sheetEntry, date1904 bool, err error) {
	dec := xmlDecoder(part, r)
	for {
		tok, err := dec.Token()
		if err != nil {
			return sheets, date1904, eofOrError(err)
		}
		e, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch e.Name.Local {
		case "workbookPr":
			date1904 = isTrue(attr(e, "date1904"))
		case "sheet":
			entry, err := sheetEntryOf(e, len(sheets))
			if err != nil {
				return nil, false, err
			}
			sheets = append(sheets, entry)
		}
	}
}

// sheetEntryOf reads a sheet of the list: the lists and the texts in them are within their limits.
func sheetEntryOf(e xml.StartElement, have int) (sheetEntry, error) {
	if have >= maxSheets {
		return sheetEntry{}, fixedLimit("max_sheets", maxSheets, "by the list of sheets of the workbook")
	}
	entry := sheetEntry{name: attr(e, "name"), state: attr(e, "state"), rid: ridOf(e)}
	err := firstError(
		checkLen("max_sheet_name_bytes", maxSheetNameBytes, entry.name, "a sheet name"),
		checkLen("max_sheet_state_bytes", maxSheetStateBytes, entry.state, "the state of a sheet"),
		checkLen("max_relationship_id_bytes", maxRelIDBytes, entry.rid, "the relationship of a sheet"))
	return entry, err
}

// ridOf is the relationship id of a sheet element: the r:id attribute, in whatever prefix the writer chose.
func ridOf(e xml.StartElement) string {
	for _, a := range e.Attr {
		if a.Name.Local == "id" && a.Name.Space != "" {
			return a.Value
		}
	}
	return ""
}

// isTrue reads a boolean attribute of a workbook: 1 and true.
func isTrue(v string) bool { return v == "1" || strings.EqualFold(v, "true") }

// stateOf maps the state of a sheet to the names of the pipeline.
func stateOf(s string) string {
	switch s {
	case "hidden":
		return ingest.SheetHidden
	case "veryHidden":
		return ingest.SheetVeryHidden
	}
	return ingest.SheetVisible
}

// workbook reads the whole workbook of the archive.
func (p *pkg) workbook() (*ingest.Workbook, error) {
	main, err := p.mainPart()
	if err != nil {
		return nil, err
	}
	f := p.part(main)
	rc, err := p.open(f)
	if err != nil {
		return nil, err
	}
	entries, date1904, err := parseWorkbookPart(main, rc)
	rc.Close()
	if err != nil {
		return nil, damaged(main, err)
	}
	rels, err := p.relsOf(main)
	if err != nil {
		return nil, err
	}
	ctx, err := p.context(rels, date1904)
	if err != nil {
		return nil, err
	}
	wb := &ingest.Workbook{}
	for i, e := range entries {
		sh := ingest.Sheet{Name: e.name, Index: i + 1, State: stateOf(e.state), Kind: ingest.SheetKindWorksheet}
		rel, ok := rels[e.rid]
		if !ok || rel.external || !strings.HasSuffix(rel.typ, "/worksheet") {
			sh.Kind = "other"
			wb.Sheets = append(wb.Sheets, sh)
			continue
		}
		if err := p.readSheet(&sh, rel.target, ctx); err != nil {
			return nil, err
		}
		wb.Sheets = append(wb.Sheets, sh)
	}
	return wb, nil
}

// mainPart finds the part that is the workbook, or says why the archive is no workbook this build reads.
func (p *pkg) mainPart() (string, error) {
	if f := p.part("_rels/.rels"); f != nil {
		rc, err := p.open(f)
		if err != nil {
			return "", err
		}
		rels, err := parseRels(f.Name, rc)
		rc.Close()
		if err != nil {
			return "", damaged(f.Name, err)
		}
		for _, r := range rels {
			if strings.HasSuffix(r.typ, "/officeDocument") && !r.external && p.part(resolve("", r.target)) != nil {
				return p.part(resolve("", r.target)).Name, nil
			}
		}
	}
	switch {
	case p.part("xl/workbook.xml") != nil:
		return p.part("xl/workbook.xml").Name, nil
	case p.part("xl/workbook.bin") != nil:
		return "", kernel.Errorf(kernel.CodeUnsupported, "the file is a binary workbook (.xlsb), which this build cannot read: save it again as .xlsx")
	case p.part("mimetype") != nil:
		return "", kernel.Errorf(kernel.CodeUnsupported, "the file is an OpenDocument spreadsheet (.ods), which this build cannot read: save it again as .xlsx")
	}
	return "", kernel.Errorf(kernel.CodeInvalidValue, "the ZIP archive is no spreadsheet workbook: it has no xl/workbook.xml")
}

// sheetContext is what reading a sheet needs of the rest of the workbook.
type sheetContext struct {
	strings  []string
	kinds    []int
	date1904 bool
}

// context reads the shared strings and the styles the sheets refer to.
func (p *pkg) context(rels map[string]relationship, date1904 bool) (sheetContext, error) {
	ctx := sheetContext{date1904: date1904}
	if name := byType(rels, "/sharedStrings"); name != "" {
		if f := p.part(name); f != nil {
			rc, err := p.open(f)
			if err != nil {
				return ctx, err
			}
			ctx.strings, err = readSharedStrings(xmlDecoder(f.Name, rc), p.lim.MaxSharedStrings, p.lim.MaxCellChars)
			rc.Close()
			if err != nil {
				return ctx, damaged(f.Name, err)
			}
		}
	}
	if name := byType(rels, "/styles"); name != "" {
		if f := p.part(name); f != nil {
			rc, err := p.open(f)
			if err != nil {
				return ctx, err
			}
			ctx.kinds, err = dateKinds(xmlDecoder(f.Name, rc))
			rc.Close()
			if err != nil {
				return ctx, damaged(f.Name, err)
			}
		}
	}
	return ctx, nil
}
