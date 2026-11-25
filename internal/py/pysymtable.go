// Ported from CPython 3.12 (Python/symtable.c, Python/future.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The syntax errors compile() raises before the code generator runs: the future statements, then
// the two passes of the symbol table (the walk that records every name a block binds or uses, and
// the analysis that resolves global, nonlocal and free names), in CPython's order.

package py

// Symbol flags, as symtable.h names them.
const (
	stGlobal   = 1 << iota // DEF_GLOBAL
	stLocal                // DEF_LOCAL
	stParam                // DEF_PARAM
	stNonlocal             // DEF_NONLOCAL
	stUse                  // USE
	stImport               // DEF_IMPORT
	stAnnot                // DEF_ANNOT
	stCompIter             // DEF_COMP_ITER

	stBound = stLocal | stParam | stImport
)

// The kinds of comprehension a block is (ste_comprehension).
const (
	compNone = iota
	compList
	compSet
	compDict
	compGenexp
)

// stEntry is a symbol table entry: one block and the flags of its names.
type stEntry struct {
	kind scopeKind
	// names are the block's names in the order they were first met, as
	// the symbols dict keeps them; flags holds their flags.
	names []string
	flags map[string]int
	// directives are the global and nonlocal statements (and assignment
	// expressions) naming each name, the first one per name.
	directives map[string]*Node
	children   []*stEntry
	// comprehension is set for the block of a comprehension; generator
	// for one that yields.
	comprehension int
	generator     bool
	// compIterTarget is set while the targets of a comprehension are
	// visited, compIterExpr while (counting nesting) its iterables are.
	compIterTarget bool
	compIterExpr   int
}

func newSTEntry(kind scopeKind) *stEntry {
	return &stEntry{kind: kind, flags: map[string]int{}, directives: map[string]*Node{}}
}

// symtable is the state of the walk; err is the first error.
type symtable struct {
	top, cur *stEntry
	stack    []*stEntry
	// private is the name of the class whose body (or nested function)
	// is being visited, for name mangling.
	private string
	err     *syntaxErr
}

// symtableCheck returns the first syntax error the future statements and
// the symbol table find in module statements (or in an expression when expr
// is not nil).
func symtableCheck(stmts []*Node, expr *Node) *syntaxErr {
	if err := futureCheck(stmts); err != nil {
		return err
	}
	st := &symtable{top: newSTEntry(scopeModule)}
	st.cur = st.top
	st.stack = []*stEntry{st.top}
	if expr != nil {
		st.expr(expr)
	} else {
		st.stmts(stmts)
	}
	if st.err != nil {
		return st.err
	}
	return st.analyze(st.top, nil)
}

// futureCheck is future_parse: the features of the from __future__ imports
// that start a module (after its docstring), "braces" refused.
func futureCheck(stmts []*Node) *syntaxErr {
	i := 0
	if docstring(stmts) != nil {
		i++
	}
	for ; i < len(stmts); i++ {
		s := stmts[i]
		if s.Kind != "ImportFrom" || s.NoID || s.ID != "__future__" {
			return nil
		}
		for _, a := range s.L1 {
			switch a.ID {
			case "nested_scopes", "generators", "division", "absolute_import", "with_statement",
				"print_function", "unicode_literals", "barry_as_FLUFL", "generator_stop", "annotations":
			case "braces":
				return errAt(a, "not a chance")
			default:
				return errAt(a, "future feature "+a.ID+" is not defined")
			}
		}
	}
	return nil
}

func (st *symtable) fail(n *Node, msg string) {
	if st.err == nil {
		st.err = errAt(n, msg)
	}
}

// enter starts a nested block, exit returns to the enclosing one.
func (st *symtable) enter(kind scopeKind) {
	e := newSTEntry(kind)
	st.cur.children = append(st.cur.children, e)
	st.stack = append(st.stack, e)
	st.cur = e
}

func (st *symtable) exit() {
	st.stack = st.stack[:len(st.stack)-1]
	st.cur = st.stack[len(st.stack)-1]
}

// addDef is symtable_add_def: it records a flag for the (mangled) name in
// the current block, with the errors of a parameter given twice and of a
// comprehension variable rebinding an assignment expression's target.
func (st *symtable) addDef(name string, flag int, at *Node) {
	st.addDefIn(st.cur, name, flag, at)
}

func (st *symtable) addDefIn(e *stEntry, name string, flag int, at *Node) {
	if st.err != nil {
		return
	}
	name = mangle(st.private, name)
	val, seen := e.flags[name]
	if seen && flag&stParam != 0 && val&stParam != 0 {
		st.fail(at, "duplicate argument '"+name+"' in function definition")
		return
	}
	val |= flag
	if e.compIterTarget {
		if val&(stGlobal|stNonlocal) != 0 {
			st.fail(at, "comprehension inner loop cannot rebind assignment expression target '"+name+"'")
			return
		}
		val |= stCompIter
	}
	if !seen {
		e.names = append(e.names, name)
	}
	e.flags[name] = val
	if flag&stGlobal != 0 && e != st.top {
		// A global declared anywhere is known to the module's table.
		g, ok := st.top.flags[name]
		if !ok {
			st.top.names = append(st.top.names, name)
		}
		st.top.flags[name] = g | flag
	}
}

func (st *symtable) lookup(name string) int { return st.cur.flags[mangle(st.private, name)] }

// directive records the global or nonlocal statement naming name.
func (st *symtable) directive(name string, at *Node) {
	name = mangle(st.private, name)
	if _, ok := st.cur.directives[name]; !ok {
		st.cur.directives[name] = at
	}
}

func (st *symtable) stmts(ns []*Node) {
	for _, n := range ns {
		st.stmt(n)
	}
}

func (st *symtable) exprs(ns []*Node) {
	for _, n := range ns {
		if n != nil {
			st.expr(n)
		}
	}
}

func (st *symtable) stmt(s *Node) {
	if st.err != nil {
		return
	}
	switch s.Kind {
	case "FunctionDef", "AsyncFunctionDef":
		st.addDef(s.ID, stLocal, s)
		st.exprs(s.A.Body)
		st.exprs(s.A.L4)
		st.exprs(s.L1)
		st.annotations(s.A, s.B)
		st.enter(scopeFunction)
		st.arguments(s.A)
		st.stmts(s.Body)
		st.exit()
	case "ClassDef":
		st.addDef(s.ID, stLocal, s)
		st.exprs(s.L3)
		saved := st.private
		st.exprs(s.L1)
		for _, k := range s.L2 {
			st.expr(k.A)
		}
		st.enter(scopeClass)
		st.private = s.ID
		st.stmts(s.Body)
		st.exit()
		st.private = saved
	case "Return":
		if s.A != nil {
			st.expr(s.A)
		}
	case "Delete":
		st.exprs(s.L1)
	case "Assign":
		st.exprs(s.L1)
		st.expr(s.A)
	case "AnnAssign":
		if s.A.Kind == "Name" {
			cur := st.lookup(s.A.ID)
			if cur&(stGlobal|stNonlocal) != 0 && st.cur != st.top && s.Simple != 0 {
				msg := "annotated name '" + s.A.ID + "' can't be nonlocal"
				if cur&stGlobal != 0 {
					msg = "annotated name '" + s.A.ID + "' can't be global"
				}
				st.fail(s, msg)
				return
			}
			if s.Simple != 0 {
				st.addDef(s.A.ID, stAnnot|stLocal, s.A)
			} else if s.C != nil {
				st.addDef(s.A.ID, stLocal, s.A)
			}
		} else {
			st.expr(s.A)
		}
		st.expr(s.B)
		if s.C != nil {
			st.expr(s.C)
		}
	case "AugAssign":
		st.expr(s.A)
		st.expr(s.B)
	case "For", "AsyncFor":
		st.expr(s.A)
		st.expr(s.B)
		st.stmts(s.Body)
		st.stmts(s.Orelse)
	case "While", "If":
		st.expr(s.A)
		st.stmts(s.Body)
		st.stmts(s.Orelse)
	case "Match":
		st.expr(s.A)
		for _, c := range s.L1 {
			st.pattern(c.A)
			if c.B != nil {
				st.expr(c.B)
			}
			st.stmts(c.Body)
		}
	case "Raise":
		if s.A != nil {
			st.expr(s.A)
			if s.B != nil {
				st.expr(s.B)
			}
		}
	case "Try", "TryStar":
		st.stmts(s.Body)
		st.stmts(s.Orelse)
		for _, h := range s.L1 {
			if h.A != nil {
				st.expr(h.A)
			}
			if !h.NoID && h.ID != "" {
				st.addDef(h.ID, stLocal, h)
			}
			st.stmts(h.Body)
		}
		st.stmts(s.Finalbody)
	case "Assert":
		st.expr(s.A)
		if s.B != nil {
			st.expr(s.B)
		}
	case "Import", "ImportFrom":
		for _, a := range s.L1 {
			st.alias(a)
		}
	case "Global", "Nonlocal":
		st.declaration(s)
	case "Expr":
		st.expr(s.A)
	case "With", "AsyncWith":
		for _, it := range s.L1 {
			st.expr(it.A)
			if it.B != nil {
				st.expr(it.B)
			}
		}
		st.stmts(s.Body)
	}
}

// declaration visits a global or nonlocal statement: a name the block
// already uses, binds, annotates or takes as a parameter cannot be
// declared.
func (st *symtable) declaration(s *Node) {
	kind, flag := "global", stGlobal
	if s.Kind == "Nonlocal" {
		kind, flag = "nonlocal", stNonlocal
	}
	for _, name := range s.Names {
		cur := st.lookup(name)
		if cur&(stParam|stLocal|stUse|stAnnot) != 0 {
			var msg string
			switch {
			case cur&stParam != 0:
				msg = "name '" + name + "' is parameter and " + kind
			case cur&stUse != 0:
				msg = "name '" + name + "' is used prior to " + kind + " declaration"
			case cur&stAnnot != 0:
				msg = "annotated name '" + name + "' can't be " + kind
			default:
				msg = "name '" + name + "' is assigned to before " + kind + " declaration"
			}
			st.fail(s, msg)
			return
		}
		st.addDef(name, flag, s)
		st.directive(name, s)
	}
}

// alias records the name an import binds; import * is only allowed in a
// module.
func (st *symtable) alias(a *Node) {
	if a.ID == "*" {
		if st.cur.kind != scopeModule {
			st.fail(a, "import * only allowed at module level")
		}
		return
	}
	st.addDef(importBinding(a), stImport, a)
}

// annotations visits the annotations of a function's parameters and its
// return annotation, in the enclosing block.
func (st *symtable) annotations(a, returns *Node) {
	for _, l := range [][]*Node{a.L1, a.L2} {
		for _, p := range l {
			if p.A != nil {
				st.expr(p.A)
			}
		}
	}
	if a.A != nil && a.A.A != nil {
		st.expr(a.A.A)
	}
	if a.B != nil && a.B.A != nil {
		st.expr(a.B.A)
	}
	for _, p := range a.L3 {
		if p.A != nil {
			st.expr(p.A)
		}
	}
	if returns != nil {
		st.expr(returns)
	}
}

// arguments records the parameters of a function block.
func (st *symtable) arguments(a *Node) {
	for _, l := range [][]*Node{a.L1, a.L2, a.L3} {
		for _, p := range l {
			st.addDef(p.ID, stParam, p)
		}
	}
	if a.A != nil {
		st.addDef(a.A.ID, stParam, a.A)
	}
	if a.B != nil {
		st.addDef(a.B.ID, stParam, a.B)
	}
}

func (st *symtable) pattern(p *Node) {
	if st.err != nil {
		return
	}
	switch p.Kind {
	case "MatchValue":
		st.expr(p.A)
	case "MatchSequence", "MatchOr":
		for _, c := range p.L1 {
			st.pattern(c)
		}
	case "MatchStar":
		if !p.NoID {
			st.addDef(p.ID, stLocal, p)
		}
	case "MatchMapping":
		st.exprs(p.L1)
		for _, c := range p.L2 {
			st.pattern(c)
		}
		if !p.NoID {
			st.addDef(p.ID, stLocal, p)
		}
	case "MatchClass":
		st.expr(p.A)
		for _, c := range p.L1 {
			st.pattern(c)
		}
		for _, c := range p.L2 {
			st.pattern(c)
		}
	case "MatchAs":
		if p.A != nil {
			st.pattern(p.A)
		}
		if !p.NoID {
			st.addDef(p.ID, stLocal, p)
		}
	}
}

func (st *symtable) expr(e *Node) {
	if st.err != nil {
		return
	}
	switch e.Kind {
	case "NamedExpr":
		st.namedExpr(e)
	case "Lambda":
		st.exprs(e.A.Body)
		st.exprs(e.A.L4)
		st.enter(scopeFunction)
		st.arguments(e.A)
		st.expr(e.B)
		st.exit()
	case "GeneratorExp":
		st.comprehension(e, compGenexp, e.A, nil)
	case "ListComp":
		st.comprehension(e, compList, e.A, nil)
	case "SetComp":
		st.comprehension(e, compSet, e.A, nil)
	case "DictComp":
		st.comprehension(e, compDict, e.A, e.B)
	case "Yield", "YieldFrom":
		if e.A != nil {
			st.expr(e.A)
		}
		st.cur.generator = true
		if c := st.cur.comprehension; c != compNone {
			what := map[int]string{compList: "list comprehension", compSet: "set comprehension",
				compDict: "dict comprehension", compGenexp: "generator expression"}[c]
			st.fail(e, "'yield' inside "+what)
		}
	case "Name":
		flag := stLocal
		if e.Ctx == "Load" {
			flag = stUse
		}
		st.addDef(e.ID, flag, e)
		if e.Ctx == "Load" && st.cur.kind == scopeFunction && e.ID == "super" {
			st.addDef("__class__", stUse, e)
		}
	case "Dict":
		st.exprs(e.L1)
		st.exprs(e.L2)
	case "Call":
		st.expr(e.A)
		st.exprs(e.L1)
		for _, k := range e.L2 {
			st.expr(k.A)
		}
	default:
		for _, f := range Fields(e) {
			if f.Node != nil {
				st.expr(f.Node)
			}
			st.exprs(f.Nodes)
		}
	}
}

// comprehension is symtable_handle_comprehension: the first iterable runs
// in the enclosing block, the rest in the comprehension's own.
func (st *symtable) comprehension(e *Node, kind int, elt, value *Node) {
	outer := e.L1[0]
	st.cur.compIterExpr++
	st.expr(outer.B)
	st.cur.compIterExpr--
	st.enter(scopeComp)
	st.cur.comprehension = kind
	st.addDef(".0", stParam, e)
	st.cur.compIterTarget = true
	st.expr(outer.A)
	st.cur.compIterTarget = false
	st.exprs(outer.L1)
	for _, g := range e.L1[1:] {
		st.cur.compIterTarget = true
		st.expr(g.A)
		st.cur.compIterTarget = false
		st.cur.compIterExpr++
		st.expr(g.B)
		st.cur.compIterExpr--
		st.exprs(g.L1)
	}
	if value != nil {
		st.expr(value)
	}
	st.expr(elt)
	st.exit()
}

// namedExpr is symtable_handle_namedexpr: the target of := in a
// comprehension binds in the enclosing function or module.
func (st *symtable) namedExpr(e *Node) {
	if st.cur.compIterExpr > 0 {
		st.fail(e, "assignment expression cannot be used in a comprehension iterable expression")
		return
	}
	if st.cur.comprehension != compNone && !st.extendNamedExprScope(e.A) {
		return
	}
	st.expr(e.B)
	st.expr(e.A)
}

// extendNamedExprScope is symtable_extend_namedexpr_scope.
func (st *symtable) extendNamedExprScope(target *Node) bool {
	name := target.ID
	for i := len(st.stack) - 1; i >= 0; i-- {
		e := st.stack[i]
		if e.comprehension != compNone {
			if fl := e.flags[mangle(st.private, name)]; fl&stCompIter != 0 && fl&stLocal != 0 {
				st.fail(target, "assignment expression cannot rebind comprehension iteration variable '"+name+"'")
				return false
			}
			continue
		}
		switch e.kind {
		case scopeFunction:
			if e.flags[mangle(st.private, name)]&stGlobal != 0 {
				st.addDef(name, stGlobal, target)
			} else {
				st.addDef(name, stNonlocal, target)
			}
			st.directive(name, target)
			st.addDefIn(e, name, stLocal, target)
			return st.err == nil
		case scopeModule:
			st.addDef(name, stGlobal, target)
			st.directive(name, target)
			st.addDefIn(e, name, stGlobal, target)
			return st.err == nil
		case scopeClass:
			st.fail(target, "assignment expression within a comprehension cannot be used in a class body")
			return false
		}
	}
	return true
}

// analyze is analyze_block for the errors it raises: bound holds the
// names of the enclosing function blocks (nil for the module).
func (st *symtable) analyze(e *stEntry, bound map[string]bool) *syntaxErr {
	local := map[string]bool{}
	for _, name := range e.names {
		fl := e.flags[name]
		switch {
		case fl&stGlobal != 0:
			if fl&stNonlocal != 0 {
				return errAt(e.directives[name], "name '"+name+"' is nonlocal and global")
			}
		case fl&stNonlocal != 0:
			if bound == nil {
				return errAt(e.directives[name], "nonlocal declaration not allowed at module level")
			}
			if !bound[name] {
				return errAt(e.directives[name], "no binding for nonlocal '"+name+"' found")
			}
		case fl&stBound != 0:
			local[name] = true
		}
	}
	newBound := map[string]bool{}
	for k := range bound {
		newBound[k] = true
	}
	switch e.kind {
	case scopeClass:
		newBound["__class__"] = true
		newBound["__classdict__"] = true
	case scopeFunction, scopeComp:
		for k := range local {
			newBound[k] = true
		}
	}
	for _, c := range e.children {
		if err := st.analyze(c, newBound); err != nil {
			return err
		}
	}
	return nil
}

// mangle is _Py_Mangle: in the body of class private (and the functions
// nested in it), a name __spam that does not end with two underscores (nor
// contain a dot) becomes _private__spam, the class's leading underscores
// dropped.
func mangle(private, name string) string {
	if private == "" || len(name) < 2 || name[0] != '_' || name[1] != '_' {
		return name
	}
	n := len(name)
	if name[n-1] == '_' && name[n-2] == '_' {
		return name
	}
	for i := 0; i < n; i++ {
		if name[i] == '.' {
			return name
		}
	}
	i := 0
	for i < len(private) && private[i] == '_' {
		i++
	}
	if i == len(private) {
		return name
	}
	return "_" + private[i:] + name
}
