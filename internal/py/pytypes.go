// Ported from CPython 3.12 (Objects/genericaliasobject.c, Objects/unionobject.c, Objects/typeobject.c) to
// Go, 2026-11-22.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The values typing syntax makes of builtin classes (list[int], int | None).

package py

import (
	"strings"
)

// genericClasses are the builtin classes that can be subscripted.
var genericClasses = map[string]bool{"list": true, "dict": true, "set": true, "frozenset": true, "tuple": true,
	"type": true, "enumerate": true, "BaseExceptionGroup": true, "ExceptionGroup": true}

// genericAliasV is a types.GenericAlias: origin[args].
type genericAliasV struct {
	origin *classV
	args   []Value
	// unpacked marks the *alias iterating an alias gives.
	unpacked bool
}

func (*genericAliasV) pyType() string { return "types.GenericAlias" }

var genericAliasClass = &classV{name: "GenericAlias", module: "types", base: objectClass}
var unionClass = &classV{name: "UnionType", module: "types", base: objectClass}

// classGetitem is cls[key] for a builtin class.
func classGetitem(k *classV, key Value) (Value, error) {
	if !genericClasses[k.name] || k.isUser() {
		return nil, typeErr("type '%s' is not subscriptable", k.name)
	}
	args := []Value{key}
	if t, ok := key.(*tupleV); ok {
		args = append([]Value(nil), t.items...)
	}
	return &genericAliasV{origin: k, args: args}, nil
}

// typeReprItem is ga_repr_item: the name of a class (with its module unless
// it is builtins), ... for Ellipsis, repr() of anything else.
func typeReprItem(v Value, st *reprState) (string, error) {
	switch x := v.(type) {
	case ellipsisV:
		return "...", nil
	case *genericAliasV, *unionV:
		return reprWith(v, st)
	case *classV:
		if mod := x.moduleName(); mod != "builtins" && mod != "" {
			return mod + "." + x.qualName(), nil
		}
		return x.qualName(), nil
	case *callable:
		if x.self == nil && x.typ == "builtin_function_or_method" {
			return x.name, nil
		}
	}
	return reprWith(v, st)
}

func (g *genericAliasV) repr(st *reprState) (string, error) {
	head, err := typeReprItem(g.origin, st)
	if err != nil {
		return "", err
	}
	if g.unpacked {
		head = "*" + head
	}
	if len(g.args) == 0 {
		return head + "[()]", nil
	}
	parts := make([]string, len(g.args))
	for i, a := range g.args {
		if parts[i], err = typeReprItem(a, st); err != nil {
			return "", err
		}
	}
	return head + "[" + strings.Join(parts, ", ") + "]", nil
}

func (g *genericAliasV) getAttr(name string) (Value, error) {
	switch name {
	case "__origin__":
		return g.origin, nil
	case "__args__":
		return newTuple(append([]Value(nil), g.args...)), nil
	case "__parameters__":
		return newTuple(nil), nil
	case "__class__":
		return genericAliasClass, nil
	}
	return pyGetattr(g.origin, name)
}

func (g *genericAliasV) iter() (iterator, error) {
	return &sliceIter{items: []Value{&genericAliasV{origin: g.origin, args: g.args, unpacked: true}}}, nil
}

func (g *genericAliasV) pyCall(args []Value, kw *dictV) (Value, error) {
	return callValue(nil, g.origin, args, kw)
}

func (g *genericAliasV) getItem(Value) (Value, error) {
	s, err := g.repr(&reprState{})
	if err != nil {
		return nil, err
	}
	return nil, typeErr("%s is not a generic class", s)
}

func (g *genericAliasV) eq(other Value) (bool, error) {
	o, ok := other.(*genericAliasV)
	if !ok || o.origin != g.origin {
		return false, nil
	}
	return seqEq(g.args, o.args)
}

// unionV is a types.UnionType: a | b.
type unionV struct {
	args []Value
}

func (*unionV) pyType() string { return "types.UnionType" }

func (u *unionV) repr(st *reprState) (string, error) {
	parts := make([]string, len(u.args))
	for i, a := range u.args {
		if isNoneV(a) {
			parts[i] = "None"
			continue
		}
		s, err := typeReprItem(a, st)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	return strings.Join(parts, " | "), nil
}

func (u *unionV) getAttr(name string) (Value, error) {
	switch name {
	case "__args__":
		return newTuple(append([]Value(nil), u.args...)), nil
	case "__class__":
		return unionClass, nil
	}
	return nil, nil
}

func (u *unionV) getItem(Value) (Value, error) {
	s, err := u.repr(&reprState{})
	if err != nil {
		return nil, err
	}
	return nil, typeErr("%s is not a generic class", s)
}

func (u *unionV) eq(other Value) (bool, error) {
	o, ok := other.(*unionV)
	if !ok || len(o.args) != len(u.args) {
		return false, nil
	}
	for _, a := range u.args {
		found := false
		for _, b := range o.args {
			if eq, err := pyEq(a, b); err != nil {
				return false, err
			} else if eq {
				found = true
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

// isUnionable reports whether v can be an operand of a type union.
func isUnionable(v Value) bool {
	switch v.(type) {
	case noneV, *classV, *genericAliasV, *unionV:
		return true
	}
	return false
}

// typeUnion is a | b for classes, None, generic aliases and unions; ok is
// false when an operand cannot take part.
func typeUnion(a, b Value) (Value, bool, error) {
	if !isUnionable(a) || !isUnionable(b) {
		return nil, false, nil
	}
	var args []Value
	add := func(v Value) error {
		if u, ok := v.(*unionV); ok {
			for _, x := range u.args {
				if err := addUnique(&args, x); err != nil {
					return err
				}
			}
			return nil
		}
		return addUnique(&args, v)
	}
	if err := add(a); err != nil {
		return nil, true, err
	}
	if err := add(b); err != nil {
		return nil, true, err
	}
	if len(args) == 1 {
		return args[0], true, nil
	}
	return &unionV{args: args}, true, nil
}

func addUnique(args *[]Value, v Value) error {
	for _, x := range *args {
		if sameObject(x, v) {
			return nil
		}
		if eq, err := pyEq(x, v); err != nil {
			return err
		} else if eq {
			return nil
		}
	}
	*args = append(*args, v)
	return nil
}
