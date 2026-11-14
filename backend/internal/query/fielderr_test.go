package query

import (
	"errors"
	"testing"
)

// [REQ-290]
func TestAFieldErrorKeepsItsMessageAndNamesTheField(t *testing.T) {
	err := inputf("object_type", "no object type %q", "X")
	if FieldOf(err) != "object_type" || err.Error() == "" {
		t.Fatalf("%v %q", err, FieldOf(err))
	}
	if FieldOf(errors.New("plain")) != "" {
		t.Error("an error without a field names none")
	}
	var fi *fieldInput
	if !errors.As(err, &fi) || fi.Unwrap() == nil {
		t.Error("the underlying kernel error is reachable")
	}
}
