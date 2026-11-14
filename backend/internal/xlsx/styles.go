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
				if id, err := strconv.Atoi(attr(t, "numFmtId")); err == nil {
					codes[id] = attr(t, "formatCode")
				}
			case "cellXfs":
				inXfs = true
			case "xf":
				if inXfs {
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
