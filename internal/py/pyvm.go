// Ported from CPython 3.12 (Python/symtable.c, Python/ceval.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The tree-walking evaluator that takes the place of CPython's: it runs Python syntax
// trees with Python's scoping (module code with a separate locals mapping,
// functions with closures, class bodies, inlined comprehensions, generator
// expressions), exceptions, calls and generators.

package py

import (
	"iter"
	"runtime"
	"strings"
	"sync"
)

// scopeKind is the kind of block a scope is.
type scopeKind int

const (
	scopeModule scopeKind = iota
	scopeFunction
	scopeClass
	// scopeComp is an inlined list, set or dict comprehension.
	scopeComp
)

// scopeInfo is one scope of the program being run.
type scopeInfo struct {
	kind      scopeKind
	parent    *scopeInfo
	qualname  string
	locals    map[string]bool
	globals   map[string]bool
	nonlocals map[string]bool
	resolved  map[string]byte
	// generator is set for a function whose body suspends: it yields, or
	// awaits in an async function.
	generator bool
	// async marks an async function; yields marks a body with yield,
	// which makes an async function an asynchronous generator.
	async, yields bool
	// classCell is set for a function that names super or __class__, which
	// the compiler gives the __class__ cell of the class it is defined in.
	classCell bool
	// genexp marks the scope of a generator expression.
	genexp bool
	// order holds the local variables in the order they were first bound.
	order []string
	// uses holds the names the scope's own code reads; children are the
	// scopes nested in it.
	uses     map[string]bool
	children []*scopeInfo
	// private is the name of the class the scope is in (its body or a
	// function nested in it), whose private names are mangled.
	private string
}

// addLocal makes name a local variable of the scope, remembering the
// order in which the variables were first bound.
func (s *scopeInfo) addLocal(name string) {
	if !s.locals[name] {
		s.locals[name] = true
		s.order = append(s.order, name)
	}
}

func newScope(kind scopeKind, parent *scopeInfo, qual string) *scopeInfo {
	s := &scopeInfo{kind: kind, parent: parent, qualname: qual, locals: map[string]bool{},
		globals: map[string]bool{}, nonlocals: map[string]bool{}, resolved: map[string]byte{}, uses: map[string]bool{}}
	if parent != nil {
		parent.children = append(parent.children, s)
		s.private = parent.private
	}
	return s
}

// binds reports whether a name is a variable of a function-like scope.
func (s *scopeInfo) binds(name string) bool {
	return s.locals[name] && !s.globals[name] && !s.nonlocals[name]
}

// resolve tells how a name is reached from this scope: 'l' a local of the
// running function, 'f' a variable of an enclosing function, 'g' a global,
// 'n' the name lookup of module and class code (the locals mapping, then the
// globals, then the builtins), 'c' a class body's lookup of a variable of an
// enclosing function (the class namespace first).
func (s *scopeInfo) resolve(name string) byte {
	if r, ok := s.resolved[name]; ok {
		return r
	}
	r := s.resolveUncached(name)
	s.resolved[name] = r
	return r
}

func (s *scopeInfo) resolveUncached(name string) byte {
	if s.globals[name] {
		return 'g'
	}
	switch s.kind {
	case scopeModule:
		return 'n'
	case scopeClass:
		if s.nonlocals[name] {
			return 'c'
		}
		if !s.locals[name] && s.enclosingBinds(name) {
			return 'c'
		}
		return 'n'
	}
	if s.nonlocals[name] {
		return 'f'
	}
	if s.locals[name] {
		return 'l'
	}
	if s.enclosingBinds(name) {
		return 'f'
	}
	if s.kind == scopeComp {
		inlined := true
		for p := s.parent; p != nil; p = p.parent {
			if p.kind == scopeModule {
				if inlined {
					return 'n'
				}
				break
			}
			if p.kind != scopeComp {
				inlined = false
			}
		}
	}
	return 'g'
}

// enclosingBinds reports whether an enclosing function-like scope (class
// bodies are skipped) has the name as a variable.
func (s *scopeInfo) enclosingBinds(name string) bool {
	for p := s.parent; p != nil && p.kind != scopeModule; p = p.parent {
		if p.kind == scopeClass {
			continue
		}
		if p.globals[name] {
			return false
		}
		if p.locals[name] || p.nonlocals[name] {
			return true
		}
	}
	return false
}

// qualChild is the __qualname__ of a function or class defined in scope s:
// inlined comprehensions are transparent, a generator expression's children
// are named without <locals>.
func qualChild(s *scopeInfo, name string) string {
	for s != nil && s.kind == scopeComp {
		s = s.parent
	}
	switch {
	case s == nil || s.kind == scopeModule:
		return name
	case s.kind == scopeClass || s.genexp:
		return s.qualname + "." + name
	}
	return s.qualname + ".<locals>." + name
}

// analyzeStmts records the bindings of a block in scope s and builds the
// scopes nested in it.
func analyzeStmts(stmts []*Node, s *scopeInfo) {
	for _, n := range stmts {
		analyzeStmt(n, s)
	}
}

func analyzeStmt(n *Node, s *scopeInfo) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "FunctionDef", "AsyncFunctionDef":
		s.addLocal(mangle(s.private, n.ID))
		analyzeArgsDefaults(n.A, s)
		analyzeAnnotations(n, s)
		for _, d := range n.L1 {
			analyzeExpr(d, s)
		}
		child := newScope(scopeFunction, s, qualChild(s, n.ID))
		child.async = n.Kind == "AsyncFunctionDef"
		n.scope = child
		bindArgs(n.A, child)
		analyzeStmts(n.Body, child)
	case "ClassDef":
		s.addLocal(mangle(s.private, n.ID))
		for _, b := range n.L1 {
			analyzeExpr(b, s)
		}
		for _, k := range n.L2 {
			analyzeExpr(k.A, s)
		}
		for _, d := range n.L3 {
			analyzeExpr(d, s)
		}
		child := newScope(scopeClass, s, qualChild(s, n.ID))
		child.private = n.ID
		n.scope = child
		analyzeStmts(n.Body, child)
	case "Assign":
		for _, t := range n.L1 {
			bindTarget(t, s)
		}
		analyzeExpr(n.A, s)
	case "AugAssign":
		bindTarget(n.A, s)
		analyzeExpr(n.B, s)
	case "AnnAssign":
		if n.C != nil {
			bindTarget(n.A, s)
			analyzeExpr(n.C, s)
		} else if n.A.Kind == "Name" {
			s.addLocal(n.A.ID)
		} else {
			analyzeExpr(n.A, s)
		}
		if s.kind != scopeFunction {
			analyzeExpr(n.B, s)
		}
	case "For", "AsyncFor":
		bindTarget(n.A, s)
		analyzeExpr(n.B, s)
		analyzeStmts(n.Body, s)
		analyzeStmts(n.Orelse, s)
	case "While", "If":
		analyzeExpr(n.A, s)
		analyzeStmts(n.Body, s)
		analyzeStmts(n.Orelse, s)
	case "Match":
		analyzeExpr(n.A, s)
		for _, c := range n.L1 {
			bindPattern(c.A, s)
			analyzeExpr(c.B, s)
			analyzeStmts(c.Body, s)
		}
	case "TypeAlias":
		bindTarget(n.A, s)
	case "Try", "TryStar":
		analyzeStmts(n.Body, s)
		for _, h := range n.L1 {
			analyzeExpr(h.A, s)
			if !h.NoID && h.ID != "" {
				s.addLocal(h.ID)
			}
			analyzeStmts(h.Body, s)
		}
		analyzeStmts(n.Orelse, s)
		analyzeStmts(n.Finalbody, s)
	case "With", "AsyncWith":
		for _, it := range n.L1 {
			analyzeExpr(it.A, s)
			if it.B != nil {
				bindTarget(it.B, s)
			}
		}
		analyzeStmts(n.Body, s)
	case "Delete":
		for _, t := range n.L1 {
			bindTarget(t, s)
		}
	case "Global":
		for _, name := range n.Names {
			s.globals[name] = true
		}
	case "Nonlocal":
		for _, name := range n.Names {
			s.nonlocals[name] = true
		}
	case "Import", "ImportFrom":
		for _, a := range n.L1 {
			if a.ID == "*" {
				continue
			}
			s.addLocal(importBinding(a))
		}
	case "Expr", "Return":
		analyzeExpr(n.A, s)
	case "Raise", "Assert":
		analyzeExpr(n.A, s)
		analyzeExpr(n.B, s)
	}
}

// importBinding is the name an import alias binds.
func importBinding(a *Node) string {
	if !a.NoAsname {
		return a.Asname
	}
	if i := strings.IndexByte(a.ID, '.'); i >= 0 {
		return a.ID[:i]
	}
	return a.ID
}

// analyzeAnnotations finds the scopes in the annotations of a function's
// parameters and return, which run when the function is defined.
func analyzeAnnotations(n *Node, s *scopeInfo) {
	for _, a := range argNodes(n.A) {
		analyzeExpr(a.A, s)
	}
	analyzeExpr(n.B, s)
}

// argNodes are the arg nodes of an arguments node in the order of
// __annotations__: positional-only, ordinary, *args, keyword-only, **kwargs.
func argNodes(a *Node) []*Node {
	out := append(append([]*Node(nil), a.L1...), a.L2...)
	if a.A != nil {
		out = append(out, a.A)
	}
	out = append(out, a.L3...)
	if a.B != nil {
		out = append(out, a.B)
	}
	return out
}

// bindPattern finds the names a match pattern captures and the scopes in
// the expressions it holds.
func bindPattern(p *Node, s *scopeInfo) {
	if p == nil {
		return
	}
	switch p.Kind {
	case "MatchAs", "MatchStar":
		if !p.NoID && p.ID != "" {
			s.addLocal(p.ID)
		}
		bindPattern(p.A, s)
	case "MatchValue":
		analyzeExpr(p.A, s)
	case "MatchSequence", "MatchOr":
		for _, c := range p.L1 {
			bindPattern(c, s)
		}
	case "MatchMapping":
		for _, k := range p.L1 {
			analyzeExpr(k, s)
		}
		for _, c := range p.L2 {
			bindPattern(c, s)
		}
		if !p.NoID && p.ID != "" {
			s.addLocal(p.ID)
		}
	case "MatchClass":
		analyzeExpr(p.A, s)
		for _, c := range p.L1 {
			bindPattern(c, s)
		}
		for _, c := range p.L2 {
			bindPattern(c, s)
		}
	}
}

func analyzeArgsDefaults(a *Node, s *scopeInfo) {
	for _, d := range a.Body {
		analyzeExpr(d, s)
	}
	for _, d := range a.L4 {
		analyzeExpr(d, s)
	}
}

func bindArgs(a *Node, s *scopeInfo) {
	for _, l := range [][]*Node{a.L1, a.L2, a.L3} {
		for _, p := range l {
			s.addLocal(p.ID)
		}
	}
	if a.A != nil {
		s.addLocal(a.A.ID)
	}
	if a.B != nil {
		s.addLocal(a.B.ID)
	}
}

func bindTarget(t *Node, s *scopeInfo) {
	switch t.Kind {
	case "Name":
		s.addLocal(t.ID)
	case "Tuple", "List":
		for _, e := range t.L1 {
			bindTarget(e, s)
		}
	case "Starred":
		bindTarget(t.A, s)
	default:
		analyzeExpr(t, s)
	}
}

// analyzeExpr finds the scopes inside an expression; a Name here reads, whatever
// its context says.
func analyzeExpr(n *Node, s *scopeInfo) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "Lambda":
		analyzeArgsDefaults(n.A, s)
		child := newScope(scopeFunction, s, qualChild(s, "<lambda>"))
		n.scope = child
		bindArgs(n.A, child)
		analyzeExpr(n.B, child)
	case "ListComp", "SetComp", "DictComp", "GeneratorExp":
		kind := scopeComp
		qual := s.qualname
		if n.Kind == "GeneratorExp" {
			kind = scopeFunction
			qual = qualChild(s, "<genexpr>")
		}
		child := newScope(kind, s, qual)
		child.genexp = n.Kind == "GeneratorExp"
		n.scope = child
		for i, g := range n.L1 {
			if i == 0 {
				analyzeExpr(g.B, s)
			} else {
				analyzeExpr(g.B, child)
			}
			bindTarget(g.A, child)
			for _, c := range g.L1 {
				analyzeExpr(c, child)
			}
		}
		analyzeExpr(n.A, child)
		analyzeExpr(n.B, child)
	case "NamedExpr":
		// The target of := in a comprehension or generator expression
		// binds in the enclosing function or module.
		t := s
		for t.kind == scopeComp || t.genexp {
			t = t.parent
		}
		if n.A.Kind == "Name" {
			t.addLocal(n.A.ID)
		} else {
			analyzeExpr(n.A, s)
		}
		analyzeExpr(n.B, s)
	case "Yield", "YieldFrom", "Await":
		if s.kind == scopeFunction {
			s.generator = true
			if n.Kind != "Await" {
				s.yields = true
			}
		}
		analyzeExpr(n.A, s)
	case "Name":
		s.uses[n.ID] = true
		if (n.ID == "super" || n.ID == "__class__") && s.kind == scopeFunction {
			s.classCell = true
		}
	case "Constant":
	default:
		for _, c := range n.children() {
			analyzeExpr(c, s)
		}
	}
}

// pyVM is the state shared by the code run against one globals dictionary.
type pyVM struct {
	globals *dictV
}

type ctlFlow int

const (
	ctlNone ctlFlow = iota
	ctlBreak
	ctlContinue
	ctlReturn
)

// frame is a running scope.
type frame struct {
	vm    *pyVM
	scope *scopeInfo
	// vars holds the variables of a function-like scope.
	vars map[string]Value
	// ns is the namespace of module code (the locals mapping) or of a class
	// body (its dict).
	ns     Value
	parent *frame
	ret    Value
	// fn is the function whose call this frame runs, gen the generator it
	// runs in.
	fn  *funcV
	gen *genState
	// class is the class a class body made, once it is made: the
	// __class__ cell of the methods defined in it.
	class *classV
	// co is the code object the frame runs; nil for a comprehension
	// inlined in its enclosing frame.
	co *codeV
	// back is the frame that was running when this one started.
	back *frame
	// line is the line of the statement running in the frame.
	line int
	// localsDict is the dict locals() returns in a function frame.
	localsDict *dictV
	// obj is the frame object Python code sees for the frame, once made.
	obj *frameObjV
}

// nameError is the NameError for a name found nowhere; like every
// NameError the evaluator raises for a name, its name attribute is set.
func nameError(name string) error {
	e := newPyErr("NameError", "name '"+name+"' is not defined")
	e.setField("name", strV(name))
	return e
}

// nsGet reads a name from a namespace mapping: found is false when it is
// not there (KeyError for a mapping that is not a dict).
func nsGet(ns Value, name string) (Value, bool, error) {
	if d, ok := ns.(*dictV); ok {
		v, found := d.getStr(name)
		return v, found, nil
	}
	v, err := pyGetitem(ns, strV(name))
	if err != nil {
		if isExc(err, "KeyError") {
			return nil, false, nil
		}
		return nil, false, err
	}
	return v, true, nil
}

// loadGlobal reads a global name: the globals, then the builtins.
func (f *frame) loadGlobal(name string) (Value, error) {
	if v, ok := f.vm.globals.getStr(name); ok {
		return v, nil
	}
	if interp.builtins != nil {
		if v, ok := interp.builtins.dict.getStr(name); ok {
			return v, nil
		}
	} else if v := pyBuiltinName(name); v != nil {
		return v, nil
	}
	return nil, nameError(name)
}

// nameFrame is the module or class frame whose namespace a name lookup of
// this frame reads: the frame itself, or for an inlined comprehension the
// module frame it is inlined in.
func (f *frame) nameFrame() *frame {
	g := f
	for g.scope.kind == scopeComp && g.parent != nil {
		g = g.parent
	}
	return g
}

// cellFrame is the frame of the enclosing function that has name as a
// variable.
func (f *frame) cellFrame(name string) *frame {
	for p := f.parent; p != nil; p = p.parent {
		if p.scope.kind == scopeModule {
			return nil
		}
		if p.scope.kind == scopeClass {
			continue
		}
		if p.scope.binds(name) {
			return p
		}
	}
	return nil
}

func freeUnbound(name string) error {
	e := newPyErr("NameError", "cannot access free variable '"+name+
		"' where it is not associated with a value in enclosing scope")
	e.setField("name", strV(name))
	return e
}

func (f *frame) load(name string) (Value, error) {
	switch f.scope.resolve(name) {
	case 'l':
		if v, ok := f.vars[name]; ok {
			return v, nil
		}
		return nil, newPyErr("UnboundLocalError", "cannot access local variable '"+name+
			"' where it is not associated with a value")
	case 'f':
		if p := f.cellFrame(name); p != nil {
			if v, ok := p.vars[name]; ok {
				return v, nil
			}
			return nil, freeUnbound(name)
		}
		return nil, nameError(name)
	case 'c':
		if v, ok, err := nsGet(f.ns, name); err != nil || ok {
			return v, err
		}
		if p := f.cellFrame(name); p != nil {
			if v, ok := p.vars[name]; ok {
				return v, nil
			}
			return nil, freeUnbound(name)
		}
		return nil, nameError(name)
	case 'n':
		if v, ok, err := nsGet(f.nameFrame().ns, name); err != nil || ok {
			return v, err
		}
	}
	return f.loadGlobal(name)
}

func (f *frame) storeName(name string, v Value) error {
	switch f.scope.resolve(name) {
	case 'l':
		f.vars[name] = v
		return nil
	case 'f', 'c':
		if p := f.cellFrame(name); p != nil {
			p.vars[name] = v
			return nil
		}
	case 'n':
		return pySetitem(f.nameFrame().ns, strV(name), v)
	}
	f.vm.globals.setStr(name, v)
	return nil
}

func (f *frame) delName(name string) error {
	switch f.scope.resolve(name) {
	case 'l':
		if _, ok := f.vars[name]; !ok {
			return newPyErr("UnboundLocalError", "cannot access local variable '"+name+
				"' where it is not associated with a value")
		}
		delete(f.vars, name)
		return nil
	case 'f', 'c':
		if p := f.cellFrame(name); p != nil {
			if _, ok := p.vars[name]; !ok {
				return freeUnbound(name)
			}
			delete(p.vars, name)
			return nil
		}
	case 'n':
		err := pyDelitem(f.nameFrame().ns, strV(name))
		if err != nil && isExc(err, "KeyError") {
			return nameError(name)
		}
		return err
	}
	found, err := f.vm.globals.del(strV(name))
	if err != nil {
		return err
	}
	if !found {
		return nameError(name)
	}
	return nil
}

func (f *frame) execBlock(stmts []*Node) (ctlFlow, error) {
	for _, s := range stmts {
		c, err := f.exec(s)
		if err != nil || c != ctlNone {
			return c, err
		}
	}
	return ctlNone, nil
}

func (f *frame) execNode(n *Node) (ctlFlow, error) {
	switch n.Kind {
	case "Expr":
		_, err := f.eval(n.A)
		return ctlNone, err
	case "Assign":
		v, err := f.eval(n.A)
		if err != nil {
			return ctlNone, err
		}
		for _, t := range n.L1 {
			if err := f.assign(t, v); err != nil {
				return ctlNone, err
			}
		}
		return ctlNone, nil
	case "AugAssign":
		return ctlNone, f.augAssign(n)
	case "AnnAssign":
		return ctlNone, f.execAnnAssign(n)
	case "If":
		t, err := f.evalTruth(n.A)
		if err != nil {
			return ctlNone, err
		}
		if t {
			return f.execBlock(n.Body)
		}
		return f.execBlock(n.Orelse)
	case "For":
		return f.execFor(n)
	case "While":
		for {
			t, err := f.evalTruth(n.A)
			if err != nil {
				return ctlNone, err
			}
			if !t {
				return f.execBlock(n.Orelse)
			}
			c, err := f.execBlock(n.Body)
			if err != nil {
				return ctlNone, err
			}
			if c == ctlBreak {
				return ctlNone, nil
			}
			if c == ctlReturn {
				return c, nil
			}
		}
	case "Pass", "Global", "Nonlocal":
		return ctlNone, nil
	case "Break":
		return ctlBreak, nil
	case "Continue":
		return ctlContinue, nil
	case "Return":
		f.ret = none
		if n.A != nil {
			v, err := f.eval(n.A)
			if err != nil {
				return ctlNone, err
			}
			f.ret = v
		}
		return ctlReturn, nil
	case "Try":
		return f.execTry(n)
	case "TryStar":
		return f.execTryStar(n)
	case "Raise":
		return ctlNone, f.execRaise(n)
	case "Assert":
		t, err := f.evalTruth(n.A)
		if err != nil {
			return ctlNone, err
		}
		if t {
			return ctlNone, nil
		}
		var args []Value
		if n.B != nil {
			m, err := f.eval(n.B)
			if err != nil {
				return ctlNone, err
			}
			args = []Value{m}
		}
		return ctlNone, newExc("AssertionError", args)
	case "Delete":
		for _, t := range n.L1 {
			if err := f.del(t); err != nil {
				return ctlNone, err
			}
		}
		return ctlNone, nil
	case "FunctionDef":
		return ctlNone, f.execFunctionDef(n)
	case "ClassDef":
		return ctlNone, f.execClassDef(n)
	case "Import":
		return ctlNone, f.execImport(n)
	case "ImportFrom":
		return ctlNone, f.execImportFrom(n)
	case "With":
		return f.execWith(n, 0)
	case "Match":
		return f.execMatch(n)
	case "AsyncFunctionDef":
		return ctlNone, f.execFunctionDef(n)
	}
	// The code generator refuses every other kind of statement ("Unhandled
	// node type") before a tree is run.
	return ctlNone, newPyErr("SystemError", "unknown statement kind "+n.Kind)
}

func (f *frame) execFor(n *Node) (ctlFlow, error) {
	cur, made := interp.cur, interp.generators
	seq, err := f.evalIterable(n.B)
	if err != nil {
		return ctlNone, err
	}
	it, err := pyIter(seq)
	if err != nil {
		return ctlNone, err
	}
	// A generator that the loop's iterable expression itself made in this
	// frame, by calling a generator function or as a generator expression,
	// is referenced by the loop alone: leaving the loop early drops it, and
	// CPython's reference counting closes it there. A loop in a body being
	// released ends it the same way, without running its Python code.
	var temp *genV
	if g, ok := seq.(*genV); ok && !g.started && g.creator == cur && g.serial > made &&
		(n.B.Kind == "Call" || n.B.Kind == "GeneratorExp") {
		temp = g
	}
	early := func(c ctlFlow, err error) (ctlFlow, error) {
		if temp == nil || temp.done {
			return c, err
		}
		if err == errGenStopped {
			temp.done = true
			temp.body.release()
			return c, err
		}
		// What closing it raises goes where the finalizer's exceptions
		// go in CPython: reported on stderr, not raised.
		if cerr := temp.close(); cerr != nil {
			writeUnraisable(temp, cerr)
		}
		return c, err
	}
	for {
		v, ok, err := it.next()
		if err != nil {
			// FOR_ITER has the location of the iterable's expression.
			noteTracebackLine(f, n.B.Lineno, err)
			return ctlNone, err
		}
		if !ok {
			return f.execBlock(n.Orelse)
		}
		if err := f.assign(n.A, v); err != nil {
			return early(ctlNone, err)
		}
		c, err := f.execBlock(n.Body)
		if err != nil {
			return early(ctlNone, err)
		}
		if c == ctlBreak {
			return early(ctlNone, nil)
		}
		if c == ctlReturn {
			return early(c, nil)
		}
	}
}

func (f *frame) execTry(n *Node) (ctlFlow, error) {
	run := func() (ctlFlow, error) {
		c, err := f.execBlock(n.Body)
		if err == errGenStopped {
			return ctlNone, err
		}
		if err != nil {
			x := asExc(err)
			for _, h := range n.L1 {
				matched := true
				if h.A != nil {
					t, terr := f.eval(h.A)
					if terr != nil {
						return ctlNone, terr
					}
					matched, terr = excMatches(x, t)
					if terr != nil {
						return ctlNone, terr
					}
				}
				if !matched {
					continue
				}
				return f.runHandler(h, x)
			}
			return ctlNone, err
		}
		if c != ctlNone {
			return c, nil
		}
		return f.execBlock(n.Orelse)
	}
	c, err := run()
	return f.runFinally(n, c, err)
}

// runFinally runs the finally block of a try statement whose other blocks
// ended with c and err: an exception passing through is being handled
// while it runs, and a return, break or continue there replaces it.
func (f *frame) runFinally(n *Node, c ctlFlow, err error) (ctlFlow, error) {
	if len(n.Finalbody) == 0 || err == errGenStopped {
		return c, err
	}
	saved := f.ret
	var x *excV
	if err != nil {
		x = asExc(err)
		defer pushHandled(x)()
	}
	fc, ferr := f.execBlock(n.Finalbody)
	if ferr != nil {
		if x != nil {
			chainContext(ferr, x)
		}
		return ctlNone, ferr
	}
	if fc != ctlNone {
		return fc, nil
	}
	f.ret = saved
	return c, err
}

func (f *frame) runHandler(h *Node, x *excV) (ctlFlow, error) {
	pop := pushHandled(x)
	defer pop()
	named := !h.NoID && h.ID != ""
	if named {
		if err := f.storeName(h.ID, x); err != nil {
			chainContext(err, x)
			return ctlNone, err
		}
	}
	c, err := f.execBlock(h.Body)
	if named {
		// The `name = None; del name` of the handler's finally block: an
		// exception it raises replaces the outcome of the body.
		if serr := f.storeName(h.ID, none); serr != nil {
			c, err = ctlNone, serr
		} else if derr := f.delName(h.ID); derr != nil {
			c, err = ctlNone, derr
		}
	}
	if err != nil {
		chainContext(err, x)
	}
	return c, err
}

// excMatches is the test of an except clause.
func excMatches(x *excV, t Value) (bool, error) {
	if tup, ok := t.(*tupleV); ok {
		for _, e := range tup.items {
			m, err := excMatches(x, e)
			if err != nil || m {
				return m, err
			}
		}
		return false, nil
	}
	k, ok := t.(*classV)
	if !ok || !isExcClass(k) {
		return false, newPyErr("TypeError", "catching classes that do not inherit from BaseException is not allowed")
	}
	return excClass(x).subclassOf(k), nil
}

func (f *frame) execRaise(n *Node) error {
	if n.A == nil {
		if x := currentHandled(); x != nil {
			return x
		}
		return newPyErr("RuntimeError", "No active exception to reraise")
	}
	v, err := f.eval(n.A)
	if err != nil {
		return err
	}
	exc, err := exceptionToRaise(v)
	if err != nil {
		return err
	}
	x := exc.(*excV)
	if n.B != nil {
		c, err := f.eval(n.B)
		if err != nil {
			return err
		}
		var cause Value
		switch ck := c.(type) {
		case *excV:
			cause = ck
		case noneV:
		case *classV:
			if !isExcClass(ck) {
				return newPyErr("TypeError", "exception causes must derive from BaseException")
			}
			inst, err := exceptionToRaise(ck)
			if err != nil {
				return err
			}
			cause = inst.(*excV)
		default:
			return newPyErr("TypeError", "exception causes must derive from BaseException")
		}
		x.cause = cause
		x.suppressContext = true
	}
	// A raise statement starts a new traceback entry for this frame.
	x.tbFrame = nil
	return x
}

func (f *frame) del(t *Node) error {
	switch t.Kind {
	case "Name":
		return f.delName(t.ID)
	case "Attribute":
		o, err := f.eval(t.A)
		if err != nil {
			return err
		}
		return pyDelattr(o, t.ID)
	case "Subscript":
		o, err := f.eval(t.A)
		if err != nil {
			return err
		}
		k, err := f.evalSlice(t.B)
		if err != nil {
			return err
		}
		return pyDelitem(o, k)
	case "Tuple", "List":
		for _, e := range t.L1 {
			if err := f.del(e); err != nil {
				return err
			}
		}
		return nil
	}
	return newPyErr("SyntaxError", "cannot delete "+t.Kind)
}

func (f *frame) assign(t *Node, v Value) error {
	switch t.Kind {
	case "Name":
		return f.storeName(t.ID, v)
	case "Attribute":
		o, err := f.eval(t.A)
		if err != nil {
			return err
		}
		return pySetattr(o, t.ID, v)
	case "Subscript":
		o, err := f.eval(t.A)
		if err != nil {
			return err
		}
		k, err := f.evalSlice(t.B)
		if err != nil {
			return err
		}
		return pySetitem(o, k, v)
	case "Tuple", "List":
		return f.unpack(t.L1, v)
	case "Starred":
		return f.assign(t.A, v)
	}
	return newPyErr("SyntaxError", "cannot assign to "+t.Kind)
}

func (f *frame) unpack(targets []*Node, v Value) error {
	star := -1
	for i, t := range targets {
		if t.Kind == "Starred" {
			star = i
		}
	}
	if star < 0 {
		vals, err := unpackValues(v, len(targets))
		if err != nil {
			return err
		}
		for i, t := range targets {
			if err := f.assign(t, vals[i]); err != nil {
				return err
			}
		}
		return nil
	}
	it, err := pyIterForUnpack(v)
	if err != nil {
		return err
	}
	var vals []Value
	for {
		x, ok, err := it.next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		vals = append(vals, x)
	}
	after := len(targets) - star - 1
	if len(vals) < star+after {
		return valueErr("not enough values to unpack (expected at least %d, got %d)", star+after, len(vals))
	}
	for i := 0; i < star; i++ {
		if err := f.assign(targets[i], vals[i]); err != nil {
			return err
		}
	}
	mid := append([]Value(nil), vals[star:len(vals)-after]...)
	if err := f.assign(targets[star].A, newList(mid)); err != nil {
		return err
	}
	for i := 0; i < after; i++ {
		if err := f.assign(targets[star+1+i], vals[len(vals)-after+i]); err != nil {
			return err
		}
	}
	return nil
}

// unpackValues is the n values of `t1, ..., tn = v`, with the errors of
// unpacking.
func unpackValues(v Value, n int) ([]Value, error) {
	it, err := pyIterForUnpack(v)
	if err != nil {
		return nil, err
	}
	vals := make([]Value, 0, n)
	for len(vals) < n {
		x, ok, err := it.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, valueErr("not enough values to unpack (expected %d, got %d)", n, len(vals))
		}
		vals = append(vals, x)
	}
	_, more, err := it.next()
	if err != nil {
		return nil, err
	}
	if more {
		return nil, valueErr("too many values to unpack (expected %d)", n)
	}
	return vals, nil
}

func pyIterForUnpack(v Value) (iterator, error) {
	it, err := pyIter(v)
	if err != nil && isExc(err, "TypeError") && !isIterable(v) {
		return nil, typeErr("cannot unpack non-iterable %s object", typeName(v))
	}
	return it, err
}

func isIterable(v Value) bool {
	switch unsub(v).(type) {
	case *listV, *tupleV, *dictV, strV, bytesV, *bytearrayV, rangeV:
		return true
	}
	if k := userClassOf(v); k != nil {
		if definesIteration(k) {
			return true
		}
		x, isInst := v.(*instV)
		return isInst && x.base != nil && isIterable(x.base)
	}
	if _, isClass := v.(*classV); isClass {
		return false
	}
	_, ok := v.(iterable)
	return ok
}

func (f *frame) augAssign(n *Node) error {
	op := n.Op
	t := n.A
	switch t.Kind {
	case "Name":
		cur, err := f.load(t.ID)
		if err != nil {
			return err
		}
		v, err := f.eval(n.B)
		if err != nil {
			return err
		}
		out, err := inplaceOp(op, cur, v)
		if err != nil {
			return err
		}
		return f.storeName(t.ID, out)
	case "Attribute":
		o, err := f.eval(t.A)
		if err != nil {
			return err
		}
		cur, err := pyGetattr(o, t.ID)
		if err != nil {
			return err
		}
		v, err := f.eval(n.B)
		if err != nil {
			return err
		}
		out, err := inplaceOp(op, cur, v)
		if err != nil {
			return err
		}
		return pySetattr(o, t.ID, out)
	case "Subscript":
		o, err := f.eval(t.A)
		if err != nil {
			return err
		}
		k, err := f.evalSlice(t.B)
		if err != nil {
			return err
		}
		cur, err := pyGetitem(o, k)
		if err != nil {
			return err
		}
		v, err := f.eval(n.B)
		if err != nil {
			return err
		}
		out, err := inplaceOp(op, cur, v)
		if err != nil {
			return err
		}
		return pySetitem(o, k, out)
	}
	return newPyErr("SyntaxError", "illegal expression for augmented assignment")
}

// inplaceOp is x op= y: lists, sets and the like change in place.
func inplaceOp(op string, a, b Value) (Value, error) {
	if userClassOf(a) != nil {
		name := "__i" + strings.TrimPrefix(binopNames[op][0], "__")
		if r, ok, err := callSpecial(a, name, b); ok && (err != nil || !isNotImplemented(r)) {
			return r, err
		}
		if x, isInst := a.(*instV); isInst && x.base != nil && !hasNumberSlot(a, op) {
			// The in-place operator of the builtin type, which changes the
			// value the instance holds and gives the instance back.
			r, err := inplaceOp(op, x.base, b)
			if err != nil || r != x.base {
				return r, err
			}
			return a, nil
		}
	}
	switch x := a.(type) {
	case *listV:
		if hasNumberSlot(b, op) {
			r, err := slotBinary(op, a, b)
			if err != nil || !isNotImplemented(r) {
				return r, err
			}
		}
		switch op {
		case "Add":
			items, err := toSliceExtend(b)
			if err != nil {
				return nil, err
			}
			x.items = append(x.items, items...)
			return x, nil
		case "Mult":
			n, ok, err := repeatCount(b)
			if err != nil {
				return nil, err
			}
			if ok {
				x.items = repeatItems(x.items, n)
				return x, nil
			}
		}
	case *dictV:
		if op == "BitOr" {
			if _, isDict := unsub(b).(*dictV); isDict {
				if _, err := methodsDictUpdate(x, []Value{b}, nil); err != nil {
					return nil, err
				}
				return x, nil
			}
		}
	case *setV:
		if o, ok := b.(*setV); ok && !x.frozen && isSetOperator(op) {
			r, err := binop(op, a, o)
			if err != nil {
				return nil, err
			}
			rs, isSet := r.(*setV)
			if !isSet {
				return r, nil
			}
			x.clear()
			if err := x.merge(rs); err != nil {
				return nil, err
			}
			return x, nil
		}
	}
	r, err := binop(op, a, b)
	if err != nil {
		if ie, ok := err.(*excV); ok && ie.Class == "TypeError" && strings.HasPrefix(ie.Message, "unsupported operand type(s) for ") {
			sym := binopSymbol(op)
			m := strings.Replace(ie.Message, "for "+sym+":", "for "+sym+"=:", 1)
			return nil, newPyErr("TypeError", m)
		}
		return nil, err
	}
	return r, nil
}

// isSetOperator reports whether op is one of the operators set and frozenset
// define (|, &, ^, -), which a set updates in place.
func isSetOperator(op string) bool {
	switch op {
	case "BitOr", "BitAnd", "BitXor", "Sub":
		return true
	}
	return false
}

func toSliceExtend(b Value) ([]Value, error) {
	if !isIterable(b) {
		return nil, typeErr("'%s' object is not iterable", typeName(b))
	}
	return toSlice(b)
}

// binopSymbol is the operator CPython names in the TypeError of a binary
// operation its operands do not support.
func binopSymbol(op string) string {
	switch op {
	case "Pow":
		return "** or pow()"
	case "MatMult":
		return "@"
	case "DivMod":
		return "divmod()"
	}
	return binaryOperators[op]
}

func (f *frame) evalTruth(n *Node) (bool, error) {
	v, err := f.eval(n)
	if err != nil {
		return false, err
	}
	return truthy(v)
}

func (f *frame) evalSlice(n *Node) (Value, error) {
	if n.Kind == "Slice" {
		parts := []Value{none, none, none}
		for i, p := range []*Node{n.A, n.B, n.C} {
			if p != nil {
				v, err := f.eval(p)
				if err != nil {
					return nil, err
				}
				parts[i] = v
			}
		}
		return &sliceV{start: parts[0], stop: parts[1], step: parts[2]}, nil
	}
	return f.eval(n)
}

func (f *frame) evalList(nodes []*Node) ([]Value, error) {
	out := make([]Value, 0, len(nodes))
	for _, e := range nodes {
		if e.Kind == "Starred" {
			v, err := f.eval(e.A)
			if err != nil {
				return nil, err
			}
			if !isIterable(v) {
				return nil, typeErr("Value after * must be an iterable, not %s", typeName(v))
			}
			items, err := toSlice(v)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
			continue
		}
		v, err := f.eval(e)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (f *frame) evalNode(n *Node) (Value, error) {
	switch n.Kind {
	case "Constant":
		return n.Value, nil
	case "Name":
		return f.load(n.ID)
	case "Attribute":
		o, err := f.eval(n.A)
		if err != nil {
			return nil, err
		}
		return pyGetattr(o, n.ID)
	case "Subscript":
		o, err := f.eval(n.A)
		if err != nil {
			return nil, err
		}
		k, err := f.evalSlice(n.B)
		if err != nil {
			return nil, err
		}
		return pyGetitem(o, k)
	case "Call":
		return f.evalCall(n)
	case "BinOp":
		a, err := f.eval(n.A)
		if err != nil {
			return nil, err
		}
		b, err := f.eval(n.B)
		if err != nil {
			return nil, err
		}
		return binop(n.Op, a, b)
	case "UnaryOp":
		v, err := f.eval(n.A)
		if err != nil {
			return nil, err
		}
		if n.Op == "Not" {
			t, err := truthy(v)
			if err != nil {
				return nil, err
			}
			return pyBool(!t), nil
		}
		return unop(n.Op, v)
	case "BoolOp":
		var last Value
		for _, e := range n.L1 {
			v, err := f.eval(e)
			if err != nil {
				return nil, err
			}
			last = v
			t, err := truthy(v)
			if err != nil {
				return nil, err
			}
			if (n.Op == "And") != t {
				return v, nil
			}
		}
		return last, nil
	case "Compare":
		left, err := f.eval(n.A)
		if err != nil {
			return nil, err
		}
		var res Value = trueV
		for i, c := range n.L1 {
			var right Value
			var err error
			if n.Ops[i] == "In" || n.Ops[i] == "NotIn" {
				right, err = f.evalIterable(c)
			} else {
				right, err = f.eval(c)
			}
			if err != nil {
				return nil, err
			}
			res, err = compareOp(n.Ops[i], left, right)
			if err != nil {
				return nil, err
			}
			if i < len(n.L1)-1 {
				t, err := truthy(res)
				if err != nil {
					return nil, err
				}
				if !t {
					return res, nil
				}
			}
			left = right
		}
		return res, nil
	case "IfExp":
		t, err := f.evalTruth(n.A)
		if err != nil {
			return nil, err
		}
		if t {
			return f.eval(n.B)
		}
		return f.eval(n.C)
	case "Lambda":
		return f.makeFunc(n, "<lambda>")
	case "List":
		items, err := f.evalList(n.L1)
		if err != nil {
			return nil, err
		}
		return newList(items), nil
	case "Tuple":
		items, err := f.evalList(n.L1)
		if err != nil {
			return nil, err
		}
		return newTuple(items), nil
	case "Slice":
		// A slice inside the tuple of an extended subscription.
		return f.evalSlice(n)
	case "Set":
		items, err := f.evalList(n.L1)
		if err != nil {
			return nil, err
		}
		if len(n.L1) > 2 && allFoldable(n.L1) {
			return ConstSetDisplay(items)
		}
		s := newSet(false)
		for _, it := range items {
			if err := s.add(it); err != nil {
				return nil, err
			}
		}
		return s, nil
	case "Dict":
		return f.evalDict(n)
	case "ListComp", "SetComp", "DictComp":
		return f.evalComp(n)
	case "GeneratorExp":
		return f.evalGenExp(n)
	case "JoinedStr":
		var b strings.Builder
		for _, k := range n.L1 {
			v, err := f.eval(k)
			if err != nil {
				return nil, err
			}
			s, _ := v.(strV)
			b.WriteString(string(s))
		}
		return strV(b.String()), nil
	case "FormattedValue":
		return f.evalFormatted(n)
	case "NamedExpr":
		v, err := f.eval(n.B)
		if err != nil {
			return nil, err
		}
		return v, f.assign(n.A, v)
	case "Starred":
		return nil, newPyErr("SyntaxError", "can't use starred expression here")
	case "Yield":
		var v Value = none
		if n.A != nil {
			var err error
			if v, err = f.eval(n.A); err != nil {
				return nil, err
			}
		}
		return f.yieldValue(v)
	case "Await":
		return f.await(n)
	case "YieldFrom":
		return f.yieldFrom(n)
	}
	// The code generator refuses every other kind of expression ("Unhandled
	// node type") before a tree is run.
	return nil, newPyErr("SystemError", "unknown expression kind "+n.Kind)
}

// evalDict builds a dict display as CPython's bytecode does: the pairs are
// evaluated in runs of up to 17 (or up to a ** unpacking) before they are
// stored.
func (f *frame) evalDict(n *Node) (Value, error) {
	d := newDict()
	var pending []Value
	flush := func() error {
		for i := 0; i < len(pending); i += 2 {
			if err := d.set(pending[i], pending[i+1]); err != nil {
				return err
			}
		}
		pending = pending[:0]
		return nil
	}
	for i, k := range n.L1 {
		if k == nil {
			if err := flush(); err != nil {
				return nil, err
			}
			m, err := f.eval(n.L2[i])
			if err != nil {
				return nil, err
			}
			if ok, err := isMapping(m); err != nil {
				return nil, err
			} else if !ok {
				return nil, typeErr("'%s' object is not a mapping", typeName(m))
			}
			if _, err := methodsDictUpdate(d, []Value{m}, nil); err != nil {
				return nil, err
			}
			continue
		}
		kv, err := f.eval(k)
		if err != nil {
			return nil, err
		}
		vv, err := f.eval(n.L2[i])
		if err != nil {
			return nil, err
		}
		pending = append(pending, kv, vv)
		if len(pending) == 34 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	return d, flush()
}

// isMapping reports whether ** accepts v: a dict, or an object with keys;
// the error is what looking keys up raises other than AttributeError.
func isMapping(v Value) (bool, error) {
	if _, ok := v.(*dictV); ok {
		return true, nil
	}
	keys, err := getattrOpt(v, "keys")
	return keys != nil, err
}

func (f *frame) evalFormatted(n *Node) (Value, error) {
	val, err := f.eval(n.A)
	if err != nil {
		return nil, err
	}
	switch n.Conversion {
	case 115:
		s, err := pyStr(val)
		if err != nil {
			return nil, err
		}
		val = strV(s)
	case 114:
		s, err := pyRepr(val)
		if err != nil {
			return nil, err
		}
		val = strV(s)
	case 97:
		s, err := fmtASCII(val)
		if err != nil {
			return nil, err
		}
		val = strV(s)
	}
	spec := ""
	if n.B != nil {
		sv, err := f.eval(n.B)
		if err != nil {
			return nil, err
		}
		s, err := pyStr(sv)
		if err != nil {
			return nil, err
		}
		spec = s
	}
	r, err := formatValue(val, spec)
	if err != nil {
		return nil, err
	}
	return strV(r), nil
}

func (f *frame) evalComp(n *Node) (Value, error) {
	iter0, err := f.compFirstIter(n)
	if err != nil {
		return nil, err
	}
	child := &frame{vm: f.vm, scope: n.scope, vars: map[string]Value{}, parent: f}
	var items []Value
	var set *setV
	var dict *dictV
	switch n.Kind {
	case "SetComp":
		set = newSet(false)
	case "DictComp":
		dict = newDict()
	}
	err = child.compLoop(n.L1, 0, iter0, func() error {
		if n.Kind == "DictComp" {
			k, err := child.eval(n.A)
			if err != nil {
				return err
			}
			v, err := child.eval(n.B)
			if err != nil {
				return err
			}
			return dict.set(k, v)
		}
		v, err := child.eval(n.A)
		if err != nil {
			return err
		}
		if set != nil {
			return set.add(v)
		}
		items = append(items, v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	switch n.Kind {
	case "SetComp":
		return set, nil
	case "DictComp":
		return dict, nil
	}
	return newList(items), nil
}

func (f *frame) compFirstIter(n *Node) (iterator, error) {
	seq, err := f.evalIterable(n.L1[0].B)
	if err != nil {
		return nil, err
	}
	return pyIter(seq)
}

func (f *frame) compLoop(gens []*Node, i int, it iterator, body func() error) error {
	g := gens[i]
	if it == nil {
		seq, err := f.evalIterable(g.B)
		if err != nil {
			return err
		}
		it, err = pyIter(seq)
		if err != nil {
			noteTracebackLine(f, g.B.Lineno, err)
			return err
		}
	}
outer:
	for {
		v, ok, err := it.next()
		if err != nil {
			// FOR_ITER has the location of the iterable's expression.
			noteTracebackLine(f, g.B.Lineno, err)
			return err
		}
		if !ok {
			return nil
		}
		if err := f.assign(g.A, v); err != nil {
			return err
		}
		for _, c := range g.L1 {
			t, err := f.evalTruth(c)
			if err != nil {
				return err
			}
			if !t {
				continue outer
			}
		}
		if i+1 < len(gens) {
			if err := f.compLoop(gens, i+1, nil, body); err != nil {
				return err
			}
			continue
		}
		if err := body(); err != nil {
			return err
		}
	}
}

// genState is the link between a running generator body and its
// generator object. Neither it nor the body's frame refers to the generator
// object, so that a generator nobody refers to any more can be collected
// while its body is paused.
type genState struct {
	yield func(Value, error) bool
	// sent is the value send() passes to the paused yield expression;
	// thrown is the exception throw() or close() raises there.
	sent   Value
	thrown error
	// delegate is the iterator a yield from in the body is delegating to.
	delegate Value
	// handled holds the exceptions the paused body is handling: the
	// generator's own exception state, which sys.exception() shows on top
	// of its caller's while the body runs.
	handled []*excV
	// retval is the value the body returned, for StopIteration.
	retval Value
}

// errGenStopped ends a paused generator body that will never be resumed:
// the body unwinds without running any more Python code (no except
// handler, finally block or __exit__).
var errGenStopped = &excV{Class: "GeneratorExit"}

// genBody is what ends a generator body: stop (from iter.Pull2) unwinds a
// paused body with errGenStopped, run with the body's own exception state
// and frame.
type genBody struct {
	stop  func()
	st    *genState
	frame *frame
}

// release ends the body: at once if it never started or has finished,
// else by unwinding it from its paused yield without running Python code.
// The interpreter's exception state and current frame are those of the
// body while it unwinds, and are restored afterwards.
func (b *genBody) release() {
	outer := interp.handled
	interp.handled = append([]*excV(nil), b.st.handled...)
	b.st.handled = nil
	leave := b.frame.enter()
	b.stop()
	leave()
	b.frame.back = nil
	interp.handled = outer
}

// dropped holds the bodies of the generators collected while paused,
// which releaseDropped ends on the interpreter's goroutine.
var dropped struct {
	sync.Mutex
	bodies []*genBody
}

// dropBody queues the body of a collected generator object; it runs on the
// goroutine of the runtime's cleanups, so it only queues.
func dropBody(b *genBody) {
	dropped.Lock()
	dropped.bodies = append(dropped.bodies, b)
	dropped.Unlock()
}

// releaseDropped ends the bodies of the generators collected since the
// last call.
func releaseDropped() {
	dropped.Lock()
	bodies := dropped.bodies
	dropped.bodies = nil
	dropped.Unlock()
	for _, b := range bodies {
		b.release()
	}
}

// genV is a generator object, made by a generator expression or by calling a
// generator function.
type genV struct {
	qual    string
	next    func() (Value, error, bool)
	body    *genBody
	state   *genState
	started bool
	running bool
	done    bool
	// frame is the frame the body runs in.
	frame *frame
	// creator is the frame that was running when the generator was made,
	// serial the number of generators made before it.
	creator *frame
	serial  uint64
	// kind is the type name: generator, coroutine or async_generator.
	kind string
	// awaited is set once a coroutine has been awaited.
	awaited bool
}

func (g *genV) pyType() string { return g.kind }

func (g *genV) repr(*reprState) (string, error) {
	return "<" + g.kind + " object " + g.qual + " at " + objAddr(g) + ">", nil
}

// resume runs the body to its next yield, with sent as the value of the
// paused yield expression (or thrown raised there). more is false when the
// body finished.
func (g *genV) resume(sent Value, thrown error) (Value, bool, error) {
	st := g.state
	if g.done {
		// Only the call that finished the body reports its return value;
		// an exhausted generator raises a bare StopIteration.
		st.retval = nil
		if thrown != nil {
			return nil, false, thrown
		}
		return nil, false, nil
	}
	if g.running {
		return nil, false, valueErr("generator already executing")
	}
	if !g.started && thrown != nil {
		g.done = true
		g.body.release()
		return nil, false, thrown
	}
	g.started = true
	st.sent, st.thrown = sent, thrown
	// A generator's frame counts towards the recursion limit while it
	// runs; at the limit the frame is left without running its handlers,
	// and the generator is finished.
	if interp.depth >= interp.recursionLimit {
		g.done = true
		g.body.release()
		return nil, false, newPyErr("RecursionError", "maximum recursion depth exceeded")
	}
	interp.depth++
	g.running = true
	// The body runs with its own exception state on top of the caller's,
	// and keeps what it is still handling when it yields.
	outer := interp.handled
	interp.handled = append(outer[:len(outer):len(outer)], st.handled...)
	leave := g.frame.enter()
	v, err, ok := g.next()
	leave()
	// A paused generator's frame has no f_back, and does not keep its
	// last caller alive.
	g.frame.back = nil
	st.handled = append([]*excV(nil), interp.handled[len(outer):]...)
	interp.handled = outer
	g.running = false
	interp.depth--
	if !ok || err != nil {
		g.done = true
		g.body.release()
		if err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	return v, true, nil
}

func (g *genV) iter() (iterator, error) {
	if g.kind != "generator" {
		return nil, typeErr("'%s' object is not iterable", g.kind)
	}
	return funcIter(func() (Value, bool, error) { return g.resume(none, nil) }), nil
}

// close is generator.close(): GeneratorExit is raised at the paused yield.
func (g *genV) close() error {
	if !g.started || g.done {
		g.done = true
		g.body.release()
		return nil
	}
	_, more, err := g.resume(none, newExc("GeneratorExit", nil))
	if err != nil {
		if isExc(err, "GeneratorExit", "StopIteration") {
			return nil
		}
		return err
	}
	if more {
		return newPyErr("RuntimeError", "generator ignored GeneratorExit")
	}
	return nil
}

// stopIteration is the StopIteration a finished generator raises.
func (g *genV) stopIteration() error {
	if r := g.state.retval; r != nil {
		return newExc("StopIteration", []Value{r})
	}
	return newExc("StopIteration", nil)
}

func (g *genV) getAttr(name string) (Value, error) {
	switch name {
	case "send":
		return methodsBound(g, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("generator.send", args, kw); err != nil {
				return nil, err
			}
			if !g.started && !isNoneV(args[0]) {
				return nil, typeErr("can't send non-None value to a just-started generator")
			}
			v, more, err := g.resume(args[0], nil)
			if err != nil {
				return nil, err
			}
			if !more {
				return nil, g.stopIteration()
			}
			return v, nil
		}), nil
	case "throw":
		return methodsBound(g, name, func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("throw", 1, 3, args, kw); err != nil {
				return nil, err
			}
			exc, err := exceptionToRaise(args[0])
			if err != nil {
				return nil, err
			}
			v, more, err := g.resume(none, exc)
			if err != nil {
				return nil, err
			}
			if !more {
				return nil, g.stopIteration()
			}
			return v, nil
		}), nil
	case "close":
		return methodsBound(g, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("generator.close", args, kw); err != nil {
				return nil, err
			}
			return none, g.close()
		}), nil
	case "__next__":
		return methodsBound(g, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("generator.__next__", args, kw); err != nil {
				return nil, err
			}
			v, more, err := g.resume(none, nil)
			if err != nil {
				return nil, err
			}
			if !more {
				return nil, g.stopIteration()
			}
			return v, nil
		}), nil
	case "__name__":
		i := strings.LastIndexByte(g.qual, '.')
		return strV(g.qual[i+1:]), nil
	case "__qualname__":
		return strV(g.qual), nil
	}
	return g.stateAttr(name)
}

// stateAttr is one of the attributes that show the state of a generator
// (gi_running, gi_frame, gi_code, gi_suspended, gi_yieldfrom), of a
// coroutine (cr_running, ..., cr_await) or of an asynchronous generator
// (ag_running, ..., ag_await).
func (g *genV) stateAttr(name string) (Value, error) {
	prefix, delegate := "gi_", "gi_yieldfrom"
	switch g.kind {
	case "coroutine":
		prefix, delegate = "cr_", "cr_await"
	case "async_generator":
		prefix, delegate = "ag_", "ag_await"
	}
	switch name {
	case prefix + "running":
		return pyBool(g.running), nil
	case prefix + "suspended":
		return pyBool(g.started && !g.running && !g.done), nil
	case prefix + "frame":
		if g.done || g.frame == nil {
			return none, nil
		}
		return g.frame.codeFrame().object(), nil
	case prefix + "code":
		if g.frame != nil {
			if co := g.frame.codeObj(); co != nil {
				return co, nil
			}
		}
	case delegate:
		if g.state != nil && g.state.delegate != nil && !g.done {
			return g.state.delegate, nil
		}
		return none, nil
	}
	return nil, nil
}

// exceptionToRaise turns the operand of raise (an exception or an exception
// class) into the exception raised.
func exceptionToRaise(v Value) (error, error) {
	switch x := v.(type) {
	case *excV:
		return x, nil
	case *classV:
		if isExcClass(x) {
			inst, err := callValue(nil, x, nil, nil)
			if err != nil {
				return nil, err
			}
			if e, ok := inst.(*excV); ok {
				return e, nil
			}
			r, err := pyRepr(x)
			if err != nil {
				return nil, err
			}
			return nil, typeErr("calling %s should have returned an instance of BaseException, not %s",
				r, typeName(inst))
		}
	}
	return nil, typeErr("exceptions must derive from BaseException")
}

// startGenerator makes the generator object running body in frame f.
func startGenerator(qual string, f *frame, body func() (Value, error)) *genV {
	return startGeneratorKind("generator", qual, f, body)
}

// startGeneratorKind is startGenerator for a generator, a coroutine or an
// asynchronous generator (kind).
func startGeneratorKind(kind, qual string, f *frame, body func() (Value, error)) *genV {
	releaseDropped()
	st := &genState{sent: none}
	f.gen = st
	seq := iter.Seq2[Value, error](func(yield func(Value, error) bool) {
		st.yield = yield
		v, err := body()
		if err != nil {
			if err == errGenStopped {
				return
			}
			if isExc(err, "StopIteration") {
				re := newPyErr("RuntimeError", "generator raised StopIteration")
				stop := asExc(err)
				re.cause, re.context, re.suppressContext = stop, stop, true
				err = re
			}
			yield(nil, err)
			return
		}
		if v != nil && !isNoneV(v) {
			st.retval = v
		}
	})
	next, stop := iter.Pull2(seq)
	interp.generators++
	g := &genV{kind: kind, qual: qual, state: st, frame: f, creator: interp.cur, serial: interp.generators,
		next: next, body: &genBody{stop: stop, st: st, frame: f}}
	// A generator object collected while its body is paused gets that body
	// ended, by releaseDropped on the interpreter's goroutine, without
	// running any of its Python code: CPython would run the finally blocks
	// when the last reference went, which this runtime cannot tell, so they
	// do not run at all rather than at some other point.
	runtime.AddCleanup(g, dropBody, g.body)
	return g
}

// yieldValue is a yield expression in the generator body run by f.
func (f *frame) yieldValue(v Value) (Value, error) {
	st := f.gen
	if st == nil {
		return nil, newPyErr("SyntaxError", "'yield' outside function")
	}
	if !st.yield(v, nil) {
		return nil, errGenStopped
	}
	if st.thrown != nil {
		e := st.thrown
		st.thrown = nil
		return nil, e
	}
	r := st.sent
	st.sent = none
	return r, nil
}

func (f *frame) evalGenExp(n *Node) (Value, error) {
	iter0, err := f.compFirstIter(n)
	if err != nil {
		return nil, err
	}
	child := &frame{vm: f.vm, scope: n.scope, vars: map[string]Value{}, parent: f,
		co: nodeCode(n, f.codeObj().filename)}
	return startGenerator(n.scope.qualname, child, func() (Value, error) {
		return nil, child.compLoop(n.L1, 0, iter0, func() error {
			v, err := child.eval(n.A)
			if err != nil {
				return err
			}
			_, err = child.yieldValue(v)
			return err
		})
	}), nil
}

// funcV is a Python function made by def or lambda, or by FunctionType
// from a code object.
type funcV struct {
	name, qual string
	code       *codeV
	closure    *frame
	vm         *pyVM
	defaults   []Value
	kwdefaults map[string]Value
	doc        Value
	// module is __module__, the globals' __name__ when the function was
	// made.
	module Value
	// dict is the function's __dict__, nil until an attribute is set.
	dict *dictV
	// annotations is __annotations__, nil for an empty dict.
	annotations *dictV
}

func (*funcV) pyType() string { return "function" }

func (fn *funcV) repr(*reprState) (string, error) {
	return "<function " + fn.qual + " at " + objAddr(fn) + ">", nil
}

// yieldFrom is yield from: the items of the iterable are yielded in turn,
// and the expression's value is what a delegated generator returned.
func (f *frame) yieldFrom(n *Node) (Value, error) {
	src, err := f.eval(n.A)
	if err != nil {
		return nil, err
	}
	return f.delegate(src)
}

// await is an await expression: the awaitable's iterator (a coroutine
// itself, or what __await__ returns) is delegated to as yield from does.
func (f *frame) await(n *Node) (Value, error) {
	v, err := f.eval(n.A)
	if err != nil {
		return nil, err
	}
	if g, ok := v.(*genV); ok && g.kind == "coroutine" {
		if g.done || g.started && !g.running && g.awaited {
			return nil, newPyErr("RuntimeError", "cannot reuse already awaited coroutine")
		}
		g.awaited = true
		return f.delegate(g)
	}
	m, ok := specialOf(v, "__await__")
	if !ok {
		return nil, typeErr("object %s can't be used in 'await' expression", typeName(v))
	}
	it, err := callValue(nil, m, nil, nil)
	if err != nil {
		return nil, err
	}
	if g, ok := it.(*genV); ok && g.kind != "generator" || !isIterator(it) {
		return nil, typeErr("__await__() returned non-iterator of type '%s'", typeName(it))
	}
	return f.delegate(it)
}

// delegate is the delegation of yield from to src: values yielded by src
// pass through, sent values and thrown exceptions go to it, and its return
// value is the result.
func (f *frame) delegate(src Value) (Value, error) {
	if st := f.codeFrame().gen; st != nil {
		st.delegate = src
		defer func() { st.delegate = nil }()
	}
	if g, ok := src.(*genV); ok {
		var sent Value = none
		var thrown error
		for {
			v, more, err := g.resume(sent, thrown)
			if err != nil {
				return nil, err
			}
			if !more {
				if r := g.state.retval; r != nil {
					return r, nil
				}
				return none, nil
			}
			sent, thrown = none, nil
			r, yerr := f.yieldValue(v)
			if yerr == nil {
				sent = r
				continue
			}
			// A delegating body that is being released leaves the inner
			// generator as it is: it is a generator object of its own.
			if yerr == errGenStopped {
				return nil, yerr
			}
			// close() of the delegating generator closes the inner one;
			// throw() passes the exception on to it.
			if isExc(yerr, "GeneratorExit") {
				if cerr := g.close(); cerr != nil {
					return nil, cerr
				}
				return nil, yerr
			}
			thrown = yerr
		}
	}
	it, err := pyIter(src)
	if err != nil {
		return nil, err
	}
	for {
		v, more, err := it.next()
		if err != nil {
			return nil, err
		}
		if !more {
			return none, nil
		}
		if _, err := f.yieldValue(v); err != nil {
			return nil, err
		}
	}
}

func (f *frame) makeFunc(n *Node, name string) (Value, error) {
	a := n.A
	defaults := make([]Value, 0, len(a.Body))
	for _, d := range a.Body {
		v, err := f.eval(d)
		if err != nil {
			return nil, err
		}
		defaults = append(defaults, v)
	}
	kwd := map[string]Value{}
	for i, d := range a.L4 {
		if d == nil {
			continue
		}
		v, err := f.eval(d)
		if err != nil {
			return nil, err
		}
		kwd[a.L3[i].ID] = v
	}
	var ann *dictV
	if n.Kind == "FunctionDef" || n.Kind == "AsyncFunctionDef" {
		d, err := f.annotations(n)
		if err != nil {
			return nil, err
		}
		ann = d
	}
	mod, _ := f.vm.globals.getStr("__name__")
	if mod == nil {
		mod = none
	}
	fn := &funcV{annotations: ann, name: name, module: mod, qual: n.scope.qualname,
		code: nodeCode(n, f.codeObj().filename), closure: f, vm: f.vm, defaults: defaults, kwdefaults: kwd}
	if doc := docstring(fn.code.body); doc != nil {
		fn.doc = doc
	}
	return fn, nil
}

// bind places call arguments as CPython's initialize_locals does.
func (fn *funcV) bind(args []Value, kw *dictV) (map[string]Value, error) {
	a := fn.code.args
	vars := map[string]Value{}
	posNames := make([]string, 0, len(a.L1)+len(a.L2))
	for _, p := range a.L1 {
		posNames = append(posNames, p.ID)
	}
	for _, p := range a.L2 {
		posNames = append(posNames, p.ID)
	}
	n := len(args)
	if n > len(posNames) {
		n = len(posNames)
	}
	for i := 0; i < n; i++ {
		vars[posNames[i]] = args[i]
	}
	if a.A != nil {
		rest := []Value{}
		if len(args) > len(posNames) {
			rest = append(rest, args[len(posNames):]...)
		}
		vars[a.A.ID] = newTuple(rest)
	}
	var kwdict *dictV
	if a.B != nil {
		kwdict = newDict()
		vars[a.B.ID] = kwdict
	}
	posOnlyCount := len(a.L1)
	var posOnlyGiven []string
	if kw != nil {
		for i, k := range kw.keys {
			name, _ := k.(strV)
			key := string(name)
			found := false
			for j, p := range posNames {
				if p == key && j >= posOnlyCount {
					if _, dup := vars[key]; dup {
						return nil, typeErr("%s() got multiple values for argument '%s'", fn.qual, key)
					}
					vars[key] = kw.vals[i]
					found = true
					break
				}
			}
			if found {
				continue
			}
			for _, p := range a.L3 {
				if p.ID == key {
					vars[key] = kw.vals[i]
					found = true
					break
				}
			}
			if found {
				continue
			}
			if kwdict != nil {
				if err := kwdict.set(k, kw.vals[i]); err != nil {
					return nil, err
				}
				continue
			}
			isPosOnly := false
			for j := 0; j < posOnlyCount; j++ {
				if posNames[j] == key {
					isPosOnly = true
				}
			}
			if isPosOnly {
				posOnlyGiven = append(posOnlyGiven, key)
				continue
			}
			return nil, typeErr("%s() got an unexpected keyword argument '%s'", fn.qual, key)
		}
	}
	if len(posOnlyGiven) > 0 {
		return nil, typeErr("%s() got some positional-only arguments passed as keyword arguments: '%s'", fn.qual,
			strings.Join(posOnlyGiven, ", "))
	}
	if len(args) > len(posNames) && a.A == nil {
		return nil, fn.tooManyPositional(len(args), vars)
	}
	nDefaults := len(fn.defaults)
	required := len(posNames) - nDefaults
	var missing []string
	for i := n; i < required; i++ {
		if _, ok := vars[posNames[i]]; !ok {
			missing = append(missing, posNames[i])
		}
	}
	if len(missing) > 0 {
		return nil, typeErr("%s() missing %d required positional argument%s: %s", fn.qual, len(missing),
			plural(len(missing)), quoteList(missing))
	}
	for i := required; i < len(posNames); i++ {
		if _, ok := vars[posNames[i]]; !ok {
			vars[posNames[i]] = fn.defaults[i-required]
		}
	}
	var missingKw []string
	for _, p := range a.L3 {
		if _, ok := vars[p.ID]; ok {
			continue
		}
		if d, ok := fn.kwdefaults[p.ID]; ok {
			vars[p.ID] = d
			continue
		}
		missingKw = append(missingKw, p.ID)
	}
	if len(missingKw) > 0 {
		return nil, typeErr("%s() missing %d required keyword-only argument%s: %s", fn.qual, len(missingKw),
			plural(len(missingKw)), quoteList(missingKw))
	}
	return vars, nil
}

func (fn *funcV) tooManyPositional(given int, vars map[string]Value) error {
	total := len(fn.code.args.L1) + len(fn.code.args.L2)
	kwGiven := 0
	for _, p := range fn.code.args.L3 {
		if _, ok := vars[p.ID]; ok {
			kwGiven++
		}
	}
	return newPyErr("TypeError", tooManyPositionalMsg(fn.qual, total-len(fn.defaults), total, given, kwGiven))
}

func (fn *funcV) call(args []Value, kw *dictV) (Value, error) {
	vars, err := fn.bind(args, kw)
	if err != nil {
		return nil, err
	}
	if fn.code.goBody != nil {
		return fn.code.callGo(vars)
	}
	vm := fn.vm
	code := fn.code
	f := &frame{vm: vm, scope: code.scope, vars: vars, parent: fn.closure, fn: fn, co: code}
	if code.scope.generator || code.scope.async {
		kind := "generator"
		if code.scope.async {
			kind = "coroutine"
			if code.scope.yields {
				kind = "async_generator"
			}
		}
		return startGeneratorKind(kind, fn.qual, f, func() (Value, error) {
			if code.expr != nil {
				return f.eval(code.expr)
			}
			c, err := f.execBlock(code.body)
			if err != nil || c != ctlReturn {
				return none, err
			}
			return f.ret, nil
		}), nil
	}
	if interp.depth >= interp.recursionLimit {
		return nil, newPyErr("RecursionError", "maximum recursion depth exceeded")
	}
	interp.depth++
	defer func() { interp.depth-- }()
	defer f.enter()()
	if code.expr != nil {
		return f.eval(code.expr)
	}
	c, err := f.execBlock(code.body)
	if err != nil {
		return nil, err
	}
	if c == ctlReturn {
		return f.ret, nil
	}
	return none, nil
}

func (f *frame) evalCall(n *Node) (Value, error) {
	fnv, err := f.eval(n.A)
	if err != nil {
		return nil, err
	}
	args := make([]Value, 0, len(n.L1))
	deferredType := ""
	for _, a := range n.L1 {
		if a.Kind == "Starred" {
			v, err := f.eval(a.A)
			if err != nil {
				return nil, err
			}
			if !isIterable(v) {
				if len(n.L1) > 1 {
					return nil, typeErr("Value after * must be an iterable, not %s", typeName(v))
				}
				deferredType = typeName(v)
				continue
			}
			items, err := toSlice(v)
			if err != nil {
				return nil, err
			}
			args = append(args, items...)
			continue
		}
		v, err := f.eval(a)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	var kw *dictV
	if len(n.L2) > 0 {
		kw = newDict()
		for _, k := range n.L2 {
			v, err := f.eval(k.A)
			if err != nil {
				return nil, err
			}
			if k.NoID {
				if ok, err := isMapping(v); err != nil {
					return nil, err
				} else if !ok {
					name, err := funcStr(fnv)
					if err != nil {
						return nil, err
					}
					return nil, typeErr("%s argument after ** must be a mapping, not %s", name, typeName(v))
				}
				tmp := newDict()
				if _, err := methodsDictUpdate(tmp, []Value{v}, nil); err != nil {
					return nil, err
				}
				for i, key := range tmp.keys {
					ks, ok := key.(strV)
					if !ok {
						return nil, typeErr("keywords must be strings")
					}
					if _, dup := kw.getStr(string(ks)); dup {
						name, err := funcStr(fnv)
						if err != nil {
							return nil, err
						}
						return nil, typeErr("%s got multiple values for keyword argument '%s'", name, string(ks))
					}
					if err := kw.set(key, tmp.vals[i]); err != nil {
						return nil, err
					}
				}
				continue
			}
			if _, dup := kw.getStr(k.ID); dup {
				name, err := funcStr(fnv)
				if err != nil {
					return nil, err
				}
				return nil, typeErr("%s got multiple values for keyword argument '%s'", name, k.ID)
			}
			kw.setStr(k.ID, v)
		}
	}
	if deferredType != "" {
		name, err := funcStr(fnv)
		if err != nil {
			return nil, err
		}
		return nil, typeErr("%s argument after * must be an iterable, not %s", name, deferredType)
	}
	return callValue(&callCtx{f: f}, fnv, args, kw)
}

// funcStr is _PyObject_FunctionStr: the callee's qualified name (with its
// module unless that is builtins) followed by (), read from its attributes.
func funcStr(v Value) (string, error) {
	switch x := v.(type) {
	case *funcV:
		if m, ok := x.module.(strV); ok && m != "builtins" {
			return string(m) + "." + x.qual + "()", nil
		}
		return x.qual + "()", nil
	case *goFuncV:
		if m, ok := x.module.(strV); ok && m != "builtins" {
			return string(m) + "." + x.qual + "()", nil
		}
		return x.qual + "()", nil
	case *methodV:
		return funcStr(x.fn)
	case *callable:
		if x.qual != "" {
			return x.qual + "()", nil
		}
		if x.self != nil {
			return typeName(x.self) + "." + x.name + "()", nil
		}
		return x.name + "()", nil
	case *classV:
		if mod := x.moduleName(); mod != "builtins" && mod != "" {
			return mod + "." + x.qualName() + "()", nil
		}
		return x.qualName() + "()", nil
	}
	q, err := getattrOpt(v, "__qualname__")
	if err != nil {
		return "", err
	}
	if q == nil {
		return pyStr(v)
	}
	qs, err := pyStr(q)
	if err != nil {
		return "", err
	}
	m, err := getattrOpt(v, "__module__")
	if err != nil {
		return "", err
	}
	if m != nil && !isNoneV(m) {
		ms, err := pyStr(m)
		if err != nil {
			return "", err
		}
		if ms != "builtins" {
			return ms + "." + qs + "()", nil
		}
	}
	return qs + "()", nil
}

// execFunctionDef runs a def or async def statement: decorators, defaults
// and annotations are evaluated, the function made, decorated and bound.
func (f *frame) execFunctionDef(n *Node) error {
	decos, err := f.evalList(n.L1)
	if err != nil {
		return err
	}
	fn, err := f.makeFunc(n, n.ID)
	if err != nil {
		return err
	}
	v, err := f.applyDecorators(n.L1, decos, fn)
	if err != nil {
		return err
	}
	return f.storeName(mangle(f.scope.private, n.ID), v)
}

// annotations evaluates the annotations of a def's parameters and return,
// in the order CPython builds __annotations__: ordinary parameters,
// positional-only ones, *args, keyword-only ones, **kwargs, return. It is
// nil when there are none.
func (f *frame) annotations(n *Node) (*dictV, error) {
	a := n.A
	var d *dictV
	add := func(name string, e *Node) error {
		if e == nil {
			return nil
		}
		v, err := f.eval(e)
		if err != nil {
			return err
		}
		if d == nil {
			d = newDict()
		}
		d.setStr(name, v)
		return nil
	}
	params := append(append([]*Node(nil), a.L2...), a.L1...)
	if a.A != nil {
		params = append(params, a.A)
	}
	params = append(params, a.L3...)
	if a.B != nil {
		params = append(params, a.B)
	}
	for _, p := range params {
		if err := add(p.ID, p.A); err != nil {
			return nil, err
		}
	}
	if err := add("return", n.B); err != nil {
		return nil, err
	}
	return d, nil
}

// execAnnAssign runs an annotated assignment: the value is assigned, and in
// module and class code the annotation is evaluated and, for a simple name,
// stored in __annotations__.
func (f *frame) execAnnAssign(n *Node) error {
	if n.C != nil {
		v, err := f.eval(n.C)
		if err != nil {
			return err
		}
		if err := f.assign(n.A, v); err != nil {
			return err
		}
	}
	if f.scope.kind == scopeFunction || f.scope.kind == scopeComp {
		return nil
	}
	ann, err := f.eval(n.B)
	if err != nil {
		return err
	}
	if n.Simple == 0 || n.A.Kind != "Name" {
		return nil
	}
	d, ok, err := nsGet(f.ns, "__annotations__")
	if err != nil {
		return err
	}
	if !ok {
		return nameError("__annotations__")
	}
	return pySetitem(d, strV(n.A.ID), ann)
}

// setupAnnotations is SETUP_ANNOTATIONS: module and class code with
// annotated assignments gets an __annotations__ dict.
func setupAnnotations(stmts []*Node, ns Value) error {
	if !hasAnnAssign(stmts) {
		return nil
	}
	if _, ok, err := nsGet(ns, "__annotations__"); err != nil || ok {
		return err
	}
	return pySetitem(ns, strV("__annotations__"), newDict())
}

// hasAnnAssign reports whether a block holds an annotated assignment
// outside nested functions and classes.
func hasAnnAssign(stmts []*Node) bool {
	for _, s := range stmts {
		switch s.Kind {
		case "AnnAssign":
			return true
		case "FunctionDef", "AsyncFunctionDef", "ClassDef":
			continue
		case "Match":
			for _, c := range s.L1 {
				if hasAnnAssign(c.Body) {
					return true
				}
			}
		case "Try", "TryStar":
			for _, h := range s.L1 {
				if hasAnnAssign(h.Body) {
					return true
				}
			}
		}
		if hasAnnAssign(s.Body) || hasAnnAssign(s.Orelse) || hasAnnAssign(s.Finalbody) {
			return true
		}
	}
	return false
}

// foldable reports whether the compiler's constant folding turns an
// expression into a constant: a constant, a sign or inversion of a number,
// or a tuple of such.
func foldable(n *Node) bool {
	switch n.Kind {
	case "Constant":
		return true
	case "UnaryOp":
		if n.Op == "Not" || n.A.Kind != "Constant" {
			return false
		}
		switch n.A.Value.(type) {
		case intV, floatV, complexV, boolV:
			return true
		}
	case "Tuple":
		return n.Ctx != "Store" && allFoldable(n.L1)
	}
	return false
}

func allFoldable(ns []*Node) bool {
	for _, n := range ns {
		if !foldable(n) {
			return false
		}
	}
	return true
}

// evalIterable evaluates the iterable of a for loop or comprehension, or
// the right operand of in: a set display of constants there becomes the
// frozenset constant the compiler folds it into.
func (f *frame) evalIterable(n *Node) (Value, error) {
	if n.Kind == "Set" && len(n.L1) > 0 && allFoldable(n.L1) {
		items, err := f.evalList(n.L1)
		if err != nil {
			return nil, err
		}
		return ConstFrozenset(items)
	}
	return f.eval(n)
}
