// Ported from CPython 3.12 (Python/compile.c, Python/symtable.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The checks CPython's compiler makes on a tree the parser accepted: the
// syntax errors compile() raises after parsing.

package py

// compileChecker walks a tree for the compiler's syntax errors.
type compileChecker struct {
	inFunc, inLoop bool
	// inAsync is set in an async function; asyncGen in one that yields.
	inAsync, asyncGen bool
	comp              string
	// warnings are the SyntaxWarnings the compiler issues, in order.
	warnings []*syntaxErr
	// inExceptStar is set in the handlers of except* clauses (outside
	// nested loops for break and continue).
	inExceptStar, exceptStarLoop bool
	// futureEnd is where the from __future__ imports that start the
	// module end (line and column of the last one), zero when there are
	// none.
	futureLine, futureCol int
}

// compileCheck is the compilation of module statements (or of an
// expression when expr is not nil) to code with the given file name, as
// far as its outcome goes: the SyntaxWarnings it issues are shown, and the
// first syntax error it finds is returned.
func compileCheck(stmts []*Node, expr *Node, filename string) error {
	if err := symtableCheck(stmts, expr); err != nil {
		return err.exceptionIn(filename)
	}
	c := &compileChecker{}
	c.futureLine, c.futureCol = futureLocation(stmts)
	var serr *syntaxErr
	if expr != nil {
		serr = c.expr(expr, false)
	} else {
		serr = c.stmts(stmts)
	}
	if serr != nil {
		return serr.exceptionIn(filename)
	}
	for _, w := range c.warnings {
		warnExplicit("SyntaxWarning", w.msg, filename, w.line)
	}
	return nil
}

// futureLocation is ff_location: the position of the last of the from
// __future__ imports that start a module (after its docstring).
func futureLocation(stmts []*Node) (int, int) {
	line, col := -1, -1
	i := 0
	if docstring(stmts) != nil {
		i++
	}
	for ; i < len(stmts); i++ {
		s := stmts[i]
		if s.Kind != "ImportFrom" || s.NoID || s.ID != "__future__" {
			break
		}
		line, col = s.Lineno, s.ColOffset
	}
	return line, col
}

// forbidden is forbidden_name: __debug__ cannot be assigned to or deleted.
func forbidden(name, ctx string, at *Node) *syntaxErr {
	if name != "__debug__" {
		return nil
	}
	switch ctx {
	case "Store":
		return errAt(at, "cannot assign to __debug__")
	case "Del":
		return errAt(at, "cannot delete __debug__")
	}
	return nil
}

// debugArgs is compiler_check_debug_args: no parameter may be __debug__.
func debugArgs(a *Node) *syntaxErr {
	var params []*Node
	params = append(append(params, a.L1...), a.L2...)
	if a.A != nil {
		params = append(params, a.A)
	}
	params = append(params, a.L3...)
	if a.B != nil {
		params = append(params, a.B)
	}
	for _, p := range params {
		if err := forbidden(p.ID, "Store", p); err != nil {
			return err
		}
	}
	return nil
}

// warn records a SyntaxWarning at node n.
func (c *compileChecker) warn(n *Node, msg string) {
	c.warnings = append(c.warnings, errAt(n, msg))
}

// isLiteralOperand is the negation of check_is_arg: a constant other than
// None, True, False and Ellipsis.
func isLiteralOperand(n *Node) bool {
	if n.Kind != "Constant" {
		return false
	}
	switch n.Value.(type) {
	case noneV, boolV, ellipsisV:
		return false
	}
	return true
}

// inferType is the type name the compiler infers for an expression, ""
// when it does not.
func inferType(n *Node) string {
	switch n.Kind {
	case "Tuple":
		return "tuple"
	case "List", "ListComp":
		return "list"
	case "Dict", "DictComp":
		return "dict"
	case "Set", "SetComp":
		return "set"
	case "GeneratorExp":
		return "generator"
	case "Lambda":
		return "function"
	case "JoinedStr", "FormattedValue":
		return "str"
	case "Constant":
		return typeName(n.Value)
	}
	return ""
}

// checkCompare is compiler_check_compare: "is" with a literal operand.
func (c *compileChecker) checkCompare(n *Node) {
	left := n.A
	for i, op := range n.Ops {
		right := n.L1[i]
		if (op == "Is" || op == "IsNot") && (isLiteralOperand(left) || isLiteralOperand(right)) {
			lit := right
			if isLiteralOperand(left) {
				lit = left
			}
			if op == "Is" {
				c.warn(n, "\"is\" with '"+inferType(lit)+"' literal. Did you mean \"==\"?")
			} else {
				c.warn(n, "\"is not\" with '"+inferType(lit)+"' literal. Did you mean \"!=\"?")
			}
			return
		}
		left = right
	}
}

// checkCaller is check_caller: calling a literal or display.
func (c *compileChecker) checkCaller(n *Node) {
	switch n.A.Kind {
	case "Constant", "Tuple", "List", "ListComp", "Dict", "DictComp", "Set", "SetComp", "GeneratorExp",
		"JoinedStr", "FormattedValue":
		c.warn(n, "'"+inferType(n.A)+"' object is not callable; perhaps you missed a comma?")
	}
}

// checkSubscript is check_subscripter and check_index: subscripting a
// literal that cannot be, or a sequence literal with a non-integer.
func (c *compileChecker) checkSubscript(n *Node) {
	v := n.A
	switch v.Kind {
	case "Constant":
		switch v.Value.(type) {
		case noneV, ellipsisV, intV, boolV, floatV, complexV, *setV:
			c.warn(n, "'"+inferType(v)+"' object is not subscriptable; perhaps you missed a comma?")
			return
		}
	case "Set", "SetComp", "GeneratorExp", "Lambda":
		c.warn(n, "'"+inferType(v)+"' object is not subscriptable; perhaps you missed a comma?")
		return
	}
	switch v.Kind {
	case "Constant":
		switch v.Value.(type) {
		case strV, bytesV, *tupleV:
		default:
			return
		}
	case "Tuple", "List", "ListComp", "JoinedStr", "FormattedValue":
	default:
		return
	}
	idx := n.B
	bad := false
	switch idx.Kind {
	case "Constant":
		switch idx.Value.(type) {
		case intV, boolV:
		default:
			bad = true
		}
	case "Tuple", "List", "ListComp", "JoinedStr", "FormattedValue", "Dict", "DictComp", "Set", "SetComp",
		"GeneratorExp", "Lambda":
		bad = true
	}
	if bad {
		c.warn(n, inferType(v)+" indices must be integers or slices, not "+inferType(idx)+"; perhaps you missed a comma?")
	}
}

func (c *compileChecker) stmts(ns []*Node) *syntaxErr {
	for _, n := range ns {
		if err := c.stmt(n); err != nil {
			return err
		}
	}
	return nil
}

func (c *compileChecker) exprs(ns []*Node, starOK bool) *syntaxErr {
	for _, n := range ns {
		if err := c.expr(n, starOK); err != nil {
			return err
		}
	}
	return nil
}

// errAt is a syntax error reported at node n.
func errAt(n *Node, msg string) *syntaxErr {
	e := &syntaxErr{msg: msg, line: n.Lineno}
	if n.HasPos {
		e.offset, e.endLine, e.endOffset = n.ColOffset+1, n.EndLineno, n.EndColOffset+1
	}
	return e
}

// nested runs fn in a new function or class body.
func (c *compileChecker) nested(inFunc bool, fn func() *syntaxErr) *syntaxErr {
	saved := *c
	c.inFunc, c.inLoop, c.comp, c.inAsync, c.asyncGen = inFunc, false, "", false, false
	c.inExceptStar, c.exceptStarLoop = false, false
	err := fn()
	warnings := c.warnings
	*c = saved
	c.warnings = warnings
	return err
}

func (c *compileChecker) arguments(a *Node) *syntaxErr {
	if err := c.exprs(a.Body, false); err != nil {
		return err
	}
	for _, d := range a.L4 {
		if d != nil {
			if err := c.expr(d, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *compileChecker) stmt(n *Node) *syntaxErr {
	switch n.Kind {
	case "FunctionDef", "AsyncFunctionDef":
		if err := debugArgs(n.A); err != nil {
			return err
		}
		if err := c.exprs(n.L1, false); err != nil {
			return err
		}
		if err := c.arguments(n.A); err != nil {
			return err
		}
		if err := c.nested(true, func() *syntaxErr {
			if n.Kind == "AsyncFunctionDef" {
				c.inAsync, c.asyncGen = true, containsYield(n.Body)
			}
			return c.stmts(n.Body)
		}); err != nil {
			return err
		}
		return forbidden(n.ID, "Store", n)
	case "Match":
		if err := c.expr(n.A, false); err != nil {
			return err
		}
		for i, mc := range n.L1 {
			if err := c.matchCase(mc, i == len(n.L1)-1); err != nil {
				return err
			}
		}
	case "AsyncFor", "AsyncWith":
		if !c.inAsync {
			what := "for"
			if n.Kind == "AsyncWith" {
				what = "with"
			}
			return errAt(n, "'async "+what+"' outside async function")
		}
	case "ClassDef":
		// The body is compiled before the call that builds the class.
		if err := c.exprs(n.L3, false); err != nil {
			return err
		}
		if err := c.nested(false, func() *syntaxErr { return c.stmts(n.Body) }); err != nil {
			return err
		}
		if err := c.keywords(n.L2); err != nil {
			return err
		}
		if err := c.exprs(n.L1, true); err != nil {
			return err
		}
		for _, k := range n.L2 {
			if err := c.expr(k.A, false); err != nil {
				return err
			}
		}
		return forbidden(n.ID, "Store", n)
	case "Import":
		for _, a := range n.L1 {
			name := a.Asname
			if a.NoAsname {
				name = importBinding(a)
			}
			if err := forbidden(name, "Store", n); err != nil {
				return err
			}
		}
	case "ImportFrom":
		if !n.NoID && n.ID == "__future__" && n.Level == 0 &&
			(n.Lineno > c.futureLine || n.Lineno == c.futureLine && n.ColOffset > c.futureCol) {
			return errAt(n, "from __future__ imports must occur at the beginning of the file")
		}
		for _, a := range n.L1 {
			if a.ID == "*" {
				continue
			}
			name := a.Asname
			if a.NoAsname {
				name = a.ID
			}
			if err := forbidden(name, "Store", n); err != nil {
				return err
			}
		}
	case "AnnAssign":
		switch n.A.Kind {
		case "Name":
			if err := forbidden(n.A.ID, "Store", n.A); err != nil {
				return err
			}
		case "Attribute":
			if err := forbidden(n.A.ID, "Store", n.A); err != nil {
				return err
			}
		}
		if n.C != nil {
			if err := c.expr(n.C, false); err != nil {
				return err
			}
		}
		if n.A.Kind != "Name" {
			return c.expr(n.A.A, false)
		}
	case "Return":
		if !c.inFunc {
			return errAt(n, "'return' outside function")
		}
		if c.inExceptStar {
			return errAt(n, "'break', 'continue' and 'return' cannot appear in an except* block")
		}
		if n.A != nil && c.asyncGen {
			return errAt(n, "'return' with value in async generator")
		}
		if n.A != nil {
			return c.expr(n.A, false)
		}
	case "Break", "Continue":
		if !c.inLoop {
			if n.Kind == "Break" {
				return errAt(n, "'break' outside loop")
			}
			return errAt(n, "'continue' not properly in loop")
		}
		if c.inExceptStar && !c.exceptStarLoop {
			return errAt(n, "'break', 'continue' and 'return' cannot appear in an except* block")
		}
	case "Expr":
		return c.expr(n.A, false)
	case "Assign":
		if err := c.exprs(n.L1, false); err != nil {
			return err
		}
		return c.expr(n.A, false)
	case "AugAssign":
		if err := c.expr(n.A, false); err != nil {
			return err
		}
		return c.expr(n.B, false)
	case "Delete":
		return c.exprs(n.L1, false)
	case "For", "While":
		if n.Kind == "For" {
			if err := c.expr(n.A, false); err != nil {
				return err
			}
			if err := c.expr(n.B, false); err != nil {
				return err
			}
		} else if err := c.expr(n.A, false); err != nil {
			return err
		}
		saved, savedLoop := c.inLoop, c.exceptStarLoop
		c.inLoop, c.exceptStarLoop = true, true
		err := c.stmts(n.Body)
		c.inLoop, c.exceptStarLoop = saved, savedLoop
		if err != nil {
			return err
		}
		return c.stmts(n.Orelse)
	case "If":
		if err := c.expr(n.A, false); err != nil {
			return err
		}
		if err := c.stmts(n.Body); err != nil {
			return err
		}
		return c.stmts(n.Orelse)
	case "With":
		for _, it := range n.L1 {
			if err := c.expr(it.A, false); err != nil {
				return err
			}
			if it.B != nil {
				if err := c.expr(it.B, false); err != nil {
					return err
				}
			}
		}
		return c.stmts(n.Body)
	case "Try", "TryStar":
		// The else block is compiled after the body, before the handlers.
		if err := c.stmts(n.Body); err != nil {
			return err
		}
		if err := c.stmts(n.Orelse); err != nil {
			return err
		}
		for _, h := range n.L1 {
			if h.A != nil {
				if err := c.expr(h.A, false); err != nil {
					return err
				}
			}
			if !h.NoID && h.ID != "" {
				if err := forbidden(h.ID, "Store", h); err != nil {
					return err
				}
			}
			saved, savedLoop := c.inExceptStar, c.exceptStarLoop
			if n.Kind == "TryStar" {
				c.inExceptStar, c.exceptStarLoop = true, false
			}
			err := c.stmts(h.Body)
			c.inExceptStar, c.exceptStarLoop = saved, savedLoop
			if err != nil {
				return err
			}
		}
		return c.stmts(n.Finalbody)
	case "Raise", "Assert":
		if n.Kind == "Assert" && n.A != nil && n.A.Kind == "Tuple" && len(n.A.L1) > 0 {
			c.warn(n, "assertion is always true, perhaps remove parentheses?")
		}
		if n.A != nil {
			if err := c.expr(n.A, false); err != nil {
				return err
			}
		}
		if n.B != nil {
			return c.expr(n.B, false)
		}
	}
	return nil
}

func (c *compileChecker) expr(n *Node, starOK bool) *syntaxErr {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case "Starred":
		if !starOK {
			if n.Ctx == "Store" {
				return errAt(n, "starred assignment target must be in a list or tuple")
			}
			return errAt(n, "can't use starred expression here")
		}
		return c.expr(n.A, false)
	case "List", "Tuple", "Set":
		if n.Ctx == "Store" {
			stars := 0
			for _, e := range n.L1 {
				if e.Kind == "Starred" {
					stars++
				}
			}
			if stars > 1 {
				return errAt(n, "multiple starred expressions in assignment")
			}
		}
		return c.exprs(n.L1, true)
	case "Call":
		c.checkCaller(n)
		if err := c.expr(n.A, false); err != nil {
			return err
		}
		if err := c.keywords(n.L2); err != nil {
			return err
		}
		if err := c.exprs(n.L1, true); err != nil {
			return err
		}
		for _, k := range n.L2 {
			if err := c.expr(k.A, false); err != nil {
				return err
			}
		}
		return nil
	case "Await":
		if !c.inFunc {
			return errAt(n, "'await' outside function")
		}
		if !c.inAsync {
			return errAt(n, "'await' outside async function")
		}
		return c.expr(n.A, false)
	case "Yield", "YieldFrom":
		if c.comp != "" {
			return errAt(n, "'yield' inside "+c.comp)
		}
		if !c.inFunc {
			return errAt(n, "'yield' outside function")
		}
		if n.Kind == "YieldFrom" && c.inAsync {
			return errAt(n, "'yield from' inside async function")
		}
		return c.expr(n.A, false)
	case "Lambda":
		if err := debugArgs(n.A); err != nil {
			return err
		}
		if err := c.arguments(n.A); err != nil {
			return err
		}
		return c.nested(true, func() *syntaxErr { return c.expr(n.B, false) })
	case "ListComp", "SetComp", "DictComp", "GeneratorExp":
		if n.Kind != "GeneratorExp" && !c.inAsync {
			for _, g := range n.L1 {
				if g.IsAsync != 0 {
					return errAt(n, "asynchronous comprehension outside of an asynchronous function")
				}
			}
		}
		name := map[string]string{"ListComp": "list comprehension", "SetComp": "set comprehension",
			"DictComp": "dict comprehension", "GeneratorExp": "generator expression"}[n.Kind]
		for i, g := range n.L1 {
			if i == 0 {
				if err := c.expr(g.B, false); err != nil {
					return err
				}
			}
		}
		saved := c.comp
		c.comp = name
		defer func() { c.comp = saved }()
		for i, g := range n.L1 {
			if err := c.expr(g.A, false); err != nil {
				return err
			}
			if i > 0 {
				if err := c.expr(g.B, false); err != nil {
					return err
				}
			}
			if err := c.exprs(g.L1, false); err != nil {
				return err
			}
		}
		if err := c.expr(n.A, false); err != nil {
			return err
		}
		return c.expr(n.B, false)
	case "Dict":
		for _, k := range n.L1 {
			if err := c.expr(k, false); err != nil {
				return err
			}
		}
		return c.exprs(n.L2, false)
	case "Subscript":
		if n.Ctx == "Load" || n.Ctx == "" {
			c.checkSubscript(n)
		}
		if err := c.expr(n.A, false); err != nil {
			return err
		}
		return c.expr(n.B, false)
	case "Name":
		return forbidden(n.ID, n.Ctx, n)
	case "Attribute":
		if err := forbidden(n.ID, n.Ctx, n); err != nil {
			return err
		}
	case "Constant":
		return nil
	case "Compare":
		c.checkCompare(n)
	}
	for _, ch := range n.children() {
		if err := c.expr(ch, false); err != nil {
			return err
		}
	}
	return nil
}

// keywords is validate_keywords: no keyword argument may be __debug__ or
// repeated.
func (c *compileChecker) keywords(kws []*Node) *syntaxErr {
	for i, k := range kws {
		if k.NoID {
			continue
		}
		if err := forbidden(k.ID, "Store", k); err != nil {
			return err
		}
		for _, o := range kws[i+1:] {
			if !o.NoID && o.ID == k.ID {
				return errAt(o, "keyword argument repeated: "+k.ID)
			}
		}
	}
	return nil
}

// containsYield reports whether a function body yields (outside nested
// functions and classes).
func containsYield(stmts []*Node) bool {
	var walk func(n *Node) bool
	walk = func(n *Node) bool {
		switch n.Kind {
		case "Yield", "YieldFrom":
			return true
		case "FunctionDef", "AsyncFunctionDef", "ClassDef", "Lambda":
			return false
		}
		for _, c := range n.children() {
			if walk(c) {
				return true
			}
		}
		return false
	}
	for _, s := range stmts {
		if walk(s) {
			return true
		}
	}
	return false
}

// matchCase checks one case of a match statement: an irrefutable pattern
// before the last case makes the rest unreachable, a pattern binds each
// name once, and the alternatives of an or-pattern bind the same names.
func (c *compileChecker) matchCase(mc *Node, last bool) *syntaxErr {
	if !last && mc.B == nil {
		if p := irrefutable(mc.A); p != nil {
			if p.NoID || p.ID == "" {
				return errAt(p, "wildcard makes remaining patterns unreachable")
			}
			return errAt(p, "name capture '"+p.ID+"' makes remaining patterns unreachable")
		}
	}
	if _, err := c.patternNames(mc.A); err != nil {
		return err
	}
	if mc.B != nil {
		if err := c.expr(mc.B, false); err != nil {
			return err
		}
	}
	return c.stmts(mc.Body)
}

// irrefutable is the capture or wildcard pattern that makes p match
// anything, nil when p can fail.
func irrefutable(p *Node) *Node {
	switch p.Kind {
	case "MatchAs":
		if p.A == nil {
			return p
		}
		return irrefutable(p.A)
	case "MatchOr":
		for _, alt := range p.L1 {
			if r := irrefutable(alt); r != nil {
				return r
			}
		}
	}
	return nil
}

// patternNames returns the names a pattern binds, in order, checking it as
// the compiler does.
func (c *compileChecker) patternNames(p *Node) ([]string, *syntaxErr) {
	var names []string
	add := func(name string, at *Node) *syntaxErr {
		if err := forbidden(name, "Store", at); err != nil {
			return err
		}
		for _, n := range names {
			if n == name {
				return errAt(at, "multiple assignments to name '"+name+"' in pattern")
			}
		}
		names = append(names, name)
		return nil
	}
	sub := func(q *Node) *syntaxErr {
		ns, err := c.patternNames(q)
		if err != nil {
			return err
		}
		for _, n := range ns {
			if err := add(n, q); err != nil {
				return errAt(findCapture(q, n), "multiple assignments to name '"+n+"' in pattern")
			}
		}
		return nil
	}
	switch p.Kind {
	case "MatchAs":
		if p.A != nil {
			if err := sub(p.A); err != nil {
				return nil, err
			}
		}
		if !p.NoID && p.ID != "" {
			if err := add(p.ID, p); err != nil {
				return nil, err
			}
		}
	case "MatchStar":
		if !p.NoID && p.ID != "" {
			if err := add(p.ID, p); err != nil {
				return nil, err
			}
		}
	case "MatchSequence":
		for _, q := range p.L1 {
			if err := sub(q); err != nil {
				return nil, err
			}
		}
	case "MatchMapping":
		for i, k := range p.L1 {
			for _, o := range p.L1[:i] {
				if k.Kind == "Constant" && o.Kind == "Constant" {
					eq, err := pyEq(k.Value, o.Value)
					if err != nil {
						return nil, &syntaxErr{leaf: err}
					}
					if eq {
						r, err := pyRepr(k.Value)
						if err != nil {
							return nil, &syntaxErr{leaf: err}
						}
						return nil, errAt(p, "mapping pattern checks duplicate key ("+r+")")
					}
				}
			}
		}
		for _, q := range p.L2 {
			if err := sub(q); err != nil {
				return nil, err
			}
		}
		if !p.NoID && p.ID != "" {
			if err := add(p.ID, p); err != nil {
				return nil, err
			}
		}
	case "MatchClass":
		// validate_kwd_attrs: the errors point at the patterns.
		for i, attr := range p.Names {
			if err := forbidden(attr, "Store", p.L2[i]); err != nil {
				return nil, err
			}
			for j := i + 1; j < len(p.Names); j++ {
				if p.Names[j] == attr {
					return nil, errAt(p.L2[j], "attribute name repeated in class pattern: "+attr)
				}
			}
		}
		for _, q := range append(append([]*Node(nil), p.L1...), p.L2...) {
			if err := sub(q); err != nil {
				return nil, err
			}
		}
	case "MatchOr":
		var first []string
		for i, alt := range p.L1 {
			if i < len(p.L1)-1 {
				if r := irrefutable(alt); r != nil {
					if r.NoID || r.ID == "" {
						return nil, errAt(r, "wildcard makes remaining patterns unreachable")
					}
					return nil, errAt(r, "name capture '"+r.ID+"' makes remaining patterns unreachable")
				}
			}
			ns, err := c.patternNames(alt)
			if err != nil {
				return nil, err
			}
			if i == 0 {
				first = ns
				continue
			}
			if !sameNameSet(first, ns) {
				return nil, errAt(p, "alternative patterns bind different names")
			}
		}
		for _, n := range first {
			if err := add(n, p); err != nil {
				return nil, err
			}
		}
	}
	return names, nil
}

func sameNameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, n := range a {
		set[n] = true
	}
	for _, n := range b {
		if !set[n] {
			return false
		}
	}
	return true
}

// findCapture is the pattern inside p that captures name (p itself when
// none is found).
func findCapture(p *Node, name string) *Node {
	if r := captureOf(p, name); r != nil {
		return r
	}
	return p
}

func captureOf(p *Node, name string) *Node {
	if (p.Kind == "MatchAs" || p.Kind == "MatchStar") && !p.NoID && p.ID == name {
		return p
	}
	for _, c := range p.children() {
		if r := captureOf(c, name); r != nil {
			return r
		}
	}
	return nil
}
