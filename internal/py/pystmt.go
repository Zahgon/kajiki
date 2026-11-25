// Ported from CPython 3.12 (Python/ceval.c, Python/bltinmodule.c, Python/import.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The class and with statements, compile(), and the builtins that look at
// the running code: exec, eval, globals, locals and super.

package py

import (
	"strings"
)

// applyDecorators applies decorators (in source order) to v.
// applyDecorators applies the decorators (whose expressions are nodes) to
// v, innermost first; a decorator's failure is attributed to its
// expression, as the call instruction's position is.
func (f *frame) applyDecorators(nodes []*Node, decos []Value, v Value) (Value, error) {
	for i := len(decos) - 1; i >= 0; i-- {
		r, err := callValue(nil, decos[i], []Value{v}, nil)
		if err != nil {
			noteTraceback(f, nodes[i], err)
			return nil, err
		}
		v = r
	}
	return v, nil
}

// execClassDef runs a class statement.
func (f *frame) execClassDef(n *Node) error {
	decos, err := f.evalList(n.L3)
	if err != nil {
		return err
	}
	baseVals, err := f.evalList(n.L1)
	if err != nil {
		return err
	}
	kw := newDict()
	for _, k := range n.L2 {
		v, err := f.eval(k.A)
		if err != nil {
			return err
		}
		if !k.NoID {
			kw.setStr(k.ID, v)
			continue
		}
		if ok, err := isMapping(v); err != nil {
			return err
		} else if !ok {
			return typeErr("__build_class__() argument after ** must be a mapping, not %s", typeName(v))
		}
		if _, err := methodsDictUpdate(kw, []Value{v}, nil); err != nil {
			return err
		}
	}
	for _, key := range kw.keys {
		if _, ok := key.(strV); !ok {
			return typeErr("keywords must be strings")
		}
	}
	// __build_class__: the metaclass is the one given, or the class of the
	// first base, or type; a class as metaclass gives way to the most
	// derived metaclass of the bases.
	meta, explicit := kw.getStr("metaclass")
	if explicit {
		kw.delStr("metaclass")
	} else if len(baseVals) > 0 {
		meta = classOf(baseVals[0])
	} else {
		meta = classes["type"]
	}
	if mk, ok := meta.(*classV); ok {
		w, err := calculateMeta(mk, baseVals)
		if err != nil {
			return err
		}
		meta = w
	}
	ns, err := prepareNamespace(meta, n.ID, baseVals, kw)
	if err != nil {
		return err
	}
	body := &frame{vm: f.vm, scope: n.scope, ns: ns, parent: f, vars: map[string]Value{},
		co: nodeCode(n, f.codeObj().filename)}
	mod, err := body.load("__name__")
	if err != nil {
		return err
	}
	if err := pySetitem(ns, strV("__module__"), mod); err != nil {
		return err
	}
	if err := pySetitem(ns, strV("__qualname__"), strV(n.scope.qualname)); err != nil {
		return err
	}
	if len(n.Body) > 0 && n.Body[0].Kind == "Expr" && n.Body[0].A.Kind == "Constant" {
		if doc, ok := n.Body[0].A.Value.(strV); ok {
			if err := pySetitem(ns, strV("__doc__"), doc); err != nil {
				return err
			}
		}
	}
	leave := body.enter()
	err = setupAnnotations(n.Body, ns)
	if err == nil {
		_, err = body.execBlock(n.Body)
	}
	leave()
	if err != nil {
		return err
	}
	if kw.Len() == 0 {
		kw = nil
	}
	cls, err := callValue(&callCtx{f: f}, meta, []Value{strV(n.ID), newTuple(baseVals), ns}, kw)
	if err != nil {
		return err
	}
	if k, ok := cls.(*classV); ok {
		body.class = k
	}
	v, err := f.applyDecorators(n.L3, decos, cls)
	if err != nil {
		return err
	}
	return f.storeName(mangle(f.scope.private, n.ID), v)
}

// contextMethod finds __enter__ or __exit__ of a context manager.
func contextMethod(v Value, name string) (Value, error) {
	if m, ok := specialOf(v, name); ok {
		return m, nil
	}
	if userClassOf(v) == nil {
		// A builtin type's own special methods, such as those of the
		// standard streams.
		if ga, ok := v.(attrGetter); ok {
			return ga.getAttr(name)
		}
	}
	return nil, nil
}

// execWith runs the items of a with statement from item i on, then the body.
func (f *frame) execWith(n *Node, i int) (ctlFlow, error) {
	if i == len(n.L1) {
		return f.execBlock(n.Body)
	}
	item := n.L1[i]
	mgr, err := f.eval(item.A)
	if err != nil {
		return ctlNone, err
	}
	enter, err := contextMethod(mgr, "__enter__")
	if err != nil {
		return ctlNone, err
	}
	exit, err := contextMethod(mgr, "__exit__")
	if err != nil {
		return ctlNone, err
	}
	if enter == nil || exit == nil {
		if enter == nil {
			return ctlNone, typeErr("'%s' object does not support the context manager protocol", typeName(mgr))
		}
		return ctlNone, typeErr("'%s' object does not support the context manager protocol (missed __exit__ method)", typeName(mgr))
	}
	val, err := callValue(nil, enter, nil, nil)
	if err != nil {
		noteTraceback(f, item.A, err)
		return ctlNone, err
	}
	c, err := func() (ctlFlow, error) {
		if item.B != nil {
			if err := f.assign(item.B, val); err != nil {
				return ctlNone, err
			}
		}
		return f.execWith(n, i+1)
	}()
	if err != nil {
		if err == errGenStopped {
			return ctlNone, err
		}
		x := asExc(err)
		pop := pushHandled(x)
		r, eerr := callValue(nil, exit, []Value{excClass(x), x, none}, nil)
		pop()
		if eerr != nil {
			noteTraceback(f, item.A, eerr)
			chainContext(eerr, x)
			return ctlNone, eerr
		}
		suppress, terr := truthy(r)
		if terr != nil {
			return ctlNone, terr
		}
		if suppress {
			return ctlNone, nil
		}
		return ctlNone, err
	}
	if _, err := callValue(nil, exit, []Value{none, none, none}, nil); err != nil {
		noteTraceback(f, item.A, err)
		return ctlNone, err
	}
	return c, nil
}

// localsOf is locals() in frame f: the namespace of module and class code.
// A function frame has one locals dict (f_locals), which every call updates
// with what the variables hold, a variable without a value removed: the
// parameters and other locals in co_varnames order, then the cell
// variables and the free ones.
func localsOf(f *frame) Value {
	if f.scope.kind == scopeModule || f.scope.kind == scopeClass {
		return f.ns
	}
	if f.localsDict == nil {
		f.localsDict = newDict()
	}
	d := f.localsDict
	for _, name := range localNames(f) {
		var v Value
		ok := false
		if f.scope.binds(name) {
			v, ok = f.vars[name]
		} else if p := f.cellFrame(name); p != nil {
			v, ok = p.vars[name]
		}
		if ok {
			d.setStr(name, v)
		} else if _, present := d.getStr(name); present {
			d.delStr(name)
		}
	}
	return d
}

// compileSource is compile(source, filename, mode): the source parsed and
// checked; mode is "exec", "eval" or "single".
func compileSource(src Value, filename, mode string) (*codeV, error) {
	return compileText(src, filename, mode, false)
}

// compileText is compileSource; stripIndent drops the spaces and tabs that
// start the source, as eval() does before it compiles a string.
func compileText(src Value, filename, mode string, stripIndent bool) (*codeV, error) {
	text, ok := unsub(src).(strV)
	if !ok {
		b, isBytes := bytesOf(src)
		if !isBytes {
			return nil, typeErr("compile() arg 1 must be a string, bytes or AST object")
		}
		text = strV(b)
	}
	if strings.ContainsRune(string(text), 0) {
		return nil, newPyErr("SyntaxError", "source code string cannot contain null bytes")
	}
	if stripIndent {
		text = strV(strings.TrimLeft(string(text), " \t"))
	}
	var tree *Node
	var serr *syntaxErr
	switch mode {
	case "eval":
		tree, serr = parseSource(string(text), filename, "eval")
	case "exec", "single":
		tree, serr = parseSource(string(text), filename, "exec")
	default:
		return nil, valueErr("compile() mode must be 'exec', 'eval' or 'single'")
	}
	if serr != nil {
		return nil, serr.exceptionIn(filename)
	}
	var err error
	if mode == "eval" {
		err = compileCheck(nil, tree.A, filename)
	} else {
		err = compileCheck(tree.Body, nil, filename)
	}
	if err != nil {
		return nil, err
	}
	mangleTree(tree, "")
	return moduleCode(tree, filename), nil
}

// runCode runs the source or code object given to exec or eval in globals
// g and locals ns.
func runCode(name string, src Value, g *dictV, ns Value, isEval bool) (Value, error) {
	code, ok := src.(*codeV)
	if !ok {
		if _, isStr := unsub(src).(strV); !isStr {
			if _, isBytes := bytesOf(src); !isBytes {
				return nil, typeErr("%s() arg 1 must be a string, bytes or code object", name)
			}
		}
		mode := "exec"
		if isEval {
			mode = "eval"
		}
		c, err := compileText(src, "<string>", mode, isEval)
		if err != nil {
			return nil, err
		}
		code = c
	}
	if code.node == nil || code.node.Kind != "Module" && code.node.Kind != "Expression" {
		return nil, typeErr("code object passed to %s() may not contain free variables", name)
	}
	v, err := runModuleCode(code, g, ns)
	if err != nil {
		return nil, err
	}
	if code.expr == nil {
		return none, nil
	}
	return v, nil
}

// builtinCompile is compile(source, filename, mode, flags=0,
// dont_inherit=False, optimize=-1).
func builtinCompile(c *callCtx, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKwRequired("compile", []string{"source", "filename", "mode", "flags", "dont_inherit",
		"optimize", "_feature_version"}, 3, 6, args, kw)
	if err != nil {
		return nil, err
	}
	filename, ok := unsub(buf[1]).(strV)
	if !ok {
		return nil, typeErr("compile() argument 'filename' must be str, not %s", typeName(buf[1]))
	}
	mode, ok := unsub(buf[2]).(strV)
	if !ok {
		return nil, typeErr("compile() argument 'mode' must be str, not %s", typeName(buf[2]))
	}
	return compileSource(buf[0], string(filename), string(mode))
}

// execEvalBuiltin is exec() (isEval false) or eval().
func execEvalBuiltin(name string, isEval bool) func(c *callCtx, args []Value, kw *dictV) (Value, error) {
	return func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		buf, err := methodsArgsKw(name, []string{"source", "globals", "locals"}, 3, args, kw)
		if err != nil {
			return nil, err
		}
		if buf[0] == nil {
			return nil, typeErr("%s expected at least 1 argument, got 0", name)
		}
		var g *dictV
		var ns Value
		if buf[1] != nil && !isNoneV(buf[1]) {
			d, ok := buf[1].(*dictV)
			if !ok {
				return nil, typeErr("%s() globals must be a dict, not %s", name, typeName(buf[1]))
			}
			g, ns = d, d
		} else if c != nil && c.f != nil {
			g, ns = c.f.vm.globals, localsOf(c.f)
		} else {
			g = newDict()
			ns = g
		}
		if buf[2] != nil && !isNoneV(buf[2]) {
			ns = buf[2]
		}
		return runCode(name, buf[0], g, ns, isEval)
	}
}

// builtinSuper is super().
func builtinSuper(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("super", 0, 2, args, kw); err != nil {
		return nil, err
	}
	var start *classV
	var obj Value
	switch len(args) {
	case 0:
		var f *frame
		if c != nil {
			f = c.f
		}
		if f == nil || f.fn == nil || len(f.fn.code.args.L1)+len(f.fn.code.args.L2) == 0 {
			return nil, newPyErr("RuntimeError", "super(): no arguments")
		}
		if f.fn.code.scope.classCell {
			for p := f.fn.closure; p != nil; p = p.parent {
				if p.scope.kind == scopeClass {
					start = p.class
					break
				}
			}
		}
		if start == nil {
			return nil, newPyErr("RuntimeError", "super(): __class__ cell not found")
		}
		a := f.fn.code.args
		var first string
		switch {
		case len(a.L1) > 0:
			first = a.L1[0].ID
		case len(a.L2) > 0:
			first = a.L2[0].ID
		default:
			return nil, newPyErr("RuntimeError", "super(): no arguments")
		}
		v, ok := f.vars[first]
		if !ok {
			return nil, newPyErr("RuntimeError", "super(): arg[0] deleted")
		}
		obj = v
	default:
		k, ok := args[0].(*classV)
		if !ok {
			return nil, typeErr("super() argument 1 must be a type, not %s", typeName(args[0]))
		}
		if len(args) == 1 {
			// An unbound super object, which super.__get__ binds.
			return &superV{start: k}, nil
		}
		start, obj = k, args[1]
	}
	return newSuper(start, obj)
}

// localNames are the variables of a function frame in the order of
// co_varnames and the cells: the parameters (positional, keyword-only,
// *args, **kwargs), the other locals in the order they are first bound,
// the locals nested functions use, then the free variables.
func localNames(f *frame) []string {
	var out []string
	seen := map[string]bool{}
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if f.fn != nil {
		a := f.fn.code.args
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
	}
	cells := cellVars(f.scope)
	for _, n := range f.scope.order {
		if f.scope.binds(n) && !cells[n] {
			add(n)
		}
	}
	for _, n := range f.scope.order {
		if f.scope.binds(n) {
			add(n)
		}
	}
	if f.scope.kind == scopeFunction {
		for _, n := range freeVars(f.scope) {
			add(n)
		}
	}
	return out
}

// cellVars are the locals of a function scope that the functions, classes
// and generator expressions nested in it use.
func cellVars(s *scopeInfo) map[string]bool {
	cells := map[string]bool{}
	var walk func(c *scopeInfo)
	walk = func(c *scopeInfo) {
		if c.kind == scopeComp && !c.genexp {
			for _, g := range c.children {
				walk(g)
			}
			return
		}
		for _, n := range freeVars(c) {
			if s.binds(n) {
				cells[n] = true
			}
		}
	}
	for _, c := range s.children {
		walk(c)
	}
	return cells
}
