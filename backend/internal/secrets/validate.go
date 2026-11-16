package secrets

import (
	"net/netip"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

const (
	maxIDLen    = 64
	maxValueLen = 16 << 10
	maxListLen  = 256
	maxHostLen  = 253
	maxLabelLen = 63
	maxVersions = 1000
)

// ValidateID checks the syntax of a secret id (and of a master key id): a lower-case letter first, then lower-case
// letters, digits, _ and -, up to 64 characters.
func ValidateID(id string) error {
	if !validName(id) {
		return kernel.Errorf(kernel.CodeInvalidValue, "invalid secret id: use 1-%d characters, a lower-case letter first, then lower-case letters, digits, _ and -", maxIDLen)
	}
	return nil
}

func validName(id string) bool {
	if id == "" || len(id) > maxIDLen || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// checkFields makes sure fields has exactly the fields of the kind, each non-empty. The message names fields, never values.
func checkFields(kind Kind, fields Fields) error {
	names, ok := kindFields[kind]
	if !ok {
		return kernel.Errorf(kernel.CodeInvalidValue, "unknown secret kind %q: use password or token", string(kind))
	}
	for _, name := range fields.Names() {
		if !contains(names, name) {
			return kernel.Errorf(kernel.CodeInvalidValue, "a %s secret has no field %q; its fields are %s", kind, name, strings.Join(names, ", "))
		}
	}
	for _, name := range names {
		v, present := fields.values()[name]
		switch {
		case !present:
			return kernel.Errorf(kernel.CodeInvalidValue, "a %s secret needs the field %q", kind, name)
		case v == "":
			return kernel.Errorf(kernel.CodeInvalidValue, "the field %q is empty", name)
		case len(v) > maxValueLen:
			return kernel.Errorf(kernel.CodeInvalidValue, "the field %q is longer than %d bytes", name, maxValueLen)
		case !utf8.ValidString(v):
			return kernel.Errorf(kernel.CodeInvalidValue, "the field %q is not valid UTF-8 text", name)
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type sourceMarker struct{}

// normalizeScope checks the source ids and returns them sorted without repeats.
func normalizeScope(ids []string) ([]string, error) {
	if len(ids) > maxListLen {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a scope lists at most %d sources", maxListLen)
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, err := kernel.ParseID[sourceMarker](id); err != nil {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "invalid source id %q in the scope", id)
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return compact(out), nil
}

func compact[T comparable](sorted []T) []T {
	out := sorted[:0]
	for i, x := range sorted {
		if i == 0 || x != sorted[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// NormalizeHost gives the one form of a host that is compared and stored. The rules:
//   - an IP address is parsed and written in its canonical form: IPv6 in lower case, compressed, with or without
//     the brackets of a URL (they are removed); an IPv4-mapped IPv6 address is the IPv4 address it maps. A zone
//     (fe80::1%eth0) and an IPv4 address with leading zeros are refused.
//   - a name is ASCII (an international name comes as its xn-- form), compared in lower case, and one dot at the
//     end is the same name without it. A label is 1 to 63 letters, digits, _ and -, not starting or ending with -.
//     A name whose last label is a number (127.1, 0x7f.1) is refused: a resolver may read it as an address.
//   - an empty host, a host with a port, a path, a space or a wildcard is refused.
func NormalizeHost(raw string) (string, error) {
	bad := kernel.Errorf(kernel.CodeInvalidValue, "invalid host %q", raw)
	if raw == "" || len(raw) > maxHostLen+2 {
		return "", bad
	}
	if strings.HasPrefix(raw, "[") {
		return normalizeBracketed(raw, bad)
	}
	if strings.ContainsAny(raw, ":%") {
		return normalizeIP(raw, bad)
	}
	if addr, err := netip.ParseAddr(raw); err == nil {
		return addr.Unmap().String(), nil
	}
	return normalizeName(raw, bad)
}

func normalizeBracketed(raw string, bad error) (string, error) {
	if !strings.HasSuffix(raw, "]") || len(raw) < 3 {
		return "", bad
	}
	inner := raw[1 : len(raw)-1]
	if addr, err := netip.ParseAddr(inner); err != nil || addr.Is4() {
		return "", bad
	}
	return normalizeIP(inner, bad)
}

func normalizeIP(raw string, bad error) (string, error) {
	addr, err := netip.ParseAddr(raw)
	if err != nil || addr.Zone() != "" || strings.Contains(raw, "%") {
		return "", bad
	}
	return addr.Unmap().String(), nil
}

func normalizeName(raw string, bad error) (string, error) {
	name := strings.TrimSuffix(raw, ".")
	if name == "" || len(name) > maxHostLen {
		return "", bad
	}
	name = asciiLower(name)
	labels := strings.Split(name, ".")
	for _, label := range labels {
		if !validLabel(label) {
			return "", bad
		}
	}
	if looksNumeric(labels[len(labels)-1]) {
		return "", bad
	}
	return name, nil
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func validLabel(label string) bool {
	if label == "" || len(label) > maxLabelLen || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// looksNumeric is a label of digits, or 0x and hexadecimal digits: a form some resolvers read as part of an address.
func looksNumeric(label string) bool {
	digits := label
	hex := strings.HasPrefix(label, "0x")
	if hex {
		digits = label[2:]
	}
	if digits == "" {
		return false
	}
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if !(c >= '0' && c <= '9' || hex && c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func normalizeAddress(a Address) (Address, error) {
	host, err := NormalizeHost(a.Host)
	if err != nil {
		return Address{}, err
	}
	if a.Port < 1 || a.Port > 65535 {
		return Address{}, kernel.Errorf(kernel.CodeInvalidValue, "invalid port %d: use 1 to 65535", a.Port)
	}
	return Address{Host: host, Port: a.Port}, nil
}

// normalizeAddresses checks the addresses and returns them sorted without repeats.
func normalizeAddresses(list []Address) ([]Address, error) {
	if len(list) > maxListLen {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "a secret lists at most %d addresses", maxListLen)
	}
	out := make([]Address, 0, len(list))
	for _, a := range list {
		n, err := normalizeAddress(a)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Port < out[j].Port
	})
	return compact(out), nil
}

// checkUse validates the parts of a use and returns the address in its normal form.
func checkUse(use Use) (Address, error) {
	if _, err := kernel.ParseID[sourceMarker](use.SourceID); err != nil {
		return Address{}, kernel.Errorf(kernel.CodeInvalidValue, "invalid source id %q in the use", use.SourceID)
	}
	switch use.Purpose {
	case PurposeRun, PurposeCheck, PurposeDiscover:
	default:
		return Address{}, kernel.Errorf(kernel.CodeInvalidValue, "unknown purpose %q: use run, check or discover", string(use.Purpose))
	}
	return normalizeAddress(Address{Host: use.Host, Port: use.Port})
}
