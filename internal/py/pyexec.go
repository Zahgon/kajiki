// Ported from CPython 3.12 (Python/ceval.c, Python/traceback.c, Python/bltinmodule.c) to Go,
// 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Running trees as exec and eval do, the stack of running frames, and the tracebacks and implicit
// chaining of exceptions as they leave statements, expressions and except clauses.

package py

// tbEntry is one entry of a traceback: the frame, the code it ran and the
// line of the source the instruction running in it came from.
type tbEntry struct {
	fr   *frame
	co   *codeV
	line int
}

// TraceFrame is one entry of a traceback: the code object a frame ran, its
// co_filename and co_name, and the line the traceback reports (tb_lineno),
// HasLine false when the instruction had none.
type TraceFrame struct {
	Code     *Code
	Filename string
	Name     string
	Lineno   int
	HasLine  bool
}

// exec runs a statement; an exception leaving it records the statement's
// position in its traceback.
func (f *frame) exec(n *Node) (ctlFlow, error) {
	if n.HasPos {
		f.line = n.Lineno
	}
	c, err := f.execNode(n)
	if err != nil {
		noteTraceback(f, n, err)
	}
	return c, err
}

// eval evaluates an expression; an exception leaving it records the
// expression's position in its traceback.
func (f *frame) eval(n *Node) (Value, error) {
	v, err := f.evalNode(n)
	if err != nil {
		noteTraceback(f, n, err)
	}
	return v, err
}

// noteTraceback adds the frame running n to the traceback of the exception
// err stands for, at the line of the instruction of n that failed, unless
// the innermost node of that frame the exception left is already recorded.
func noteTraceback(f *frame, n *Node, err error) { noteTracebackLine(f, instructionLine(n), err) }

// noteTracebackLine is noteTraceback for an instruction on the given line.
func noteTracebackLine(f *frame, line int, err error) {
	x, ok := err.(*excV)
	if !ok || x == errGenStopped {
		return
	}
	g := f.codeFrame()
	if x.tbFrame == g {
		return
	}
	x.tbFrame = g
	x.tb = append(x.tb, tbEntry{fr: g, co: g.co, line: line})
}

// instructionLine is the line of the instruction an exception leaving node n
// comes from. An attribute that spans lines is read at the line of its name,
// and so is a method called through one.
func instructionLine(n *Node) int {
	switch n.Kind {
	case "Attribute":
		return n.EndLineno
	case "Call":
		if isMethodCall(n) {
			return n.A.EndLineno
		}
	}
	return n.Lineno
}

// isMethodCall reports whether the compiler calls n as a method (LOAD_ATTR
// of the method, then CALL): its function is an attribute, with no * or **
// argument.
func isMethodCall(n *Node) bool {
	if n.A.Kind != "Attribute" {
		return false
	}
	for _, a := range n.L1 {
		if a.Kind == "Starred" {
			return false
		}
	}
	for _, k := range n.L2 {
		if k.NoID {
			return false
		}
	}
	return true
}

// codeFrame is the frame whose code runs at f: f itself, or for an inlined
// comprehension the frame it is inlined in.
func (f *frame) codeFrame() *frame {
	g := f
	for g.co == nil && g.parent != nil {
		g = g.parent
	}
	return g
}

// codeObj is the code object running at f.
func (f *frame) codeObj() *codeV { return f.codeFrame().co }

// enter makes f the running frame until the returned function runs.
func (f *frame) enter() func() {
	f.back = interp.cur
	interp.cur = f
	return func() { interp.cur = f.back }
}

// chainContext is the implicit chaining of an exception raised while x was
// being handled: its __context__ becomes x, unless that would make a cycle.
func chainContext(err error, x *excV) {
	e, ok := err.(*excV)
	if !ok || e == x || e == errGenStopped || e.context != nil {
		return
	}
	for c := x; c != nil; c = c.context {
		if c == e {
			// Break the cycle as _PyErr_SetObject does.
			return
		}
	}
	e.context = x
}

// moduleBuiltins makes sure the globals have __builtins__, as exec does.
func moduleBuiltins(g *dictV) {
	if _, ok := g.getStr("__builtins__"); !ok {
		ensureInit()
		g.setStr("__builtins__", interp.builtins.dict)
	}
}

// runModuleCode runs code compiled from a module (exec) or an expression
// (eval) with the globals g and the locals ns; it returns the value of an
// expression, None for a module.
func runModuleCode(code *codeV, g *dictV, ns Value) (Value, error) {
	moduleBuiltins(g)
	f := &frame{vm: &pyVM{globals: g}, scope: code.scope, ns: ns, vars: map[string]Value{}, co: code}
	defer f.enter()()
	if code.expr != nil {
		return f.eval(code.expr)
	}
	// The module's frame counts towards the recursion limit.
	interp.depth++
	defer func() { interp.depth-- }()
	if err := setupAnnotations(code.body, ns); err != nil {
		return nil, err
	}
	if doc := docstring(code.body); doc != nil {
		// The compiler stores a module's docstring in __doc__ first.
		if err := pySetitem(ns, strV("__doc__"), doc); err != nil {
			return nil, err
		}
	}
	_, err := f.execBlock(code.body)
	return none, err
}

// moduleCode is the code object of a Module or Expression node compiled
// from the named file, its scopes analysed.
func moduleCode(tree *Node, filename string) *codeV {
	scope := newScope(scopeModule, nil, "")
	c := &codeV{name: "<module>", qualname: "<module>", filename: filename, firstLine: 1, node: tree, scope: scope}
	if tree.Kind == "Expression" {
		analyzeExpr(tree.A, scope)
		c.expr = tree.A
	} else {
		analyzeStmts(tree.Body, scope)
		c.body = tree.Body
	}
	makeNestedCodes(tree, filename)
	return c
}

// makeNestedCodes makes the code objects of the functions, classes and
// generator expressions in a tree, as the compiler makes them constants of
// the code that defines them: they keep the file name compiled with
// whatever happens later to the code around them.
func makeNestedCodes(n *Node, filename string) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "FunctionDef", "AsyncFunctionDef", "Lambda", "ClassDef", "GeneratorExp":
		nodeCode(n, filename)
	}
	for _, f := range Fields(n) {
		makeNestedCodes(f.Node, filename)
		for _, ch := range f.Nodes {
			makeNestedCodes(ch, filename)
		}
	}
}

// namespaces checks the globals and locals given to Exec or Eval.
func namespaces(globals, locals Value) (*dictV, Value, error) {
	g, ok := globals.(*dictV)
	if !ok {
		return nil, nil, typeErr("globals must be a dict")
	}
	if locals == nil || isNoneV(locals) {
		return g, g, nil
	}
	if ok, err := isMapping(locals); err != nil {
		return nil, nil, err
	} else if !ok {
		return nil, nil, typeErr("locals must be a mapping")
	}
	return g, locals, nil
}

// CallerGlobals is the globals dict of the innermost running Python frame
// (the code that called the Go function asking), nil outside Python code.
func CallerGlobals() Value {
	if interp.cur == nil {
		return nil
	}
	return interp.cur.vm.globals
}

// CallerLocals is locals() of the innermost running Python frame, nil
// outside Python code.
func CallerLocals() Value {
	if interp.cur == nil {
		return nil
	}
	return localsOf(interp.cur)
}

// Traceback returns the traceback of the exception err stands for, the
// outermost frame first: the Python frames it left, each with the line of
// the instruction that was running in it.
func Traceback(err error) []TraceFrame {
	x := excOf(err)
	if x == nil {
		return nil
	}
	out := make([]TraceFrame, len(x.tb))
	for i, t := range x.tb {
		line, ok := t.co.line(t.line)
		out[len(x.tb)-1-i] = TraceFrame{Code: t.co.public(), Filename: t.co.filename, Name: t.co.name, Lineno: line,
			HasLine: ok}
	}
	return out
}

// excOf is the exception object err stands for, nil for an error that is
// not a Python exception.
func excOf(err error) *excV {
	switch e := err.(type) {
	case *excV:
		return e
	case *Error:
		return e.exc
	}
	return nil
}

// docstring is the docstring of a body: its first statement when that is a
// str constant; nil otherwise.
func docstring(body []*Node) Value {
	if len(body) > 0 && body[0].Kind == "Expr" && body[0].A.Kind == "Constant" {
		if s, ok := body[0].A.Value.(strV); ok {
			return s
		}
	}
	return nil
}
