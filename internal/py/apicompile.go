// Ported from CPython 3.12 (Python/bltinmodule.c, Objects/exceptions.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// compile(), exec() and eval() for Go code, and the location a SyntaxError carries.

package py

// Code is a code object: made by Compile, or the __code__ of a function.
// Each code object has one *Code, so == compares code objects.
type Code struct {
	c *codeV
}

// public is the *Code of a code object.
func (c *codeV) public() *Code {
	if c.pub == nil {
		c.pub = &Code{c: c}
	}
	return c.pub
}

// Compile is compile(source, filename, mode) for mode "exec" or "eval": the
// code object, or the SyntaxError (IndentationError, TabError) compile()
// raises, as an *Error whose Syntax method gives its location.
func Compile(source, filename, mode string) (*Code, error) {
	ensureInit()
	c, err := compileSource(strV(source), filename, mode)
	if err != nil {
		return nil, toError(err)
	}
	return c.public(), nil
}

// Value is the code object as a Python value.
func (c *Code) Value() Value { return c.c }

// Exec is exec(code, globals): globals must be a dict, which gets
// __builtins__ when it has none.
func (c *Code) Exec(globals Value) error {
	g, ok := globals.(*dictV)
	if !ok {
		return toError(typeErr("exec() globals must be a dict, not %s", typeName(globals)))
	}
	_, err := runCode("exec", c.c, g, g, false)
	return toError(err)
}

// Eval is eval(code, globals).
func (c *Code) Eval(globals Value) (Value, error) {
	g, ok := globals.(*dictV)
	if !ok {
		return nil, toError(typeErr("eval() globals must be a dict, not %s", typeName(globals)))
	}
	v, err := runCode("eval", c.c, g, g, true)
	return v, toError(err)
}

// SyntaxInfo is what a SyntaxError (or an IndentationError or TabError)
// holds besides its message: the attributes filename, lineno, offset, text,
// end_lineno and end_offset. The offsets count characters from 1; a missing
// one is 0, and HasText is false when text is None.
type SyntaxInfo struct {
	Msg                                  string
	Filename                             string
	Lineno, Offset, EndLineno, EndOffset int
	Text                                 string
	HasText                              bool
}

// Syntax returns the location of a SyntaxError, IndentationError or
// TabError, and false for any other exception.
func (e *Error) Syntax() (SyntaxInfo, bool) {
	if e.exc == nil || !excIsInstance(e.exc, "SyntaxError") {
		return SyntaxInfo{}, false
	}
	x := e.exc
	str := func(name string) (string, bool) {
		s, ok := unsub(x.field(name)).(strV)
		return string(s), ok
	}
	num := func(name string) int {
		if i, ok := asInt(x.field(name)); ok && i.IsInt64() {
			return int(i.Int64())
		}
		return 0
	}
	info := SyntaxInfo{Lineno: num("lineno"), Offset: num("offset"), EndLineno: num("end_lineno"),
		EndOffset: num("end_offset")}
	info.Msg, _ = str("msg")
	info.Filename, _ = str("filename")
	info.Text, info.HasText = str("text")
	return info, true
}

// Name is co_name: the name of the function or class, "<lambda>",
// "<genexpr>" or "<module>".
func (c *Code) Name() string { return c.c.name }

// Qualname is co_qualname.
func (c *Code) Qualname() string { return c.c.qualname }

// Filename is co_filename.
func (c *Code) Filename() string { return c.c.filename }

// FirstLine is co_firstlineno.
func (c *Code) FirstLine() int { return c.c.firstLine }

// WithLines is the code object CodeType makes from c with another
// co_filename, co_firstlineno and line table: each instruction reports the
// line lines gives for the line it reported in c, and no line when lines has
// none for it. The code runs as c does; the code objects of the functions
// and generator expressions defined in it are unchanged.
func (c *Code) WithLines(filename string, firstLine int, lines map[int]int) *Code {
	n := *c.c
	n.filename, n.firstLine, n.pub = filename, firstLine, nil
	n.lineMaps = append(append([]map[int]int(nil), c.c.lineMaps...), lines)
	return n.public()
}

// FunctionCode is fn.__code__ of a function written in Python.
func FunctionCode(fn Value) (*Code, bool) {
	f, ok := fn.(*funcV)
	if !ok || f.code.node == nil {
		return nil, false
	}
	return f.code.public(), true
}

// FunctionType is types.FunctionType(code, globals, name, argdefs, closure):
// a new function running code with the globals dict, named name (or
// co_name when name is None), with the positional defaults argdefs (a tuple
// or None) and the cells of closure (a tuple, or None for code without free
// variables). Like CPython it has no keyword-only defaults.
func FunctionType(code *Code, globals, name, argdefs, closure Value) (Value, error) {
	g, ok := globals.(*dictV)
	if !ok {
		return nil, toError(typeErr("function() argument 'globals' must be dict, not %s", typeName(globals)))
	}
	c := code.c
	fname := c.name
	if !isNoneV(name) {
		s, ok := unsub(name).(strV)
		if !ok {
			return nil, toError(typeErr("arg 3 (name) must be None or string"))
		}
		fname = string(s)
	}
	var defaults []Value
	if !isNoneV(argdefs) {
		t, ok := argdefs.(*tupleV)
		if !ok {
			return nil, toError(typeErr("arg 4 (defaults) must be None or tuple"))
		}
		defaults = append(defaults, t.items...)
	}
	enclosing, err := closureFrame(c.name, c.nfree(), closure)
	if err != nil {
		return nil, toError(err)
	}
	mod, ok := g.getStr("__name__")
	if !ok {
		mod = none
	}
	fn := &funcV{name: fname, qual: c.qualname, code: c, closure: enclosing, vm: &pyVM{globals: g},
		defaults: defaults, kwdefaults: map[string]Value{}, module: mod, doc: none}
	if doc := docstring(c.body); doc != nil {
		fn.doc = doc
	}
	return fn, nil
}

// closureFrame is the frame the free variables of a function made by
// FunctionType from the code object named name are read from: the innermost
// frame holding one of the cells of closure, which must have one cell per
// free variable.
func closureFrame(name string, nfree int, closure Value) (*frame, error) {
	if isNoneV(closure) {
		if nfree > 0 {
			return nil, typeErr("arg 5 (closure) must be tuple")
		}
		return nil, nil
	}
	t, ok := closure.(*tupleV)
	if !ok {
		return nil, typeErr("arg 5 (closure) must be None or tuple")
	}
	if len(t.items) != nfree {
		return nil, valueErr("%s requires closure of length %d, not %d", name, nfree, len(t.items))
	}
	var inner *frame
	depth := -1
	for _, item := range t.items {
		cell, ok := item.(*cellV)
		if !ok {
			return nil, typeErr("arg 5 (closure) expected cell, found %s", typeName(item))
		}
		if cell.frame == nil {
			continue
		}
		d := 0
		for p := cell.frame; p != nil; p = p.parent {
			d++
		}
		if d > depth {
			inner, depth = cell.frame, d
		}
	}
	return inner, nil
}

// RebindFunction is types.FunctionType(fn.__code__, globals, fn.__name__,
// fn.__defaults__, fn.__closure__) for a function written in Python: the
// same code (FunctionCode gives the same *Code) run with another globals
// dict, without keyword-only defaults and with an empty __dict__.
func RebindFunction(fn, globals Value) (Value, error) {
	f, ok := fn.(*funcV)
	if !ok {
		return nil, toError(typeErr("RebindFunction() needs a function, not %s", typeName(fn)))
	}
	defaults, err := f.getAttr("__defaults__")
	if err != nil {
		return nil, toError(err)
	}
	return FunctionType(f.code.public(), globals, strV(f.name), defaults, closureOf(f))
}
