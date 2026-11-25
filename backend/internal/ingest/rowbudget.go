package ingest

import "strconv"

// RowBytesFactor times the bytes a read has taken in is the most the cells of the rows made from it may hold. The rows of a path that
// opens an array are in one bound with the outer record read by ^. (a value of the outer record is in every row of its elements), so
// a small input can make a very large output; the bound stops that. HTTP bounds a response by max_response_bytes (ADR-0109), and a
// file by the bytes of the file read so far plus DefaultPageBytes (ADR-0109, N1). Both use RowsOver: there is one bound, not two.
const RowBytesFactor = 4

// RowsOver says whether the cells of the rows made so far are more than RowBytesFactor times base bytes plus fixed bytes.
func RowsOver(cells, base, fixed int) bool {
	return cells > RowBytesFactor*base+fixed
}

// CellBytes is the bytes of the natural keys and the cells of the rows of a table (the names of the cells count too): the measure
// RowsOver bounds.
func CellBytes(t Table) int {
	n := 0
	for _, r := range t.Rows {
		n += len(r.Key)
		for k, v := range r.Fields {
			n += len(k) + len(v)
		}
	}
	return n
}

// rowBytesError says that the rows made from the lines read are more than RowBytesFactor times the bytes read, plus DefaultPageBytes.
type rowBytesError struct{}

func (e *rowBytesError) Error() string {
	return "the rows made from the lines read are more than " + strconv.Itoa(RowBytesFactor) + " times the bytes read plus " +
		strconv.Itoa(DefaultPageBytes) + " bytes (a value of the outer record, read as ^., is in every row of its elements); the rest was not read"
}
