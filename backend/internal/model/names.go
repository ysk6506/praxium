package model

import "regexp"

// The naming rules of kg-model.md section 4. The rules check the form of a
// name; that an object type is a singular noun and a link type a verb phrase
// is for the people who name them.
var (
	pascalCase = regexp.MustCompile(`^[A-Z][a-z0-9]*([A-Z][a-z0-9]*)*$`)
	upperSnake = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)
	snakeCase  = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	// orgProperty is the name of an organisation property: the namespace, a
	// dot and snake_case. A standard name has no dot, so no standard name,
	// now or in a later version, can be the same.
	orgProperty = regexp.MustCompile(`^org\.[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	extProperty = regexp.MustCompile(`^ext\.[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
)

// MaxPropertyNameLength is the most characters of the name of an org or ext property.
const MaxPropertyNameLength = 64

// ValidPropertyName reports whether name is a good name for a property of
// the namespace: snake_case for an empty one, the namespace and a dot in front
// of snake_case for org and ext, at most MaxPropertyNameLength characters.
func ValidPropertyName(ns Namespace, name string) bool {
	switch ns {
	case "":
		return snakeCase.MatchString(name)
	case NamespaceOrg:
		return len(name) <= MaxPropertyNameLength && orgProperty.MatchString(name)
	case NamespaceExt:
		return len(name) <= MaxPropertyNameLength && extProperty.MatchString(name)
	}
	return false
}

// propertyNameForm describes what ValidPropertyName wants, for messages.
func propertyNameForm(ns Namespace) string {
	if ns == "" {
		return "snake_case"
	}
	return string(ns) + ".snake_case of at most 64 characters"
}

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
