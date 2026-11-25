// Ported from CPython 3.12 (Parser/action_helpers.c, Parser/pegen.c, Parser/string_parser.c) to
// Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The actions of the grammar rules: building the nodes of the tree, the
// values of number and string literals, f-strings, and the helpers the
// invalid_* rules use to name what went wrong.

package py

import (
	"errors"
	"github.com/jackrosenthal/kajiki-go/internal/pyuni"
	"math/big"
	"strconv"
	"strings"
)

// The helper values of the rules (pegen.h).
type (
	kvPair  struct{ key, value *Node }
	cmpPair struct {
		op   string
		expr *Node
	}
	nameDefault  struct{ arg, value *Node }
	slashDefault struct {
		plain    []*Node
		defaults []*nameDefault
	}
	starEtc struct {
		vararg *Node
		kwonly []*nameDefault
		kwarg  *Node
	}
	kwOrStar struct {
		elem      *Node
		isKeyword bool
	}
	keyPattern struct{ key, pattern *Node }
	// withMeta is a node with the debug text of an f-string replacement
	// field (ResultTokenWithMetadata).
	withMeta struct {
		res  *Node
		meta *string
	}
	augOp struct{ op string }
)

// span is a node of the kind spanning the tokens from start to the last one
// read (EXTRA).
func (p *parser) span(kind string, start int) *Node {
	s, e := p.toks[start], p.lastToken()
	return &Node{Kind: kind, Lineno: s.line, ColOffset: s.col, EndLineno: e.endLine, EndColOffset: e.endCol, HasPos: true}
}

// tokNode is a node of the kind spanning one token.
func tokNode(kind string, t *ptok) *Node {
	return &Node{Kind: kind, Lineno: t.line, ColOffset: t.col, EndLineno: t.endLine, EndColOffset: t.endCol, HasPos: true}
}

// nameNode is the Name node of a NAME token, its identifier normalised to
// NFKC (_PyPegen_name_from_token).
func nameNode(t *ptok) *Node {
	n := tokNode("Name", t)
	n.ID, n.Ctx = t.text, "Load"
	if !isASCII(t.text) {
		n.ID = pyuni.NFKC(t.text)
	}
	return n
}

// name matches a NAME token (_PyPegen_name_token).
func (p *parser) name() *Node {
	t := p.expect(tNAME)
	if t == nil {
		return nil
	}
	return nameNode(t)
}

// softKeywordToken matches any soft keyword (SOFT_KEYWORD).
func (p *parser) softKeywordToken() *Node {
	t := p.expect(tNAME)
	if t == nil || !softKeywords[t.text] {
		return nil
	}
	return nameNode(t)
}

// setCtx is a copy of e with the context ctx, for the target kinds
// (_PyPegen_set_expr_context).
func setCtx(e *Node, ctx string) *Node {
	switch e.Kind {
	case "Name", "Subscript", "Attribute":
		c := *e
		c.Ctx = ctx
		return &c
	case "Tuple", "List":
		c := *e
		c.Ctx = ctx
		c.L1 = make([]*Node, len(e.L1))
		for i, x := range e.L1 {
			c.L1[i] = setCtx(x, ctx)
		}
		return &c
	case "Starred":
		c := *e
		c.Ctx = ctx
		c.A = setCtx(e.A, ctx)
		return &c
	}
	return e
}

// exprNames are the words for the kinds of expressions in error messages
// (_PyPegen_get_expr_name).
var exprNames = map[string]string{
	"Attribute": "attribute", "Subscript": "subscript", "Starred": "starred", "Name": "name",
	"List": "list", "Tuple": "tuple", "Lambda": "lambda", "Call": "function call",
	"BoolOp": "expression", "BinOp": "expression", "UnaryOp": "expression",
	"GeneratorExp": "generator expression", "Yield": "yield expression", "YieldFrom": "yield expression",
	"Await": "await expression", "ListComp": "list comprehension", "SetComp": "set comprehension",
	"DictComp": "dict comprehension", "Dict": "dict literal", "Set": "set display",
	"JoinedStr": "f-string expression", "FormattedValue": "f-string expression",
	"Compare": "comparison", "IfExp": "conditional expression", "NamedExpr": "named expression",
}

func exprName(e *Node) string {
	if e.Kind == "Constant" {
		switch e.Value {
		case none:
			return "None"
		case falseV:
			return "False"
		case trueV:
			return "True"
		case ellipsisV{}:
			return "ellipsis"
		}
		return "literal"
	}
	return exprNames[e.Kind]
}

// Target kinds for invalidTarget.
const (
	starTargets = iota
	delTargets
	forTargets
)

// invalidTarget is the part of e that cannot be an assignment, deletion or
// for target, or nil (_PyPegen_get_invalid_target).
func invalidTarget(e *Node, kind int) *Node {
	if e == nil {
		return nil
	}
	switch e.Kind {
	case "List", "Tuple":
		for _, x := range e.L1 {
			if c := invalidTarget(x, kind); c != nil {
				return c
			}
		}
		return nil
	case "Starred":
		if kind == delTargets {
			return e
		}
		return invalidTarget(e.A, kind)
	case "Compare":
		// The "a in b" of "for a in b" parses as a comparison: its left
		// side is the target.
		if kind == forTargets {
			if e.Ops[0] == "In" {
				return invalidTarget(e.A, kind)
			}
			return nil
		}
		return e
	case "Name", "Subscript", "Attribute":
		return nil
	}
	return e
}

// raiseInvalidTarget is RAISE_SYNTAX_ERROR_INVALID_TARGET: "cannot assign
// to ..." or "cannot delete ..." for the invalid part of e, if any.
func (p *parser) raiseInvalidTarget(kind int, e *Node) {
	t := invalidTarget(e, kind)
	if t == nil {
		return
	}
	verb := "cannot assign to "
	if kind == delTargets {
		verb = "cannot delete "
	}
	p.raiseNode(t, verb+exprName(t))
}

// checkLegacyStmt reports whether name is print or exec, the statements of
// Python 2.
func checkLegacyStmt(name *Node) bool {
	return name.Kind == "Name" && (name.ID == "print" || name.ID == "exec")
}

// number matches a NUMBER token and makes its Constant
// (_PyPegen_number_token).
func (p *parser) number() *Node {
	t := p.expect(tNUMBER)
	if t == nil {
		return nil
	}
	v, err := parseNumber(t.text)
	if err != nil {
		// The error leaves out the columns of a literal that may be
		// thousands of digits long.
		p.raiseLocation("", t.line, -1, t.endLine, -1, err.Error())
		return nil
	}
	n := tokNode("Constant", t)
	n.Value = v
	return n
}

// parseNumber converts the text of a NUMBER token to its value.
func parseNumber(text string) (Value, error) {
	s := strings.ReplaceAll(text, "_", "")
	last := s[len(s)-1]
	if last == 'j' || last == 'J' {
		f, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil && !isRangeErr(err) {
			return nil, err
		}
		return complexV(complex(0, f)), nil
	}
	if len(s) > 1 && s[0] == '0' && strings.IndexByte("xXoObB", s[1]) >= 0 {
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[s[1]]
		n, _ := new(big.Int).SetString(s[2:], base)
		return intV{n}, nil
	}
	if strings.ContainsAny(s, ".eE") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil && !isRangeErr(err) {
			return nil, err
		}
		return floatV(f), nil
	}
	if len(s) > maxStrDigits {
		return nil, errors.New(digitLimitErr(len(s)).Message +
			" - Consider hexadecimal for huge integer literals to avoid decimal conversion limits.")
	}
	n, _ := new(big.Int).SetString(s, 10)
	return intV{n}, nil
}

func isRangeErr(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}

// warnInvalidEscape issues the SyntaxWarning for the first invalid escape
// sequence of a literal on the given line, in the first pass only. The
// tokenizer has already warned about a backslash before a brace in an
// f-string.
func (p *parser) warnInvalidEscape(invalid string, fstring bool, line int) {
	if invalid == "" || p.invalid {
		return
	}
	c := invalid[0]
	if fstring && (c == '{' || c == '}') {
		return
	}
	if c >= '4' && c <= '7' {
		warnExplicit("SyntaxWarning", "invalid octal escape sequence '\\"+invalid+"'", p.filename, line)
		return
	}
	warnExplicit("SyntaxWarning", "invalid escape sequence '\\"+string(c)+"'", p.filename, line)
}

// decodeString decodes the body of a str literal or f-string part
// (_PyPegen_decode_string); an error is raised as the "(unicode error)"
// SyntaxError.
func (p *parser) decodeString(raw bool, s string, fstring bool, line int) (string, bool) {
	if raw || strings.IndexByte(s, '\\') < 0 {
		return s, true
	}
	v, invalid, err := decodeStrBody(s, false)
	if err != nil {
		p.raiseDecodeError(err)
		return "", false
	}
	p.warnInvalidEscape(invalid, fstring, line)
	return v, true
}

// raiseDecodeError raises the error of decoding the escapes of a literal as
// a SyntaxError (_Pypegen_raise_decode_error).
func (p *parser) raiseDecodeError(err error) {
	kind, text := "value error", err.Error()
	switch e := err.(type) {
	case *codecError:
		kind = "unicode error"
	case *excV:
		text = e.Message
	}
	p.raise("(" + kind + ") " + text)
}

// constantFromString is the Constant of a STRING token
// (_PyPegen_constant_from_string and _PyPegen_parse_string).
func (p *parser) constantFromString(t *ptok) *Node {
	s := t.text
	i := 0
	bytesMode, rawMode := false, false
	for ; s[i] != '\'' && s[i] != '"'; i++ {
		switch s[i] {
		case 'b', 'B':
			bytesMode = true
		case 'r', 'R':
			rawMode = true
		}
	}
	q := 1
	if len(s)-i >= 6 && s[i+1] == s[i] && s[i+2] == s[i] {
		q = 3
	}
	body := s[i+q : len(s)-q]
	n := tokNode("Constant", t)
	if s[0] == 'u' {
		n.ConstKind = "u"
	}
	if !bytesMode {
		v, ok := p.decodeString(rawMode, body, false, t.line)
		if !ok {
			return nil
		}
		n.Value = strV(v)
		return n
	}
	if !isASCII(body) {
		p.raiseTok(t, "bytes can only contain ASCII literal characters")
		return nil
	}
	if rawMode || strings.IndexByte(body, '\\') < 0 {
		n.Value = bytesV(body)
		return n
	}
	v, invalid, err := decodeStrBody(body, true)
	if err != nil {
		p.raiseDecodeError(err)
		return nil
	}
	p.warnInvalidEscape(invalid, false, t.line)
	n.Value = bytesV(v)
	return n
}

// concatenateStrings joins adjacent string literals and f-strings into one
// Constant or JoinedStr spanning start to the last token read
// (_PyPegen_concatenate_strings).
func (p *parser) concatenateStrings(strs []*Node, start int) *Node {
	return p.concatAt(strs, p.span("JoinedStr", start))
}

// concatAt is concatenateStrings with the position of the result given by
// at.
func (p *parser) concatAt(strs []*Node, at *Node) *Node {
	fstringFound, unicodeFound, bytesFound := false, false, false
	for _, e := range strs {
		switch e.Kind {
		case "Constant":
			if _, ok := e.Value.(bytesV); ok {
				bytesFound = true
			} else {
				unicodeFound = true
			}
		default:
			fstringFound = true
		}
	}
	if (unicodeFound || fstringFound) && bytesFound {
		p.raise("cannot mix bytes and nonbytes literals")
		return nil
	}
	if bytesFound {
		var b strings.Builder
		for _, e := range strs {
			b.WriteString(string(e.Value.(bytesV)))
		}
		n := *at
		n.Kind, n.Value, n.ID = "Constant", bytesV(b.String()), strs[0].ID
		return &n
	}
	if !fstringFound && len(strs) == 1 {
		return strs[0]
	}

	var flat []*Node
	for _, e := range strs {
		if e.Kind == "JoinedStr" {
			flat = append(flat, e.L1...)
		} else {
			flat = append(flat, e)
		}
	}
	// Fold runs of constants; drop empty ones next to f-string parts.
	var values []*Node
	for i := 0; i < len(flat); i++ {
		e := flat[i]
		if e.Kind == "Constant" {
			if i+1 < len(flat) && flat[i+1].Kind == "Constant" {
				first, last := e, e
				var b strings.Builder
				j := i
				for ; j < len(flat) && flat[j].Kind == "Constant"; j++ {
					b.WriteString(string(flat[j].Value.(strV)))
					last = flat[j]
				}
				i = j - 1
				e = &Node{Kind: "Constant", Value: strV(b.String()), ID: first.ID, Lineno: first.Lineno, ColOffset: first.ColOffset,
					EndLineno: last.EndLineno, EndColOffset: last.EndColOffset, HasPos: true}
			}
			if fstringFound && e.Value.(strV) == "" {
				continue
			}
		}
		values = append(values, e)
	}
	if !fstringFound {
		return values[0]
	}
	n := *at
	n.Kind, n.L1 = "JoinedStr", values
	return &n
}

// joinedStr is the JoinedStr of an f-string from its FSTRING_START token a
// to its FSTRING_END token b: debug fields are flattened, literal parts
// decoded and empty ones dropped (_PyPegen_joined_str).
func (p *parser) joinedStr(a *ptok, parts []*Node, b *ptok) *Node {
	raw := strings.ContainsAny(a.text, "rR")
	var values []*Node
	for _, item := range parts {
		switch item.Kind {
		case "JoinedStr":
			values = append(values, item.L1...)
			continue
		case "Constant":
			s := string(item.Value.(strV))
			if s == "{{" || s == "}}" {
				s = s[:1]
			}
			v, ok := p.decodeString(raw, s, true, item.Lineno)
			if !ok {
				return nil
			}
			if v == "" {
				continue
			}
			c := *item
			c.Value = strV(v)
			item = &c
		}
		values = append(values, item)
	}
	return &Node{Kind: "JoinedStr", L1: values, Lineno: a.line, ColOffset: a.col, EndLineno: b.endLine, EndColOffset: b.endCol,
		HasPos: true}
}

// constantFromToken is the Constant of the text of an f-string part
// (_PyPegen_constant_from_token).
func constantFromToken(t *ptok) *Node {
	n := tokNode("Constant", t)
	n.Value = strV(t.text)
	return n
}

// decodedConstantFromToken is the Constant of a part of a format spec,
// its escapes decoded even in a raw f-string
// (_PyPegen_decoded_constant_from_token). A decoding error here is raised
// as the UnicodeDecodeError itself, not as a SyntaxError.
func (p *parser) decodedConstantFromToken(t *ptok) *Node {
	v := t.text
	if strings.IndexByte(v, '\\') >= 0 {
		d, invalid, err := decodeStrBody(v, false)
		if err != nil {
			if !p.errInd || p.exc == nil {
				p.errInd = true
				p.exc = &syntaxErr{msg: err.Error(), leaf: err}
			}
			return nil
		}
		p.warnInvalidEscape(invalid, true, t.line)
		v = d
	}
	n := tokNode("Constant", t)
	n.Value = strV(v)
	return n
}

// checkFStringConversion checks that the conversion character follows the
// '!' directly (_PyPegen_check_fstring_conversion).
func (p *parser) checkFStringConversion(bang *ptok, conv *Node) *withMeta {
	if bang.line != conv.Lineno || bang.endCol != conv.ColOffset {
		p.raiseRange(tokLoc(bang), nodeLoc(conv), "f-string: conversion type must come right after the exclamanation mark")
		return nil
	}
	return &withMeta{res: conv, meta: bang.meta}
}

// setupFullFormatSpec is the JoinedStr of a format spec from ':' at
// colon (_PyPegen_setup_full_format_spec).
func (p *parser) setupFullFormatSpec(colon *ptok, spec []*Node, start int) *withMeta {
	// An empty format spec is an empty JoinedStr, as in 3.11.
	if len(spec) == 1 && spec[0].Kind == "Constant" {
		if s, ok := spec[0].Value.(strV); ok && s == "" {
			spec = nil
		}
	}
	at := p.span("JoinedStr", start)
	var res *Node
	if len(spec) == 0 || len(spec) == 1 && spec[0].Kind == "Constant" {
		at.L1 = spec
		res = at
	} else if res = p.concatAt(spec, at); res == nil {
		return nil
	}
	return &withMeta{res: res, meta: colon.meta}
}

// formattedValue is the FormattedValue of a replacement field, preceded by
// the Constant of its text for a debug field (f"{x=}")
// (_PyPegen_formatted_value).
func (p *parser) formattedValue(expr *Node, debug *ptok, conv, format *withMeta, rbrace *ptok, start int) *Node {
	conversion := -1
	if conv != nil {
		id := conv.res.ID
		if id != "s" && id != "r" && id != "a" {
			p.raiseNode(conv.res, "f-string: invalid conversion character '"+id+
				"': expected 's', 'r', or 'a'")
			return nil
		}
		conversion = int(id[0])
	} else if debug != nil && format == nil {
		// A debug field without a conversion uses !r.
		conversion = 'r'
	}
	fv := p.span("FormattedValue", start)
	fv.A, fv.Conversion = expr, conversion
	if format != nil {
		fv.B = format.res
	}
	if debug == nil {
		return fv
	}
	var endLine, endCol int
	var meta *string
	switch {
	case conv != nil:
		endLine, endCol, meta = conv.res.Lineno, conv.res.ColOffset, conv.meta
	case format != nil:
		endLine, endCol, meta = format.res.Lineno, format.res.ColOffset+1, format.meta
	default:
		endLine, endCol, meta = fv.EndLineno, fv.EndColOffset, rbrace.meta
	}
	text := &Node{Kind: "Constant", Lineno: fv.Lineno, ColOffset: fv.ColOffset + 1, EndLineno: endLine, EndColOffset: endCol - 1,
		HasPos: true, Value: strV("")}
	if meta != nil {
		text.Value = strV(*meta)
	}
	return &Node{Kind: "JoinedStr", L1: []*Node{text, fv}, Lineno: fv.Lineno, ColOffset: fv.ColOffset, EndLineno: endLine,
		EndColOffset: endCol, HasPos: true}
}

// makeArguments builds the arguments node of a parameter list
// (_PyPegen_make_arguments).
func makeArguments(slashNoDefault []*Node, slashWithDefault *slashDefault, plain []*Node,
	withDefault []*nameDefault, star *starEtc) *Node {
	args := &Node{Kind: "arguments"}
	switch {
	case slashNoDefault != nil:
		args.L1 = slashNoDefault
	case slashWithDefault != nil:
		args.L1 = append(append([]*Node{}, slashWithDefault.plain...), defaultNames(slashWithDefault.defaults)...)
	}
	args.L2 = append(append([]*Node{}, plain...), defaultNames(withDefault)...)
	if slashWithDefault != nil {
		args.Body = defaultValues(slashWithDefault.defaults)
	}
	args.Body = append(args.Body, defaultValues(withDefault)...)
	if star != nil {
		args.A, args.B = star.vararg, star.kwarg
		args.L3, args.L4 = defaultNames(star.kwonly), defaultValues(star.kwonly)
	}
	return args
}

func defaultNames(ds []*nameDefault) []*Node {
	var out []*Node
	for _, d := range ds {
		out = append(out, d.arg)
	}
	return out
}

func defaultValues(ds []*nameDefault) []*Node {
	var out []*Node
	for _, d := range ds {
		out = append(out, d.value)
	}
	return out
}

// collectCallSeqs is the Call of the args rule: the positional arguments
// followed by the starred ones among the keywords
// (_PyPegen_collect_call_seqs).
func (p *parser) collectCallSeqs(args []*Node, kws []*kwOrStar, start int) *Node {
	c := p.span("Call", start)
	c.L1 = args
	if kws != nil {
		c.L1 = append(append([]*Node{}, args...), starredOf(kws)...)
		c.L2 = keywordArgsOf(kws)
	}
	return c
}

// starredOf are the starred expressions of kws
// (_PyPegen_seq_extract_starred_exprs).
func starredOf(kws []*kwOrStar) []*Node {
	var out []*Node
	for _, k := range kws {
		if !k.isKeyword {
			out = append(out, k.elem)
		}
	}
	return out
}

// keywordArgsOf are the keywords of kws (_PyPegen_seq_delete_starred_exprs).
func keywordArgsOf(kws []*kwOrStar) []*Node {
	var out []*Node
	for _, k := range kws {
		if k.isKeyword {
			out = append(out, k.elem)
		}
	}
	return out
}

// nonparenGenexpInCall raises the error for a generator expression that is
// one of several arguments without parentheses
// (_PyPegen_nonparen_genexp_in_call).
func (p *parser) nonparenGenexpInCall(args *Node, comps []*Node) {
	if len(args.L1) <= 1 {
		return
	}
	p.raiseRange(nodeLoc(args.L1[len(args.L1)-1]), nodeLoc(lastComprehensionItem(comps)),
		"Generator expression must be parenthesized")
}

// lastComprehensionItem is the expression a list of comprehension clauses
// ends with: the last condition of the last clause, or its iterable
// (_PyPegen_get_last_comprehension_item).
func lastComprehensionItem(comps []*Node) *Node {
	c := comps[len(comps)-1]
	if len(c.L1) == 0 {
		return c.B
	}
	return c.L1[len(c.L1)-1]
}

// argumentsParsingError raises the error for a positional argument after
// keyword arguments (_PyPegen_arguments_parsing_error).
func (p *parser) argumentsParsingError(call *Node) {
	for _, k := range call.L2 {
		if k.NoID {
			p.raise("positional argument follows keyword argument unpacking")
			return
		}
	}
	p.raise("positional argument follows keyword argument")
}

// checkBarryAsFlufl reports whether a NOTEQUAL token is the wrong spelling
// of "not equal": "<>" normally, "!=" after
// "from __future__ import barry_as_FLUFL", which also raises an error
// (_PyPegen_check_barry_as_flufl).
func (p *parser) checkBarryAsFlufl(t *ptok) bool {
	if p.barry && t.text != "<>" {
		p.raise("with Barry as BDFL, use '<>' instead of '!='")
		return true
	}
	return !p.barry && t.text != "!="
}
