// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The attributes of sets, bytes, exceptions, slices, dicts, and the unbound methods of the
// builtin classes.

package py

import (
	"strings"
	"sync"
)

// excMethod is an exception's attribute other than args: one set on the
// instance, or one BaseException defines.
func excMethod(x *excV, name string) (Value, bool) {
	if x.attrs != nil {
		if v, ok := x.attrs.getStr(name); ok {
			return v, true
		}
	}
	return excBuiltinAttr(x, name)
}

// dictMethod is an attribute of a dict.
func dictMethod(obj Value, d *dictV, name string, isGroup bool) (Value, bool) {
	if m, ok := methodsDict[name]; ok {
		return methodsBound(obj, name, func(args []Value, kw *dictV) (Value, error) { return m(d, args, kw) }), true
	}
	return slotWrapper(obj, name)
}

// setMethod is an attribute of a set or frozenset.
func setMethod(s *setV, name string) (Value, bool) {
	typ := s.pyType()
	bound := func(fn func(args []Value, kw *dictV) (Value, error)) (Value, bool) {
		return methodsBound(s, name, fn), true
	}
	others := func(args []Value) ([]*setV, error) {
		out := make([]*setV, len(args))
		for i, a := range args {
			o, err := toSet(a)
			if err != nil {
				return nil, err
			}
			out[i] = o
		}
		return out, nil
	}
	switch name {
	case "copy":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone(typ+".copy", args, kw); err != nil {
				return nil, err
			}
			if s.frozen {
				return s, nil
			}
			return s.copyAs(false), nil
		})
	case "union", "intersection", "difference", "symmetric_difference":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := noKw(typ+"."+name, kw); err != nil {
				return nil, err
			}
			if name == "symmetric_difference" {
				if err := methodsArgsOne(typ+".symmetric_difference", args, kw); err != nil {
					return nil, err
				}
				return setSymmetricDifference(s, args[0], s.frozen)
			}
			if name == "union" {
				return setUnion(s, args, s.frozen)
			}
			r := s.copyAs(s.frozen)
			if len(args) == 0 {
				return r, nil
			}
			for _, a := range args {
				var err error
				if name == "intersection" {
					r, err = setIntersection(r, a, s.frozen)
				} else {
					r, err = setDifference(r, a, s.frozen)
				}
				if err != nil {
					return nil, err
				}
			}
			return r, nil
		})
	case "isdisjoint", "issubset", "issuperset":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne(typ+"."+name, args, kw); err != nil {
				return nil, err
			}
			os, err := others(args)
			if err != nil {
				return nil, err
			}
			o := os[0]
			switch name {
			case "issubset":
				r, err := s.subsetOf(o)
				return pyBool(r), err
			case "issuperset":
				r, err := o.subsetOf(s)
				return pyBool(r), err
			}
			inter, err := setIntersection(s, o, true)
			return pyBool(err == nil && inter.used == 0), err
		})
	}
	if s.frozen {
		return slotWrapper(s, name)
	}
	switch name {
	case "add", "remove", "discard":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("set."+name, args, kw); err != nil {
				return nil, err
			}
			if name == "add" {
				return none, s.add(args[0])
			}
			found, err := s.discard(args[0])
			if err != nil {
				return nil, err
			}
			if !found && name == "remove" {
				return nil, newExc("KeyError", []Value{args[0]})
			}
			return none, nil
		})
	case "pop":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("set.pop", args, kw); err != nil {
				return nil, err
			}
			return s.pop()
		})
	case "clear":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("set.clear", args, kw); err != nil {
				return nil, err
			}
			s.clear()
			return none, nil
		})
	case "update":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := noKw("set.update", kw); err != nil {
				return nil, err
			}
			for _, a := range args {
				if err := s.update(a); err != nil {
					return nil, err
				}
			}
			return none, nil
		})
	case "intersection_update", "difference_update", "symmetric_difference_update":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := noKw("set."+name, kw); err != nil {
				return nil, err
			}
			r := s
			for _, a := range args {
				var err error
				switch name {
				case "intersection_update":
					r, err = setIntersection(r, a, false)
				case "difference_update":
					r, err = setDifference(r, a, false)
				default:
					r, err = setSymmetricDifference(r, a, false)
				}
				if err != nil {
					return nil, err
				}
			}
			if r != s {
				s.clear()
				s.fillFrom(r)
			}
			return none, nil
		})
	}
	return slotWrapper(s, name)
}

func sliceIndicesMethod(s *sliceV) Value {
	return methodsBound(s, "indices", func(args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne("slice.indices", args, kw); err != nil {
			return nil, err
		}
		n, err := methodsIndex(args[0])
		if err != nil {
			return nil, err
		}
		if n.Sign() < 0 {
			return nil, valueErr("length should not be negative")
		}
		start, stop, step, err := pySliceIndices(int(n.Int64()), s.start, s.stop, s.step)
		if err != nil {
			return nil, err
		}
		return newTuple([]Value{mkInt(int64(start)), mkInt(int64(stop)), mkInt(int64(step))}), nil
	})
}

// classAttr is an attribute of a builtin class: its methods, unbound, its
// own or those it inherits from BaseException and object. A method or
// attribute descriptor is the same object on each lookup.
func classAttr(k *classV, name string) (Value, bool) {
	v, ok := makeClassAttr(k, name)
	if !ok {
		return nil, false
	}
	return canonicalDescriptor(v), true
}

// descriptors holds the descriptor objects of the builtin classes that
// canonicalDescriptor has seen, by kind and qualified name.
var descriptors sync.Map

// canonicalDescriptor is the one object standing for the method or
// attribute descriptor v of a builtin class (the first v seen with its
// qualified name), and v itself for any other value.
func canonicalDescriptor(v Value) Value {
	var key string
	switch x := v.(type) {
	case *methodDescV:
		key = x.typ + "|" + x.qual
	case *callable:
		if x.self != nil || !strings.Contains(x.qual, ".") {
			return v
		}
		switch {
		case x.typ == "wrapper_descriptor", x.typ == "method_descriptor", x.name == "__new__":
			key = x.typ + "|" + x.qual
		default:
			return v
		}
	case *dataDescV:
		key = x.pyType() + "|" + x.owner + "." + x.name
	default:
		return v
	}
	c, _ := descriptors.LoadOrStore(key, v)
	return c.(Value)
}

// makeClassAttr builds the attribute classAttr returns.
func makeClassAttr(k *classV, name string) (Value, bool) {
	if m, ok := builtinSlotMethod(k, name); ok {
		return m, true
	}
	if v, ok := builtinClassAttr(k.name, k, name); ok {
		return v, true
	}
	if m, ok := ownClassAttr(k, name); ok {
		return m, true
	}
	for b := k.base; b != nil; b = b.base {
		if m, ok := builtinSlotMethod(b, name); ok {
			return m, true
		}
	}
	return nil, false
}

// ownClassAttr is a method or data descriptor a builtin class defines
// itself, found on a sample value of the class.
func ownClassAttr(k *classV, name string) (Value, bool) {
	sample := map[string]Value{"str": strV(""), "list": newList(nil), "dict": newDict(), "tuple": newTuple(nil),
		"set": newSet(false), "frozenset": newSet(true), "int": mkInt(0), "float": floatV(0), "bytes": bytesV(""),
		"complex": complexV(0),
		"bool":    falseV, "range": rangeV{stop: 0, step: 1}, "slice": &sliceV{start: none, stop: none, step: none},
		"bytearray": &bytearrayV{}}[k.name]
	if sample == nil {
		return nil, false
	}
	a, ok := attrOf(sample, name)
	if !ok {
		return nil, false
	}
	if _, isCall := a.(*callable); !isCall {
		return &dataDescV{name: name, owner: k.name}, true
	}
	typ := "method_descriptor"
	if isSlotName(name) {
		typ = "wrapper_descriptor"
	}
	desc := &callable{name: name, typ: typ, qual: k.name + "." + name}
	desc.fn = func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		// A slot wrapper checks its self as wrapperdescr_call does, a
		// method descriptor as method_check_args does.
		wrapper := typ == "wrapper_descriptor"
		if len(args) == 0 {
			if wrapper {
				return nil, typeErr("descriptor '%s' of '%s' object needs an argument", name, k.name)
			}
			return nil, typeErr("unbound method %s.%s() needs an argument", k.name, name)
		}
		if !classOf(args[0]).subclassOf(k) {
			if wrapper {
				return nil, typeErr("descriptor '%s' requires a '%s' object but received a '%s'", name, k.name, args[0].pyType())
			}
			return nil, typeErr("descriptor '%s' for '%s' objects doesn't apply to a '%s' object", name, k.name, args[0].pyType())
		}
		self := args[0]
		if x, isInst := self.(*instV); isInst && x.base != nil {
			// The method acts on the value of the builtin type that an
			// instance of a subclass holds.
			self = x.base
		}
		m, ok := attrOf(self, name)
		if !ok {
			return nil, newPyErr("AttributeError", name)
		}
		return callValue(c, m, args[1:], kw)
	}
	return &methodDescV{desc}, true
}

// dictFromkeysOf is dict.fromkeys(iterable, value=None) called on cls: cls()
// with each key set to the value.
func dictFromkeysOf(c *callCtx, cls *classV, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("fromkeys", 1, 2, args, kw); err != nil {
		return nil, err
	}
	var val Value = none
	if len(args) == 2 {
		val = args[1]
	}
	keys, err := toSlice(args[0])
	if err != nil {
		return nil, err
	}
	var d Value = newDict()
	if cls.isUser() {
		if d, err = callValue(c, cls, nil, nil); err != nil {
			return nil, err
		}
	}
	for _, key := range keys {
		if err := pySetitem(d, key, val); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// dataDescV is a data attribute of a builtin class, as the class holds it:
// a getset or member descriptor.
type dataDescV struct {
	name, owner string
	// member marks a member descriptor, such as a __slots__ entry.
	member bool
}

func (d *dataDescV) pyType() string {
	if d.member || d.owner == "range" || d.owner == "slice" {
		return "member_descriptor"
	}
	return "getset_descriptor"
}

func (d *dataDescV) repr(*reprState) (string, error) {
	if d.pyType() == "member_descriptor" {
		return "<member '" + d.name + "' of '" + d.owner + "' objects>", nil
	}
	return "<attribute '" + d.name + "' of '" + d.owner + "' objects>", nil
}

// methodDescV is an unbound method of a builtin class.
type methodDescV struct{ *callable }

// eq is identity, as for any descriptor.
func (m *methodDescV) eq(other Value) (bool, error) {
	o, ok := other.(*methodDescV)
	return ok && o == m, nil
}

func (m *methodDescV) repr(*reprState) (string, error) {
	i := strings.IndexByte(m.qual, '.')
	if m.typ == "wrapper_descriptor" {
		return "<slot wrapper '" + m.name + "' of '" + m.qual[:i] + "' objects>", nil
	}
	return "<method '" + m.name + "' of '" + m.qual[:i] + "' objects>", nil
}
