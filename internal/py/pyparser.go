// Ported from CPython 3.12 (Parser/pegen.c, Parser/pegen_errors.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The parser half of ast.parse: the machinery of CPython's PEG parser
// (tokens read on demand, memoization, backtracking, the second pass with
// the invalid_* rules) and its error reporting. The grammar rules
// themselves, ported from Grammar/python.gram, are in the pyparse_*.go
// files.

package py

import (
	"strings"
	"unicode/utf8"
)

// ptok is a token as the parser keeps it (Token in pegen.h).
type ptok struct {
	typ                        int
	text                       string
	level                      int
	line, col, endLine, endCol int
	// meta is the debug text of an f-string replacement field, on the
	// token that ends its expression.
	meta *string
}

type memoKey struct{ mark, rule int }

type memoEntry struct {
	res any
	end int
}

// parser is CPython's Parser.
type parser struct {
	tz   *tokenizer
	toks []*ptok
	mark int
	// errInd is error_indicator: a rule failed with an error, and every
	// rule now fails at once.
	errInd bool
	// exc is the pending exception (PyErr_Occurred).
	exc *syntaxErr
	// invalid is call_invalid_rules: the second pass, which tries the
	// invalid_* alternatives for a specific message.
	invalid bool
	// barry is set by "from __future__ import barry_as_FLUFL".
	barry bool
	memo  map[memoKey]memoEntry
	// level is the depth of the rules being run, counted as CPython's
	// generated parser counts its functions, against maxStack.
	level int
	// filename is the name the source is compiled with; execInput is set
	// for exec mode, where an error shows the line of that file.
	filename  string
	execInput bool
}

// maxStack is the depth of rules at which CPython's parser gives up.
const maxStack = 6000

// parseSource is the parser of compile(src, filename, mode) for mode
// "exec" (a Module) or "eval" (an Expression): the tree, or the error.
func parseSource(src, filename, mode string) (*Node, *syntaxErr) {
	exec := mode == "exec"
	p := &parser{tz: newTokenizer(src, filename, exec), memo: map[memoKey]memoEntry{}, filename: filename, execInput: exec}
	start := p.file
	if !exec {
		start = p.eval
	}
	if tree := start(); tree != nil {
		return tree, nil
	}
	if p.exc != nil && !p.exc.syntax() {
		return nil, p.exc
	}
	// A second pass with the invalid_* rules looks for a specific error;
	// a generic one is reported where the first pass stopped.
	last := p.toks[len(p.toks)-1]
	p.memo = map[memoKey]memoEntry{}
	p.mark = 0
	p.invalid = true
	start()
	p.setSyntaxError(last)
	return nil, p.exc
}

// fillToken reads the next token into toks (_PyPegen_fill_token); false
// after a tokenizer error, which it raises.
func (p *parser) fillToken() bool {
	var rt rawToken
	typ := p.tz.next(&rt)
	if p.tz.exc != nil {
		p.exc, p.tz.exc = p.tz.exc, nil
	}
	t := &ptok{typ: typ, level: rt.level, line: rt.lineno, col: rt.col, endLine: rt.endLineno,
		endCol: rt.endCol, meta: rt.meta}
	if rt.start >= 0 {
		t.text = p.tz.buf[rt.start:rt.end]
	}
	if typ == tNAME {
		t.typ = keywordType(t.text)
	}
	p.toks = append(p.toks, t)
	if typ == tERRORTOKEN {
		p.tokenizerError()
		return false
	}
	return true
}

// tokenizerError raises the error for a tokenizer failure that did not
// raise one itself (_Pypegen_tokenizer_error).
func (p *parser) tokenizerError() {
	if p.exc != nil {
		return
	}
	p.errInd = true
	class, msg, col := "", "", -1
	switch p.tz.done {
	case eEOF:
		if p.tz.level > 0 {
			p.raiseUnclosed()
		} else {
			p.raise("unexpected EOF while parsing")
		}
		return
	case eDEDENT:
		p.raiseClass("IndentationError", "unindent does not match any outer indentation level")
		return
	case eTABSPACE:
		class, msg = "TabError", "inconsistent use of tabs and spaces in indentation"
	case eTOODEEP:
		class, msg = "IndentationError", "too many levels of indentation"
	case eLINECONT:
		col = p.tz.cur - p.tz.bufStart - 1
		msg = "unexpected character after line continuation character"
	default:
		msg = "unknown parsing error"
	}
	p.raiseLocation(class, p.tz.lineno, max(col, 0), p.tz.lineno, -1, msg)
}

// raiseUnclosed reports the innermost bracket left open
// (raise_unclosed_parentheses_error).
func (p *parser) raiseUnclosed() {
	lv := p.tz.level - 1
	line := p.tz.parenlinenostack[lv]
	p.raiseLocation("", line, p.tz.parencolstack[lv], line, -1, "'"+string(p.tz.parenstack[lv])+"' was never closed")
}

// currentPos is CURRENT_POS: the line or the column the tokenizer has reached.
const currentPos = -5

// raiseKnown raises an error of the class ("" for SyntaxError) at a
// location given as the parser computes it: lines, and 1-based byte columns
// (or currentPos), unless one is already raised
// (_PyPegen_raise_error_known_location). The columns become the character
// offsets the SyntaxError reports, counted in the line it shows.
func (p *parser) raiseKnown(class string, line, col, endLine, endCol int, msg string) {
	if p.errInd && p.exc != nil {
		return
	}
	p.errInd = true
	if endLine == currentPos {
		endLine = p.tz.lineno
	}
	if endCol == currentPos {
		endCol = p.tz.cur - p.tz.lineStart
	}
	text := p.errorLine(line)
	e := &syntaxErr{msg: msg, class: class, line: line, offset: byteToCharOffset(text, col), endLine: endLine,
		endOffset: endCol, text: text, hasText: true}
	if endCol > 0 {
		e.endOffset = byteToCharOffset(text, endCol)
	}
	p.exc = e
}

// errorLine is the text of line lineno a SyntaxError shows: for exec input
// the line of the file named, when there is one; else the line the
// tokenizer is on when the error is not on an earlier one, or the line of
// the source read so far (get_error_line_from_tokenizer_buffers).
func (p *parser) errorLine(lineno int) string {
	if p.execInput {
		if line, ok := fileLine(p.filename, lineno); ok {
			return line
		}
	}
	t := p.tz
	if t.lineno <= lineno && t.inp > t.bufStart {
		return t.buf[t.lineStart:t.inp]
	}
	cur, end := 0, t.inp
	if end < cur {
		end = len(t.buf)
	}
	for i := 0; i < lineno-1; i++ {
		nl := strings.IndexByte(t.buf[cur:], '\n')
		if nl < 0 || cur+nl+1 > end {
			break
		}
		cur += nl + 1
	}
	if nl := strings.IndexByte(t.buf[cur:], '\n'); nl >= 0 {
		return t.buf[cur : cur+nl]
	}
	return t.buf[cur:]
}

// byteToCharOffset is _PyPegen_byte_offset_to_character_offset: the number
// of characters in the first col bytes of line, decoded with errors
// replaced; a column past the end counts the terminating NUL.
func byteToCharOffset(line string, col int) int {
	switch {
	case col <= 0:
		return 0
	case col > len(line):
		return utf8.RuneCountInString(line) + 1
	}
	return utf8.RuneCountInString(line[:col])
}

// raiseLocation is RAISE_ERROR_KNOWN_LOCATION: the columns are 0-based, as
// nodes and tokens hold them.
func (p *parser) raiseLocation(class string, line, col, endLine, endCol int, msg string) {
	if col != currentPos {
		col++
	}
	if endCol != currentPos {
		endCol++
	}
	p.raiseKnown(class, line, col, endLine, endCol, msg)
}

// loc is the location of a node or a token, as the error macros read it.
type loc struct{ line, col, endLine, endCol int }

func nodeLoc(n *Node) loc { return loc{n.Lineno, n.ColOffset, n.EndLineno, n.EndColOffset} }

func tokLoc(t *ptok) loc { return loc{t.line, t.col, t.endLine, t.endCol} }

// raiseNode is RAISE_SYNTAX_ERROR_KNOWN_LOCATION: an error over a node.
func (p *parser) raiseNode(a *Node, msg string) { p.raiseRange(nodeLoc(a), nodeLoc(a), msg) }

// raiseTok is RAISE_SYNTAX_ERROR_KNOWN_LOCATION: an error over a token.
func (p *parser) raiseTok(t *ptok, msg string) { p.raiseRange(tokLoc(t), tokLoc(t), msg) }

// raiseRange is RAISE_SYNTAX_ERROR_KNOWN_RANGE: an error from the start of
// a to the end of b.
func (p *parser) raiseRange(a, b loc, msg string) {
	p.raiseLocation("", a.line, a.col, b.endLine, b.endCol, msg)
}

// raiseStartingFrom is RAISE_SYNTAX_ERROR_STARTING_FROM: an error from the
// start of a to where the tokenizer is.
func (p *parser) raiseStartingFrom(a loc, msg string) {
	p.raiseLocation("", a.line, a.col, currentPos, currentPos, msg)
}

// raiseError is _PyPegen_raise_error: an error over the last token read, or
// with useMark over the token at the mark.
func (p *parser) raiseError(class string, useMark bool, msg string) {
	if p.errInd && p.exc != nil {
		return
	}
	if len(p.toks) == 0 {
		p.raiseKnown(class, 0, 0, 0, -1, msg)
		return
	}
	if useMark && p.mark == len(p.toks) && !p.fillToken() {
		p.errInd = true
		return
	}
	t := p.toks[len(p.toks)-1]
	if useMark {
		t = p.toks[p.mark]
	}
	col, endCol := t.col+1, -1
	if t.col == -1 {
		col = 0
		if p.tz.cur != p.tz.bufStart {
			col = p.tz.cur - p.tz.lineStart
		}
	}
	if t.endCol != -1 {
		endCol = t.endCol + 1
	}
	p.raiseKnown(class, t.line, col, t.endLine, endCol, msg)
}

// raise is RAISE_SYNTAX_ERROR: an error over the last token read.
func (p *parser) raise(msg string) { p.raiseError("", false, msg) }

// raiseClass is raise for an error of the class (RAISE_INDENTATION_ERROR).
func (p *parser) raiseClass(class, msg string) { p.raiseError(class, false, msg) }

// raiseOnNextToken is RAISE_SYNTAX_ERROR_ON_NEXT_TOKEN: an error over the
// token at the mark.
func (p *parser) raiseOnNextToken(msg string) { p.raiseError("", true, msg) }

// setSyntaxError settles the error after the second pass
// (_Pypegen_set_syntax_error); last is the last token the first pass read.
func (p *parser) setSyntaxError(last *ptok) {
	if p.exc != nil {
		// A tokenizer error later in the source takes priority over a
		// SyntaxError the parser raised.
		if p.tz.done == eOK && p.exc.syntax() {
			p.checkFullSource()
		}
		return
	}
	if last.typ == tERRORTOKEN && p.tz.done == eEOF {
		if p.tz.level > 0 {
			p.raiseUnclosed()
		} else {
			p.raise("unexpected EOF while parsing")
		}
		return
	}
	if last.typ == tINDENT {
		p.raiseClass("IndentationError", "unexpected indent")
		return
	}
	if last.typ == tDEDENT {
		p.raiseClass("IndentationError", "unexpected unindent")
		return
	}
	// A generic error is reported where the first pass stopped, not
	// where the second pass's invalid_* rules reached.
	p.raiseRange(tokLoc(last), tokLoc(last), "invalid syntax")
	p.checkFullSource()
}

// checkFullSource tokenizes the rest of the source: a tokenizer error
// there replaces the error raised (outside an f-string), and a bracket left
// open before the error's line is reported instead
// (_PyPegen_tokenize_full_source_to_check_for_errors).
func (p *parser) checkFullSource() {
	saved := p.exc
	p.exc = nil
	errLine := p.toks[len(p.toks)-1].line
	for {
		var rt rawToken
		typ := p.tz.next(&rt)
		if p.tz.exc != nil {
			p.exc, p.tz.exc = p.tz.exc, nil
		}
		if typ == tERRORTOKEN {
			if p.exc == nil && p.tz.level != 0 && errLine > p.tz.parenlinenostack[p.tz.level-1] {
				p.raiseUnclosed()
			}
			break
		}
		if typ == tENDMARKER {
			break
		}
	}
	if p.exc == nil || p.tz.modeIdx > 0 {
		p.exc = saved
	}
}

// fillMark makes sure the token at the mark has been read; false after an
// error.
func (p *parser) fillMark() bool {
	if p.errInd {
		return false
	}
	if p.mark == len(p.toks) && !p.fillToken() {
		p.errInd = true
		return false
	}
	return true
}

// enter starts a rule, or one of the helper rules CPython generates for a
// group, a repetition or a lookahead: it counts the rule's depth against
// the stack limit, fails after an error, and reads the token at the rule's
// start. A rule that entered leaves when it returns.
func (p *parser) enter() bool {
	if p.level == maxStack {
		// _Pypegen_stack_overflow: a MemoryError replaces any error.
		p.errInd = true
		p.exc = &syntaxErr{msg: "Parser stack overflowed - Python source too complex to parse", class: "MemoryError"}
	}
	p.level++
	if !p.fillMark() {
		p.level--
		return false
	}
	return true
}

// leave ends a rule that entered.
func (p *parser) leave() { p.level-- }

// seed stands for the call a left-recursive rule's _raw function makes to
// the rule to read the result grown so far, a call that only counts
// towards the stack limit.
func (p *parser) seed() bool {
	if !p.enter() {
		return false
	}
	p.leave()
	return true
}

// retry resets the mark after a failed alternative; false after an error.
func (p *parser) retry(m int) bool {
	p.mark = m
	return !p.errInd
}

// expect is _PyPegen_expect_token: the token at the mark if it has the type.
func (p *parser) expect(typ int) *ptok {
	if !p.fillMark() {
		return nil
	}
	t := p.toks[p.mark]
	if t.typ != typ {
		return nil
	}
	p.mark++
	return t
}

// tok matches a token of the type.
func (p *parser) tok(typ int) bool { return p.expect(typ) != nil }

// optTok matches an optional token: false only after an error.
func (p *parser) optTok(typ int) bool {
	p.expect(typ)
	return !p.errInd
}

// at is the lookahead &TOKEN: whether the token at the mark has the type.
func (p *parser) at(typ int) bool {
	m := p.mark
	ok := p.tok(typ)
	p.mark = m
	return ok
}

// atAny is the lookahead &(T1 | T2 | ...), a helper rule of its own when
// there are several tokens.
func (p *parser) atAny(types ...int) bool {
	if len(types) == 1 {
		return p.at(types[0])
	}
	if !p.enter() {
		return false
	}
	defer p.leave()
	for _, typ := range types {
		if p.at(typ) {
			return true
		}
	}
	return false
}

// maybe ends an optional item: whatever it matched, it fails only after an
// error.
func (p *parser) maybe(bool) bool { return !p.errInd }

// ahead runs f as a lookahead: the mark is restored afterwards.
func (p *parser) ahead(f func() bool) bool {
	m := p.mark
	ok := f()
	p.mark = m
	return ok
}

// expectForced is &&TOKEN: the token, or the error "expected '...'".
func (p *parser) expectForced(typ int, text string) bool {
	if !p.fillMark() {
		return false
	}
	t := p.toks[p.mark]
	if t.typ != typ {
		p.raiseTok(t, "expected '"+text+"'")
		return false
	}
	p.mark++
	return true
}

// softKeyword matches the soft keyword kw: a NAME token with that text.
func (p *parser) softKeyword(kw string) *Node {
	if !p.fillMark() {
		return nil
	}
	if t := p.toks[p.mark]; t.typ != tNAME || t.text != kw {
		return nil
	}
	return p.name()
}

// atSoftKeyword is the lookahead &"kw".
func (p *parser) atSoftKeyword(kw string) bool {
	return p.ahead(func() bool { return p.softKeyword(kw) != nil })
}

// got stores v in *dst and reports whether the item matched.
func got[T any](dst **T, v *T) bool {
	*dst = v
	return v != nil
}

// gotSeq is got for an item that is a sequence.
func gotSeq[T any](dst *[]T, v []T) bool {
	*dst = v
	return v != nil
}

// opt stores an optional item; it fails only after an error.
func opt[T any](p *parser, dst **T, v *T) bool {
	*dst = v
	return !p.errInd
}

// optSeq is opt for an item that is a sequence.
func optSeq[T any](p *parser, dst *[]T, v []T) bool {
	*dst = v
	return !p.errInd
}

// group runs a parenthesised group of items as a rule of its own: on
// failure the mark is reset.
func group[T any](p *parser, f func() *T) *T {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	if r := f(); r != nil {
		return r
	}
	p.mark = m
	return nil
}

// sub runs the items f matches as a helper rule of its own (a group CPython
// generates a function for): it reports whether they matched, resetting the
// mark if not.
func (p *parser) sub(f func() bool) bool {
	if !p.enter() {
		return false
	}
	defer p.leave()
	m := p.mark
	if f() {
		return true
	}
	p.mark = m
	return false
}

// loop0 is e*: the items, never nil but after an error.
func loop0[T any](p *parser, f func() *T) []*T {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	return repeat(p, f)
}

// repeat reads items with f as long as it matches.
func repeat[T any](p *parser, f func() *T) []*T {
	m := p.mark
	out := []*T{}
	for {
		x := f()
		if x == nil {
			break
		}
		out = append(out, x)
		m = p.mark
	}
	p.mark = m
	return out
}

// loop1 is e+: nil when there is no item.
func loop1[T any](p *parser, f func() *T) []*T {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	out := repeat(p, f)
	if len(out) == 0 || p.errInd {
		return nil
	}
	return out
}

// gather is sep.e+: one or more items separated by the token sep, the
// first read by the gather rule, the others by a loop rule within it.
func gather[T any](p *parser, sep int, f func() *T) []*T {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	m := p.mark
	first := f()
	if first == nil || !p.enter() {
		p.mark = m
		return nil
	}
	defer p.leave()
	out := []*T{first}
	m = p.mark
	for p.tok(sep) {
		x := f()
		if x == nil {
			break
		}
		out = append(out, x)
		m = p.mark
	}
	p.mark = m
	return out
}

// memo runs the body of a memoized rule, or returns its result recorded
// for this position.
func memo[T any](p *parser, rule int, f func() *T) *T {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	k := memoKey{p.mark, rule}
	if e, ok := p.memo[k]; ok {
		p.mark = e.end
		return e.res.(*T)
	}
	r := f()
	p.memo[k] = memoEntry{r, p.mark}
	return r
}

// The memoized rules.
const (
	rSimpleStmt = iota
	rBlock
	rClosedPattern
	rStarPattern
	rTypeParam
	rExpression
	rStarExpression
	rDisjunction
	rConjunction
	rInversion
	rBitwiseOr
	rBitwiseXor
	rBitwiseAnd
	rShiftExpr
	rSum
	rTerm
	rFactor
	rAwaitPrimary
	rPrimary
	rStrings
	rArguments
	rStarTarget
	rTargetWithStarAtom
	rTPrimary
	rDelTarget
	rDottedName
	rAttr
	rInvalidNamedExpression
)

// lastToken is the last token read before the mark that is not a NEWLINE,
// INDENT, DEDENT or ENDMARKER (_PyPegen_get_last_nonnwhitespace_token).
func (p *parser) lastToken() *ptok {
	var t *ptok
	for m := p.mark - 1; m >= 0; m-- {
		t = p.toks[m]
		if t.typ != tENDMARKER && t.typ != tNEWLINE && t.typ != tINDENT && t.typ != tDEDENT {
			break
		}
	}
	return t
}

// Keyword token types, after the token types.
const (
	kwFalse = tERRORTOKEN + 1 + iota
	kwNone
	kwTrue
	kwAnd
	kwAs
	kwAssert
	kwBreak
	kwClass
	kwContinue
	kwDef
	kwDel
	kwElif
	kwElse
	kwExcept
	kwFinally
	kwFor
	kwFrom
	kwGlobal
	kwIf
	kwImport
	kwIn
	kwIs
	kwLambda
	kwNonlocal
	kwNot
	kwOr
	kwPass
	kwRaise
	kwReturn
	kwTry
	kwWhile
	kwWith
	kwYield
)

var keywordTypes = map[string]int{
	"False": kwFalse, "None": kwNone, "True": kwTrue, "and": kwAnd, "as": kwAs, "assert": kwAssert,
	"break": kwBreak, "class": kwClass, "continue": kwContinue, "def": kwDef, "del": kwDel, "elif": kwElif,
	"else": kwElse, "except": kwExcept, "finally": kwFinally, "for": kwFor, "from": kwFrom,
	"global": kwGlobal, "if": kwIf, "import": kwImport, "in": kwIn, "is": kwIs, "lambda": kwLambda,
	"nonlocal": kwNonlocal, "not": kwNot, "or": kwOr, "pass": kwPass, "raise": kwRaise,
	"return": kwReturn, "try": kwTry, "while": kwWhile, "with": kwWith, "yield": kwYield,
}

// keywordType is the token type of a NAME token: a keyword's own type, or
// NAME.
func keywordType(text string) int {
	if t, ok := keywordTypes[text]; ok {
		return t
	}
	return tNAME
}

// The soft keywords.
var softKeywords = map[string]bool{"_": true, "case": true, "match": true, "type": true}
