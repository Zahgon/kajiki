// Ported from CPython 3.12 (Objects/typeobject.c object methods, Lib/copyreg.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The methods of object that every class inherits and dir() lists: __dir__, __format__,
// __getstate__, __reduce__, __reduce_ex__, __sizeof__ and the class method __subclasshook__; and the
// descriptors BaseException defines for its instances' attributes and methods.

package py

import "sync"

// objectMethods holds the methods objectMethod made: one object per
// method, as object's type dict holds them.
var objectMethods sync.Map

// objectMethod is the method name of object, unbound, when object has one
// of these.
func objectMethod(name string) (Value, bool) {
	if m, ok := objectMethods.Load(name); ok {
		return m.(Value), true
	}
	made, ok := makeObjectMethod(name)
	if !ok {
		return nil, false
	}
	m, _ := objectMethods.LoadOrStore(name, made)
	return m.(Value), true
}

// objectClassMethod is k.name for the class methods of object,
// __init_subclass__ and __subclasshook__: a builtin method bound to k, made
// on each lookup.
func objectClassMethod(k *classV, name string) (Value, bool) {
	if name != "__init_subclass__" && name != "__subclasshook__" {
		return nil, false
	}
	m, ok := builtinSlotMethod(objectClass, name)
	if !ok {
		return nil, false
	}
	c, ok := descriptorCallable(m)
	if !ok {
		return nil, false
	}
	return boundBuiltin(c, k), true
}

// objectInstanceAttr is self.name for an attribute of object no class of
// self's type overrides: object's slots as method-wrappers and its other
// methods as builtin methods bound to self, its class methods bound to the
// class k, and object.__new__ itself.
func objectInstanceAttr(self Value, k *classV, name string) (Value, bool) {
	if m, ok := objectClassMethod(k, name); ok {
		return m, true
	}
	switch name {
	case "__class__", "__doc__", "__dict__", "__module__", "__weakref__":
		return nil, false
	}
	m, ok := builtinSlotMethod(objectClass, name)
	if !ok {
		return nil, false
	}
	if name == "__new__" {
		return m, true
	}
	c, ok := descriptorCallable(m)
	if !ok {
		return nil, false
	}
	return boundBuiltin(c, self), true
}

// descriptorCallable is the function of a method descriptor of a builtin
// class.
func descriptorCallable(m Value) (*callable, bool) {
	switch x := m.(type) {
	case *callable:
		return x, true
	case *methodDescV:
		return x.callable, true
	}
	return nil, false
}

// boundBuiltin is the method c of a builtin class bound to self: a
// method-wrapper for a slot wrapper, a builtin method otherwise.
func boundBuiltin(c *callable, self Value) *callable {
	typ := "builtin_function_or_method"
	if c.typ == "wrapper_descriptor" {
		typ = "method-wrapper"
	}
	return &callable{name: c.name, typ: typ, qual: c.qual, self: self,
		fn: func(cc *callCtx, args []Value, kw *dictV) (Value, error) {
			return c.fn(cc, append([]Value{self}, args...), kw)
		}}
}

// makeObjectMethod builds the method objectMethod returns.
func makeObjectMethod(name string) (Value, bool) {
	var fn func(self Value, args []Value, kw *dictV) (Value, error)
	switch name {
	case "__dir__":
		fn = func(self Value, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 0, 0, args, kw); err != nil {
				return nil, err
			}
			return dirOf(self)
		}
	case "__format__":
		fn = func(self Value, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 1, 1, args, kw); err != nil {
				return nil, err
			}
			spec, ok := unsub(args[0]).(strV)
			if !ok {
				return nil, typeErr("__format__() argument must be str, not %s", typeName(args[0]))
			}
			if spec != "" {
				return nil, typeErr("unsupported format string passed to %s.__format__", typeName(self))
			}
			s, err := pyStr(self)
			if err != nil {
				return nil, err
			}
			return strV(s), nil
		}
	case "__getstate__":
		fn = func(self Value, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 0, 0, args, kw); err != nil {
				return nil, err
			}
			return objectState(self), nil
		}
	case "__reduce__":
		fn = func(self Value, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 0, 0, args, kw); err != nil {
				return nil, err
			}
			return reduceEx(self, 0)
		}
	case "__reduce_ex__":
		fn = func(self Value, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 1, 1, args, kw); err != nil {
				return nil, err
			}
			p, err := methodsSsize(args[0])
			if err != nil {
				return nil, err
			}
			if k := userClassOf(self); k != nil {
				if m, ok := k.lookupUser("__reduce__"); ok {
					return callValue(nil, bindDescriptor(m, self, k), nil, nil)
				}
			}
			return reduceEx(self, p)
		}
	case "__sizeof__":
		fn = func(self Value, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 0, 0, args, kw); err != nil {
				return nil, err
			}
			size := 16
			if k := userClassOf(self); k != nil {
				if slots, err := classSlots(k.dict, k.bases); err == nil {
					size += 8 * len(slots)
				}
			}
			return mkInt(int64(size)), nil
		}
	case "__subclasshook__":
		return &callable{name: name, typ: "builtin_function_or_method", qual: "object." + name,
			fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
				return notImplementedV{}, nil
			}}, true
	default:
		return nil, false
	}
	return &methodDescV{&callable{name: name, typ: "method_descriptor", qual: "object." + name,
		fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
			if len(args) == 0 {
				return nil, typeErr("unbound method object.%s() needs an argument", name)
			}
			return fn(args[0], args[1:], kw)
		}}}, true
}

// objectState is object.__getstate__(): the instance's __dict__, None when
// it has none or an empty one.
func objectState(self Value) Value {
	if x, ok := self.(*instV); ok && x.dict != nil && x.dict.Len() > 0 {
		return x.dict
	}
	return none
}

// copyregNewobj is copyreg.__newobj__(cls, *args): cls.__new__(cls, *args).
func copyregNewobj() Value {
	return FuncQual("__newobj__", "__newobj__", "copyreg", func(args []Value, kwargs []Keyword) (Value, error) {
		if len(args) == 0 {
			return nil, toError(typeErr("__newobj__() missing 1 required positional argument: 'cls'"))
		}
		return CallMethodKw(args[0], "__new__", args, kwargs)
	})
}

// copyregReconstructor is copyreg._reconstructor(cls, base, state): an
// instance of cls made by base.__new__, initialised from state when base
// is not object.
func copyregReconstructor() Value {
	return FuncQual("_reconstructor", "_reconstructor", "copyreg", func(args []Value, kwargs []Keyword) (Value, error) {
		if err := CheckPositional("_reconstructor", len(args), 3, 3); err != nil {
			return nil, err
		}
		if Is(args[1], objectClass) {
			return CallMethod(objectClass, "__new__", args[0])
		}
		obj, err := CallMethod(args[1], "__new__", args[0], args[2])
		if err != nil {
			return nil, err
		}
		if init, err := GetAttr(args[1], "__init__"); err == nil && !Is(init, objectClass) {
			if _, err := Call(init, []Value{obj, args[2]}, nil); err != nil {
				return nil, err
			}
		}
		return obj, nil
	})
}

// reduceEx is object.__reduce_ex__(protocol) for an object whose class does
// not define __reduce__: copyreg._reduce_ex below protocol 2, the
// __newobj__ reduction from protocol 2 on.
func reduceEx(self Value, protocol int) (Value, error) {
	k := classOf(self)
	state := objectState(self)
	if protocol >= 2 {
		return newTuple([]Value{copyregNewobj(), newTuple([]Value{k}), state, none, none}), nil
	}
	return newTuple([]Value{copyregReconstructor(), newTuple([]Value{k, objectClass, none}), state}), nil
}

// baseExceptionAttr is an attribute of the class BaseException: the data
// descriptors of args, __cause__, __context__, __suppress_context__ and
// __traceback__, and the methods add_note, with_traceback, __reduce__ and
// __setstate__, which call the instance's own.
func baseExceptionAttr(name string) (Value, bool) {
	switch name {
	case "args", "__cause__", "__context__", "__traceback__":
		return &dataDescV{name: name, owner: "BaseException"}, true
	case "__suppress_context__":
		return &dataDescV{name: name, owner: "BaseException", member: true}, true
	case "add_note", "with_traceback", "__reduce__", "__setstate__":
		return &methodDescV{&callable{name: name, typ: "method_descriptor", qual: "BaseException." + name,
			fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
				if len(args) == 0 {
					return nil, typeErr("unbound method BaseException.%s() needs an argument", name)
				}
				x, ok := args[0].(*excV)
				if !ok {
					return nil, typeErr("descriptor '%s' for 'BaseException' objects doesn't apply to a '%s' object",
						name, typeName(args[0]))
				}
				m, err := x.getAttr(name)
				if err != nil {
					return nil, err
				}
				return callValue(nil, m, args[1:], kw)
			}}}, true
	}
	return nil, false
}
