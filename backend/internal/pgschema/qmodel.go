package pgschema

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/model"
)

// qAttr is one property as the query schema shows it: its number, its name (the name of its column), the kind and the type of
// the column, and what the description tables say of it.
type qAttr struct {
	No        int
	Name      string
	Kind      model.BaseKind
	ColType   string
	ValueType string
	Unit      string
	Enum      []string
	Sens      model.Sensitivity
	Volatile  bool
	Desc      string
	tmpl      string // the expression that reads the value out of a document, with docPlaceholder for the document
}

// docPlaceholder stands for the document column in the expression of an attribute until its table alias is known.
const docPlaceholder = "%DOC%"

// docExpr is the expression that reads the value of the attribute out of the document of the alias.
func (a qAttr) docExpr(alias string) string {
	return strings.ReplaceAll(a.tmpl, docPlaceholder, alias+".doc")
}

// column says whether the attribute has a column of its own in a view of a type: the name of an object is the column name of
// every view, and it is read from the table of the type, where it is kept besides the document.
func (a qAttr) column() bool { return a.Name != colName }

// timeColumn is the column that has the time the source saw a volatile value.
func (a qAttr) timeColumn() string { return a.Name + "_at" }

// qType is one object type as the query schema shows it.
type qType struct {
	No, ParentNo int
	Name, View   string
	Parent       string // the view of the parent type, empty for a type without one
	Layer, Desc  string
	Docs         []qAttr // the versioned properties, kept in the document, by attribute number
	Own          []qAttr // the volatile properties that not every type has, kept in the table of values of the type
	Subtree      []int   // the number of the type and of every type below it, ascending
}

// qLink is one link type as the query schema shows it.
type qLink struct {
	No         int
	Name, View string
	Desc       string
	Props      []qAttr
}

// qRule is one binding of a link type: which kind of object it may start at and end at.
type qRule struct {
	LinkNo         int
	FromKind, From string
	ToKind, To     string
	Cardinality    string
}

// qModel is what the query schema is made from: the model by number, with the names the views use.
type qModel struct {
	Common []qAttr // the volatile properties that every type has, kept in object_state
	Types  []qType
	Links  []qLink
	Rules  []qRule
	Search map[[2]int]bool
}

const (
	colID         = "id"
	colRef        = "ref"
	colName       = "name"
	colMinGrade   = "min_grade"
	colProvenance = "provenance"
)

// typeColumns are the columns of a view of a type that are not properties.
func typeColumns() []string { return []string{colID, colRef, colName, colMinGrade, colProvenance} }

// toAttr reads a property. Its sensitivity is the one attrGrades says: the sensitivity of a property is read in one place.
func toAttr(reg *model.Registry, grades map[int]model.Sensitivity, p model.Property) (qAttr, error) {
	vt, ok := reg.ValueType(p.ValueType)
	if !ok {
		return qAttr{}, kernel.Errorf(kernel.CodeInvalidValue, "the property %s has the unknown value type %s", p.ID, p.ValueType)
	}
	typ, err := ColumnType(vt.Base)
	if err != nil {
		return qAttr{}, err
	}
	tmpl, err := Expr(docPlaceholder, p.AttrNo, vt.Base)
	if err != nil {
		return qAttr{}, err
	}
	return qAttr{No: p.AttrNo, Name: p.Name, Kind: vt.Base, ColType: typ, ValueType: vt.Name, Unit: p.Unit, Enum: p.EnumValues,
		Sens: grades[p.AttrNo], Volatile: p.EffectiveHistory() == model.HistoryVolatile, Desc: p.Description, tmpl: tmpl}, nil
}

func sortAttrs(as []qAttr) { sort.Slice(as, func(i, j int) bool { return as[i].No < as[j].No }) }

// gatherType reads one object type: its versioned and its own volatile properties, its parent and the types below it. The volatile
// properties that every type has are put into the model once, in qm.Common.
func gatherType(reg *model.Registry, grades map[int]model.Sensitivity, t model.ObjectType, common, seenCommon map[int]bool, qm *qModel) (qType, error) {
	qt := qType{No: t.TypeNo, Name: t.Name, View: TypeViewName(t.Name), Layer: string(t.Layer), Desc: t.Description, Subtree: []int{t.TypeNo}}
	for _, p := range reg.EffectiveProperties(t.ID) {
		a, err := toAttr(reg, grades, p)
		if err != nil {
			return qType{}, err
		}
		switch {
		case common[a.No]:
			if !seenCommon[a.No] {
				seenCommon[a.No] = true
				qm.Common = append(qm.Common, a)
			}
		case a.Volatile:
			qt.Own = append(qt.Own, a)
		default:
			qt.Docs = append(qt.Docs, a)
		}
	}
	sortAttrs(qt.Docs)
	sortAttrs(qt.Own)
	if !t.Parent.IsZero() {
		if parent, ok := reg.ObjectType(t.Parent); ok {
			qt.Parent, qt.ParentNo = TypeViewName(parent.Name), parent.TypeNo
		}
	}
	for _, s := range reg.Subtypes(t.ID) {
		qt.Subtree = append(qt.Subtree, s.TypeNo)
	}
	sort.Ints(qt.Subtree)
	return qt, nil
}

// gatherModel reads the registry into the names and numbers the views are made from. It refuses a model whose names cannot be
// views of one schema: a name that is taken, one that is too long for PostgreSQL, one that a column of the view already has, or
// a text that PostgreSQL cannot keep.
func gatherModel(reg *model.Registry, sh shape, search SearchSet) (qModel, error) {
	qm := qModel{Search: map[[2]int]bool{}}
	for _, e := range search {
		qm.Search[[2]int{e.TypeNo, e.AttrNo}] = true
	}
	common := map[int]bool{}
	for _, c := range sh.Common {
		common[c.AttrNo] = true
	}
	seenCommon := map[int]bool{}
	grades := attrGrades(reg)
	for _, t := range reg.ObjectTypes() {
		qt, err := gatherType(reg, grades, t, common, seenCommon, &qm)
		if err != nil {
			return qModel{}, err
		}
		qm.Types = append(qm.Types, qt)
	}
	sortAttrs(qm.Common)
	sort.Slice(qm.Types, func(i, j int) bool { return qm.Types[i].No < qm.Types[j].No })
	if err := gatherLinks(reg, grades, &qm); err != nil {
		return qModel{}, err
	}
	return qm, qm.check()
}

// gatherLinks reads the link types: their properties and the bindings.
func gatherLinks(reg *model.Registry, grades map[int]model.Sensitivity, qm *qModel) error {
	for _, l := range reg.LinkTypes() {
		ql := qLink{No: l.LinkTypeNo, Name: l.Name, View: LinkViewName(l.Name), Desc: l.Description}
		for _, p := range l.Properties {
			a, err := toAttr(reg, grades, p)
			if err != nil {
				return err
			}
			ql.Props = append(ql.Props, a)
		}
		sortAttrs(ql.Props)
		qm.Links = append(qm.Links, ql)
		for _, b := range l.Bindings {
			from, err := endpointOf(reg, b.From)
			if err != nil {
				return err
			}
			to, err := endpointOf(reg, b.To)
			if err != nil {
				return err
			}
			qm.Rules = append(qm.Rules, qRule{LinkNo: l.LinkTypeNo, FromKind: string(b.From.Kind), From: from, ToKind: string(b.To.Kind), To: to,
				Cardinality: string(b.Cardinality)})
		}
	}
	sort.Slice(qm.Links, func(i, j int) bool { return qm.Links[i].No < qm.Links[j].No })
	return nil
}

// endpointOf is the name a binding end has in the query schema: the view of a type, the name of a trait, or nothing for any object.
func endpointOf(reg *model.Registry, e model.Endpoint) (string, error) {
	switch e.Kind {
	case model.EndpointAny:
		return "", nil
	case model.EndpointType:
		id, err := kernel.ParseID[model.ObjectTypeKind](e.Ref)
		if err != nil {
			return "", err
		}
		if t, ok := reg.ObjectType(id); ok {
			return TypeViewName(t.Name), nil
		}
	case model.EndpointTrait:
		id, err := kernel.ParseID[model.TraitKind](e.Ref)
		if err != nil {
			return "", err
		}
		if t, ok := reg.Trait(id); ok {
			return t.Name, nil
		}
	}
	return "", kernel.Errorf(kernel.CodeInvalidValue, "a binding names %s %q, which the model does not have", e.Kind, e.Ref)
}

// check refuses what cannot be shown as the views of one schema.
func (qm qModel) check() error {
	taken := reservedViewNames()
	for _, v := range qm.views() {
		if err := checkIdent(v.name, "the view of "+v.owner); err != nil {
			return err
		}
		if taken[v.name] {
			return kernel.Errorf(kernel.CodeInvalidValue, "the name %q of %s is taken by another view of the query schema", v.name, v.owner)
		}
		taken[v.name] = true
		if v.at {
			taken[AtFunctionName(v.name)] = true
		}
	}
	for _, t := range qm.Types {
		if err := qm.checkType(t); err != nil {
			return err
		}
	}
	return qm.checkLinks()
}

// viewName is the view a type or a link type is, with the owner for a message.
type viewName struct {
	name, owner string
	at          bool
}

func (qm qModel) views() []viewName {
	var out []viewName
	for _, t := range qm.Types {
		out = append(out, viewName{t.View, "the type " + t.Name, true})
	}
	for _, l := range qm.Links {
		out = append(out, viewName{l.View, "the link type " + l.Name, false})
	}
	return out
}

func (qm qModel) checkType(t qType) error {
	if err := checkIdent(AtFunctionName(t.View), "the function of the past of "+t.Name); err != nil {
		return err
	}
	cols := map[string]bool{}
	for _, c := range typeColumns() {
		cols[c] = true
	}
	for _, a := range qm.typeAttrs(t) {
		if !a.column() {
			continue
		}
		names := []string{a.Name}
		if a.Volatile {
			names = append(names, a.timeColumn())
		}
		for _, n := range names {
			if err := checkIdent(n, "the column of the property of "+t.Name); err != nil {
				return err
			}
			if cols[n] {
				return kernel.Errorf(kernel.CodeInvalidValue, "the type %s has the column %q twice, or one of the columns every view of a type has", t.Name, n)
			}
			cols[n] = true
		}
		if err := checkText(a.Desc, "the description of the property "+a.Name); err != nil {
			return err
		}
	}
	return checkText(t.Desc, "the description of the type "+t.Name)
}

func (qm qModel) checkLinks() error {
	for _, l := range qm.Links {
		cols := map[string]bool{"src": true, "dst": true, "grade": true, "basis": true}
		for _, a := range l.Props {
			if err := checkIdent(a.Name, "the column of the property of "+l.Name); err != nil {
				return err
			}
			if cols[a.Name] {
				return kernel.Errorf(kernel.CodeInvalidValue, "the link type %s has the column %q twice, or one of the columns every view of a link type has", l.Name, a.Name)
			}
			cols[a.Name] = true
			if err := checkText(a.Desc, "the description of the property "+a.Name); err != nil {
				return err
			}
		}
		if err := checkText(l.Desc, "the description of the link type "+l.Name); err != nil {
			return err
		}
	}
	return nil
}

// typeAttrs are the properties a view of the type shows, in the order of their columns: the versioned ones in the document, the
// volatile ones every type has, and the volatile ones of the type.
func (qm qModel) typeAttrs(t qType) []qAttr {
	out := append(append(append([]qAttr(nil), t.Docs...), qm.Common...), t.Own...)
	return out
}

func checkIdent(name, what string) error {
	if name == "" || len(name) > MaxIdentBytes || strings.ContainsRune(name, 0) || !utf8.ValidString(name) {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s has the name %q, which PostgreSQL cannot keep (it is empty or longer than %d bytes)", what, name, MaxIdentBytes)
	}
	return nil
}

func checkText(text, what string) error {
	if strings.ContainsRune(text, 0) || !utf8.ValidString(text) {
		return kernel.Errorf(kernel.CodeInvalidValue, "%s is not text that PostgreSQL can keep", what)
	}
	return nil
}
