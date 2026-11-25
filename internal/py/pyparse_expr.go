// Ported from CPython 3.12 (Grammar/python.gram) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The expression, literal, comprehension, call argument and target rules of
// the grammar. The left-recursive rules (bitwise_or down to term, primary,
// t_primary, attr) grow their result in a loop, which tries the same
// alternatives in the same order as CPython's seed-growing memo does.

package py

// expressions:
//
//	| expression (',' expression)+ [',']
//	| expression ','
//	| expression
func (p *parser) expressions() *Node { return p.tupleOf(p.expression) }

// star_expressions: as expressions, of star_expression.
func (p *parser) starExpressions() *Node { return p.tupleOf(p.starExpression) }

// tupleOf is the rule
//
//	| e (',' e)+ [',']
//	| e ','
//	| e
//
// a Tuple when there is a comma.
func (p *parser) tupleOf(e func() *Node) *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	var b []*Node
	if got(&a, e()) && gotSeq(&b, loop1(p, func() *Node {
		return group(p, func() *Node {
			if p.tok(tCOMMA) {
				return e()
			}
			return nil
		})
	})) && p.optTok(tCOMMA) {
		n := p.span("Tuple", m)
		n.L1, n.Ctx = append([]*Node{a}, b...), "Load"
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if got(&a, e()) && p.tok(tCOMMA) {
		n := p.span("Tuple", m)
		n.L1, n.Ctx = []*Node{a}, "Load"
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if a := e(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// expression (memo):
//
//	| invalid_expression
//	| invalid_legacy_expression
//	| disjunction 'if' disjunction 'else' expression
//	| disjunction
//	| lambdef
func (p *parser) expression() *Node {
	return memo(p, rExpression, func() *Node {
		m := p.mark
		if p.invalid {
			p.invalidExpression()
			if !p.retry(m) {
				return nil
			}
			p.invalidLegacyExpression()
			if !p.retry(m) {
				return nil
			}
		}
		return p.expressionAlts(m)
	})
}

// expressionAlts are the alternatives of expression that are not invalid_
// rules, from the mark m.
func (p *parser) expressionAlts(m int) *Node {
	var a, b, c *Node
	if got(&a, p.disjunction()) && p.tok(kwIf) && got(&b, p.disjunction()) && p.tok(kwElse) && got(&c, p.expression()) {
		n := p.span("IfExp", m)
		n.A, n.B, n.C = b, a, c
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.disjunction(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.lambdef(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// expression_without_invalid: expression without the invalid_ rules,
// down to every rule it calls.
func (p *parser) expressionWithoutInvalid() *Node {
	prev := p.invalid
	p.invalid = false
	defer func() { p.invalid = prev }()
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return p.expressionAlts(p.mark)
}

// yield_expr:
//
//	| 'yield' 'from' expression
//	| 'yield' [star_expressions]
func (p *parser) yieldExpr() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	if p.tok(kwYield) && p.tok(kwFrom) && got(&a, p.expression()) {
		n := p.span("YieldFrom", m)
		n.A = a
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(kwYield) && opt(p, &a, p.starExpressions()) {
		n := p.span("Yield", m)
		n.A = a
		return n
	}
	p.mark = m
	return nil
}

// star_expression (memo): '*' bitwise_or | expression
func (p *parser) starExpression() *Node {
	return memo(p, rStarExpression, func() *Node { return p.starredOr(p.expression) })
}

// star_named_expression: '*' bitwise_or | named_expression
func (p *parser) starNamedExpression() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return p.starredOr(p.namedExpression)
}

// starredOr is the body of the rule '*' bitwise_or | e.
func (p *parser) starredOr(e func() *Node) *Node {
	m := p.mark
	var a *Node
	if p.tok(tSTAR) && got(&a, p.bitwiseOr()) {
		n := p.span("Starred", m)
		n.A, n.Ctx = a, "Load"
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if a := e(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// star_named_expressions: ','.star_named_expression+ [',']
func (p *parser) starNamedExpressions() []*Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*Node
	if gotSeq(&a, gather(p, tCOMMA, p.starNamedExpression)) && p.optTok(tCOMMA) {
		return a
	}
	p.mark = m
	return nil
}

// assignment_expression: NAME ':=' ~ expression
func (p *parser) assignmentExpression() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *Node
	if got(&a, p.name()) && p.tok(tCOLONEQUAL) {
		if got(&b, p.expression()) {
			n := p.span("NamedExpr", m)
			n.A, n.B = setCtx(a, "Store"), b
			return n
		}
	}
	p.mark = m
	return nil
}

// named_expression:
//
//	| assignment_expression
//	| invalid_named_expression
//	| expression !':='
func (p *parser) namedExpression() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.assignmentExpression(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidNamedExpression()
		if !p.retry(m) {
			return nil
		}
	}
	var a *Node
	if got(&a, p.expression()) && !p.at(tCOLONEQUAL) {
		return a
	}
	p.mark = m
	return nil
}

// assignmentOrExpression: (assignment_expression | expression !':=')
func (p *parser) assignmentOrExpression() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.assignmentExpression(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	var a *Node
	if got(&a, p.expression()) && !p.at(tCOLONEQUAL) {
		return a
	}
	p.mark = m
	return nil
}

// disjunction (memo): conjunction ('or' conjunction)+ | conjunction
func (p *parser) disjunction() *Node {
	return memo(p, rDisjunction, func() *Node { return p.boolOp(kwOr, "Or", p.conjunction) })
}

// conjunction (memo): inversion ('and' inversion)+ | inversion
func (p *parser) conjunction() *Node {
	return memo(p, rConjunction, func() *Node { return p.boolOp(kwAnd, "And", p.inversion) })
}

// boolOp is the body of the rule next (kw next)+ | next.
func (p *parser) boolOp(kw int, op string, next func() *Node) *Node {
	m := p.mark
	var a *Node
	var b []*Node
	if got(&a, next()) && gotSeq(&b, loop1(p, func() *Node {
		return group(p, func() *Node {
			if p.tok(kw) {
				return next()
			}
			return nil
		})
	})) {
		n := p.span("BoolOp", m)
		n.Op, n.L1 = op, append([]*Node{a}, b...)
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if a := next(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// inversion (memo): 'not' inversion | comparison
func (p *parser) inversion() *Node {
	return memo(p, rInversion, func() *Node {
		m := p.mark
		var a *Node
		if p.tok(kwNot) && got(&a, p.inversion()) {
			n := p.span("UnaryOp", m)
			n.Op, n.A = "Not", a
			return n
		}
		if !p.retry(m) {
			return nil
		}
		if a := p.comparison(); a != nil {
			return a
		}
		p.mark = m
		return nil
	})
}

// comparison: bitwise_or compare_op_bitwise_or_pair+ | bitwise_or
func (p *parser) comparison() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	var b []*cmpPair
	if got(&a, p.bitwiseOr()) && gotSeq(&b, loop1(p, p.compareOpPair)) {
		n := p.span("Compare", m)
		n.A = a
		for _, c := range b {
			n.Ops = append(n.Ops, c.op)
			n.L1 = append(n.L1, c.expr)
		}
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.bitwiseOr(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// compare_op_bitwise_or_pair:
//
//	| '==' bitwise_or | '!=' bitwise_or | '<=' bitwise_or | '<' bitwise_or
//	| '>=' bitwise_or | '>' bitwise_or | 'not' 'in' bitwise_or
//	| 'in' bitwise_or | 'is' 'not' bitwise_or | 'is' bitwise_or
func (p *parser) compareOpPair() *cmpPair {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	alts := []struct {
		toks []int
		op   string
	}{
		{[]int{tEQEQUAL}, "Eq"}, {[]int{tNOTEQUAL}, "NotEq"}, {[]int{tLESSEQUAL}, "LtE"}, {[]int{tLESS}, "Lt"},
		{[]int{tGREATEREQUAL}, "GtE"}, {[]int{tGREATER}, "Gt"}, {[]int{kwNot, kwIn}, "NotIn"}, {[]int{kwIn}, "In"},
		{[]int{kwIs, kwNot}, "IsNot"}, {[]int{kwIs}, "Is"},
	}
	for _, alt := range alts {
		if !p.retry(m) {
			return nil
		}
		if c := p.compareOp(alt.toks, alt.op); c != nil {
			return c
		}
	}
	p.mark = m
	return nil
}

// compareOp is one of eq_bitwise_or, noteq_bitwise_or, ...: the operator's
// tokens and a bitwise_or.
func (p *parser) compareOp(toks []int, op string) *cmpPair {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	ok := true
	for _, t := range toks {
		if t == tNOTEQUAL {
			// (tok='!=' { _PyPegen_check_barry_as_flufl(p, tok) ? NULL : tok })
			ok = group(p, func() *ptok {
				tk := p.expect(t)
				if tk == nil || p.checkBarryAsFlufl(tk) {
					return nil
				}
				return tk
			}) != nil
		} else {
			ok = p.tok(t)
		}
		if !ok {
			break
		}
	}
	var a *Node
	if ok && got(&a, p.bitwiseOr()) {
		return &cmpPair{op, a}
	}
	p.mark = m
	return nil
}

// binOp grows a left-recursive binary operator rule: rule ops next | next.
func (p *parser) binOp(rule int, next func() *Node, ops []int, names []string) *Node {
	return memo(p, rule, func() *Node {
		// The growing runs in the rule's _raw function, a rule of its own.
		if !p.enter() {
			return nil
		}
		defer p.leave()
		m := p.mark
		var left *Node
		if !p.seed() || !got(&left, next()) {
			p.mark = m
			return nil
		}
	grow:
		for {
			for i, op := range ops {
				mm := p.mark
				var b *Node
				if p.seed() && p.tok(op) && got(&b, next()) {
					n := p.span("BinOp", m)
					n.A, n.Op, n.B = left, names[i], b
					left = n
					continue grow
				}
				if p.errInd {
					return nil
				}
				p.mark = mm
			}
			return left
		}
	})
}

// bitwise_or: bitwise_or '|' bitwise_xor | bitwise_xor
func (p *parser) bitwiseOr() *Node {
	return p.binOp(rBitwiseOr, p.bitwiseXor, []int{tVBAR}, []string{"BitOr"})
}

// bitwise_xor: bitwise_xor '^' bitwise_and | bitwise_and
func (p *parser) bitwiseXor() *Node {
	return p.binOp(rBitwiseXor, p.bitwiseAnd, []int{tCIRCUMFLEX}, []string{"BitXor"})
}

// bitwise_and: bitwise_and '&' shift_expr | shift_expr
func (p *parser) bitwiseAnd() *Node {
	return p.binOp(rBitwiseAnd, p.shiftExpr, []int{tAMPER}, []string{"BitAnd"})
}

// shift_expr: shift_expr '<<' sum | shift_expr '>>' sum | sum
func (p *parser) shiftExpr() *Node {
	return p.binOp(rShiftExpr, p.sum, []int{tLEFTSHIFT, tRIGHTSHIFT}, []string{"LShift", "RShift"})
}

// sum: sum '+' term | sum '-' term | term
func (p *parser) sum() *Node {
	return p.binOp(rSum, p.term, []int{tPLUS, tMINUS}, []string{"Add", "Sub"})
}

// term: term '*' factor | term '/' factor | term '//' factor | term '%' factor | term '@' factor | factor
func (p *parser) term() *Node {
	return p.binOp(rTerm, p.factor, []int{tSTAR, tSLASH, tDOUBLESLASH, tPERCENT, tAT},
		[]string{"Mult", "Div", "FloorDiv", "Mod", "MatMult"})
}

// factor (memo): '+' factor | '-' factor | '~' factor | power
func (p *parser) factor() *Node {
	return memo(p, rFactor, func() *Node {
		m := p.mark
		for i, t := range []int{tPLUS, tMINUS, tTILDE} {
			var a *Node
			if p.tok(t) && got(&a, p.factor()) {
				n := p.span("UnaryOp", m)
				n.Op, n.A = []string{"UAdd", "USub", "Invert"}[i], a
				return n
			}
			if !p.retry(m) {
				return nil
			}
		}
		if a := p.power(); a != nil {
			return a
		}
		p.mark = m
		return nil
	})
}

// power: await_primary '**' factor | await_primary
func (p *parser) power() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *Node
	if got(&a, p.awaitPrimary()) && p.tok(tDOUBLESTAR) && got(&b, p.factor()) {
		n := p.span("BinOp", m)
		n.A, n.Op, n.B = a, "Pow", b
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.awaitPrimary(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// await_primary (memo): AWAIT primary | primary
func (p *parser) awaitPrimary() *Node {
	return memo(p, rAwaitPrimary, func() *Node {
		m := p.mark
		var a *Node
		if p.tok(tAWAIT) && got(&a, p.primary()) {
			n := p.span("Await", m)
			n.A = a
			return n
		}
		if !p.retry(m) {
			return nil
		}
		if a := p.primary(); a != nil {
			return a
		}
		p.mark = m
		return nil
	})
}

// primary:
//
//	| primary '.' NAME
//	| primary genexp
//	| primary '(' [arguments] ')'
//	| primary '[' slices ']'
//	| atom
func (p *parser) primary() *Node {
	return memo(p, rPrimary, func() *Node {
		// The growing runs in the rule's _raw function, a rule of its own.
		if !p.enter() {
			return nil
		}
		defer p.leave()
		m := p.mark
		var a *Node
		if !p.seed() || !got(&a, p.atom()) {
			p.mark = m
			return nil
		}
		for {
			if b := p.primaryTrailer(a, m, false); b != nil {
				a = b
				continue
			}
			if p.errInd {
				return nil
			}
			return a
		}
	})
}

// primaryTrailer tries the alternatives of primary (or of t_primary, when
// target is set) that extend a, which starts at the mark start.
func (p *parser) primaryTrailer(a *Node, start int, target bool) *Node {
	m := p.mark
	if !p.seed() {
		return nil
	}
	// The lookahead t_primary requires after each trailer.
	look := func() bool { return !target || p.atAny(tLPAR, tLSQB, tDOT) }
	var b *Node
	if p.tok(tDOT) && got(&b, p.name()) && look() {
		n := p.span("Attribute", start)
		n.A, n.ID, n.Ctx = a, b.ID, "Load"
		return n
	}
	if !p.retry(m) {
		return nil
	}
	genexp := func() *Node { return p.primaryGenexp(a, start, look) }
	call := func() *Node { return p.primaryCall(a, start, look) }
	subscript := func() *Node {
		var s *Node
		if p.tok(tLSQB) && got(&s, p.slices()) && p.tok(tRSQB) && look() {
			n := p.span("Subscript", start)
			n.A, n.B, n.Ctx = a, s, "Load"
			return n
		}
		return nil
	}
	alts := []func() *Node{genexp, call, subscript}
	if target {
		// t_primary tries the subscript before the calls.
		alts = []func() *Node{subscript, genexp, call}
	}
	for _, alt := range alts {
		if n := alt(); n != nil {
			return n
		}
		if !p.retry(m) {
			return nil
		}
	}
	return nil
}

func (p *parser) primaryGenexp(a *Node, start int, look func() bool) *Node {
	var g *Node
	if got(&g, p.genexp()) && look() {
		n := p.span("Call", start)
		n.A, n.L1 = a, []*Node{g}
		return n
	}
	return nil
}

func (p *parser) primaryCall(a *Node, start int, look func() bool) *Node {
	var b *Node
	if p.tok(tLPAR) && opt(p, &b, p.arguments()) && p.tok(tRPAR) && look() {
		n := p.span("Call", start)
		n.A = a
		if b != nil {
			n.L1, n.L2 = b.L1, b.L2
		}
		return n
	}
	return nil
}

// slices:
//
//	| slice !','
//	| ','.(slice | starred_expression)+ [',']
func (p *parser) slices() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	if got(&a, p.slice()) && !p.at(tCOMMA) {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	var l []*Node
	if gotSeq(&l, gather(p, tCOMMA, func() *Node {
		return group(p, func() *Node {
			mm := p.mark
			if s := p.slice(); s != nil {
				return s
			}
			if !p.retry(mm) {
				return nil
			}
			return p.starredExpression()
		})
	})) && p.optTok(tCOMMA) {
		n := p.span("Tuple", m)
		n.L1, n.Ctx = l, "Load"
		return n
	}
	p.mark = m
	return nil
}

// slice:
//
//	| [expression] ':' [expression] [':' [expression]]
//	| named_expression
func (p *parser) slice() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b, c *Node
	if opt(p, &a, p.expression()) && p.tok(tCOLON) && opt(p, &b, p.expression()) {
		// [':' d=[expression] { d }]: a step of "x[::]" is None.
		if p.enter() {
			mm := p.mark
			if p.tok(tCOLON) {
				c = p.expression()
			} else {
				p.mark = mm
			}
			p.leave()
		}
		if !p.errInd {
			n := p.span("Slice", m)
			n.A, n.B, n.C = a, b, c
			return n
		}
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.namedExpression(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// atom:
//
//	| NAME
//	| 'True' | 'False' | 'None'
//	| &(STRING | FSTRING_START) strings
//	| NUMBER
//	| &'(' (tuple | group | genexp)
//	| &'[' (list | listcomp)
//	| &'{' (dict | set | dictcomp | setcomp)
//	| '...'
func (p *parser) atom() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.name(); a != nil {
		return a
	}
	for _, kw := range []int{kwTrue, kwFalse, kwNone} {
		if !p.retry(m) {
			return nil
		}
		if p.tok(kw) {
			n := p.span("Constant", m)
			n.Value = map[int]Value{kwTrue: trueV, kwFalse: falseV, kwNone: none}[kw]
			return n
		}
	}
	if !p.retry(m) {
		return nil
	}
	if p.atAny(tSTRING, tFSTRINGSTART) {
		if a := p.strings(); a != nil {
			return a
		}
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.number(); a != nil {
		return a
	}
	brackets := []struct {
		open  int
		rules []func() *Node
	}{
		{tLPAR, []func() *Node{p.tuple, p.group, p.genexp}},
		{tLSQB, []func() *Node{p.list, p.listcomp}},
		{tLBRACE, []func() *Node{p.dict, p.set, p.dictcomp, p.setcomp}},
	}
	for _, br := range brackets {
		if !p.retry(m) {
			return nil
		}
		if p.at(br.open) {
			// (tuple | group | genexp) and the like are a rule of their own.
			if a := group(p, func() *Node {
				for _, rule := range br.rules {
					if a := rule(); a != nil {
						return a
					}
					if !p.retry(m) {
						return nil
					}
				}
				return nil
			}); a != nil {
				return a
			}
		}
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(tELLIPSIS) {
		n := p.span("Constant", m)
		n.Value = ellipsisV{}
		return n
	}
	p.mark = m
	return nil
}

// group: '(' (yield_expr | named_expression) ')' | invalid_group
func (p *parser) group() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	if p.tok(tLPAR) && got(&a, group(p, func() *Node {
		mm := p.mark
		if y := p.yieldExpr(); y != nil {
			return y
		}
		if !p.retry(mm) {
			return nil
		}
		return p.namedExpression()
	})) && p.tok(tRPAR) {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidGroup()
		p.mark = m
	}
	return nil
}

// lambdef: 'lambda' [lambda_params] ':' expression
func (p *parser) lambdef() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *Node
	if p.tok(kwLambda) && opt(p, &a, p.lambdaParams()) && p.tok(tCOLON) && got(&b, p.expression()) {
		n := p.span("Lambda", m)
		if a == nil {
			a = &Node{Kind: "arguments"}
		}
		n.A, n.B = a, b
		return n
	}
	p.mark = m
	return nil
}

// lambda_params: invalid_lambda_parameters | lambda_parameters
func (p *parser) lambdaParams() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidLambdaParameters()
		if !p.retry(m) {
			return nil
		}
	}
	if a := p.lambdaParameters(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// fstring_middle: fstring_replacement_field | FSTRING_MIDDLE
func (p *parser) fstringMiddle() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.fstringReplacementField(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if t := p.expect(tFSTRINGMIDDLE); t != nil {
		return constantFromToken(t)
	}
	p.mark = m
	return nil
}

// fstring_replacement_field:
//
//	| '{' (yield_expr | star_expressions) '='? [fstring_conversion] [fstring_full_format_spec] '}'
//	| invalid_replacement_field
func (p *parser) fstringReplacementField() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	var conv, format *withMeta
	var rbrace *ptok
	if p.tok(tLBRACE) && got(&a, p.yieldOrStarExpressions()) {
		debug := p.expect(tEQUAL)
		if !p.errInd && opt(p, &conv, p.fstringConversion()) && opt(p, &format, p.fstringFullFormatSpec()) &&
			got(&rbrace, p.expect(tRBRACE)) {
			return p.formattedValue(a, debug, conv, format, rbrace, m)
		}
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidReplacementField()
		p.mark = m
	}
	return nil
}

// fstring_conversion: "!" NAME
func (p *parser) fstringConversion() *withMeta {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var bang *ptok
	var conv *Node
	if got(&bang, p.expect(tEXCLAMATION)) && got(&conv, p.name()) {
		return p.checkFStringConversion(bang, conv)
	}
	p.mark = m
	return nil
}

// fstring_full_format_spec: ':' fstring_format_spec*
func (p *parser) fstringFullFormatSpec() *withMeta {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var colon *ptok
	var spec []*Node
	if got(&colon, p.expect(tCOLON)) && gotSeq(&spec, loop0(p, p.fstringFormatSpec)) {
		return p.setupFullFormatSpec(colon, spec, m)
	}
	p.mark = m
	return nil
}

// fstring_format_spec: FSTRING_MIDDLE | fstring_replacement_field
func (p *parser) fstringFormatSpec() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if t := p.expect(tFSTRINGMIDDLE); t != nil {
		return p.decodedConstantFromToken(t)
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.fstringReplacementField(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// fstring: FSTRING_START fstring_middle* FSTRING_END
func (p *parser) fstring() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, c *ptok
	var b []*Node
	if got(&a, p.expect(tFSTRINGSTART)) && gotSeq(&b, loop0(p, p.fstringMiddle)) && got(&c, p.expect(tFSTRINGEND)) {
		return p.joinedStr(a, b, c)
	}
	p.mark = m
	return nil
}

// string: STRING
func (p *parser) stringRule() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	if t := p.expect(tSTRING); t != nil {
		return p.constantFromString(t)
	}
	return nil
}

// strings (memo): (fstring | string)+
func (p *parser) strings() *Node {
	return memo(p, rStrings, func() *Node {
		m := p.mark
		var a []*Node
		if gotSeq(&a, loop1(p, func() *Node {
			return group(p, func() *Node {
				mm := p.mark
				if f := p.fstring(); f != nil {
					return f
				}
				if !p.retry(mm) {
					return nil
				}
				return p.stringRule()
			})
		})) {
			return p.concatenateStrings(a, m)
		}
		p.mark = m
		return nil
	})
}

// list: '[' [star_named_expressions] ']'
func (p *parser) list() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*Node
	if p.tok(tLSQB) && optSeq(p, &a, p.starNamedExpressions()) && p.tok(tRSQB) {
		n := p.span("List", m)
		n.L1, n.Ctx = a, "Load"
		return n
	}
	p.mark = m
	return nil
}

// tuple: '(' [star_named_expression ',' [star_named_expressions]] ')'
func (p *parser) tuple() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *[]*Node
	if p.tok(tLPAR) && opt(p, &a, group(p, func() *[]*Node {
		var y *Node
		var z []*Node
		if got(&y, p.starNamedExpression()) && p.tok(tCOMMA) && optSeq(p, &z, p.starNamedExpressions()) {
			l := append([]*Node{y}, z...)
			return &l
		}
		return nil
	})) && p.tok(tRPAR) {
		n := p.span("Tuple", m)
		n.Ctx = "Load"
		if a != nil {
			n.L1 = *a
		}
		return n
	}
	p.mark = m
	return nil
}

// set: '{' star_named_expressions '}'
func (p *parser) set() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*Node
	if p.tok(tLBRACE) && gotSeq(&a, p.starNamedExpressions()) && p.tok(tRBRACE) {
		n := p.span("Set", m)
		n.L1 = a
		return n
	}
	p.mark = m
	return nil
}

// dict:
//
//	| '{' [double_starred_kvpairs] '}'
//	| '{' invalid_double_starred_kvpairs '}'
func (p *parser) dict() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*kvPair
	if p.tok(tLBRACE) && optSeq(p, &a, p.doubleStarredKvpairs()) && p.tok(tRBRACE) {
		n := p.span("Dict", m)
		for _, kv := range a {
			n.L1 = append(n.L1, kv.key)
			n.L2 = append(n.L2, kv.value)
		}
		return n
	}
	if !p.retry(m) {
		return nil
	}
	// Not a lone invalid_ rule, so tried in the first pass too.
	if p.tok(tLBRACE) {
		p.invalidDoubleStarredKvpairs()
	}
	p.mark = m
	return nil
}

// double_starred_kvpairs: ','.double_starred_kvpair+ [',']
func (p *parser) doubleStarredKvpairs() []*kvPair {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*kvPair
	if gotSeq(&a, gather(p, tCOMMA, p.doubleStarredKvpair)) && p.optTok(tCOMMA) {
		return a
	}
	p.mark = m
	return nil
}

// double_starred_kvpair: '**' bitwise_or | kvpair
func (p *parser) doubleStarredKvpair() *kvPair {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	if p.tok(tDOUBLESTAR) && got(&a, p.bitwiseOr()) {
		return &kvPair{nil, a}
	}
	if !p.retry(m) {
		return nil
	}
	if kv := p.kvpair(); kv != nil {
		return kv
	}
	p.mark = m
	return nil
}

// kvpair: expression ':' expression
func (p *parser) kvpair() *kvPair {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b *Node
	if got(&a, p.expression()) && p.tok(tCOLON) && got(&b, p.expression()) {
		return &kvPair{a, b}
	}
	p.mark = m
	return nil
}

// for_if_clauses: for_if_clause+
func (p *parser) forIfClauses() []*Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return loop1(p, p.forIfClause)
}

// for_if_clause:
//
//	| ASYNC 'for' star_targets 'in' ~ disjunction ('if' disjunction)*
//	| 'for' star_targets 'in' ~ disjunction ('if' disjunction)*
//	| invalid_for_target
func (p *parser) forIfClause() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	for _, async := range []bool{true, false} {
		var a, b *Node
		var c []*Node
		if (!async || p.tok(tASYNC)) && p.tok(kwFor) && got(&a, p.starTargets()) && p.tok(kwIn) {
			if got(&b, p.disjunction()) && gotSeq(&c, loop0(p, func() *Node {
				return group(p, func() *Node {
					if p.tok(kwIf) {
						return p.disjunction()
					}
					return nil
				})
			})) {
				n := &Node{Kind: "comprehension", A: a, B: b, L1: c}
				if async {
					n.IsAsync = 1
				}
				return n
			}
			// The cut: no other alternative is tried.
			p.mark = m
			return nil
		}
		if !p.retry(m) {
			return nil
		}
	}
	if p.invalid {
		p.invalidForTarget()
		p.mark = m
	}
	return nil
}

// listcomp: '[' named_expression for_if_clauses ']' | invalid_comprehension
func (p *parser) listcomp() *Node {
	return p.comprehension("ListComp", tLSQB, tRSQB, p.namedExpression)
}

// setcomp: '{' named_expression for_if_clauses '}' | invalid_comprehension
func (p *parser) setcomp() *Node {
	return p.comprehension("SetComp", tLBRACE, tRBRACE, p.namedExpression)
}

// genexp: '(' (assignment_expression | expression !':=') for_if_clauses ')' | invalid_comprehension
func (p *parser) genexp() *Node {
	return p.comprehension("GeneratorExp", tLPAR, tRPAR, p.assignmentOrExpression)
}

func (p *parser) comprehension(kind string, open, close int, elt func() *Node) *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	var b []*Node
	if p.tok(open) && got(&a, elt()) && gotSeq(&b, p.forIfClauses()) && p.tok(close) {
		n := p.span(kind, m)
		n.A, n.L1 = a, b
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidComprehension()
		p.mark = m
	}
	return nil
}

// dictcomp: '{' kvpair for_if_clauses '}' | invalid_dict_comprehension
func (p *parser) dictcomp() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *kvPair
	var b []*Node
	if p.tok(tLBRACE) && got(&a, p.kvpair()) && gotSeq(&b, p.forIfClauses()) && p.tok(tRBRACE) {
		n := p.span("DictComp", m)
		n.A, n.B, n.L1 = a.key, a.value, b
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidDictComprehension()
		p.mark = m
	}
	return nil
}

// arguments (memo): args [','] &')' | invalid_arguments
func (p *parser) arguments() *Node {
	return memo(p, rArguments, func() *Node {
		m := p.mark
		var a *Node
		if got(&a, p.args()) && p.optTok(tCOMMA) && p.at(tRPAR) {
			return a
		}
		if !p.retry(m) {
			return nil
		}
		if p.invalid {
			p.invalidArguments()
			p.mark = m
		}
		return nil
	})
}

// args:
//
//	| ','.(starred_expression | (assignment_expression | expression !':=') !'=')+ [',' kwargs]
//	| kwargs
func (p *parser) args() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*Node
	var b []*kwOrStar
	if gotSeq(&a, gather(p, tCOMMA, p.positionalArg)) && optSeq(p, &b, p.commaKwargs()) {
		return p.collectCallSeqs(a, b, m)
	}
	if !p.retry(m) {
		return nil
	}
	if gotSeq(&b, p.kwargs()) {
		n := p.span("Call", m)
		n.L1, n.L2 = starredOf(b), keywordArgsOf(b)
		return n
	}
	p.mark = m
	return nil
}

// positionalArg: (starred_expression | (assignment_expression | expression !':=') !'=')
func (p *parser) positionalArg() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.starredExpression(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	var a *Node
	if got(&a, p.assignmentOrExpression()) && !p.at(tEQUAL) {
		return a
	}
	p.mark = m
	return nil
}

// commaKwargs: ',' kwargs
func (p *parser) commaKwargs() []*kwOrStar {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var k []*kwOrStar
	if p.tok(tCOMMA) && gotSeq(&k, p.kwargs()) {
		return k
	}
	p.mark = m
	return nil
}

// kwargs:
//
//	| ','.kwarg_or_starred+ ',' ','.kwarg_or_double_starred+
//	| ','.kwarg_or_starred+
//	| ','.kwarg_or_double_starred+
func (p *parser) kwargs() []*kwOrStar {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, b []*kwOrStar
	if gotSeq(&a, gather(p, tCOMMA, p.kwargOrStarred)) && p.tok(tCOMMA) &&
		gotSeq(&b, gather(p, tCOMMA, p.kwargOrDoubleStarred)) {
		return append(a, b...)
	}
	if !p.retry(m) {
		return nil
	}
	if gotSeq(&a, gather(p, tCOMMA, p.kwargOrStarred)) {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if gotSeq(&a, gather(p, tCOMMA, p.kwargOrDoubleStarred)) {
		return a
	}
	p.mark = m
	return nil
}

// starred_expression:
//
//	| invalid_starred_expression
//	| '*' expression
//	| '*'
func (p *parser) starredExpression() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidStarredExpression()
		if !p.retry(m) {
			return nil
		}
	}
	var a *Node
	if p.tok(tSTAR) && got(&a, p.expression()) {
		n := p.span("Starred", m)
		n.A, n.Ctx = a, "Load"
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(tSTAR) {
		p.raise("Invalid star expression")
		return nil
	}
	p.mark = m
	return nil
}

// kwarg_or_starred: invalid_kwarg | NAME '=' expression | starred_expression
func (p *parser) kwargOrStarred() *kwOrStar { return p.kwarg(false) }

// kwarg_or_double_starred: invalid_kwarg | NAME '=' expression | '**' expression
func (p *parser) kwargOrDoubleStarred() *kwOrStar { return p.kwarg(true) }

func (p *parser) kwarg(double bool) *kwOrStar {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidKwarg()
		if !p.retry(m) {
			return nil
		}
	}
	var a, b *Node
	if got(&a, p.name()) && p.tok(tEQUAL) && got(&b, p.expression()) {
		k := p.span("keyword", m)
		k.ID, k.A = a.ID, b
		return &kwOrStar{k, true}
	}
	if !p.retry(m) {
		return nil
	}
	if !double {
		if a := p.starredExpression(); a != nil {
			return &kwOrStar{a, false}
		}
	} else if p.tok(tDOUBLESTAR) && got(&a, p.expression()) {
		k := p.span("keyword", m)
		k.NoID, k.A = true, a
		return &kwOrStar{k, true}
	}
	p.mark = m
	return nil
}

// star_targets:
//
//	| star_target !','
//	| star_target (',' star_target)* [',']
func (p *parser) starTargets() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	if got(&a, p.starTarget()) && !p.at(tCOMMA) {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	var b []*Node
	if got(&a, p.starTarget()) && gotSeq(&b, loop0(p, p.commaStarTarget)) && p.optTok(tCOMMA) {
		n := p.span("Tuple", m)
		n.L1, n.Ctx = append([]*Node{a}, b...), "Store"
		return n
	}
	p.mark = m
	return nil
}

// commaStarTarget: ',' star_target
func (p *parser) commaStarTarget() *Node {
	return group(p, func() *Node {
		if p.tok(tCOMMA) {
			return p.starTarget()
		}
		return nil
	})
}

// star_targets_list_seq: ','.star_target+ [',']
func (p *parser) starTargetsListSeq() []*Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*Node
	if gotSeq(&a, gather(p, tCOMMA, p.starTarget)) && p.optTok(tCOMMA) {
		return a
	}
	p.mark = m
	return nil
}

// star_targets_tuple_seq:
//
//	| star_target (',' star_target)+ [',']
//	| star_target ','
func (p *parser) starTargetsTupleSeq() []*Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	var b []*Node
	if got(&a, p.starTarget()) && gotSeq(&b, loop1(p, p.commaStarTarget)) && p.optTok(tCOMMA) {
		return append([]*Node{a}, b...)
	}
	if !p.retry(m) {
		return nil
	}
	if got(&a, p.starTarget()) && p.tok(tCOMMA) {
		return []*Node{a}
	}
	p.mark = m
	return nil
}

// star_target (memo): '*' (!'*' star_target) | target_with_star_atom
func (p *parser) starTarget() *Node {
	return memo(p, rStarTarget, func() *Node {
		m := p.mark
		var a *Node
		if p.tok(tSTAR) && got(&a, group(p, func() *Node {
			if p.at(tSTAR) {
				return nil
			}
			return p.starTarget()
		})) {
			n := p.span("Starred", m)
			n.A, n.Ctx = setCtx(a, "Store"), "Store"
			return n
		}
		if !p.retry(m) {
			return nil
		}
		if a := p.targetWithStarAtom(); a != nil {
			return a
		}
		p.mark = m
		return nil
	})
}

// target_with_star_atom (memo):
//
//	| t_primary '.' NAME !t_lookahead
//	| t_primary '[' slices ']' !t_lookahead
//	| star_atom
func (p *parser) targetWithStarAtom() *Node {
	return memo(p, rTargetWithStarAtom, func() *Node {
		m := p.mark
		if a := p.subscriptAttributeTarget("Store"); a != nil {
			return a
		}
		if !p.retry(m) {
			return nil
		}
		if a := p.starAtom(); a != nil {
			return a
		}
		p.mark = m
		return nil
	})
}

// subscriptAttributeTarget is the pair of alternatives
//
//	| t_primary '.' NAME !t_lookahead
//	| t_primary '[' slices ']' !t_lookahead
//
// of the target rules, with the context ctx.
func (p *parser) subscriptAttributeTarget(ctx string) *Node {
	m := p.mark
	var a, b *Node
	if got(&a, p.tPrimary()) && p.tok(tDOT) && got(&b, p.name()) && !p.tLookahead() {
		n := p.span("Attribute", m)
		n.A, n.ID, n.Ctx = a, b.ID, ctx
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if got(&a, p.tPrimary()) && p.tok(tLSQB) && got(&b, p.slices()) && p.tok(tRSQB) && !p.tLookahead() {
		n := p.span("Subscript", m)
		n.A, n.B, n.Ctx = a, b, ctx
		return n
	}
	p.mark = m
	return nil
}

// star_atom:
//
//	| NAME
//	| '(' target_with_star_atom ')'
//	| '(' [star_targets_tuple_seq] ')'
//	| '[' [star_targets_list_seq] ']'
func (p *parser) starAtom() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.name(); a != nil {
		return setCtx(a, "Store")
	}
	if !p.retry(m) {
		return nil
	}
	var a *Node
	if p.tok(tLPAR) && got(&a, p.targetWithStarAtom()) && p.tok(tRPAR) {
		return setCtx(a, "Store")
	}
	if !p.retry(m) {
		return nil
	}
	var l []*Node
	if p.tok(tLPAR) && optSeq(p, &l, p.starTargetsTupleSeq()) && p.tok(tRPAR) {
		n := p.span("Tuple", m)
		n.L1, n.Ctx = l, "Store"
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(tLSQB) && optSeq(p, &l, p.starTargetsListSeq()) && p.tok(tRSQB) {
		n := p.span("List", m)
		n.L1, n.Ctx = l, "Store"
		return n
	}
	p.mark = m
	return nil
}

// single_target:
//
//	| single_subscript_attribute_target
//	| NAME
//	| '(' single_target ')'
func (p *parser) singleTarget() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.singleSubscriptAttributeTarget(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.name(); a != nil {
		return setCtx(a, "Store")
	}
	if !p.retry(m) {
		return nil
	}
	var a *Node
	if p.tok(tLPAR) && got(&a, p.singleTarget()) && p.tok(tRPAR) {
		return a
	}
	p.mark = m
	return nil
}

// single_subscript_attribute_target:
//
//	| t_primary '.' NAME !t_lookahead
//	| t_primary '[' slices ']' !t_lookahead
func (p *parser) singleSubscriptAttributeTarget() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return p.subscriptAttributeTarget("Store")
}

// t_primary:
//
//	| t_primary '.' NAME &t_lookahead
//	| t_primary '[' slices ']' &t_lookahead
//	| t_primary genexp &t_lookahead
//	| t_primary '(' [arguments] ')' &t_lookahead
//	| atom &t_lookahead
func (p *parser) tPrimary() *Node {
	return memo(p, rTPrimary, func() *Node {
		// The growing runs in the rule's _raw function, a rule of its own.
		if !p.enter() {
			return nil
		}
		defer p.leave()
		m := p.mark
		var a *Node
		if !p.seed() || !got(&a, p.atom()) || !p.tLookahead() {
			p.mark = m
			return nil
		}
		for {
			if b := p.primaryTrailer(a, m, true); b != nil {
				a = b
				continue
			}
			if p.errInd {
				return nil
			}
			return a
		}
	})
}

// t_lookahead: '(' | '[' | '.'
func (p *parser) tLookahead() bool { return p.atAny(tLPAR, tLSQB, tDOT) }

// del_targets: ','.del_target+ [',']
func (p *parser) delTargets() []*Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a []*Node
	if gotSeq(&a, gather(p, tCOMMA, p.delTarget)) && p.optTok(tCOMMA) {
		return a
	}
	p.mark = m
	return nil
}

// del_target (memo):
//
//	| t_primary '.' NAME !t_lookahead
//	| t_primary '[' slices ']' !t_lookahead
//	| del_t_atom
func (p *parser) delTarget() *Node {
	return memo(p, rDelTarget, func() *Node {
		m := p.mark
		if a := p.subscriptAttributeTarget("Del"); a != nil {
			return a
		}
		if !p.retry(m) {
			return nil
		}
		if a := p.delTAtom(); a != nil {
			return a
		}
		p.mark = m
		return nil
	})
}

// del_t_atom:
//
//	| NAME
//	| '(' del_target ')'
//	| '(' [del_targets] ')'
//	| '[' [del_targets] ']'
func (p *parser) delTAtom() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.name(); a != nil {
		return setCtx(a, "Del")
	}
	if !p.retry(m) {
		return nil
	}
	var a *Node
	if p.tok(tLPAR) && got(&a, p.delTarget()) && p.tok(tRPAR) {
		return setCtx(a, "Del")
	}
	if !p.retry(m) {
		return nil
	}
	var l []*Node
	if p.tok(tLPAR) && optSeq(p, &l, p.delTargets()) && p.tok(tRPAR) {
		n := p.span("Tuple", m)
		n.L1, n.Ctx = l, "Del"
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(tLSQB) && optSeq(p, &l, p.delTargets()) && p.tok(tRSQB) {
		n := p.span("List", m)
		n.L1, n.Ctx = l, "Del"
		return n
	}
	p.mark = m
	return nil
}
