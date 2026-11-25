// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Interfaces the value layer dispatches through, str and int subclasses, the
// builtins beyond the classes and functions of pybuiltins.go, and compare
// helpers.

package py

import (
	"strings"
)

type getitemer interface {
	getItem(key Value) (Value, error)
}
type setitemer interface {
	setItem(key, value Value) error
}
type delitemer interface {
	delItem(key Value) error
}
type attrGetter interface {
	getAttr(name string) (Value, error)
}
type attrSetter interface {
	setAttr(name string, value Value) error
}
type attrDeleter interface {
	delAttr(name string) error
}
type truther interface {
	truth() (bool, error)
}
type callableObj interface {
	pyCall(args []Value, kw *dictV) (Value, error)
}
type callableChecker interface {
	isCallable() bool
}

// unsub gives the value of a builtin type inside an instance of a subclass
// of it: the str of a host type deriving from str, the base
// value of an instance of a Python-defined class deriving from a builtin
// type, or v itself.
func unsub(v Value) Value {
	switch x := v.(type) {
	case *instV:
		if x.base != nil {
			return unsub(x.base)
		}
	}
	return v
}

// isStrSubclass reports whether v is an instance of a str subclass.
func isStrSubclass(v Value) bool {
	switch x := v.(type) {
	case *instV:
		_, ok := unsub(x).(strV)
		return ok
	}
	return false
}

// isBuiltinSubclass reports whether v is an instance of a subclass of a
// builtin type that unsub unwraps.
func isBuiltinSubclass(v Value) bool {
	if x, ok := v.(*instV); ok {
		return x.base != nil
	}
	return isStrSubclass(v)
}

func unsubAll(args []Value) []Value {
	for i, a := range args {
		if isStrSubclass(a) {
			out := append([]Value(nil), args...)
			for j := i; j < len(out); j++ {
				out[j] = unsub(out[j])
			}
			return out
		}
	}
	return args
}

// commonAttr holds the attributes every object answers to.
func commonAttr(obj Value, name string) (Value, bool) {
	switch name {
	case "__class__":
		if _, ok := obj.(*classV); ok {
			return classes["type"], true
		}
		return classOf(obj), true
	case "__name__", "__qualname__":
		if k, ok := obj.(*classV); ok {
			return strV(k.name), true
		}
		if c, ok := obj.(*callable); ok {
			return strV(c.name), true
		}
	}
	return nil, false
}

// allPythonBuiltins is dir(builtins) under CPython 3.12.
var allPythonBuiltins = strings.Fields(`ArithmeticError AssertionError AttributeError BaseException
BaseExceptionGroup BlockingIOError BrokenPipeError BufferError BytesWarning ChildProcessError
ConnectionAbortedError ConnectionError ConnectionRefusedError ConnectionResetError DeprecationWarning EOFError
Ellipsis EncodingWarning EnvironmentError Exception ExceptionGroup False FileExistsError FileNotFoundError
FloatingPointError FutureWarning GeneratorExit IOError ImportError ImportWarning IndentationError IndexError
InterruptedError IsADirectoryError KeyError KeyboardInterrupt LookupError MemoryError ModuleNotFoundError
NameError None NotADirectoryError NotImplemented NotImplementedError OSError OverflowError
PendingDeprecationWarning PermissionError ProcessLookupError RecursionError ReferenceError ResourceWarning
RuntimeError RuntimeWarning StopAsyncIteration StopIteration SyntaxError SyntaxWarning SystemError SystemExit
TabError TimeoutError True TypeError UnboundLocalError UnicodeDecodeError UnicodeEncodeError UnicodeError
UnicodeTranslateError UnicodeWarning UserWarning ValueError Warning ZeroDivisionError __build_class__ __debug__
__doc__ __import__ __loader__ __name__ __package__ __spec__ abs aiter all anext any ascii bin bool breakpoint
bytearray bytes callable chr classmethod compile complex copyright credits delattr dict dir divmod enumerate
eval exec exit filter float format frozenset getattr globals hasattr hash help hex id input int isinstance
issubclass iter len license list locals map max memoryview min next object oct open ord pow print property quit
range repr reversed round set setattr slice sorted staticmethod str sum super tuple type vars zip`)

// initExtraBuiltins adds the builtins beyond the classes and functions of
// pybuiltins.go.
func initExtraBuiltins() {
	classes["object"] = objectClass
	for _, singleton := range []struct {
		name, typ string
		v         Value
	}{{"ellipsis", "EllipsisType", ellipsisV{}}, {"NoneType", "NoneType", none},
		{"NotImplementedType", "NotImplementedType", notImplementedV{}}} {
		singleton := singleton
		cls(singleton.name, objectClass, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if len(args) > 0 || kw != nil && kw.Len() > 0 {
				return nil, typeErr("%s takes no arguments", singleton.typ)
			}
			return singleton.v, nil
		})
	}
	objectClass.construct = func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if len(args) > 0 || kw != nil && kw.Len() > 0 {
			return nil, typeErr("object() takes no arguments")
		}
		return &objectV{}, nil
	}
	typeC := classes["type"]
	typeC.construct = func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if len(args) != 1 && len(args) != 3 {
			return nil, typeErr("type() takes 1 or 3 arguments")
		}
		if len(args) == 3 {
			return typeNew(c, typeC, args, kw)
		}
		if err := noKw("type", kw); err != nil {
			return nil, err
		}
		return classOf(args[0]), nil
	}
	extra := map[string]func(c *callCtx, args []Value, kw *dictV) (Value, error){
		"callable": oneArg("callable", func(v Value) (Value, error) { return pyBool(isCallable(v)), nil }),
		"ascii": oneArg("ascii", func(v Value) (Value, error) {
			s, err := fmtASCII(v)
			return strV(s), err
		}),
		"getattr": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := noKw("getattr", kw); err != nil {
				return nil, err
			}
			if len(args) < 2 {
				return nil, typeErr("getattr expected at least 2 arguments, got %d", len(args))
			}
			if len(args) > 3 {
				return nil, typeErr("getattr expected at most 3 arguments, got %d", len(args))
			}
			name, ok := unsub(args[1]).(strV)
			if !ok {
				return nil, typeErr("attribute name must be string, not '%s'", typeName(args[1]))
			}
			v, err := pyGetattr(args[0], string(name))
			if err != nil && len(args) == 3 && isExc(err, "AttributeError") {
				return args[2], nil
			}
			return v, err
		},
		"hasattr": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("hasattr", 2, 2, args, kw); err != nil {
				return nil, err
			}
			name, ok := unsub(args[1]).(strV)
			if !ok {
				return nil, typeErr("attribute name must be string, not '%s'", typeName(args[1]))
			}
			_, err := pyGetattr(args[0], string(name))
			if err != nil {
				if isExc(err, "AttributeError") {
					return falseV, nil
				}
				return nil, err
			}
			return trueV, nil
		},
		"setattr": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("setattr", 3, 3, args, kw); err != nil {
				return nil, err
			}
			name, ok := unsub(args[1]).(strV)
			if !ok {
				return nil, typeErr("attribute name must be string, not '%s'", typeName(args[1]))
			}
			return none, pySetattr(args[0], string(name), args[2])
		},
		"delattr": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("delattr", 2, 2, args, kw); err != nil {
				return nil, err
			}
			name, ok := unsub(args[1]).(strV)
			if !ok {
				return nil, typeErr("attribute name must be string, not '%s'", typeName(args[1]))
			}
			return none, pyDelattr(args[0], string(name))
		},
		"issubclass": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("issubclass", 2, 2, args, kw); err != nil {
				return nil, err
			}
			if cls, ok := args[1].(*classV); ok {
				if r, hooked, err := metaHook(cls, "__subclasscheck__", args[0]); hooked {
					return pyBool(r), err
				}
			}
			k, ok := args[0].(*classV)
			if !ok {
				return nil, typeErr("issubclass() arg 1 must be a class")
			}
			match := func(o Value) (bool, error) {
				ok2, isCls := o.(*classV)
				if !isCls {
					return false, typeErr("issubclass() arg 2 must be a class, a tuple of classes, or a union")
				}
				return k.subclassOf(ok2) || ok2 == objectClass, nil
			}
			classinfo := args[1]
			if u, ok := classinfo.(*unionV); ok {
				classinfo = newTuple(u.args)
			}
			if t, ok := classinfo.(*tupleV); ok {
				for _, o := range t.items {
					m, err := match(o)
					if err != nil || m {
						return pyBool(m), err
					}
				}
				return falseV, nil
			}
			m, err := match(args[1])
			return pyBool(m), err
		},
		"iter": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := noKw("iter", kw); err != nil {
				return nil, err
			}
			switch len(args) {
			case 0:
				return nil, typeErr("iter expected at least 1 argument, got 0")
			case 1:
				return makeIterObject(args[0])
			case 2:
				return callableIter(args[0], args[1])
			}
			return nil, typeErr("iter expected at most 2 arguments, got %d", len(args))
		},
		"next": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("next", 1, 2, args, kw); err != nil {
				return nil, err
			}
			var dflt Value
			if len(args) == 2 {
				dflt = args[1]
			}
			return builtinNext(args[0], dflt)
		},
		"print": builtinPrint,
	}
	for name, fn := range extra {
		builtinsTable[name] = newBuiltin(name, fn)
	}
	builtinsTable["NotImplemented"] = notImplementedV{}
	builtinsTable["Ellipsis"] = ellipsisV{}
	builtinsTable["__name__"] = strV("builtins")
	builtinsTable["__package__"] = strV("")
	builtinsTable["__debug__"] = trueV
	builtinsTable["exec"] = newBuiltin("exec", execEvalBuiltin("exec", false))
	builtinsTable["eval"] = newBuiltin("eval", execEvalBuiltin("eval", true))
	builtinsTable["compile"] = newBuiltin("compile", builtinCompile)
	builtinsTable["input"] = newBuiltin("input", builtinInput)
	builtinsTable["__import__"] = newBuiltin("__import__", builtinImport)
	builtinsTable["globals"] = newBuiltin("globals", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("globals", 0, 0, args, kw); err != nil {
			return nil, err
		}
		if c == nil || c.f == nil {
			return newDict(), nil
		}
		return c.f.vm.globals, nil
	})
	builtinsTable["vars"] = newBuiltin("vars", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("vars", 0, 1, args, kw); err != nil {
			return nil, err
		}
		if len(args) == 0 {
			if c == nil || c.f == nil {
				return newDict(), nil
			}
			return localsOf(c.f), nil
		}
		d, err := pyGetattr(args[0], "__dict__")
		if err != nil {
			if isExc(err, "AttributeError") {
				return nil, typeErr("vars() argument must have __dict__ attribute")
			}
			return nil, err
		}
		return d, nil
	})
	builtinsTable["locals"] = newBuiltin("locals", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("locals", 0, 0, args, kw); err != nil {
			return nil, err
		}
		if c == nil || c.f == nil {
			return newDict(), nil
		}
		return localsOf(c.f), nil
	})
	cls("super", objectClass, builtinSuper)
	cls("property", objectClass, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		buf, err := methodsArgsKw("property", []string{"fget", "fset", "fdel", "doc"}, 4, args, kw)
		if err != nil {
			return nil, err
		}
		p := &propertyV{}
		for i, dst := range []*Value{&p.fget, &p.fset, &p.fdel, &p.doc} {
			if buf[i] != nil && !isNoneV(buf[i]) {
				*dst = buf[i]
			}
		}
		return p, nil
	})
	cls("staticmethod", objectClass, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("staticmethod", 1, 1, args, kw); err != nil {
			return nil, err
		}
		return &staticmethodV{fn: args[0]}, nil
	})
	cls("classmethod", objectClass, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("classmethod", 1, 1, args, kw); err != nil {
			return nil, err
		}
		return &classmethodV{fn: args[0]}, nil
	})
	builtinsTable["__doc__"] = strV(builtinsDoc)
	builtinsTable["__loader__"] = builtinImporterClass
	builtinsTable["__spec__"] = &moduleSpecV{name: "builtins", loader: builtinImporterClass, origin: "built-in"}
	// The objects the site module adds keep their reprs; exit() and quit()
	// raise SystemExit, while the printers and help, which write to the
	// terminal, are not callable here.
	for name, text := range sitePrinters {
		o := &siteObjV{class: "_Printer", text: text}
		switch name {
		case "exit", "quit":
			o.class = "Quitter"
		case "help":
			o.class = "_Helper"
		}
		builtinsTable[name] = o
	}

}

// builtinNext is next(v) or, when dflt is not nil, next(v, dflt).
func builtinNext(v, dflt Value) (Value, error) {
	if !isIterator(v) {
		// Builtin objects that are their own iterators (files) provide
		// __next__ as an attribute.
		if ga, ok := v.(attrGetter); ok && userClassOf(v) == nil {
			if m, err := ga.getAttr("__next__"); err != nil || m != nil {
				if err != nil {
					return nil, err
				}
				r, err := callValue(nil, m, nil, nil)
				if err != nil && dflt != nil && isExc(err, "StopIteration") {
					return dflt, nil
				}
				return r, err
			}
		}
		return nil, typeErr("'%s' object is not an iterator", typeName(v))
	}
	var r Value
	var more bool
	if userClassOf(v) != nil {
		x, _, err := callSpecial(v, "__next__")
		if err != nil {
			if dflt != nil && isExc(err, "StopIteration") {
				return dflt, nil
			}
			return nil, err
		}
		r, more = x, true
	} else {
		it, err := pyIter(v)
		if err != nil {
			return nil, err
		}
		if r, more, err = it.next(); err != nil {
			return nil, err
		}
	}
	if !more {
		if dflt != nil {
			return dflt, nil
		}
		if g, isGen := v.(*genV); isGen {
			return nil, g.stopIteration()
		}
		return nil, newExc("StopIteration", nil)
	}
	return r, nil
}

// callableIter is iter(callable, sentinel).
func callableIter(fn, sentinel Value) (Value, error) {
	if !isCallable(fn) {
		return nil, typeErr("iter(v, w): v must be callable")
	}
	done := false
	return &iterObj{typ: "callable_iterator", it: funcIter(func() (Value, bool, error) {
		if done {
			return nil, false, nil
		}
		v, err := callValue(nil, fn, nil, nil)
		if err != nil {
			if isExc(err, "StopIteration") {
				done = true
				return nil, false, nil
			}
			return nil, false, err
		}
		eq, err := pyEq(v, sentinel)
		if err != nil {
			return nil, false, err
		}
		if eq {
			done = true
			return nil, false, nil
		}
		return v, true, nil
	})}, nil
}

// builtinsDoc is builtins.__doc__.
const builtinsDoc = "Built-in functions, types, exceptions, and other objects.\n\nThis module provides direct " +
	"access to all 'built-in'\nidentifiers of Python; for example, builtins.len is\nthe full name for the " +
	"built-in function len().\n\nThis module is not normally accessed explicitly by most\napplications, but " +
	"can be useful in modules that provide\nobjects with the same name as a built-in value, but in\nwhich the " +
	"built-in of that name is also needed."

// sitePrinters are the reprs of the builtins the site module adds.
var sitePrinters = map[string]string{
	"exit": "Use exit() or Ctrl-D (i.e. EOF) to exit",
	"quit": "Use quit() or Ctrl-D (i.e. EOF) to exit",
	"help": "Type help() for interactive help, or help(object) for help about object.",
	"copyright": "Copyright (c) 2001-2023 Python Software Foundation.\nAll Rights Reserved.\n\nCopyright (c) 2000 " +
		"BeOpen.com.\nAll Rights Reserved.\n\nCopyright (c) 1995-2001 Corporation for National Research " +
		"Initiatives.\nAll Rights Reserved.\n\nCopyright (c) 1991-1995 Stichting Mathematisch Centrum, " +
		"Amsterdam.\nAll Rights Reserved.",
	"credits": "    Thanks to CWI, CNRI, BeOpen, Zope Corporation, the Python Software\n    Foundation, and a cast " +
		"of thousands for supporting Python\n    development.  See www.python.org for more information.",
	"license": "Type license() to see the full license text",
}

// notImplementedV is the NotImplemented singleton.
type notImplementedV struct{}

func (notImplementedV) pyType() string                  { return "NotImplementedType" }
func (notImplementedV) repr(*reprState) (string, error) { return "NotImplemented", nil }

// makeIterObject is iter(v) as a value.
func makeIterObject(v Value) (Value, error) {
	switch v.(type) {
	case *iterObj, *genV:
		return v, nil
	}
	if userClassOf(v) != nil {
		if r, ok, err := callSpecial(v, "__iter__"); ok {
			if err != nil {
				return nil, err
			}
			if !isIterator(r) {
				return nil, typeErr("iter() returned non-iterator of type '%s'", typeName(r))
			}
			return r, nil
		}
	}
	it, err := pyIter(v)
	if err != nil {
		return nil, err
	}
	typ := map[string]string{"list": "list_iterator", "tuple": "tuple_iterator", "str": "str_ascii_iterator",
		"dict": "dict_keyiterator", "range": "range_iterator", "bytes": "bytes_iterator", "set": "set_iterator",
		"frozenset": "set_iterator", "dict_keys": "dict_keyiterator", "dict_values": "dict_valueiterator",
		"dict_items": "dict_itemiterator"}[unsub(v).pyType()]
	if typ == "" {
		typ = "iterator"
	}
	if typ == "str_ascii_iterator" {
		if s, _ := unsub(v).(strV); !isASCII(string(s)) || isStrSubclass(v) {
			typ = "str_iterator"
		}
	}
	return &iterObj{typ: typ, it: it}, nil
}

// compareOp is one operator of a comparison.
func compareOp(op string, a, b Value) (Value, error) {
	switch op {
	case "Is":
		return pyBool(sameObject(a, b)), nil
	case "IsNot":
		return pyBool(!sameObject(a, b)), nil
	case "In", "NotIn":
		in, err := pyContains(b, a)
		if err != nil {
			return nil, err
		}
		return pyBool(in == (op == "In")), nil
	}
	sym := comparisonOperators[op]
	if userClassOf(a) != nil || userClassOf(b) != nil {
		// The comparison methods of a Python-defined class may return any
		// object, which the comparison evaluates to.
		return userRichCompare(sym, a, b)
	}
	r, err := pyCompare(sym, a, b)
	return pyBool(r), err
}

func isNoneV(v Value) bool {
	_, ok := v.(noneV)
	return ok
}

// objectV is an instance of object itself.
type objectV struct{}

func (*objectV) pyType() string { return "object" }

// siteObjV is one of the objects the site module puts in builtins: a
// printer (copyright, credits, license), help, or exit and quit, which
// raise SystemExit when called.
type siteObjV struct {
	class, text string
}

func (o *siteObjV) pyType() string { return o.class }

func (o *siteObjV) repr(*reprState) (string, error) { return o.text, nil }

func (o *siteObjV) isCallable() bool { return o.class == "Quitter" }

func (o *siteObjV) pyCall(args []Value, kw *dictV) (Value, error) {
	if o.class != "Quitter" {
		return nil, typeErr("'%s' object is not callable", o.class)
	}
	code := Value(none)
	if len(args) > 0 {
		code = args[0]
	}
	return nil, newExc("SystemExit", []Value{code})
}

func (o *objectV) getAttr(name string) (Value, error) {
	if m, ok := builtinSlotMethod(objectClass, name); ok {
		return &methodV{fn: m, self: o}, nil
	}
	return nil, nil
}
