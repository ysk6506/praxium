package main

import (
	"io"
	"os"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The values of a secret come in by a file or by the standard input, never by an argument. Nothing here puts a value in
// a message: the messages name fields and say what to do.

const (
	// maxSecretValueRead is the most that is read for one value: the unit's limit of a value, with room for the line
	// break; a longer one is read whole to its limit and refused by the unit with the words it has for it.
	maxSecretValueRead = 16<<10 + 8
	maxSecretStdin     = 64 << 10
)

// readSecretFields collects the fields of a secret: from the -field files when any is given (then every field of the
// kind has to be), else from the standard input, one line per field in the order of the kind.
func readSecretFields(fieldArgs, names []string, w wiring) (secrets.Fields, error) {
	var values map[string]string
	var err error
	if len(fieldArgs) > 0 {
		values, err = readFieldFiles(fieldArgs)
	} else {
		values, err = readFieldLines(names, w)
	}
	if err != nil {
		return secrets.Fields{}, err
	}
	return secrets.NewFields(values), nil
}

// checkFieldArgs makes sure every -field is <name>=<file>. The text is not repeated: it may be the value itself, given
// by mistake.
func checkFieldArgs(args []string) error {
	for _, arg := range args {
		name, path, ok := strings.Cut(arg, "=")
		if !ok || name == "" || path == "" {
			return kernel.Errorf(kernel.CodeInvalidValue, "a -field is not <name>=<file>: it wants the name of the field, an equals sign and the name of a file that holds the value, never the value itself")
		}
	}
	return nil
}

func readFieldFiles(args []string) (map[string]string, error) {
	values := map[string]string{}
	for _, arg := range args {
		name, path, _ := strings.Cut(arg, "=")
		if _, dup := values[name]; dup {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "-field %s is given twice", safeName(name))
		}
		value, err := readValueFile(name, path)
		if err != nil {
			return nil, err
		}
		values[name] = value
	}
	return values, nil
}

// safeName is a field name as it may be put in a message: the name when it is short and plain, else a placeholder.
func safeName(name string) string {
	if len(name) > 40 {
		return "(a field)"
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return "(a field)"
		}
	}
	return name
}

// readValueFile reads the file of one field. The path is not put in a message: a value typed in its place by mistake
// would be repeated by it.
func readValueFile(name, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", kernel.Errorf(kernel.CodeInvalidValue, "-field %s: the file cannot be opened; -field wants the name of a file that holds the value, never the value itself", safeName(name))
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxSecretValueRead))
	if err != nil {
		return "", kernel.Errorf(kernel.CodeInvalidValue, "-field %s: the file cannot be read", safeName(name))
	}
	text := string(raw)
	zero(raw)
	text = strings.TrimSuffix(text, "\n")
	return strings.TrimSuffix(text, "\r"), nil
}

// readFieldLines reads one line per field from the standard input. A terminal is not read: what is typed would show
// on the screen, and the standard library has no way to hide it.
func readFieldLines(names []string, w wiring) (map[string]string, error) {
	in := w.stdin
	if in == nil {
		in = strings.NewReader("")
	}
	if f, ok := in.(*os.File); ok {
		if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			return nil, kernel.Errorf(kernel.CodeInvalidValue,
				"the standard input is a terminal (or another device), which would show the value on the screen; pipe it in (one line per field: %s) or give a file for every field with -field <name>=<file>", strings.Join(names, ", "))
		}
	}
	raw, err := io.ReadAll(io.LimitReader(in, maxSecretStdin+1))
	if err != nil {
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the standard input cannot be read")
	}
	if len(raw) > maxSecretStdin {
		zero(raw)
		return nil, kernel.Errorf(kernel.CodeInvalidValue, "the standard input is longer than %d bytes", maxSecretStdin)
	}
	text := strings.TrimSuffix(string(raw), "\n")
	zero(raw)
	lines := strings.Split(text, "\n")
	if text == "" || len(lines) != len(names) {
		return nil, kernel.Errorf(kernel.CodeInvalidValue,
			"the values come one line per field, in this order: %s (%d line(s)); the standard input has %d. Or give a file for every field with -field <name>=<file>", strings.Join(names, ", "), len(names), lineCount(text))
	}
	values := make(map[string]string, len(names))
	for i, name := range names {
		values[name] = strings.TrimSuffix(lines[i], "\r")
	}
	return values, nil
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}
