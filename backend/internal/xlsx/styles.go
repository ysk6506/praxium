package xlsx

import (
	"encoding/xml"
	"strconv"
)

// dateKinds reads the styles of a workbook and gives, for every cell style (the s attribute of a cell), whether a
// number in it is shown as a date: the number format of the style is a built-in date format or a custom one whose
// code has the letters of a date or a time.
func dateKinds(dec *xml.Decoder) ([]int, error) {
	codes := map[int]string{}
	var kinds []int
	formats := 0
	inXfs := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return kinds, eofOrError(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "numFmt":
				if err := numFormat(t, codes, &formats); err != nil {
					return nil, err
				}
			case "cellXfs":
				inXfs = true
			case "xf":
				if inXfs {
					if len(kinds) >= maxStyles {
						return nil, fixedLimit("max_styles", maxStyles, "by the cell styles of the workbook")
					}
					id, _ := strconv.Atoi(attr(t, "numFmtId"))
					kinds = append(kinds, kindOfFormat(id, codes))
				}
			}
		case xml.EndElement:
			if t.Name.Local == "cellXfs" {
				return kinds, nil
			}
		}
	}
}

// kindOfFormat is the date kind of a number format id: a custom code is read, a built-in id is looked up.
func kindOfFormat(id int, codes map[int]string) int {
	if code, ok := codes[id]; ok {
		return formatDateKind(code)
	}
	return builtinDateKind(id)
}

// numFormat keeps a number format of the styles part: the list and the code of each are within their limits.
func numFormat(e xml.StartElement, codes map[int]string, count *int) error {
	if *count++; *count > maxNumberFormats {
		return fixedLimit("max_number_formats", maxNumberFormats, "by the styles of the workbook")
	}
	code := attr(e, "formatCode")
	if err := checkLen("max_number_format_bytes", maxFormatCodeBytes, code, "a number format"); err != nil {
		return err
	}
	if id, err := strconv.Atoi(attr(e, "numFmtId")); err == nil {
		codes[id] = code
	}
	return nil
}
