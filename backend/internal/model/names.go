package model

import "regexp"

// The naming rules of kg-model.md section 4. The rules check the form of a
// name; that an object type is a singular noun and a link type a verb phrase
// is for the people who name them.
var (
	pascalCase = regexp.MustCompile(`^[A-Z][a-z0-9]*([A-Z][a-z0-9]*)*$`)
	upperSnake = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)
	snakeCase  = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
)

// NameForm is the casing a kind of definition uses for its names.
type NameForm int

// The three forms.
const (
	// FormPascal is an object type or trait name: PhysicalServer.
	FormPascal NameForm = iota
	// FormUpperSnake is a link type name: HOSTED_ON.
	FormUpperSnake
	// FormSnake is a property, action type, value type, layer or enum value name: rack_unit.
	FormSnake
)

// ValidName reports whether name has the form.
func ValidName(form NameForm, name string) bool {
	switch form {
	case FormPascal:
		return pascalCase.MatchString(name)
	case FormUpperSnake:
		return upperSnake.MatchString(name)
	case FormSnake:
		return snakeCase.MatchString(name)
	}
	return false
}
