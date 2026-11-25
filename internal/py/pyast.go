// Ported from CPython 3.12 (Parser/Python.asdl, Lib/ast.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The syntax tree ast.parse builds and the compilers of this module produce: one node type whose
// Kind is the CPython class name, with the fields of that class and its source positions.

package py

// Node is an ast.AST node. Kind is the class name ("Module", "FunctionDef",
// "Name", "arguments", "match_case", ...). Which of the other fields a node
// uses depends on its kind; Fields lists them in the class's _fields order.
//
// The scalar fields:
//
//	ID, NoID        the identifier field: Name.id, Attribute.attr,
//	                FunctionDef/AsyncFunctionDef/ClassDef.name, arg.arg,
//	                keyword.arg, alias.name, ExceptHandler.name,
//	                ImportFrom.module, MatchMapping.rest, MatchStar.name,
//	                MatchAs.name, TypeVar/ParamSpec/TypeVarTuple.name.
//	                NoID marks Python's None for the optional ones
//	                (keyword.arg of **, ExceptHandler.name, ImportFrom.module,
//	                MatchMapping.rest, MatchStar.name, MatchAs.name).
//	Asname, NoAsname alias.asname (NoAsname for None).
//	Names           Global.names, Nonlocal.names, MatchClass.kwd_attrs.
//	Ctx             Load, Store or Del: Name, Attribute, Subscript, Starred,
//	                List, Tuple.
//	Op              the operator class of BinOp, UnaryOp, BoolOp, AugAssign
//	                (Add, Sub, ..., Not, Invert, UAdd, USub, And, Or).
//	Ops             Compare.ops (Eq, NotEq, Lt, LtE, Gt, GtE, Is, IsNot, In,
//	                NotIn).
//	Value           Constant.value, MatchSingleton.value.
//	ConstKind       Constant.kind: "" for None, "u" for a u-prefixed string.
//	Conversion      FormattedValue.conversion (-1 for none, else 's', 'r'
//	                or 'a' as an int).
//	Level           ImportFrom.level.
//	Simple          AnnAssign.simple.
//	IsAsync         comprehension.is_async.
//
// The child fields, by role:
//
//	Module, Interactive                     Body=body
//	Expression                              A=body
//	FunctionDef, AsyncFunctionDef           A=args Body=body L1=decorator_list B=returns L2=type_params
//	ClassDef                                L1=bases L2=keywords Body=body L3=decorator_list L4=type_params
//	Return, Expr, Await, Yield, YieldFrom   A=value
//	Delete                                  L1=targets
//	Assign                                  L1=targets A=value
//	TypeAlias                               A=name L1=type_params B=value
//	AugAssign                               A=target B=value
//	AnnAssign                               A=target B=annotation C=value
//	For, AsyncFor                           A=target B=iter Body=body Orelse=orelse
//	While, If                               A=test Body=body Orelse=orelse
//	With, AsyncWith                         L1=items Body=body
//	withitem                                A=context_expr B=optional_vars
//	Match                                   A=subject L1=cases
//	match_case                              A=pattern B=guard Body=body
//	Raise                                   A=exc B=cause
//	Try, TryStar                            Body=body L1=handlers Orelse=orelse Finalbody=finalbody
//	ExceptHandler                           A=type Body=body
//	Assert                                  A=test B=msg
//	Import, ImportFrom                      L1=names (alias nodes)
//	arguments                               L1=posonlyargs L2=args A=vararg L3=kwonlyargs
//	                                        L4=kw_defaults (nil entries for None) B=kwarg Body=defaults
//	arg                                     A=annotation
//	keyword                                 A=value
//	Lambda                                  A=args B=body
//	BoolOp                                  L1=values
//	NamedExpr                               A=target B=value
//	BinOp                                   A=left B=right
//	UnaryOp                                 A=operand
//	IfExp                                   A=test B=body C=orelse
//	Dict                                    L1=keys (nil entries for **) L2=values
//	Set, List, Tuple                        L1=elts
//	ListComp, SetComp, GeneratorExp         A=elt L1=generators
//	DictComp                                A=key B=value L1=generators
//	comprehension                           A=target B=iter L1=ifs
//	Compare                                 A=left L1=comparators
//	Call                                    A=func L1=args L2=keywords
//	FormattedValue                          A=value B=format_spec
//	JoinedStr                               L1=values
//	Attribute, Starred                      A=value
//	Subscript                               A=value B=slice
//	Slice                                   A=lower B=upper C=step
//	MatchValue                              A=value
//	MatchSequence, MatchOr                  L1=patterns
//	MatchMapping                            L1=keys L2=patterns
//	MatchClass                              A=cls L1=patterns L2=kwd_patterns
//	MatchAs                                 A=pattern
//	TypeVar                                 A=bound
//
// Kinds without children: Pass, Break, Continue, Global, Nonlocal, Name,
// Constant, alias, MatchSingleton, MatchStar, ParamSpec, TypeVarTuple.
// A nil child stands for None. A Node with an empty Kind stands for a
// Python object that is not a node (held in Value), as code building trees
// may put one in a field.
//
// Positions are CPython's: lines from 1, columns as UTF-8 byte offsets.
// HasPos is false for the classes without positions (Module, Interactive,
// Expression, arguments, comprehension, withitem, match_case) and for nodes
// made without them; the type_ignores of Module and every type_comment are
// always empty.
type Node struct {
	Kind string

	Lineno, ColOffset, EndLineno, EndColOffset int
	HasPos                                     bool

	ID         string
	NoID       bool
	Asname     string
	NoAsname   bool
	Names      []string
	Ctx        string
	Op         string
	Ops        []string
	Value      Value
	ConstKind  string
	Conversion int
	Level      int
	Simple     int
	IsAsync    int
	// Unset names the fields the node does not have at all (getattr
	// raises AttributeError), as for a node built without them, such as a
	// FunctionDef made without type_params.
	Unset []string

	A, B, C                 *Node
	L1, L2, L3, L4          []*Node
	Body, Orelse, Finalbody []*Node

	// outer is the span of the tokens the expression was parsed from when
	// that differs from its own (parentheses around it): a parent node's
	// span starts and ends there, as CPython's rule spans do.
	outer *[4]int
	// scope is the scope of a FunctionDef, Lambda, ClassDef or
	// comprehension, filled in by the scope analysis before it runs.
	scope *scopeInfo
	// code is the code object of a FunctionDef, Lambda, ClassDef or
	// GeneratorExp, made the first time the node runs.
	code *codeV
}

// FieldKind says which member of a Field holds its value.
type FieldKind int

const (
	// FieldNode is a single child (Node, nil for None).
	FieldNode FieldKind = iota
	// FieldNodes is a list of children (Nodes; an entry may be nil for
	// None, as in Dict.keys and arguments.kw_defaults).
	FieldNodes
	// FieldConstant is a constant (Value): Constant.value,
	// MatchSingleton.value.
	FieldConstant
	// FieldIdent is an identifier or string (Str), or None (IsNone).
	FieldIdent
	// FieldIdents is a list of identifiers (Strs).
	FieldIdents
	// FieldInt is an int (Int).
	FieldInt
	// FieldSingleton is an operator or context class with no fields, such
	// as Add or Load (its class name in Str).
	FieldSingleton
	// FieldSingletons is a list of them (Strs): Compare.ops.
	FieldSingletons
)

// Field is one field of a node, as getattr(node, name) gives it.
type Field struct {
	Name string
	// Absent is set for a field the node does not have (see Node.Unset);
	// the other members are then zero.
	Absent bool
	Kind   FieldKind
	Node   *Node
	Nodes  []*Node
	Value  Value
	Str    string
	IsNone bool
	Strs   []string
	Int    int
}

// fieldRole names where a field of a node class is held.
type fieldRole int

const (
	roleA fieldRole = iota
	roleB
	roleC
	roleL1
	roleL2
	roleL3
	roleL4
	roleBody
	roleOrelse
	roleFinalbody
	roleID
	roleAsname
	roleNames
	roleCtx
	roleOp
	roleOps
	roleValue
	roleConstKind
	roleConversion
	roleLevel
	roleSimple
	roleIsAsync
	// roleNone is a field that is always None (type_comment).
	roleNone
	// roleEmpty is a list field that is always empty (type_ignores).
	roleEmpty
)

type fieldSpec struct {
	name string
	role fieldRole
}

func fs(pairs ...any) []fieldSpec {
	out := make([]fieldSpec, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, fieldSpec{pairs[i].(string), pairs[i+1].(fieldRole)})
	}
	return out
}

// nodeFields gives each node class's _fields (CPython 3.12) and where they
// are held.
var nodeFields = map[string][]fieldSpec{
	"Module":           fs("body", roleBody, "type_ignores", roleEmpty),
	"Interactive":      fs("body", roleBody),
	"Expression":       fs("body", roleA),
	"FunctionDef":      fs("name", roleID, "args", roleA, "body", roleBody, "decorator_list", roleL1, "returns", roleB, "type_comment", roleNone, "type_params", roleL2),
	"AsyncFunctionDef": fs("name", roleID, "args", roleA, "body", roleBody, "decorator_list", roleL1, "returns", roleB, "type_comment", roleNone, "type_params", roleL2),
	"ClassDef":         fs("name", roleID, "bases", roleL1, "keywords", roleL2, "body", roleBody, "decorator_list", roleL3, "type_params", roleL4),
	"Return":           fs("value", roleA),
	"Delete":           fs("targets", roleL1),
	"Assign":           fs("targets", roleL1, "value", roleA, "type_comment", roleNone),
	"TypeAlias":        fs("name", roleA, "type_params", roleL1, "value", roleB),
	"AugAssign":        fs("target", roleA, "op", roleOp, "value", roleB),
	"AnnAssign":        fs("target", roleA, "annotation", roleB, "value", roleC, "simple", roleSimple),
	"For":              fs("target", roleA, "iter", roleB, "body", roleBody, "orelse", roleOrelse, "type_comment", roleNone),
	"AsyncFor":         fs("target", roleA, "iter", roleB, "body", roleBody, "orelse", roleOrelse, "type_comment", roleNone),
	"While":            fs("test", roleA, "body", roleBody, "orelse", roleOrelse),
	"If":               fs("test", roleA, "body", roleBody, "orelse", roleOrelse),
	"With":             fs("items", roleL1, "body", roleBody, "type_comment", roleNone),
	"AsyncWith":        fs("items", roleL1, "body", roleBody, "type_comment", roleNone),
	"Match":            fs("subject", roleA, "cases", roleL1),
	"Raise":            fs("exc", roleA, "cause", roleB),
	"Try":              fs("body", roleBody, "handlers", roleL1, "orelse", roleOrelse, "finalbody", roleFinalbody),
	"TryStar":          fs("body", roleBody, "handlers", roleL1, "orelse", roleOrelse, "finalbody", roleFinalbody),
	"Assert":           fs("test", roleA, "msg", roleB),
	"Import":           fs("names", roleL1),
	"ImportFrom":       fs("module", roleID, "names", roleL1, "level", roleLevel),
	"Global":           fs("names", roleNames),
	"Nonlocal":         fs("names", roleNames),
	"Expr":             fs("value", roleA),
	"Pass":             nil,
	"Break":            nil,
	"Continue":         nil,

	"BoolOp":         fs("op", roleOp, "values", roleL1),
	"NamedExpr":      fs("target", roleA, "value", roleB),
	"BinOp":          fs("left", roleA, "op", roleOp, "right", roleB),
	"UnaryOp":        fs("op", roleOp, "operand", roleA),
	"Lambda":         fs("args", roleA, "body", roleB),
	"IfExp":          fs("test", roleA, "body", roleB, "orelse", roleC),
	"Dict":           fs("keys", roleL1, "values", roleL2),
	"Set":            fs("elts", roleL1),
	"ListComp":       fs("elt", roleA, "generators", roleL1),
	"SetComp":        fs("elt", roleA, "generators", roleL1),
	"DictComp":       fs("key", roleA, "value", roleB, "generators", roleL1),
	"GeneratorExp":   fs("elt", roleA, "generators", roleL1),
	"Await":          fs("value", roleA),
	"Yield":          fs("value", roleA),
	"YieldFrom":      fs("value", roleA),
	"Compare":        fs("left", roleA, "ops", roleOps, "comparators", roleL1),
	"Call":           fs("func", roleA, "args", roleL1, "keywords", roleL2),
	"FormattedValue": fs("value", roleA, "conversion", roleConversion, "format_spec", roleB),
	"JoinedStr":      fs("values", roleL1),
	"Constant":       fs("value", roleValue, "kind", roleConstKind),
	"Attribute":      fs("value", roleA, "attr", roleID, "ctx", roleCtx),
	"Subscript":      fs("value", roleA, "slice", roleB, "ctx", roleCtx),
	"Starred":        fs("value", roleA, "ctx", roleCtx),
	"Name":           fs("id", roleID, "ctx", roleCtx),
	"List":           fs("elts", roleL1, "ctx", roleCtx),
	"Tuple":          fs("elts", roleL1, "ctx", roleCtx),
	"Slice":          fs("lower", roleA, "upper", roleB, "step", roleC),

	"comprehension": fs("target", roleA, "iter", roleB, "ifs", roleL1, "is_async", roleIsAsync),
	"ExceptHandler": fs("type", roleA, "name", roleID, "body", roleBody),
	"arguments":     fs("posonlyargs", roleL1, "args", roleL2, "vararg", roleA, "kwonlyargs", roleL3, "kw_defaults", roleL4, "kwarg", roleB, "defaults", roleBody),
	"arg":           fs("arg", roleID, "annotation", roleA, "type_comment", roleNone),
	"keyword":       fs("arg", roleID, "value", roleA),
	"alias":         fs("name", roleID, "asname", roleAsname),
	"withitem":      fs("context_expr", roleA, "optional_vars", roleB),
	"match_case":    fs("pattern", roleA, "guard", roleB, "body", roleBody),

	"MatchValue":     fs("value", roleA),
	"MatchSingleton": fs("value", roleValue),
	"MatchSequence":  fs("patterns", roleL1),
	"MatchMapping":   fs("keys", roleL1, "patterns", roleL2, "rest", roleID),
	"MatchClass":     fs("cls", roleA, "patterns", roleL1, "kwd_attrs", roleNames, "kwd_patterns", roleL2),
	"MatchStar":      fs("name", roleID),
	"MatchAs":        fs("pattern", roleA, "name", roleID),
	"MatchOr":        fs("patterns", roleL1),

	"TypeVar":      fs("name", roleID, "bound", roleA),
	"ParamSpec":    fs("name", roleID),
	"TypeVarTuple": fs("name", roleID),
}

// IsNodeKind reports whether kind is the name of a node class Fields knows.
func IsNodeKind(kind string) bool {
	_, ok := nodeFields[kind]
	return ok
}

// Fields returns the fields of n in its class's _fields order (CPython
// 3.12), with their values. A kind Fields does not know has none.
func Fields(n *Node) []Field {
	specs := nodeFields[n.Kind]
	out := make([]Field, len(specs))
	for i, s := range specs {
		f := Field{Name: s.name}
		if n.isUnset(s.name) {
			f.Absent = true
			out[i] = f
			continue
		}
		switch s.role {
		case roleA, roleB, roleC:
			f.Kind, f.Node = FieldNode, n.child(s.role)
		case roleL1, roleL2, roleL3, roleL4, roleBody, roleOrelse, roleFinalbody:
			f.Kind, f.Nodes = FieldNodes, n.list(s.role)
		case roleEmpty:
			f.Kind = FieldNodes
		case roleID:
			f.Kind, f.Str, f.IsNone = FieldIdent, n.ID, n.NoID
		case roleAsname:
			f.Kind, f.Str, f.IsNone = FieldIdent, n.Asname, n.NoAsname
		case roleConstKind:
			f.Kind, f.Str, f.IsNone = FieldIdent, n.ConstKind, n.ConstKind == ""
		case roleNone:
			f.Kind, f.IsNone = FieldIdent, true
		case roleNames:
			f.Kind, f.Strs = FieldIdents, n.Names
		case roleCtx:
			f.Kind, f.Str = FieldSingleton, n.Ctx
		case roleOp:
			f.Kind, f.Str = FieldSingleton, n.Op
		case roleOps:
			f.Kind, f.Strs = FieldSingletons, n.Ops
		case roleValue:
			f.Kind, f.Value = FieldConstant, n.Value
			if f.Value == nil {
				f.Value = none
			}
		case roleConversion:
			f.Kind, f.Int = FieldInt, n.Conversion
		case roleLevel:
			f.Kind, f.Int = FieldInt, n.Level
		case roleSimple:
			f.Kind, f.Int = FieldInt, n.Simple
		case roleIsAsync:
			f.Kind, f.Int = FieldInt, n.IsAsync
		}
		out[i] = f
	}
	return out
}

// isUnset reports whether the field is one the node does not have.
func (n *Node) isUnset(name string) bool {
	for _, u := range n.Unset {
		if u == name {
			return true
		}
	}
	return false
}

func (n *Node) child(r fieldRole) *Node {
	switch r {
	case roleA:
		return n.A
	case roleB:
		return n.B
	}
	return n.C
}

func (n *Node) list(r fieldRole) []*Node {
	switch r {
	case roleL1:
		return n.L1
	case roleL2:
		return n.L2
	case roleL3:
		return n.L3
	case roleL4:
		return n.L4
	case roleBody:
		return n.Body
	case roleOrelse:
		return n.Orelse
	}
	return n.Finalbody
}

// children returns the node's child nodes in _fields order, skipping None
// and the operator and context singletons (which hold no names).
func (n *Node) children() []*Node {
	var out []*Node
	for _, s := range nodeFields[n.Kind] {
		switch s.role {
		case roleA, roleB, roleC:
			if c := n.child(s.role); c != nil {
				out = append(out, c)
			}
		case roleL1, roleL2, roleL3, roleL4, roleBody, roleOrelse, roleFinalbody:
			for _, c := range n.list(s.role) {
				if c != nil {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

// startOf is where the tokens of an expression start, parentheses included.
func startOf(n *Node) (int, int) {
	if n.outer != nil {
		return n.outer[0], n.outer[1]
	}
	return n.Lineno, n.ColOffset
}

// endOf is where the tokens of an expression end, parentheses included.
func endOf(n *Node) (int, int) {
	if n.outer != nil {
		return n.outer[2], n.outer[3]
	}
	return n.EndLineno, n.EndColOffset
}

// span gives n the span from the start of from's tokens to the end of to's.
func (n *Node) span(from, to *Node) *Node {
	n.Lineno, n.ColOffset = startOf(from)
	n.EndLineno, n.EndColOffset = endOf(to)
	n.HasPos = true
	return n
}

// The source symbols of the operator classes.
var binaryOperators = map[string]string{
	"Add": "+", "Sub": "-", "Mult": "*", "Div": "/", "Mod": "%", "Pow": "**", "LShift": "<<",
	"RShift": ">>", "BitOr": "|", "BitXor": "^", "BitAnd": "&", "FloorDiv": "//", "MatMult": "@",
}

var comparisonOperators = map[string]string{
	"Eq": "==", "NotEq": "!=", "Lt": "<", "LtE": "<=", "Gt": ">", "GtE": ">=", "Is": "is",
	"IsNot": "is not", "In": "in", "NotIn": "not in",
}

// Parse is ast.parse(source, filename, mode) for mode "exec" (a Module) or
// "eval" (an Expression). The error is the SyntaxError, as an *Error.
func Parse(source, filename, mode string) (*Node, error) {
	if mode != "exec" && mode != "eval" {
		return nil, toError(valueErr("compile() mode must be 'exec', 'eval' or 'single'"))
	}
	tree, serr := parseSource(source, filename, mode)
	if serr != nil {
		return nil, toError(serr.exceptionIn(filename))
	}
	return tree, nil
}
