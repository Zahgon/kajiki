// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// What the standard modules written in Go share (math, re, string, itertools, functools,
// operator, collections and json): each registers itself with RegisterModule and builds its
// functions and classes with these helpers, to behave as CPython 3.12's: the same results, reprs,
// argument handling and exception messages.

package py

import (
	"fmt"
)

// fn is a builtin function of a module.
type fn = func(args []Value, kwargs []Keyword) (Value, error)

// raise returns the builtin exception class(msg).
func raise(class, format string, a ...any) error {
	if len(a) > 0 {
		format = fmt.Sprintf(format, a...)
	}
	return Raise(class, String(format))
}

// builder fills a module's namespace from its init function; the first
// error it meets is kept in err and stops the import.
type builder struct {
	mod Value
	err error
}

// set binds a module attribute.
func (b *builder) set(name string, v Value) {
	if b.err == nil {
		b.err = SetAttr(b.mod, name, v)
	}
}

// fn binds a builtin function (one CPython implements in C).
func (b *builder) fn(name string, f fn) { b.set(name, BuiltinFunc(name, f)) }

// pyfn binds a function CPython's module implements in Python: its type is
// function, and as a class attribute it binds like one.
func (b *builder) pyfn(name string, f fn) {
	if b.err != nil {
		return
	}
	modName, err := GetAttr(b.mod, "__name__")
	if err != nil {
		b.err = err
		return
	}
	mod, _ := AsString(modName)
	b.set(name, FuncQual(name, name, mod, f))
}

// class makes a class of the module (__module__ the module's name,
// __qualname__ name) with the given bases and namespace, and binds it.
func (b *builder) class(name string, bases []Value, ms *methods) Value {
	cls := b.newClass(name, bases, ms)
	b.set(name, cls)
	return cls
}

// newClass is class without binding the class in the module.
func (b *builder) newClass(name string, bases []Value, ms *methods) Value {
	if b.err == nil {
		b.err = ms.err
	}
	if b.err != nil {
		return None()
	}
	modName, err := GetAttr(b.mod, "__name__")
	if err != nil {
		b.err = err
		return None()
	}
	names := append([]string{"__module__", "__qualname__"}, ms.names...)
	values := append([]Value{modName, String(name)}, ms.values...)
	cls, err := NewClass(name, bases, StrDict(names, values))
	if err != nil {
		b.err = err
		return None()
	}
	return cls
}

// source runs Python source in the module's namespace, for the parts of a
// module CPython writes in Python; filename names it in tracebacks.
func (b *builder) source(filename, src string) {
	if b.err != nil {
		return
	}
	code, err := Compile(src, filename, "exec")
	if err != nil {
		b.err = err
		return
	}
	b.err = code.Exec(ModuleDict(b.mod))
}

// register makes the module name importable, filled by init.
func register(name string, init func(b *builder)) {
	RegisterModuleInit(name, func(m Value) error {
		b := &builder{mod: m}
		init(b)
		return b.err
	})
}

// methods is the namespace of a class written in Go, in definition order.
type methods struct {
	names  []string
	values []Value
	err    error
}

// method adds a method; it receives the instance as args[0].
func (ms *methods) method(name string, f fn) {
	ms.add(name, Func(name, f))
}

// add adds any class attribute.
func (ms *methods) add(name string, v Value) {
	ms.names = append(ms.names, name)
	ms.values = append(ms.values, v)
}

// wrap adds the functions fs wrapped by the builtin descriptor class named
// desc: classmethod(f), property(f) or property(fget, fset).
func (ms *methods) wrap(desc, name string, fs ...Value) {
	d, err := Call(Builtin(desc), fs, nil)
	if err != nil {
		if ms.err == nil {
			ms.err = err
		}
		return
	}
	ms.add(name, d)
}

// getset adds a property of two Go functions, the getter and the setter.
func (ms *methods) getset(name string, get, set fn) {
	ms.wrap("property", name, Func("fget", get), Func("fset", set))
}

// classmethod adds a class method; it receives the class as args[0].
func (ms *methods) classmethod(name string, f fn) { ms.wrap("classmethod", name, Func(name, f)) }

// property adds a read-only property computed by get from the instance.
func (ms *methods) property(name string, get func(self Value) (Value, error)) {
	ms.wrap("property", name, Func(name, func(args []Value, _ []Keyword) (Value, error) { return get(args[0]) }))
}

// instance makes an instance of cls (a class of this package deriving from
// object) carrying the Go state.
func instance(cls Value, state any) (Value, error) {
	v, err := NewInstance(cls, nil)
	if err != nil {
		return nil, err
	}
	return v, SetNative(v, state)
}

// stateOf is the Go state of self, an instance of the class named
// className, with the TypeError a method raises for another receiver.
func stateOf[T any](self Value, className, method string) (T, error) {
	s, ok := Native(self).(T)
	if !ok {
		var zero T
		return zero, raise("TypeError", "descriptor '%s' for '%s' objects doesn't apply to a '%s' object",
			method, className, TypeName(self))
	}
	return s, nil
}

// kwarg returns the keyword argument name, or nil.
func kwarg(kwargs []Keyword, name string) Value {
	for _, k := range kwargs {
		if k.Name == name {
			return k.Value
		}
	}
	return nil
}

// call is f(*args), Call with the arguments listed and no keywords.
func call(f Value, args ...Value) (Value, error) { return Call(f, args, nil) }

// goIter is the state of an iterator object written in Go: next returns the
// next item, ok false at the end, after which the iterator stays exhausted;
// repr, when set, is the object's repr given its class name; data is what
// else a class keeps about its instances.
type goIter struct {
	next func() (v Value, ok bool, err error)
	repr func(className string) (string, error)
	data any
	done bool
}

// iterClass makes an iterator class of the module: newFn builds the state
// of an instance from the constructor's arguments, and more holds further
// class attributes. The class has __repr__ when withRepr is set.
func (b *builder) iterClass(name string, newFn func(args []Value, kwargs []Keyword) (*goIter, error),
	withRepr bool, more *methods) Value {
	ms := &methods{}
	if more != nil {
		*ms = *more
	}
	ms.method("__new__", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := newFn(args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		return instance(args[0], st)
	})
	addIterMethods(ms, name)
	if withRepr {
		ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
			st, err := stateOf[*goIter](args[0], name, "__repr__")
			if err != nil {
				return nil, err
			}
			s, err := st.repr(ClassName(TypeOf(args[0])))
			if err != nil {
				return nil, err
			}
			return String(s), nil
		})
	}
	return b.cClass(name, []Value{Builtin("object")}, ms)
}

// cClass is class for a class CPython implements in C: error messages name
// its instances' type module.name, as its tp_name does.
func (b *builder) cClass(name string, bases []Value, ms *methods) Value {
	cls := b.class(name, bases, ms)
	if b.err == nil {
		modName, err := GetAttr(b.mod, "__name__")
		if err != nil {
			b.err = err
			return cls
		}
		mod, _ := AsString(modName)
		SetTypeName(cls, mod+"."+name)
	}
	return cls
}

// addIterMethods adds __iter__ and __next__ over a *goIter state.
func addIterMethods(ms *methods, name string) {
	ms.method("__iter__", func(args []Value, _ []Keyword) (Value, error) { return args[0], nil })
	ms.method("__next__", func(args []Value, _ []Keyword) (Value, error) {
		st, err := stateOf[*goIter](args[0], name, "__next__")
		if err != nil {
			return nil, err
		}
		return st.step()
	})
}

// step advances the iterator: the next item, or StopIteration.
func (st *goIter) step() (Value, error) {
	if st.done {
		return nil, Raise("StopIteration")
	}
	v, ok, err := st.next()
	if err != nil {
		return nil, err
	}
	if !ok {
		st.done = true
		return nil, Raise("StopIteration")
	}
	return v, nil
}
