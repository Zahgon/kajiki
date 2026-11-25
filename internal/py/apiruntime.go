// Ported from CPython 3.12 (Objects/funcobject.c, Objects/typeobject.c, Python/pythonrun.c,
// Lib/importlib/__init__.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The interface for Go code that builds on the runtime: functions and classes written in Go,
// instances, modules, imports and exceptions.

package py

import (
	"strings"
)

// goFuncV is a Python function whose body is Go code: type(f) is function,
// and as a class attribute it binds to the instance as a method.
type goFuncV struct {
	name, qual string
	module     Value
	doc        Value
	dict       *dictV
	fn         func(args []Value, kwargs []Keyword) (Value, error)
}

func (*goFuncV) pyType() string { return "function" }

func (g *goFuncV) repr(*reprState) (string, error) {
	return "<function " + g.qual + " at " + objAddr(g) + ">", nil
}

func (g *goFuncV) pyCall(args []Value, kw *dictV) (Value, error) {
	r, err := g.fn(args, keywordsOf(kw))
	if err != nil {
		return nil, fromGoError(err)
	}
	if r == nil {
		return none, nil
	}
	return r, nil
}

func (g *goFuncV) getAttr(name string) (Value, error) {
	if v, ok := g.dict.getStr(name); ok {
		return v, nil
	}
	switch name {
	case "__name__":
		return strV(g.name), nil
	case "__qualname__":
		return strV(g.qual), nil
	case "__module__":
		return g.module, nil
	case "__doc__":
		return g.doc, nil
	case "__dict__":
		return g.dict, nil
	case "__defaults__", "__kwdefaults__":
		return none, nil
	}
	return nil, nil
}

func (g *goFuncV) setAttr(name string, v Value) error {
	switch name {
	case "__name__", "__qualname__":
		s, ok := unsub(v).(strV)
		if !ok {
			return typeErr("%s must be set to a string object", name)
		}
		if name == "__name__" {
			g.name = string(s)
		} else {
			g.qual = string(s)
		}
	case "__module__":
		g.module = v
	case "__doc__":
		g.doc = v
	default:
		g.dict.setStr(name, v)
	}
	return nil
}

// keywordsOf turns the keyword arguments of a call into Keywords.
func keywordsOf(kw *dictV) []Keyword {
	if kw == nil || kw.Len() == 0 {
		return nil
	}
	out := make([]Keyword, len(kw.keys))
	for i, k := range kw.keys {
		s, _ := unsub(k).(strV)
		out[i] = Keyword{Name: string(s), Value: kw.vals[i]}
	}
	return out
}

// Func returns a Python function named name whose body is fn. Placed in a
// class, it binds like a function written in Python: called on an
// instance, fn receives the instance as args[0]. A *Error fn returns is
// raised as that exception; any other error is raised as a RuntimeError
// holding its text. A nil result is None.
func Func(name string, fn func(args []Value, kwargs []Keyword) (Value, error)) Value {
	return &goFuncV{name: name, qual: name, module: none, doc: none, dict: newDict(), fn: fn}
}

// FuncQual is Func with a __qualname__ and __module__.
func FuncQual(name, qualname, module string, fn func(args []Value, kwargs []Keyword) (Value, error)) Value {
	return &goFuncV{name: name, qual: qualname, module: strV(module), doc: none, dict: newDict(), fn: fn}
}

// FuncGlobals is fn.__globals__ of a function written in Python, nil for
// any other value.
func FuncGlobals(fn Value) Value {
	if f, ok := fn.(*funcV); ok {
		return f.vm.globals
	}
	return nil
}

// ---------------------------------------------------------------- classes and instances

// NewClass is type(name, bases, ns): a class whose namespace is the dict
// ns, which should hold __module__ and may hold __qualname__ and __doc__.
// Its functions (Func or Python functions) are methods; __new__ is called
// with the class first; wrap a function with Call(Builtin("classmethod"),
// fn), Builtin("staticmethod") or Builtin("property") as Python code does.
// bases are classes: builtin ones (object, str, int, float, complex, bytes,
// tuple, list, dict, Exception, ...) or classes made by Python or Go code.
func NewClass(name string, bases []Value, ns Value) (Value, error) {
	ensureInit()
	if ns == nil {
		ns = newDict()
	}
	meta := Value(classes["type"])
	if len(bases) > 0 {
		k, err := calculateMeta(classOf(bases[0]), bases)
		if err != nil {
			return nil, toError(err)
		}
		meta = k
	}
	v, err := callValue(nil, meta, []Value{strV(name), newTuple(append([]Value(nil), bases...)), ns}, nil)
	return v, toError(err)
}

// NewInstance is cls.__new__ as object.__new__ (or BaseException.__new__)
// makes it, without __init__: an instance of cls with an empty __dict__.
// For a class deriving from a builtin type other than object, base is the
// value of that type the instance holds (a str for a str subclass, a tuple
// for a tuple subclass, ...); it is ignored for exception classes, whose
// instances start with empty args.
func NewInstance(cls Value, base Value) (Value, error) {
	k, ok := cls.(*classV)
	if !ok {
		return nil, toError(typeErr("NewInstance() argument must be a class, not %s", typeName(cls)))
	}
	if isExcClass(k) {
		e := &excV{Class: k.name, args: nil}
		if k.isUser() {
			e.cls = k
		}
		e.Message = excStr(k.builtinBase().name, nil)
		return e, nil
	}
	if !k.isUser() {
		return nil, toError(typeErr("NewInstance() needs a class made by Python or Go code, not %s", k.name))
	}
	b := k.builtinBase()
	if b == objectClass {
		return &instV{cls: k, dict: newDict()}, nil
	}
	if base == nil || !classOf(base).subclassOf(b) {
		return nil, toError(typeErr("%s.__new__(%s): the base value must be a %s", b.name, k.name, b.name))
	}
	return &instV{cls: k, dict: newDict(), base: unsubTo(base, b)}, nil
}

// unsubTo is the value of the builtin class b inside v.
func unsubTo(v Value, b *classV) Value {
	for {
		x, ok := v.(*instV)
		if !ok || x.base == nil {
			return v
		}
		v = x.base
	}
}

// BaseValue is the value of a builtin type that an instance of a subclass
// of that type holds (the str of a str subclass instance, ...); ok is false
// for other values.
func BaseValue(v Value) (Value, bool) {
	if x, ok := v.(*instV); ok && x.base != nil {
		return unsubTo(x.base, nil), true
	}
	return nil, false
}

// InstanceDict is v.__dict__ for an instance of a class made by Python or
// Go code (or an exception), nil for values without one.
func InstanceDict(v Value) Value {
	switch x := v.(type) {
	case *instV:
		return x.dict
	case *excV:
		return x.attrDict()
	case *moduleV:
		return x.dict
	}
	return nil
}

// TypeOf is type(v).
func TypeOf(v Value) Value { return classOf(v) }

// ClassName is cls.__name__ of a class, "" for another value.
func ClassName(cls Value) string {
	if k, ok := cls.(*classV); ok {
		return k.name
	}
	return ""
}

// IsInstance is isinstance(v, cls), cls a class or a tuple of classes.
func IsInstance(v, cls Value) (bool, error) {
	r, err := callValue(nil, pyBuiltinName("isinstance"), []Value{v, cls}, nil)
	if err != nil {
		return false, toError(err)
	}
	return r == trueV, nil
}

// IsSubclass is issubclass(a, b).
func IsSubclass(a, b Value) (bool, error) {
	r, err := callValue(nil, pyBuiltinName("issubclass"), []Value{a, b}, nil)
	if err != nil {
		return false, toError(err)
	}
	return r == trueV, nil
}

// CallSuper is super(start, obj).name(*args, **kwargs): the attribute name
// found after start in the method resolution order of obj's class (obj
// itself when it is a class, as for __new__ and class methods), called.
func CallSuper(start, obj Value, name string, args []Value, kwargs []Keyword) (Value, error) {
	k, ok := start.(*classV)
	if !ok {
		return nil, toError(typeErr("super() argument 1 must be a type, not %s", typeName(start)))
	}
	s, err := newSuper(k, obj)
	if err != nil {
		return nil, toError(err)
	}
	m, err := s.getAttr(name)
	if err != nil {
		return nil, toError(err)
	}
	if m == nil {
		return nil, toError(newPyErr("AttributeError", "'super' object has no attribute '"+name+"'"))
	}
	return Call(m, args, kwargs)
}

// ---------------------------------------------------------------- modules

// NewModule is types.ModuleType(name): a module whose dict holds __name__,
// __doc__, __package__, __loader__ and __spec__ (the last four None).
func NewModule(name string) Value { return newModuleV(name) }

// IsModule reports whether v is a module object.
func IsModule(v Value) bool {
	_, ok := v.(*moduleV)
	return ok
}

// ModuleDict is m.__dict__ of a module, nil for another value.
func ModuleDict(m Value) Value {
	if x, ok := m.(*moduleV); ok {
		return x.dict
	}
	return nil
}

// Modules is sys.modules.
func Modules() Value {
	ensureInit()
	return interp.modules
}

// SysModule is the sys module.
func SysModule() Value {
	ensureInit()
	return interp.sys
}

// BuiltinsModule is the builtins module.
func BuiltinsModule() Value {
	ensureInit()
	return interp.builtins
}

// ImportModule is importlib.import_module(name, package): the module of
// the absolute name, or of the relative name (leading dots) resolved
// against the package name.
func ImportModule(name, pkg string) (Value, error) {
	ensureInit()
	level := 0
	if strings.HasPrefix(name, ".") {
		if pkg == "" {
			return nil, toError(typeErr("the 'package' argument is required to perform a relative import for %s",
				reprStr(name)))
		}
		for level < len(name) && name[level] == '.' {
			level++
		}
		bits := strings.Split(pkg, ".")
		if len(bits) < level {
			return nil, toError(newPyErr("ImportError", "attempted relative import beyond top-level package"))
		}
		base := strings.Join(bits[:len(bits)-(level-1)], ".")
		name = name[level:]
		if name != "" {
			name = base + "." + name
		} else {
			name = base
		}
	}
	if name == "" {
		return nil, toError(valueErr("Empty module name"))
	}
	m, err := importModule(name)
	return m, toError(err)
}

func reprStr(s string) string {
	r, _ := pyRepr(strV(s))
	return r
}

// ---------------------------------------------------------------- exceptions

// RaiseValue is raise v: v is an exception instance, or an exception class
// that is instantiated without arguments.
func RaiseValue(v Value) error {
	e, err := exceptionToRaise(v)
	if err != nil {
		return toError(err)
	}
	return toError(e)
}

// ExceptionValue is the exception object an error stands for: the
// exception of a Python error, a RuntimeError holding the text of any other
// error; nil for a nil error.
func ExceptionValue(err error) Value {
	if err == nil {
		return nil
	}
	if x := excOf(fromGoError(err)); x != nil {
		return x
	}
	return nil
}

// GetAttrDefault is getattr(v, name, d): d when looking the attribute up
// raises AttributeError.
func GetAttrDefault(v Value, name string, d Value) (Value, error) {
	a, err := getattrOpt(v, name)
	if err != nil {
		return nil, toError(err)
	}
	if a == nil {
		return d, nil
	}
	return a, nil
}

// IsException reports whether err is a Python exception that is an
// instance of Exception, as `except Exception:` catches it.
func IsException(err error) bool {
	x := excOf(err)
	return x != nil && excIsInstance(x, "Exception")
}

// EnterRecursiveCall counts a frame of Go code that stands for a Python
// frame towards the recursion limit, as CPython counts the frame of a Python
// function: at the limit it raises RecursionError "maximum recursion depth
// exceeded", otherwise the frame counts until leave is called. leave is a
// no-op when err is not nil.
func EnterRecursiveCall() (leave func(), err error) {
	ensureInit()
	if interp.depth >= interp.recursionLimit {
		return func() {}, toError(newPyErr("RecursionError", "maximum recursion depth exceeded"))
	}
	interp.depth++
	return func() { interp.depth-- }, nil
}

// NewIterator is an iterator object whose __next__ calls next: ok false
// ends the iteration (StopIteration). Its type is named "generator".
func NewIterator(next func() (v Value, ok bool, err error)) Value {
	return &iterObj{typ: "generator", it: funcIter(func() (Value, bool, error) {
		v, ok, err := next()
		return v, ok, fromGoError(err)
	})}
}

// CallMethodKw is obj.name(*args, **kwargs).
func CallMethodKw(obj Value, name string, args []Value, kwargs []Keyword) (Value, error) {
	m, err := pyGetattr(obj, name)
	if err != nil {
		return nil, toError(err)
	}
	return Call(m, args, kwargs)
}

// Dir is dir(v): the sorted attribute names.
func Dir(v Value) ([]string, error) {
	l, err := builtinDir(nil, []Value{v}, nil)
	if err != nil {
		return nil, toError(err)
	}
	items := l.(*listV).items
	out := make([]string, len(items))
	for i, it := range items {
		s, _ := unsub(it).(strV)
		out[i] = string(s)
	}
	return out, nil
}
