// Ported from CPython 3.12 (Grammar/python.gram, Parser/action_helpers.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The match statement and its patterns.

package py

// match_stmt:
//
//	| "match" subject_expr ':' NEWLINE INDENT case_block+ DEDENT
//	| invalid_match_stmt
func (p *parser) matchStmt() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var subject *Node
	var cases []*Node
	if p.softKeyword("match") != nil && got(&subject, p.subjectExpr()) && p.tok(tCOLON) && p.tok(tNEWLINE) &&
		p.tok(tINDENT) && gotSeq(&cases, loop1(p, p.caseBlock)) && p.tok(tDEDENT) {
		n := p.span("Match", m)
		n.A, n.L1 = subject, cases
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidMatchStmt()
		p.mark = m
	}
	return nil
}

// subject_expr:
//
//	| star_named_expression ',' star_named_expressions?
//	| named_expression
func (p *parser) subjectExpr() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	var b []*Node
	if got(&a, p.starNamedExpression()) && p.tok(tCOMMA) && optSeq(p, &b, p.starNamedExpressions()) {
		n := p.span("Tuple", m)
		n.L1, n.Ctx = append([]*Node{a}, b...), "Load"
		return n
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

// case_block:
//
//	| invalid_case_block
//	| "case" patterns guard? ':' block
func (p *parser) caseBlock() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.invalid {
		p.invalidCaseBlock()
		if !p.retry(m) {
			return nil
		}
	}
	var pat, g *Node
	var body []*Node
	if p.softKeyword("case") != nil && got(&pat, p.patterns()) && opt(p, &g, p.guard()) && p.tok(tCOLON) &&
		gotSeq(&body, p.block()) {
		return &Node{Kind: "match_case", A: pat, B: g, Body: body}
	}
	p.mark = m
	return nil
}

// guard: 'if' named_expression
func (p *parser) guard() *Node {
	return group(p, func() *Node {
		if p.tok(kwIf) {
			return p.namedExpression()
		}
		return nil
	})
}

// patterns: open_sequence_pattern | pattern
func (p *parser) patterns() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var l []*Node
	if gotSeq(&l, p.openSequencePattern()) {
		n := p.span("MatchSequence", m)
		n.L1 = l
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.pattern(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// pattern: as_pattern | or_pattern
func (p *parser) pattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.asPattern(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.orPattern(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// as_pattern: or_pattern 'as' pattern_capture_target | invalid_as_pattern
func (p *parser) asPattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var pat, target *Node
	if got(&pat, p.orPattern()) && p.tok(kwAs) && got(&target, p.patternCaptureTarget()) {
		n := p.span("MatchAs", m)
		n.A, n.ID = pat, target.ID
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidAsPattern()
		p.mark = m
	}
	return nil
}

// or_pattern: '|'.closed_pattern+
func (p *parser) orPattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var l []*Node
	if gotSeq(&l, gather(p, tVBAR, p.closedPattern)) {
		if len(l) == 1 {
			return l[0]
		}
		n := p.span("MatchOr", m)
		n.L1 = l
		return n
	}
	p.mark = m
	return nil
}

// closed_pattern (memo):
//
//	| literal_pattern | capture_pattern | wildcard_pattern | value_pattern
//	| group_pattern | sequence_pattern | mapping_pattern | class_pattern
func (p *parser) closedPattern() *Node {
	return memo(p, rClosedPattern, func() *Node {
		m := p.mark
		for _, alt := range []func() *Node{p.literalPattern, p.capturePattern, p.wildcardPattern, p.valuePattern,
			p.groupPattern, p.sequencePattern, p.mappingPattern, p.classPattern} {
			if a := alt(); a != nil {
				return a
			}
			if !p.retry(m) {
				return nil
			}
		}
		return nil
	})
}

// literal_pattern:
//
//	| signed_number !('+' | '-')
//	| complex_number
//	| strings
//	| 'None' | 'True' | 'False'
//
// and literal_expr, the same as expressions: Constants for the keywords.
func (p *parser) literalPattern() *Node { return p.literal(true) }

func (p *parser) literalExpr() *Node { return p.literal(false) }

func (p *parser) literal(pattern bool) *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	value := func(v *Node) *Node {
		if !pattern {
			return v
		}
		n := p.span("MatchValue", m)
		n.A = v
		return n
	}
	var v *Node
	if got(&v, p.signedNumber()) && !p.atAny(tPLUS, tMINUS) {
		return value(v)
	}
	if !p.retry(m) {
		return nil
	}
	if got(&v, p.complexNumber()) {
		return value(v)
	}
	if !p.retry(m) {
		return nil
	}
	if got(&v, p.strings()) {
		return value(v)
	}
	for _, kw := range []int{kwNone, kwTrue, kwFalse} {
		if !p.retry(m) {
			return nil
		}
		if p.tok(kw) {
			kind := "MatchSingleton"
			if !pattern {
				kind = "Constant"
			}
			n := p.span(kind, m)
			n.Value = map[int]Value{kwTrue: trueV, kwFalse: falseV, kwNone: none}[kw]
			return n
		}
	}
	p.mark = m
	return nil
}

// complex_number:
//
//	| signed_real_number '+' imaginary_number
//	| signed_real_number '-' imaginary_number
func (p *parser) complexNumber() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	for i, t := range []int{tPLUS, tMINUS} {
		var real, imag *Node
		if got(&real, p.signedRealNumber()) && p.tok(t) && got(&imag, p.imaginaryNumber()) {
			n := p.span("BinOp", m)
			n.A, n.Op, n.B = real, []string{"Add", "Sub"}[i], imag
			return n
		}
		if !p.retry(m) {
			return nil
		}
	}
	return nil
}

// signed_number: NUMBER | '-' NUMBER
func (p *parser) signedNumber() *Node { return p.signed(p.number) }

// signed_real_number: real_number | '-' real_number
func (p *parser) signedRealNumber() *Node { return p.signed(p.realNumber) }

func (p *parser) signed(num func() *Node) *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := num(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	var a *Node
	if p.tok(tMINUS) && got(&a, num()) {
		n := p.span("UnaryOp", m)
		n.Op, n.A = "USub", a
		return n
	}
	p.mark = m
	return nil
}

// real_number: NUMBER, which must not be imaginary.
func (p *parser) realNumber() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	a := p.number()
	if a == nil {
		return nil
	}
	if _, ok := a.Value.(complexV); ok {
		p.raiseNode(a, "real number required in complex literal")
		return nil
	}
	return a
}

// imaginary_number: NUMBER, which must be imaginary.
func (p *parser) imaginaryNumber() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	a := p.number()
	if a == nil {
		return nil
	}
	if _, ok := a.Value.(complexV); !ok {
		p.raiseNode(a, "imaginary number required in complex literal")
		return nil
	}
	return a
}

// capture_pattern: pattern_capture_target
func (p *parser) capturePattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if t := p.patternCaptureTarget(); t != nil {
		n := p.span("MatchAs", m)
		n.ID = t.ID
		return n
	}
	p.mark = m
	return nil
}

// pattern_capture_target: !"_" NAME !('.' | '(' | '=')
func (p *parser) patternCaptureTarget() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	if !p.atSoftKeyword("_") && got(&a, p.name()) && !p.atAny(tDOT, tLPAR, tEQUAL) {
		return setCtx(a, "Store")
	}
	p.mark = m
	return nil
}

// wildcard_pattern: "_"
func (p *parser) wildcardPattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if p.softKeyword("_") != nil {
		n := p.span("MatchAs", m)
		n.NoID = true
		return n
	}
	p.mark = m
	return nil
}

// value_pattern: attr !('.' | '(' | '=')
func (p *parser) valuePattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	if got(&a, p.attr()) && !p.atAny(tDOT, tLPAR, tEQUAL) {
		n := p.span("MatchValue", m)
		n.A = a
		return n
	}
	p.mark = m
	return nil
}

// attr: name_or_attr '.' NAME
func (p *parser) attr() *Node {
	return memo(p, rAttr, func() *Node {
		// The growing runs in the rule's _raw function, a rule of its own.
		if !p.enter() {
			return nil
		}
		defer p.leave()
		m := p.mark
		var a *Node
		if !got(&a, p.nameOrAttrBase()) {
			p.mark = m
			return nil
		}
		grown := false
		for {
			mm := p.mark
			var b *Node
			if !p.nameOrAttrSeed() || !p.tok(tDOT) || !got(&b, p.name()) {
				p.mark = mm
				break
			}
			n := p.span("Attribute", m)
			n.A, n.ID, n.Ctx = a, b.ID, "Load"
			a, grown = n, true
		}
		if !grown {
			p.mark = m
			return nil
		}
		return a
	})
}

// nameOrAttrBase is name_or_attr as attr's _raw function first runs it,
// before attr has a result: attr fails, and NAME is the base.
func (p *parser) nameOrAttrBase() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	if !p.seed() {
		return nil
	}
	return p.name()
}

// nameOrAttrSeed is name_or_attr as attr's _raw function runs it once attr
// has a result: it reads that result, and only counts towards the stack
// limit.
func (p *parser) nameOrAttrSeed() bool {
	if !p.enter() {
		return false
	}
	defer p.leave()
	return p.seed()
}

// name_or_attr: attr | NAME
func (p *parser) nameOrAttr() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.attr(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.name(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// group_pattern: '(' pattern ')'
func (p *parser) groupPattern() *Node {
	return group(p, func() *Node {
		var a *Node
		if p.tok(tLPAR) && got(&a, p.pattern()) && p.tok(tRPAR) {
			return a
		}
		return nil
	})
}

// sequence_pattern:
//
//	| '[' maybe_sequence_pattern? ']'
//	| '(' open_sequence_pattern? ')'
func (p *parser) sequencePattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var l []*Node
	if p.tok(tLSQB) && optSeq(p, &l, p.maybeSequencePattern()) && p.tok(tRSQB) {
		n := p.span("MatchSequence", m)
		n.L1 = l
		return n
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(tLPAR) && optSeq(p, &l, p.openSequencePattern()) && p.tok(tRPAR) {
		n := p.span("MatchSequence", m)
		n.L1 = l
		return n
	}
	p.mark = m
	return nil
}

// open_sequence_pattern: maybe_star_pattern ',' maybe_sequence_pattern?
func (p *parser) openSequencePattern() []*Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a *Node
	var b []*Node
	if got(&a, p.maybeStarPattern()) && p.tok(tCOMMA) && optSeq(p, &b, p.maybeSequencePattern()) {
		return append([]*Node{a}, b...)
	}
	p.mark = m
	return nil
}

// maybe_sequence_pattern: ','.maybe_star_pattern+ ','?
func (p *parser) maybeSequencePattern() []*Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var l []*Node
	if gotSeq(&l, gather(p, tCOMMA, p.maybeStarPattern)) && p.optTok(tCOMMA) {
		return l
	}
	p.mark = m
	return nil
}

// maybe_star_pattern: star_pattern | pattern
func (p *parser) maybeStarPattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if a := p.starPattern(); a != nil {
		return a
	}
	if !p.retry(m) {
		return nil
	}
	if a := p.pattern(); a != nil {
		return a
	}
	p.mark = m
	return nil
}

// star_pattern (memo): '*' pattern_capture_target | '*' wildcard_pattern
func (p *parser) starPattern() *Node {
	return memo(p, rStarPattern, func() *Node {
		m := p.mark
		var t *Node
		if p.tok(tSTAR) && got(&t, p.patternCaptureTarget()) {
			n := p.span("MatchStar", m)
			n.ID = t.ID
			return n
		}
		if !p.retry(m) {
			return nil
		}
		if p.tok(tSTAR) && p.wildcardPattern() != nil {
			n := p.span("MatchStar", m)
			n.NoID = true
			return n
		}
		p.mark = m
		return nil
	})
}

// mapping_pattern:
//
//	| '{' '}'
//	| '{' double_star_pattern ','? '}'
//	| '{' items_pattern ',' double_star_pattern ','? '}'
//	| '{' items_pattern ','? '}'
func (p *parser) mappingPattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	mapping := func(items []*keyPattern, rest *Node) *Node {
		n := p.span("MatchMapping", m)
		for _, kp := range items {
			n.L1 = append(n.L1, kp.key)
			n.L2 = append(n.L2, kp.pattern)
		}
		n.NoID = rest == nil
		if rest != nil {
			n.ID = rest.ID
		}
		return n
	}
	if p.tok(tLBRACE) && p.tok(tRBRACE) {
		return mapping(nil, nil)
	}
	if !p.retry(m) {
		return nil
	}
	var rest *Node
	if p.tok(tLBRACE) && got(&rest, p.doubleStarPattern()) && p.optTok(tCOMMA) && p.tok(tRBRACE) {
		return mapping(nil, rest)
	}
	if !p.retry(m) {
		return nil
	}
	var items []*keyPattern
	if p.tok(tLBRACE) && gotSeq(&items, p.itemsPattern()) && p.tok(tCOMMA) &&
		got(&rest, p.doubleStarPattern()) && p.optTok(tCOMMA) && p.tok(tRBRACE) {
		return mapping(items, rest)
	}
	if !p.retry(m) {
		return nil
	}
	if p.tok(tLBRACE) && gotSeq(&items, p.itemsPattern()) && p.optTok(tCOMMA) && p.tok(tRBRACE) {
		return mapping(items, nil)
	}
	p.mark = m
	return nil
}

// items_pattern: ','.key_value_pattern+
func (p *parser) itemsPattern() []*keyPattern {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return gather(p, tCOMMA, p.keyValuePattern)
}

// key_value_pattern: (literal_expr | attr) ':' pattern
func (p *parser) keyValuePattern() *keyPattern {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var key, pat *Node
	if got(&key, group(p, func() *Node {
		mm := p.mark
		if a := p.literalExpr(); a != nil {
			return a
		}
		if !p.retry(mm) {
			return nil
		}
		return p.attr()
	})) && p.tok(tCOLON) && got(&pat, p.pattern()) {
		return &keyPattern{key, pat}
	}
	p.mark = m
	return nil
}

// double_star_pattern: '**' pattern_capture_target
func (p *parser) doubleStarPattern() *Node {
	return group(p, func() *Node {
		if p.tok(tDOUBLESTAR) {
			return p.patternCaptureTarget()
		}
		return nil
	})
}

// class_pattern:
//
//	| name_or_attr '(' ')'
//	| name_or_attr '(' positional_patterns ','? ')'
//	| name_or_attr '(' keyword_patterns ','? ')'
//	| name_or_attr '(' positional_patterns ',' keyword_patterns ','? ')'
//	| invalid_class_pattern
func (p *parser) classPattern() *Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	class := func(cls *Node, pats []*Node, kws []*keyPattern) *Node {
		n := p.span("MatchClass", m)
		n.A, n.L1 = cls, pats
		for _, kp := range kws {
			n.Names = append(n.Names, kp.key.ID)
			n.L2 = append(n.L2, kp.pattern)
		}
		return n
	}
	var cls *Node
	var pats []*Node
	var kws []*keyPattern
	if got(&cls, p.nameOrAttr()) && p.tok(tLPAR) && p.tok(tRPAR) {
		return class(cls, nil, nil)
	}
	if !p.retry(m) {
		return nil
	}
	if got(&cls, p.nameOrAttr()) && p.tok(tLPAR) && gotSeq(&pats, p.positionalPatterns()) && p.optTok(tCOMMA) &&
		p.tok(tRPAR) {
		return class(cls, pats, nil)
	}
	if !p.retry(m) {
		return nil
	}
	if got(&cls, p.nameOrAttr()) && p.tok(tLPAR) && gotSeq(&kws, p.keywordPatterns()) && p.optTok(tCOMMA) &&
		p.tok(tRPAR) {
		return class(cls, nil, kws)
	}
	if !p.retry(m) {
		return nil
	}
	if got(&cls, p.nameOrAttr()) && p.tok(tLPAR) && gotSeq(&pats, p.positionalPatterns()) && p.tok(tCOMMA) &&
		gotSeq(&kws, p.keywordPatterns()) && p.optTok(tCOMMA) && p.tok(tRPAR) {
		return class(cls, pats, kws)
	}
	if !p.retry(m) {
		return nil
	}
	if p.invalid {
		p.invalidClassPattern()
		p.mark = m
	}
	return nil
}

// positional_patterns: ','.pattern+
func (p *parser) positionalPatterns() []*Node {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return gather(p, tCOMMA, p.pattern)
}

// keyword_patterns: ','.keyword_pattern+
func (p *parser) keywordPatterns() []*keyPattern {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return gather(p, tCOMMA, p.keywordPattern)
}

// keyword_pattern: NAME '=' pattern
func (p *parser) keywordPattern() *keyPattern {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	var a, v *Node
	if got(&a, p.name()) && p.tok(tEQUAL) && got(&v, p.pattern()) {
		return &keyPattern{a, v}
	}
	p.mark = m
	return nil
}

// invalid_match_stmt:
//
//	| "match" subject_expr NEWLINE
//	| "match" subject_expr ':' NEWLINE !INDENT
func (p *parser) invalidMatchStmt() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	if p.softKeyword("match") != nil && p.subjectExpr() != nil && p.tok(tNEWLINE) {
		p.raise("expected ':'")
		return
	}
	if !p.retry(m) {
		return
	}
	var a *Node
	if got(&a, p.softKeyword("match")) && p.subjectExpr() != nil && p.tok(tCOLON) && p.newlineNoIndent() {
		p.raiseNoIndent("'match' statement", a.Lineno)
		return
	}
	p.mark = m
}

// invalid_case_block:
//
//	| "case" patterns guard? NEWLINE
//	| "case" patterns guard? ':' NEWLINE !INDENT
func (p *parser) invalidCaseBlock() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var g *Node
	if p.softKeyword("case") != nil && p.patterns() != nil && opt(p, &g, p.guard()) && p.tok(tNEWLINE) {
		p.raise("expected ':'")
		return
	}
	if !p.retry(m) {
		return
	}
	var a *Node
	if got(&a, p.softKeyword("case")) && p.patterns() != nil && opt(p, &g, p.guard()) && p.tok(tCOLON) &&
		p.newlineNoIndent() {
		p.raiseNoIndent("'case' statement", a.Lineno)
		return
	}
	p.mark = m
}

// invalid_as_pattern:
//
//	| or_pattern 'as' "_"
//	| or_pattern 'as' !NAME expression
func (p *parser) invalidAsPattern() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *Node
	if p.orPattern() != nil && p.tok(kwAs) && got(&a, p.softKeyword("_")) {
		p.raiseNode(a, "cannot use '_' as a target")
		return
	}
	if !p.retry(m) {
		return
	}
	if p.orPattern() != nil && p.tok(kwAs) && !p.at(tNAME) && got(&a, p.expression()) {
		p.raiseNode(a, "invalid pattern target")
		return
	}
	p.mark = m
}

// invalid_class_pattern: name_or_attr '(' invalid_class_argument_pattern
// invalid_class_argument_pattern: [positional_patterns ','] keyword_patterns ',' positional_patterns
func (p *parser) invalidClassPattern() {
	if !p.enter() {
		return
	}
	defer p.leave()
	m := p.mark
	var a *[]*Node
	if p.nameOrAttr() != nil && p.tok(tLPAR) && got(&a, group(p, func() *[]*Node {
		var l []*Node
		if p.maybe(group(p, func() *[]*Node {
			var pos []*Node
			if gotSeq(&pos, p.positionalPatterns()) && p.tok(tCOMMA) {
				return &pos
			}
			return nil
		}) != nil) && p.keywordPatterns() != nil && p.tok(tCOMMA) && gotSeq(&l, p.positionalPatterns()) {
			return &l
		}
		return nil
	})) {
		p.raiseRange(nodeLoc((*a)[0]), nodeLoc((*a)[len(*a)-1]), "positional patterns follow keyword patterns")
		return
	}
	p.mark = m
}
