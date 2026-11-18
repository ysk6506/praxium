package main

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/secrets"
)

// The values of a secret come in by a file or by the standard input, never by an argument. Nothing here puts a value in
// a message: the messages name fields (only the names a field of a kind has) and say what to do.

const (
	// maxSecretValueRead is the most that is read for one value: the unit's limit of a value, with room for the line
	// break.
	maxSecretValueRead = 16<<10 + 8
	maxSecretStdin     = 64 << 10
	bom                = "\xef\xbb\xbf"
)

// knownFieldNames are the field names the kinds have; a name that is none of them is never put in a message, since
// the text typed there may be a value that was given by mistake.
var knownFieldNames = []string{"username", "password", "token"}

func isKnownFieldName(name string) bool {
	for _, n := range knownFieldNames {
		if n == name {
			return true
		}
	}
	return false
}

// readSecretFields collects the fields of a secret: from the -field files when any is given (then every field of the
// kind has to be), else from the standard input, one line per field in the order of the kind.
func readSecretFields(fieldArgs, names []string, w wiring) (secrets.Fields, error) {
	var values map[string]string
	var err error
	if len(fieldArgs) > 0 {
		values, err = readFieldFiles(fieldArgs, names)
	} else {
		values, err = readFieldLines(names, w)
	}
	if err == nil {
		err = checkValueTexts(values, names)
	}
	if err != nil {
		return secrets.Fields{}, err
	}
	return secrets.NewFields(values), nil
}

// checkFieldArgs makes sure every -field is <name>=<file> with the name of a field. The text is not repeated: it may be
// the value itself, given by mistake.
func checkFieldArgs(args []string) error {
	for _, arg := range args {
		name, path, ok := strings.Cut(arg, "=")
		if !ok || path == "" || !isKnownFieldName(name) {
			return kernel.Errorf(kernel.CodeInvalidValue, "a -field is not <name>=<file>: it wants the name of a field (%s), an equals sign and the name of a file that holds the value, never the value itself", strings.Join(knownFieldNames, ", "))
		}
	}
	return nil
}

func readFieldFiles(args, names []string) (map[string]string, error) {
	values := map[string]string{}
	for _, arg := range args {
		name, path, _ := strings.Cut(arg, "=")
		if !contains(names, name) {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "-field %s is not a field of this kind of secret (its fields are %s)", name, strings.Join(names, ", "))
		}
		if _, dup := values[name]; dup {
			return nil, kernel.Errorf(kernel.CodeInvalidValue, "-field %s is given twice", name)
		}
		value, err := readValueFile(name, path)
		if err != nil {
			return nil, err
		}
		values[name] = value
	}
	return values, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// readValueFile reads the file of one field: a plain file (not a link, a device, a pipe or a folder). The path is not
// put in a message: a value typed in its place by mistake would be repeated by it. One line break at the end of the
// file (a CRLF too) is not part of the value.
func readValueFile(name, path string) (string, error) {
	raw, err := readFileChecked(path, maxSecretValueRead, false, "the file of -field "+name)
	if err != nil {
		var ke *kernel.Error
		if errors.As(err, &ke) {
			return "", err
		}
		return "", kernel.Errorf(kernel.CodeInvalidValue, "-field %s: the file cannot be opened or read; -field wants the name of a file that holds the value, never the value itself", name)
	}
	text := string(raw)
	zero(raw)
	return trimLineBreak(text), nil
}

// trimLineBreak takes away one line break (LF or CRLF) at the end, and nothing else.
func trimLineBreak(text string) string {
	text = strings.TrimSuffix(text, "\n")
	return strings.TrimSuffix(text, "\r")
}

// checkValueTexts refuses a value with a control character (U+0000 to U+001F, U+007F: a NUL, a second line break, a tab)
// or a byte order mark at its start: such a value is what a tool that adds a mark or a line made of a file, and the
// connection that fails for it gives no hint, since a value is never shown. The message names the field and the kind of
// trouble, never the value or where it is.
func checkValueTexts(values map[string]string, names []string) error {
	for _, name := range names {
		v := values[name]
		if strings.HasPrefix(v, bom) {
			return kernel.Errorf(kernel.CodeInvalidValue, "the field %s starts with a byte order mark (U+FEFF), which a tool adds to a text file; save the file without it", name)
		}
		for _, r := range v {
			if r < 0x20 || r == 0x7f {
				return kernel.Errorf(kernel.CodeInvalidValue, "the field %s has a control character (a NUL, a line break inside the value, a tab and the like); the value is one line, with no line break but the one at the end of the file", name)
			}
		}
	}
	return nil
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
