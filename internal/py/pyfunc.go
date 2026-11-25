// Ported from CPython 3.12 (Objects/funcobject.c, Objects/codeobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The attributes of function objects, their code objects and closures.

package py

import "sort"

// getAttr reads the attributes of a function, its __dict__ last.
func (fn *funcV) getAttr(name string) (Value, error) {
	switch name {
	case "__name__":
		return strV(fn.name), nil
	case "__qualname__":
		return strV(fn.qual), nil
	case "__doc__":
		if fn.doc != nil {
			return fn.doc, nil
		}
		return none, nil
	case "__module__":
		if fn.module == nil {
			return none, nil
		}
		return fn.module, nil
	case "__globals__":
		return fn.vm.globals, nil
	case "__builtins__":
		ensureInit()
		return interp.builtins.dict, nil
	case "__dict__":
		return fn.attrDict(), nil
	case "__code__":
		return fn.code, nil
	case "__closure__":
		return closureOf(fn), nil
	case "__type_params__":
		return newTuple(nil), nil
	case "__call__":
		return &callable{name: "__call__", typ: "method-wrapper", self: fn,
			fn: func(c *callCtx, args []Value, kw *dictV) (Value, error) { return fn.call(args, kw) }}, nil
	case "__defaults__":
		if len(fn.defaults) == 0 {
			return none, nil
		}
		return newTuple(append([]Value(nil), fn.defaults...)), nil
	case "__kwdefaults__":
		if len(fn.kwdefaults) == 0 {
			return none, nil
		}
		d := newDict()
		for _, p := range fn.code.args.L3 {
			if v, ok := fn.kwdefaults[p.ID]; ok {
				d.setStr(p.ID, v)
			}
		}
		return d, nil
	case "__annotations__":
		if fn.annotations == nil {
			fn.annotations = newDict()
		}
		return fn.annotations, nil
	}
	if fn.dict != nil {
		if v, ok := fn.dict.getStr(name); ok {
			return v, nil
		}
	}
	return nil, nil
}

func (fn *funcV) attrDict() *dictV {
	if fn.dict == nil {
		fn.dict = newDict()
	}
	return fn.dict
}

func (fn *funcV) setAttr(name string, v Value) error {
	switch name {
	case "__name__", "__qualname__":
		s, ok := unsub(v).(strV)
		if !ok {
			return typeErr("%s must be set to a string object", name)
		}
		if name == "__name__" {
			fn.name = string(s)
		} else {
			fn.qual = string(s)
		}
		return nil
	case "__doc__":
		fn.doc = v
		return nil
	case "__module__":
		fn.module = v
		return nil
	case "__defaults__":
		if isNoneV(v) {
			fn.defaults = nil
			return nil
		}
		t, ok := v.(*tupleV)
		if !ok {
			return typeErr("__defaults__ must be set to a tuple object")
		}
		fn.defaults = append([]Value(nil), t.items...)
		return nil
	case "__dict__":
		d, ok := v.(*dictV)
		if !ok {
			return typeErr("__dict__ must be set to a dictionary, not a '%s'", typeName(v))
		}
		fn.dict = d
		return nil
	case "__annotations__":
		d, ok := v.(*dictV)
		if !ok {
			return typeErr("__annotations__ must be set to a dict object")
		}
		fn.annotations = d
		return nil
	case "__code__":
		c, ok := v.(*codeV)
		if !ok {
			return typeErr("__code__ must be set to a code object")
		}
		if nfree, have := c.nfree(), fn.code.nfree(); nfree != have {
			return valueErr("%s() requires a code object with %d free vars, not %d", fn.name, have, nfree)
		}
		fn.code = c
		return nil
	case "__globals__", "__closure__", "__builtins__":
		return newPyErr("AttributeError", "readonly attribute")
	}
	fn.attrDict().setStr(name, v)
	return nil
}

func (fn *funcV) delAttr(name string) error {
	if fn.dict != nil {
		if _, ok := fn.dict.getStr(name); ok {
			fn.dict.delStr(name)
			return nil
		}
	}
	return attrErr("function", name)
}

// codeV is a code object: what a function, a class body, a generator
// expression, a module or an expression compiled by compile() runs, with its
// names, its file and the lines it reports.
type codeV struct {
	name, qualname, filename string
	firstLine                int
	// lineMaps, applied in turn, give the line reported for each line of
	// the source: an instruction whose line a map does not hold has no line.
	lineMaps []map[int]int
	// node is the FunctionDef, Lambda, ClassDef, GeneratorExp, Module or
	// Expression node the code runs, scope its scope; args are the
	// parameters of a function (nil for other code), body its statements
	// and expr the body of a lambda or of an expression.
	node  *Node
	scope *scopeInfo
	args  *Node
	body  []*Node
	expr  *Node
	// pub is the Code value Go code sees for this code object.
	pub *Code
	// goBody runs the code of a function made by NewFunction, given the
	// bound parameters in order; goOrder names them in that order.
	goBody  func(bound []Value) (Value, error)
	goOrder []string
}

// nfree is the number of free variables of the code.
func (c *codeV) nfree() int {
	if c.scope == nil {
		return 0
	}
	return len(freeVars(c.scope))
}

func (*codeV) pyType() string { return "code" }

func (c *codeV) repr(*reprState) (string, error) {
	return "<code object " + c.name + " at " + objAddr(c) + ", file \"" + c.filename + "\", line " +
		itoa(c.firstLine) + ">", nil
}

// line is the line a traceback or a frame reports for an instruction on
// line l of the source: l itself, or what the line maps make of it; ok is
// false for no line.
func (c *codeV) line(l int) (int, bool) {
	for _, m := range c.lineMaps {
		next, ok := m[l]
		if !ok {
			return 0, false
		}
		l = next
	}
	return l, l > 0
}

// varnames is co_varnames: the parameters (positional, keyword-only, then
// *args and **kwargs), then the other local variables in the order they are
// first bound.
func (c *codeV) varnames() []string {
	a := c.args
	if a == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, l := range [][]*Node{a.L1, a.L2, a.L3} {
		for _, p := range l {
			add(p.ID)
		}
	}
	if a.A != nil {
		add(a.A.ID)
	}
	if a.B != nil {
		add(a.B.ID)
	}
	if c.scope != nil {
		for _, n := range c.scope.order {
			if c.scope.binds(n) {
				add(n)
			}
		}
	}
	return out
}

func (c *codeV) getAttr(name string) (Value, error) {
	a := c.args
	if a == nil {
		a = &Node{Kind: "arguments"}
	}
	switch name {
	case "co_name":
		return strV(c.name), nil
	case "co_qualname":
		return strV(c.qualname), nil
	case "co_filename":
		return strV(c.filename), nil
	case "co_firstlineno":
		return mkInt(int64(c.firstLine)), nil
	case "co_argcount":
		return mkInt(int64(len(a.L1) + len(a.L2))), nil
	case "co_posonlyargcount":
		return mkInt(int64(len(a.L1))), nil
	case "co_kwonlyargcount":
		return mkInt(int64(len(a.L3))), nil
	case "co_varnames":
		return newTuple(strList(c.varnames()).items), nil
	case "co_freevars":
		if c.scope == nil {
			return newTuple(nil), nil
		}
		return newTuple(strList(freeVars(c.scope)).items), nil
	case "co_flags":
		flags := 0
		if c.args != nil {
			flags = 0x3
		}
		if a.A != nil {
			flags |= 0x4
		}
		if a.B != nil {
			flags |= 0x8
		}
		if c.scope != nil && c.scope.generator {
			flags |= 0x20
		}
		return mkInt(int64(flags)), nil
	}
	return nil, nil
}

// nodeCode is the code object of a FunctionDef, Lambda, ClassDef or
// GeneratorExp node compiled from the named file: one object per node, as
// the compiler makes one constant of it.
func nodeCode(n *Node, filename string) *codeV {
	if n.code != nil {
		return n.code
	}
	c := &codeV{node: n, scope: n.scope, filename: filename, firstLine: n.Lineno, qualname: n.scope.qualname}
	switch n.Kind {
	case "FunctionDef", "AsyncFunctionDef":
		c.name, c.args, c.body = n.ID, n.A, n.Body
		if len(n.L1) > 0 {
			// The code of a decorated function starts at the first
			// decorator.
			c.firstLine = n.L1[0].Lineno
		}
	case "Lambda":
		c.name, c.args, c.expr = "<lambda>", n.A, n.B
	case "ClassDef":
		c.name, c.body = n.ID, n.Body
		if len(n.L3) > 0 {
			c.firstLine = n.L3[0].Lineno
		}
	case "GeneratorExp":
		c.name = "<genexpr>"
	}
	n.code = c
	return c
}

// freeVars is co_freevars of a function scope: the variables of enclosing
// functions that it or the scopes nested in it use, sorted, with __class__
// for a method that uses super or __class__.
func freeVars(s *scopeInfo) []string {
	set := map[string]bool{}
	var walk func(c *scopeInfo)
	walk = func(c *scopeInfo) {
		for n := range c.uses {
			if r := s.resolve(n); r == 'f' && !s.locals[n] || s.nonlocals[n] {
				set[n] = true
			}
		}
		for n := range c.nonlocals {
			set[n] = true
		}
		for _, ch := range c.children {
			walk(ch)
		}
	}
	walk(s)
	for n := range set {
		if s.binds(n) || s.globals[n] {
			delete(set, n)
		}
	}
	if s.classCell {
		set["__class__"] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// cellV is a cell of a closure: the variable name of the frame that
// defines it.
type cellV struct {
	frame *frame
	name  string
	// class is the class a __class__ cell holds.
	class *classV
}

func (*cellV) pyType() string { return "cell" }

func (c *cellV) contents() (Value, bool) {
	if c.class != nil {
		return c.class, true
	}
	if c.frame == nil {
		return nil, false
	}
	v, ok := c.frame.vars[c.name]
	return v, ok
}

func (c *cellV) repr(*reprState) (string, error) {
	v, ok := c.contents()
	if !ok {
		return "<cell at " + objAddr(c) + ": empty>", nil
	}
	return "<cell at " + objAddr(c) + ": " + typeName(v) + " object at " + objAddr(v) + ">", nil
}

func (c *cellV) getAttr(name string) (Value, error) {
	if name == "cell_contents" {
		v, ok := c.contents()
		if !ok {
			return nil, valueErr("Cell is empty")
		}
		return v, nil
	}
	return nil, nil
}

// closureOf is fn.__closure__: a cell for each free variable, None when
// there is none.
func closureOf(fn *funcV) Value {
	if fn.code.scope == nil {
		return none
	}
	names := freeVars(fn.code.scope)
	if len(names) == 0 {
		return none
	}
	cells := make([]Value, len(names))
	for i, n := range names {
		c := &cellV{name: n}
		if n == "__class__" && fn.code.scope.classCell {
			for p := fn.closure; p != nil; p = p.parent {
				if p.scope.kind == scopeClass {
					c.class = p.class
					break
				}
			}
		} else if fn.closure != nil {
			c.frame = fn.closure.cellFrameOf(n)
		}
		cells[i] = c
	}
	return newTuple(cells)
}

// cellFrameOf is the frame holding variable name for code defined in f:
// f itself when its function binds it, else an enclosing function's frame.
func (f *frame) cellFrameOf(name string) *frame {
	if f.scope.kind != scopeModule && f.scope.kind != scopeClass && f.scope.binds(name) {
		return f
	}
	return f.cellFrame(name)
}

// FuncSpec describes the parameters and names of a function made by
// NewFunction, as a def statement would.
type FuncSpec struct {
	Name, Qualname, Module string
	// Doc is __doc__; nil means None.
	Doc Value
	// PosOnly, Params and KwOnly are the positional-only, ordinary and
	// keyword-only parameters.
	PosOnly, Params, KwOnly []string
	// Defaults are the defaults of the last positional parameters.
	Defaults []Value
	// KwDefaults are the defaults of keyword-only parameters.
	KwDefaults map[string]Value
	// VarArgs and VarKw name the *args and **kwargs parameters ("" for
	// none).
	VarArgs, VarKw string
}

// NewFunction returns a Python function that binds its arguments as a def
// with spec's parameters does (with the same TypeError texts), then calls
// body with the bound values in parameter order: positional-only, ordinary,
// *args (a tuple), keyword-only, **kwargs (a dict). Its __name__,
// __qualname__, __module__, __doc__, __defaults__ and __kwdefaults__ are
// those of such a def; its code object has the def's parameters (co_name,
// co_varnames, co_argcount, ...) and the file name "<go>".
func NewFunction(spec FuncSpec, body func(bound []Value) (Value, error)) Value {
	ensureInit()
	argNodes := func(names []string) []*Node {
		out := make([]*Node, len(names))
		for i, n := range names {
			out[i] = &Node{Kind: "arg", ID: n}
		}
		return out
	}
	a := &Node{Kind: "arguments", L1: argNodes(spec.PosOnly), L2: argNodes(spec.Params), L3: argNodes(spec.KwOnly)}
	a.L4 = make([]*Node, len(spec.KwOnly))
	if spec.VarArgs != "" {
		a.A = &Node{Kind: "arg", ID: spec.VarArgs}
	}
	if spec.VarKw != "" {
		a.B = &Node{Kind: "arg", ID: spec.VarKw}
	}
	qual := spec.Qualname
	if qual == "" {
		qual = spec.Name
	}
	doc := spec.Doc
	if doc == nil {
		doc = none
	}
	kwd := map[string]Value{}
	for k, v := range spec.KwDefaults {
		kwd[k] = v
	}
	order := append(append(append([]string(nil), spec.PosOnly...), spec.Params...), spec.VarArgs)
	order = append(append(order, spec.KwOnly...), spec.VarKw)
	code := &codeV{name: spec.Name, qualname: qual, filename: "<go>", args: a, goBody: body, goOrder: order}
	return &funcV{name: spec.Name, qual: qual, code: code, module: strV(spec.Module),
		vm: &pyVM{globals: newDict()}, defaults: append([]Value(nil), spec.Defaults...), kwdefaults: kwd, doc: doc}
}

// callGo runs the Go body of the code of a function made by NewFunction.
func (c *codeV) callGo(vars map[string]Value) (Value, error) {
	bound := make([]Value, 0, len(c.goOrder))
	for _, n := range c.goOrder {
		if n != "" {
			bound = append(bound, vars[n])
		}
	}
	r, err := c.goBody(bound)
	if err != nil {
		return nil, fromGoError(err)
	}
	if r == nil {
		return none, nil
	}
	return r, nil
}
