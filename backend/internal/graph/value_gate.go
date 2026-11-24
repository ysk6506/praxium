package graph

import (
	"math/big"
	"regexp"
	"strconv"
	"time"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// MaxNameBytes is the longest name of an object, in bytes. The name has two indexes in the table of its type and a row of an index
// has to fit in a page of the database (about 2,700 bytes after compression), so a name that is not kept short would stop the
// write of its batch (ADR-0101).
const MaxNameBytes = 1024

// The value check of the graph is the one gate for the values of the store: a value that passes it is a value that the column of
// its kind takes and gives back as it is (ADR-0101). The reading of a source (ingest) uses the same functions, so a value that
// cannot be kept is a problem of its record and not a failure of the whole batch.

// maxNumberText is the longest text of a number or a quantity.
const maxNumberText = 400

var (
	// decimalText is a decimal number written out: what a column of the type numeric takes (an exponent, a hexadecimal fraction, a
	// name like NaN or Infinity are not in it).
	decimalText = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)$`)
	// floatText is a decimal number that may end in a power of ten of at most four digits.
	floatText = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]{1,4})?$`)
)

const exactIntegers = 1 << 53 // every whole number up to this is a float64 as it is

// NumberFits says why a value is not one that the column of a kind of number holds as it is, or is nil. A number (double
// precision) is a finite float64 that stands for the decimal it was written as: a text that is more exact than a float64 (more
// than 17 digits, a whole number past 2^53), or too large or too small for one (1e400, 1e-400), is refused, and the record is
// asked to use the kind integer or quantity. A quantity is a decimal text (numeric) or an integer. A map of a quantity (a value
// and a unit) is not a number: it is checked where it is allowed.
func NumberFits(kind model.BaseKind, val any) error {
	switch x := val.(type) {
	case int64:
		if kind == model.BaseNumber && (x > exactIntegers || x < -exactIntegers) {
			return kernel.Errorf(kernel.CodeInvalidValue, "%d has more digits than a number keeps exactly: use the kind integer or quantity", x)
		}
		return nil
	case string:
		if len(x) > maxNumberText {
			return kernel.Errorf(kernel.CodeInvalidValue, "a number of %d characters is too long", len(x))
		}
		if kind == model.BaseQuantity {
			if !decimalText.MatchString(x) {
				return kernel.Errorf(kernel.CodeInvalidValue, "%q is not a decimal number", x)
			}
			return nil
		}
		return floatFits(x)
	}
	return kernel.Errorf(kernel.CodeInvalidValue, "%v is not a number", val)
}

func floatFits(text string) error {
	if !floatText.MatchString(text) {
		return kernel.Errorf(kernel.CodeInvalidValue, "%q is not a decimal number", text)
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return kernel.Errorf(kernel.CodeInvalidValue, "%q is out of the range of a number", text)
	}
	want, ok := new(big.Rat).SetString(text)
	got, _ := new(big.Rat).SetString(strconv.FormatFloat(f, 'f', -1, 64))
	if !ok || want.Cmp(got) != 0 {
		return kernel.Errorf(kernel.CodeInvalidValue, "%q is not kept exactly by a number (it becomes %v): use the kind integer or quantity", text, strconv.FormatFloat(f, 'g', -1, 64))
	}
	return nil
}

// TimeFits says why a text is not a time that the store keeps, or is nil: UTC, ending in Z, at most six digits of a second with no
// zero at the end of them (the form time.RFC3339Nano writes), in the years 1 to 9999 that the function of the views reads.
func TimeFits(text string) error {
	t, err := time.Parse(time.RFC3339, text)
	switch {
	case err != nil:
		return kernel.Errorf(kernel.CodeInvalidValue, "%q is not a time of the form 2006-01-02T15:04:05Z", text)
	case t.Year() < 1 || t.Year() > 9999:
		return kernel.Errorf(kernel.CodeInvalidValue, "%q is not in the years 1 to 9999", text)
	case t.Nanosecond()%1000 != 0 || t.UTC().Format(time.RFC3339Nano) != text:
		return kernel.Errorf(kernel.CodeInvalidValue, "%q is not a time in UTC ending in Z with at most six digits of a second and no zero at the end of them", text)
	}
	return nil
}
