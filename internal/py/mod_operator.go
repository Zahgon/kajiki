// Ported from CPython 3.12 (Modules/_operator.c, Lib/operator.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"strings"
)

// operatorBinary are the binary operators of the operator module with the
// ast class of each; dunder marks those that also have a __name__ alias.
var operatorBinary = []struct {
	name, op string
	dunder   bool
}{
	{"add", "Add", true}, {"sub", "Sub", true}, {"mul", "Mult", true}, {"truediv", "Div", true},
	{"floordiv", "FloorDiv", true}, {"mod", "Mod", true}, {"pow", "Pow", true}, {"lshift", "LShift", true},
	{"rshift", "RShift", true}, {"and_", "BitAnd", false}, {"or_", "BitOr", false}, {"xor", "BitXor", true},
	{"matmul", "MatMult", true},
}

// operatorCompare are the rich comparisons of the operator module.
var operatorCompare = []struct{ name, op string }{
	{"lt", "<"}, {"le", "<="}, {"eq", "=="}, {"ne", "!="}, {"gt", ">"}, {"ge", ">="},
}

func init() {
	register("operator", func(b *builder) {
		def := func(name string, f fn, dunder bool) {
			b.fn(name, f)
			if dunder {
				b.set("__"+strings.TrimSuffix(name, "_")+"__", BuiltinFunc(name, f))
			}
		}
		for _, o := range operatorBinary {
			op := o.op
			name := o.name
			def(name, binaryOperator(name, func(a, c Value) (Value, error) { return BinaryOp(op, a, c) }), o.dunder)
			iname := "i" + strings.TrimSuffix(name, "_")
			def(iname, binaryOperator(iname, func(a, c Value) (Value, error) { return InplaceOp(op, a, c) }), true)
		}
		b.set("__and__", BuiltinFunc("and_", binaryOperator("and_", func(a, c Value) (Value, error) { return BinaryOp("BitAnd", a, c) })))
		b.set("__or__", BuiltinFunc("or_", binaryOperator("or_", func(a, c Value) (Value, error) { return BinaryOp("BitOr", a, c) })))
		for _, o := range operatorCompare {
			op := o.op
			def(o.name, binaryOperator(o.name, func(a, c Value) (Value, error) { return RichCompare(op, a, c) }), true)
		}
		for _, u := range []struct{ name, op string }{{"neg", "USub"}, {"pos", "UAdd"}, {"invert", "Invert"}, {"inv", "Invert"}} {
			op := u.op
			def(u.name, unaryOperator(u.name, func(v Value) (Value, error) { return UnaryOp(op, v) }), true)
		}
		def("not_", unaryOperator("not_", func(v Value) (Value, error) {
			t, err := Truth(v)
			return Bool(!t), err
		}), true)
		def("truth", unaryOperator("truth", func(v Value) (Value, error) {
			t, err := Truth(v)
			return Bool(t), err
		}), false)
		def("abs", unaryOperator("abs", func(v Value) (Value, error) { return call(Builtin("abs"), v) }), true)
		def("index", unaryOperator("index", func(v Value) (Value, error) {
			n, err := Index(v)
			if err != nil {
				return nil, err
			}
			return BigInt(n), nil
		}), true)
		def("is_", binaryOperator("is_", func(a, c Value) (Value, error) { return Bool(Is(a, c)), nil }), false)
		def("is_not", binaryOperator("is_not", func(a, c Value) (Value, error) { return Bool(!Is(a, c)), nil }), false)
		def("contains", binaryOperator("contains", func(a, c Value) (Value, error) {
			in, err := Contains(a, c)
			return Bool(in), err
		}), true)
		def("concat", binaryOperator("concat", concat), true)
		def("iconcat", binaryOperator("iconcat", func(a, c Value) (Value, error) {
			if err := checkSequence(a); err != nil {
				return nil, err
			}
			return InplaceOp("Add", a, c)
		}), true)
		def("getitem", binaryOperator("getitem", GetItem), true)
		def("delitem", binaryOperator("delitem", func(a, c Value) (Value, error) { return None(), DelItem(a, c) }), true)
		def("setitem", func(args []Value, kwargs []Keyword) (Value, error) {
			if err := fixedArgs("setitem", 3, args, kwargs); err != nil {
				return nil, err
			}
			return None(), SetItem(args[0], args[1], args[2])
		}, true)
		def("countOf", binaryOperator("countOf", func(a, c Value) (Value, error) {
			n := 0
			err := ForEach(a, func(item Value) error {
				eq := Is(item, c)
				if !eq {
					var err error
					if eq, err = Compare("==", item, c); err != nil {
						return err
					}
				}
				if eq {
					n++
				}
				return nil
			})
			return Int(int64(n)), err
		}), false)
		def("indexOf", binaryOperator("indexOf", indexOf), false)
		def("length_hint", lengthHint, false)
		def("call", func(args []Value, kwargs []Keyword) (Value, error) {
			if len(args) == 0 {
				return nil, raise("TypeError", "call() missing 1 required positional argument: 'obj'")
			}
			return Call(args[0], args[1:], kwargs)
		}, true)
		b.iterlessClass("itemgetter", newItemgetter)
		b.iterlessClass("attrgetter", newAttrgetter)
		b.iterlessClass("methodcaller", newMethodcaller)
	})
}

func binaryOperator(name string, f func(a, b Value) (Value, error)) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := fixedArgs(name, 2, args, kwargs); err != nil {
			return nil, err
		}
		return f(args[0], args[1])
	}
}

func unaryOperator(name string, f func(v Value) (Value, error)) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := ArgsOne(name, args, kwargs); err != nil {
			return nil, err
		}
		return f(args[0])
	}
}

// checkSequence is the check of concat and iconcat that the first operand
// supports indexing as a sequence.
func checkSequence(a Value) error {
	if IsDict(a) {
		return raise("TypeError", "'%s' object can't be concatenated", TypeName(a))
	}
	if has, err := HasAttr(a, "__getitem__"); err != nil || !has {
		if err != nil {
			return err
		}
		return raise("TypeError", "'%s' object can't be concatenated", TypeName(a))
	}
	return nil
}

func concat(a, b Value) (Value, error) {
	if err := checkSequence(a); err != nil {
		return nil, err
	}
	return BinaryOp("Add", a, b)
}

func indexOf(a, b Value) (Value, error) {
	i := 0
	found := -1
	err := ForEach(a, func(item Value) error {
		if found >= 0 {
			return nil
		}
		eq := Is(item, b)
		if !eq {
			var err error
			if eq, err = Compare("==", item, b); err != nil {
				return err
			}
		}
		if eq {
			found = i
		}
		i++
		return nil
	})
	if err != nil {
		return nil, err
	}
	if found < 0 {
		return nil, raise("ValueError", "sequence.index(x): x not in sequence")
	}
	return Int(int64(found)), nil
}

// lengthHint is operator.length_hint(obj, default=0).
func lengthHint(args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("length_hint", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("length_hint", len(args), 1, 2); err != nil {
		return nil, err
	}
	dflt := Value(Int(0))
	if len(args) == 2 {
		if _, ok := AsInt(args[1]); !ok {
			return nil, raise("TypeError", "'%s' object cannot be interpreted as an integer", TypeName(args[1]))
		}
		dflt = args[1]
	}
	if n, err := Len(args[0]); err == nil {
		return Int(int64(n)), nil
	} else if !ErrorIs(err, "TypeError") {
		return nil, err
	}
	has, err := HasAttr(TypeOf(args[0]), "__length_hint__")
	if err != nil {
		return nil, err
	}
	if has {
		r, err := CallMethod(args[0], "__length_hint__")
		if err != nil {
			return nil, err
		}
		if IsNotImplemented(r) {
			return dflt, nil
		}
		return r, nil
	}
	return dflt, nil
}

// getterState is the state of an itemgetter, attrgetter or methodcaller.
type getterState struct {
	call func(obj Value) (Value, error)
	repr func() (string, error)
}

// iterlessClass makes a callable class of the module whose instances carry
// a *getterState built by newFn.
func (b *builder) iterlessClass(name string, newFn func(args []Value, kwargs []Keyword) (*getterState, error)) Value {
	ms := &methods{}
	ms.method("__new__", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := newFn(args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		return instance(args[0], st)
	})
	ms.method("__call__", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := stateOf[*getterState](args[0], name, "__call__")
		if err != nil {
			return nil, err
		}
		if err := NoKeywords(name, kwargs); err != nil {
			return nil, err
		}
		if err := CheckPositional(name, len(args)-1, 1, 1); err != nil {
			return nil, err
		}
		return st.call(args[1])
	})
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		st, err := stateOf[*getterState](args[0], name, "__repr__")
		if err != nil {
			return nil, err
		}
		s, err := st.repr()
		return String(s), err
	})
	return b.cClass(name, []Value{Builtin("object")}, ms)
}

// joinReprs is the reprs of vs separated by commas.
func joinReprs(vs []Value) (string, error) {
	parts := make([]string, len(vs))
	for i, v := range vs {
		s, err := Repr(v)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	return strings.Join(parts, ", "), nil
}

func newItemgetter(args []Value, kwargs []Keyword) (*getterState, error) {
	if err := NoKeywords("itemgetter", kwargs); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, raise("TypeError", "itemgetter expected 1 argument, got 0")
	}
	items := append([]Value(nil), args...)
	return &getterState{
		call: func(obj Value) (Value, error) {
			if len(items) == 1 {
				return GetItem(obj, items[0])
			}
			out := make([]Value, len(items))
			for i, it := range items {
				v, err := GetItem(obj, it)
				if err != nil {
					return nil, err
				}
				out[i] = v
			}
			return Tuple(out...), nil
		},
		repr: func() (string, error) {
			s, err := joinReprs(items)
			return "operator.itemgetter(" + s + ")", err
		},
	}, nil
}

func newAttrgetter(args []Value, kwargs []Keyword) (*getterState, error) {
	if err := NoKeywords("attrgetter", kwargs); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, raise("TypeError", "attrgetter expected 1 argument, got 0")
	}
	var paths [][]string
	for _, a := range args {
		s, ok := AsString(a)
		if !ok {
			return nil, raise("TypeError", "attribute name must be a string")
		}
		paths = append(paths, strings.Split(s, "."))
	}
	get := func(obj Value, path []string) (Value, error) {
		for _, name := range path {
			v, err := GetAttr(obj, name)
			if err != nil {
				return nil, err
			}
			obj = v
		}
		return obj, nil
	}
	names := append([]Value(nil), args...)
	return &getterState{
		call: func(obj Value) (Value, error) {
			if len(paths) == 1 {
				return get(obj, paths[0])
			}
			out := make([]Value, len(paths))
			for i, p := range paths {
				v, err := get(obj, p)
				if err != nil {
					return nil, err
				}
				out[i] = v
			}
			return Tuple(out...), nil
		},
		repr: func() (string, error) {
			s, err := joinReprs(names)
			return "operator.attrgetter(" + s + ")", err
		},
	}, nil
}

func newMethodcaller(args []Value, kwargs []Keyword) (*getterState, error) {
	if len(args) == 0 {
		return nil, raise("TypeError", "methodcaller needs at least one argument, the method name")
	}
	name, ok := AsString(args[0])
	if !ok {
		return nil, raise("TypeError", "method name must be a string")
	}
	rest := append([]Value(nil), args[1:]...)
	kws := append([]Keyword(nil), kwargs...)
	return &getterState{
		call: func(obj Value) (Value, error) {
			m, err := GetAttr(obj, name)
			if err != nil {
				return nil, err
			}
			return Call(m, rest, kws)
		},
		repr: func() (string, error) {
			parts := []string{}
			r, err := Repr(args[0])
			if err != nil {
				return "", err
			}
			parts = append(parts, r)
			for _, a := range rest {
				s, err := Repr(a)
				if err != nil {
					return "", err
				}
				parts = append(parts, s)
			}
			for _, k := range kws {
				s, err := Repr(k.Value)
				if err != nil {
					return "", err
				}
				parts = append(parts, k.Name+"="+s)
			}
			return "operator.methodcaller(" + strings.Join(parts, ", ") + ")", nil
		},
	}, nil
}
