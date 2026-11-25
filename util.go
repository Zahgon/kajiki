// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/util.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// method makes a function of module kajiki.<module> written in Go, bound as
// a def with params would bind its arguments.
func method(module, class, name string, params []string, body func(bound []py.Value) (py.Value, error)) py.Value {
	qual := name
	if class != "" {
		qual = class + "." + name
	}
	return py.NewFunction(py.FuncSpec{Name: name, Qualname: qual, Module: "kajiki." + module, Params: params}, body)
}

// newClass makes the class name of module kajiki.<module> with the
// attributes attrs, deriving from object.
func newClass(module, name string, attrs map[string]py.Value) py.Value {
	return subclass(module, name, py.Builtin("object"), attrs)
}

// subclass makes the class name of module kajiki.<module> deriving from
// base, with the attributes attrs.
func subclass(module, name string, base py.Value, attrs map[string]py.Value) py.Value {
	names := []string{"__module__"}
	values := []py.Value{py.String("kajiki." + module)}
	for k, v := range attrs {
		names = append(names, k)
		values = append(values, v)
	}
	cls, err := py.NewClass(name, []py.Value{base}, py.StrDict(names, values))
	if err != nil {
		panic(err)
	}
	return cls
}

// expose is kajiki.expose: it marks a function as a template function.
var expose = method("util", "", "expose", []string{"func"}, func(a []py.Value) (py.Value, error) {
	if err := py.SetAttr(a[0], "exposed", py.Bool(true)); err != nil {
		return nil, err
	}
	return a[0], nil
})

// flattenerClass is kajiki.util.flattener: it wraps the iterator a template
// function returns, and iterating it walks nested flatteners in place,
// dropping the Nones. A template function's result, and literal(), are
// flatteners. It is made in init, with the other classes.
var flattenerClass py.Value

func flattenerMethods() map[string]py.Value {
	return map[string]py.Value{
		"__init__": method("util", "flattener", "__init__", []string{"self", "iterator"},
			func(a []py.Value) (py.Value, error) { return nil, initFlattener(a[0], a[1]) }),
		"decorate": wrapIn("classmethod", method("util", "flattener", "decorate", []string{"cls", "func"},
			func(a []py.Value) (py.Value, error) { return decorate(a[0], a[1]), nil })),
		"accumulate_str": method("util", "flattener", "accumulate_str", []string{"self"},
			func(a []py.Value) (py.Value, error) { return accumulateStr(a[0]) }),
		"__iter__": method("util", "flattener", "__iter__", []string{"self"},
			func(a []py.Value) (py.Value, error) { return flattenerIter(a[0]), nil }),
	}
}

// wrapIn is the builtin descriptor class named desc (classmethod,
// property) around fn.
func wrapIn(desc string, fn py.Value) py.Value {
	d, err := py.Call(py.Builtin(desc), []py.Value{fn}, nil)
	if err != nil {
		panic(err)
	}
	return d
}

// isFlattener is type(v) == flattener.
func isFlattener(v py.Value) bool { return py.Is(py.TypeOf(v), flattenerClass) }

// newFlattener is flattener(iterator).
func newFlattener(iterator py.Value) (py.Value, error) {
	return py.Call(flattenerClass, []py.Value{iterator}, nil)
}

// initFlattener is flattener.__init__: a flattener of flatteners wraps the
// innermost one's iterator.
func initFlattener(self, iterator py.Value) error {
	for isFlattener(iterator) {
		inner, err := py.GetAttr(iterator, "iterator")
		if err != nil {
			return err
		}
		iterator = inner
	}
	return py.SetAttr(self, "iterator", iterator)
}

// decorate is flattener.decorate: a function that calls func and wraps its
// result in cls.
func decorate(cls, fn py.Value) py.Value {
	return py.NewFunction(py.FuncSpec{Name: "inner", Qualname: "flattener.decorate.<locals>.inner",
		Module: "kajiki.util", VarArgs: "args", VarKw: "kwargs"}, func(b []py.Value) (py.Value, error) {
		leave, err := py.EnterRecursiveCall()
		defer leave()
		if err != nil {
			return nil, err
		}
		args, _ := py.SeqItems(b[0])
		r, err := py.Call(fn, args, keywordsOf(b[1]))
		if err != nil {
			return nil, err
		}
		return py.Call(cls, []py.Value{r}, nil)
	})
}

// keywordsOf turns a **kwargs dict back into keyword arguments.
func keywordsOf(d py.Value) []py.Keyword {
	keys, values, _ := py.DictItems(d)
	kw := make([]py.Keyword, len(keys))
	for i, k := range keys {
		name, _ := py.AsString(k)
		kw[i] = py.Keyword{Name: name, Value: values[i]}
	}
	return kw
}

// accumulateStr is flattener.accumulate_str: the concatenation of the
// strings the iterator and the flatteners it yields give, read with next()
// one level at a time.
func accumulateStr(self py.Value) (py.Value, error) {
	leave, err := py.EnterRecursiveCall()
	defer leave()
	if err != nil {
		return nil, err
	}
	it, err := py.GetAttr(self, "iterator")
	if err != nil {
		return nil, err
	}
	if isFlattener(it) {
		return py.CallMethod(it, "accumulate_str")
	}
	s := py.String("")
	stack := []py.Value{it}
	next := py.Builtin("next")
	for len(stack) > 0 {
		x, err := py.Call(next, []py.Value{stack[len(stack)-1]}, nil)
		if py.ErrorIs(err, "StopIteration") {
			stack = stack[:len(stack)-1]
			continue
		}
		if err != nil {
			return nil, err
		}
		switch {
		case isFlattener(x):
			inner, err := py.GetAttr(x, "iterator")
			if err != nil {
				return nil, err
			}
			stack = append(stack, inner)
		case py.IsNone(x):
		default:
			if s, err = py.InplaceOp("Add", s, x); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

// flattenerIter is flattener.__iter__, a generator: it starts iterating
// self.iterator at the first next(), then yields its items, those of the
// flatteners among them in their place, without the Nones. While it runs it
// counts as a Python frame towards the recursion limit, as the generator's
// frame does.
func flattenerIter(self py.Value) py.Value {
	var outer, inner *py.Iterator
	started := false
	return py.NewIterator(func() (py.Value, bool, error) {
		leave, err := py.EnterRecursiveCall()
		defer leave()
		if err != nil {
			started, outer, inner = true, nil, nil
			return nil, false, err
		}
		if !started {
			started = true
			it, err := py.GetAttr(self, "iterator")
			if err != nil {
				return nil, false, err
			}
			if outer, err = py.Iter(it); err != nil {
				return nil, false, err
			}
		}
		for outer != nil {
			if inner != nil {
				xx, ok, err := inner.Next()
				if err != nil {
					outer, inner = nil, nil
					return nil, false, err
				}
				if !ok {
					inner = nil
					continue
				}
				if !py.IsNone(xx) {
					return xx, true, nil
				}
				continue
			}
			x, ok, err := outer.Next()
			if err != nil || !ok {
				outer = nil
				return nil, false, err
			}
			if isFlattener(x) {
				if inner, err = py.Iter(x); err != nil {
					outer = nil
					return nil, false, err
				}
				continue
			}
			if !py.IsNone(x) {
				return x, true, nil
			}
		}
		return nil, false, nil
	})
}

// literal is kajiki.util.literal: text that renders unescaped, once.
var literal = method("util", "", "literal", []string{"text"}, func(a []py.Value) (py.Value, error) {
	it, err := py.Call(py.Builtin("iter"), []py.Value{py.List(a[0])}, nil)
	if err != nil {
		return nil, err
	}
	return newFlattener(it)
})
