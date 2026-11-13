package model

import (
	"strconv"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Value types (kg-model.md 3.3). The ten base kinds are built in, one value
// type each, with the id "vt." and the kind. Named value types are listed in
// the data and need an implementation here: value types are the product's,
// so no definition can add one (ADR-0007 17). A value type checks a value and
// tidies it; identity matches on the tidied form (the stored value stays as
// received), so a rule is never written twice.

func valueTypeID(raw string) ValueTypeID {
	id, _ := kernel.ParseID[ValueTypeKind](raw)
	return id
}

// BaseValueTypeID returns the id of the built-in value type of a base kind.
func BaseValueTypeID(kind BaseKind) ValueTypeID { return valueTypeID("vt." + string(kind)) }

func builtinValueTypes() []ValueType {
	out := make([]ValueType, 0, len(BaseKinds()))
	for _, kind := range BaseKinds() {
		out = append(out, ValueType{ID: BaseValueTypeID(kind), Name: string(kind), Base: kind})
	}
	return out
}

// valueRules holds the check and the tidying of each named value type, by id.
var valueRules = map[string]func(string) (string, error){
	"vt.ipv4_address":  normalizeIPv4,
	"vt.mac_address":   normalizeMAC,
	"vt.serial_number": normalizeSerial,
	"vt.fqdn":          normalizeFQDN,
}

// NormalizeValue checks a text value against a value type and returns it in
// its tidy form. A built-in value type, which has no rule, returns the value
// as it is.
func NormalizeValue(id ValueTypeID, value string) (string, error) {
	rule, ok := valueRules[id.String()]
	if !ok {
		return value, nil
	}
	return rule(value)
}

func invalidValue(kind, value string) error {
	return kernel.Errorf(CodeValueInvalid, "%q is not a valid %s", value, kind)
}

// normalizeIPv4 wants four dotted decimal numbers of 0 to 255 and drops leading zeros.
func normalizeIPv4(value string) (string, error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 4 {
		return "", invalidValue("ipv4_address", value)
	}
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || number > 255 || !allDigits(part) {
			return "", invalidValue("ipv4_address", value)
		}
		parts[i] = strconv.Itoa(number)
	}
	return strings.Join(parts, "."), nil
}

func allDigits(text string) bool {
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return text != ""
}

// normalizeMAC wants twelve hexadecimal digits, with or without separators,
// and returns them in lower case without separators.
func normalizeMAC(value string) (string, error) {
	tidy := strings.ToLower(strings.NewReplacer(":", "", "-", "", ".", "").Replace(strings.TrimSpace(value)))
	if len(tidy) != 12 {
		return "", invalidValue("mac_address", value)
	}
	for _, r := range tidy {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", invalidValue("mac_address", value)
		}
	}
	return tidy, nil
}

// normalizeSerial trims white space at both ends and puts the text in upper
// case. It must not end up empty. This is the identity-key normaliser: the
// value a source gave is stored as received and only compared in this form.
func normalizeSerial(value string) (string, error) {
	tidy := strings.ToUpper(strings.TrimSpace(value))
	if tidy == "" {
		return "", invalidValue("serial_number", value)
	}
	return tidy, nil
}

// normalizeFQDN lowers the case, drops one dot at the end and checks the
// labels: 1 to 63 letters, digits or hyphens, none starting or ending with a hyphen.
func normalizeFQDN(value string) (string, error) {
	tidy := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if tidy == "" || len(tidy) > 253 {
		return "", invalidValue("fqdn", value)
	}
	for _, label := range strings.Split(tidy, ".") {
		if !validLabel(label) {
			return "", invalidValue("fqdn", value)
		}
	}
	return tidy, nil
}

func validLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
