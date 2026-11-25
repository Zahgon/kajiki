// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/text.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"fmt"
	"iter"
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/py"
	"github.com/jackrosenthal/kajiki-go/internal/pyre"
)

// textPattern is text._pattern: the starts of expressions and tags.
var textPattern = pyre.MustCompile(`
\$(?:
    (?P<expr_escaped>\$) |      # Escape $$
    (?P<expr_named>[_a-z][_a-z0-9.]*) | # $foo.bar
    {(?P<expr_braced>) | # ${....
    (?P<expr_invalid>)
) |
^\s*%(?:
    (?P<tag_bare>[a-z]+) | # %for, %end, etc.
    (?P<tag_bare_invalid>)
)|
^\s*{%-(?P<tag_begin_ljust>[a-z]+)|  # {%-for, {%-end, etc.
{%(?:
    (?P<tag_begin>[a-z]+) | # {%for, {%end, etc.
    (?P<tag_begin_invalid>)
)
`, pyre.VERBOSE|pyre.IGNORECASE|pyre.MULTILINE)

func textGroup(name string) int {
	i, _ := textPattern.GroupIndexOf(name)
	return i
}

var (
	textEscaped       = textGroup("expr_escaped")
	textNamed         = textGroup("expr_named")
	textBraced        = textGroup("expr_braced")
	textTagBare       = textGroup("tag_bare")
	textTagBareBad    = textGroup("tag_bare_invalid")
	textTagBeginLjust = textGroup("tag_begin_ljust")
	textTagBegin      = textGroup("tag_begin")
)

// textTemplate is TextTemplate: the template class for the source, read
// from filename with codecs.open when it is nil. "" is the None of a
// missing filename.
func textTemplate(source *string, filename string, autoescape bool, encoding string) (py.Value, error) {
	if (source == nil || *source == "") && filename == "" {
		return nil, py.Raise("AssertionError", py.String("You must either provide a *source* argument or a "+
			"*filename* argument to TextTemplate()."))
	}
	var text string
	if source != nil {
		text = *source
	} else {
		t, err := codecsOpenRead(filename, encoding)
		if err != nil {
			return nil, err
		}
		text = t
	}
	if filename == "" {
		filename = "<string>"
	}
	tree, err := compileTextTemplate(text, filename, autoescape)
	if err != nil {
		return nil, err
	}
	return fromIR(tree, nil)
}

// compileTextTemplate runs the front half of TextTemplate: scan and parse
// to the intermediate representation.
func compileTextTemplate(source, filename string, autoescape bool) (*templateNode, error) {
	tree, err := newTextParser(newScanner(filename, source), autoescape).parse()
	if err != nil {
		return nil, err
	}
	tree.filename = filename
	return tree, nil
}

// textToken is a token of the scanner: text, an expression or a tag.
type textToken struct {
	kind    textTokenKind
	lineno  int
	text    string
	tagname string
	body    string
}

type textTokenKind int

const (
	tokenText textTokenKind = iota
	tokenExpr
	tokenTag
)

// scanner is text._Scanner: it cuts the source into tokens lazily, the
// parser pulling them one at a time. The matches of textPattern are found
// in the whole source up front, as finditer finds them whatever the
// scanner has consumed.
type scanner struct {
	filename string
	source   []rune
	lineno   int
	pos      int
	matches  []*pyre.Match
	next     int
	// textDone is set once the text before matches[next] was handled.
	textDone bool
	done     bool
}

func newScanner(filename, source string) *scanner {
	s := &scanner{filename: filename, source: []rune(source), lineno: 1}
	s.matches = textPattern.FindIter(s.source, 0, len(s.source))
	return s
}

// setPos is the pos property's setter: the position never moves back.
func (s *scanner) setPos(v int) error {
	if v < s.pos {
		return py.Raise("AssertionError")
	}
	s.pos = v
	return nil
}

func (s *scanner) slice(from, to int) string {
	from = max(0, min(from, len(s.source)))
	to = max(from, min(to, len(s.source)))
	return string(s.source[from:to])
}

func (s *scanner) text(text string) *textToken {
	s.lineno += strings.Count(text, "\n")
	return &textToken{kind: tokenText, lineno: s.lineno, text: text}
}

func (s *scanner) expr(text string) *textToken {
	s.lineno += strings.Count(text, "\n")
	return &textToken{kind: tokenExpr, lineno: s.lineno, text: text}
}

func (s *scanner) tag(tagname, body string) *textToken {
	t := &textToken{kind: tokenTag, lineno: s.lineno, tagname: tagname, body: body, text: tagname + " " + body}
	s.lineno += strings.Count(t.text, "\n")
	return t
}

func matched(m *pyre.Match, group int) bool {
	_, ok := m.Group(group)
	return ok
}

// next is one step of _Scanner.__iter__: the next token (nil for the None
// of a braced expression the tokens run out in), ok false at the end.
func (s *scanner) nextToken() (tok *textToken, ok bool, err error) {
	for s.next < len(s.matches) {
		m := s.matches[s.next]
		if !s.textDone {
			s.textDone = true
			if start := m.Start(0); start > s.pos {
				t := s.text(s.slice(s.pos, start))
				s.pos = start
				return t, true, nil
			}
		}
		s.next++
		s.textDone = false
		if matched(m, textTagBareBad) && !matched(m, textTagBare) {
			continue
		}
		if !matched(m, textBraced) && !matched(m, textNamed) && !matched(m, textEscaped) &&
			!matched(m, textTagBare) && !matched(m, textTagBegin) && !matched(m, textTagBeginLjust) {
			var b strings.Builder
			fmt.Fprintf(&b, "Syntax error %s:%d", s.filename, s.lineno)
			for i, line := range splitLines(string(s.source)) {
				fmt.Fprintf(&b, "%3d %s\n", i+1, line)
			}
			return nil, false, py.Raise("SyntaxError", py.String(b.String()))
		}
		if err := s.setPos(m.End(0)); err != nil {
			return nil, false, err
		}
		switch {
		case matched(m, textBraced):
			t, err := s.bracedExpr()
			return t, err == nil, err
		case matched(m, textNamed):
			name, _ := m.Group(textNamed)
			return s.expr(name), true, nil
		case matched(m, textEscaped):
			return s.text("$"), true, nil
		case matched(m, textTagBare):
			name, _ := m.Group(textTagBare)
			t, err := s.tagBare(name)
			return t, err == nil, err
		case matched(m, textTagBegin):
			name, _ := m.Group(textTagBegin)
			t, err := s.tagBraced(name)
			return t, err == nil, err
		}
		name, _ := m.Group(textTagBeginLjust)
		t, err := s.tagBraced(name)
		return t, err == nil, err
	}
	if !s.done {
		s.done = true
		if s.pos != len(s.source) {
			return s.text(s.slice(s.pos, len(s.source))), true, nil
		}
	}
	return nil, false, nil
}

func (s *scanner) find(sub string, from int) int {
	if from > len(s.source) {
		return -1
	}
	i := strings.Index(string(s.source[from:]), sub)
	if i < 0 {
		return -1
	}
	return from + runeLen(string(s.source[from:])[:i])
}

// tagBare is _get_tag_bare: a tag that runs to the end of its line.
func (s *scanner) tagBare(tagname string) (*textToken, error) {
	end := s.find("\n", s.pos)
	if end == -1 {
		end = len(s.source)
	}
	body := s.slice(s.pos, end)
	s.lineno++
	if err := s.setPos(end + 1); err != nil {
		return nil, err
	}
	return s.tag(tagname, body), nil
}

// tagBraced is _get_tag: a tag closed by "%}"; "-%}" also drops the spaces
// and tabs that follow.
func (s *scanner) tagBraced(tagname string) (*textToken, error) {
	end := s.find("%}", s.pos)
	if end <= 0 {
		return nil, py.Raise("AssertionError")
	}
	body := s.slice(s.pos, end)
	if err := s.setPos(end + 2); err != nil {
		return nil, err
	}
	if strings.HasSuffix(body, "-") {
		body = body[:len(body)-1]
		for {
			if s.pos >= len(s.source) {
				return nil, py.Raise("IndexError", py.String("string index out of range"))
			}
			if c := s.source[s.pos]; c != ' ' && c != '\t' {
				break
			}
			if err := s.setPos(s.pos + 1); err != nil {
				return nil, err
			}
		}
	}
	return s.tag(tagname, body), nil
}

// diffPos is _diff_pos: the characters between two (row, column) points,
// counting only the columns when the row changes.
func diffPos(last, next [2]int) int {
	if last[0] == next[0] {
		return next[1] - last[1]
	}
	return next[1]
}

// bracedExpr is _Scanner._get_braced_expr: Python's tokenizer reads from
// the opening brace until the braces balance; the expression is what lies
// between them, measured with diffPos. It is nil when the tokens run out
// first.
func (s *scanner) bracedExpr() (*textToken, error) {
	tokens := py.GenerateTokens(s.slice(s.pos-1, len(s.source)))
	braces, textLen := 0, -1
	last := [2]int{1, 0}
	for {
		tok, ok, err := tokens.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		switch tok.String {
		case "{":
			braces++
		case "}":
			braces--
		}
		textLen += diffPos(last, tok.End)
		last = tok.End
		if braces == 0 {
			text := s.slice(s.pos, s.pos+textLen-1)
			if err := s.setPos(s.pos + textLen); err != nil {
				return nil, err
			}
			return s.expr(text), nil
		}
	}
}

// bodyItem is an element of the list _parse_body makes: a node (nil for
// the None a tag's parser returns), or the stop tag that ended the body,
// or the None that marks the end of the tokens.
type bodyItem struct {
	node irNode
	stop *textToken
	end  bool
}

// nodesOf is body[:-1] as nodes, the Nones of parsers that return none
// dropped as HierNode drops them.
func nodesOf(body []bodyItem) []irNode {
	var out []irNode
	for _, b := range body[:len(body)-1] {
		if b.node != nil {
			out = append(out, b.node)
		}
	}
	return out
}

// textParser is text._Parser.
type textParser struct {
	scanner    *scanner
	pushed     []*textToken
	functions  []string
	bodies     map[string][]irNode
	modPy      []*pythonNode
	autoescape bool
	inDef      bool
	isChild    bool
}

func newTextParser(s *scanner, autoescape bool) *textParser {
	p := &textParser{scanner: s, autoescape: autoescape, bodies: map[string][]irNode{}}
	p.setFunction("__main__()", nil)
	return p
}

func (p *textParser) setFunction(decl string, body []irNode) {
	if _, ok := p.bodies[decl]; !ok {
		p.functions = append(p.functions, decl)
	}
	p.bodies[decl] = body
}

func (p *textParser) parse() (*templateNode, error) {
	body, err := p.parseBody()
	if err != nil {
		return nil, err
	}
	p.setFunction("__main__()", nodesOf(body))
	defs := make([]irNode, len(p.functions))
	for i, decl := range p.functions {
		defs[i] = newDefNode(decl, p.bodies[decl])
	}
	return newTemplateNode(p.modPy, defs), nil
}

// pull is next(self.iterator): a token pushed back first.
func (p *textParser) pull() (tok *textToken, ok bool, err error) {
	if n := len(p.pushed); n > 0 {
		tok = p.pushed[n-1]
		p.pushed = p.pushed[:n-1]
		return tok, true, nil
	}
	return p.scanner.nextToken()
}

// parseBody is _parse_body: nodes until one of the stop tags (the last
// item then) or the end of the tokens (a None item).
func (p *textParser) parseBody(stoptags ...string) ([]bodyItem, error) {
	var body []bodyItem
	for {
		tok, ok, err := p.pull()
		if err != nil {
			return nil, err
		}
		if !ok {
			return append(body, bodyItem{end: true}), nil
		}
		if tok == nil {
			return nil, py.Raise("SyntaxError", py.String("Parse error: None unexpected"))
		}
		switch tok.kind {
		case tokenText:
			n, err := p.text(tok)
			if err != nil {
				return nil, err
			}
			body = append(body, bodyItem{node: n})
		case tokenExpr:
			e := &exprNode{text: tok.text, safe: !p.autoescape}
			e.lineno = tok.lineno
			body = append(body, bodyItem{node: e})
		default:
			for _, s := range stoptags {
				if tok.tagname == s {
					return append(body, bodyItem{stop: tok}), nil
				}
			}
			n, err := p.parseTag(tok)
			if err != nil {
				return nil, err
			}
			body = append(body, bodyItem{node: n})
		}
	}
}

func (p *textParser) text(tok *textToken) (irNode, error) {
	text, err := unescapeNewlines(tok.text)
	if err != nil {
		return nil, err
	}
	n := &textNode{text: text}
	n.lineno = tok.lineno
	return n, nil
}

// unescapeNewlines is _unescape_newlines: a backslash escapes the next
// character, and an escaped newline disappears.
func unescapeNewlines(text string) (string, error) {
	rs := []rune(text)
	var b strings.Builder
	for i := 0; i < len(rs); {
		if rs[i] != '\\' {
			b.WriteRune(rs[i])
			i++
			continue
		}
		if i+1 >= len(rs) {
			return "", py.Raise("IndexError", py.String("string index out of range"))
		}
		if rs[i+1] != '\n' {
			b.WriteRune(rs[i+1])
		}
		i += 2
	}
	return b.String(), nil
}

// parseTag dispatches a tag to its parser, _parse_<tagname>.
func (p *textParser) parseTag(tok *textToken) (irNode, error) {
	switch tok.tagname {
	case "def":
		return p.parseDef(tok)
	case "call":
		return p.parseCall(tok)
	case "if":
		return p.parseIf(tok)
	case "for":
		return p.parseFor(tok)
	case "switch":
		return p.parseSwitch(tok)
	case "case":
		return p.parseCase(tok)
	case "else":
		return p.parseElse()
	case "extends":
		return p.parseExtends(tok)
	case "import":
		return p.parseImport(tok)
	case "include":
		return p.parseInclude(tok)
	case "py":
		return p.parsePy(tok)
	case "block":
		return p.parseBlock(tok)
	case "body":
		return &exhaustedBody{}, nil
	}
	return nil, py.Raise("AttributeError", py.String("'_Parser' object has no attribute '_parse_"+tok.tagname+"'"))
}

// exhaustedBody is what the tag %body gives: getattr finds the parser's own
// _parse_body, whose generator is only run when the module is generated,
// after the tokens ran out, so it yields nothing but a None.
type exhaustedBody struct{ irBase }

func (n *exhaustedBody) nodes() iter.Seq[irNode] { return func(func(irNode) bool) {} }
func (n *exhaustedBody) py() []pyItem            { return nil }

func (p *textParser) parseDef(tok *textToken) (irNode, error) {
	old := p.inDef
	p.inDef = true
	body, err := p.parseBody("end")
	if err != nil {
		return nil, err
	}
	p.inDef = old
	if p.inDef {
		d := newDefNode(tok.body, nodesOf(body))
		d.inner = true
		return d, nil
	}
	p.setFunction(strip(tok.body), nodesOf(body))
	return nil, nil
}

func (p *textParser) parseCall(tok *textToken) (irNode, error) {
	b := strings.Index(tok.body, "(")
	e := -1
	if b >= 0 {
		if i := strings.Index(tok.body[b:], ")"); i >= 0 {
			e = b + i
		}
	} else if i := strings.LastIndex(tok.body, ")"); i >= 0 {
		e = strings.Index(tok.body, ")")
	}
	if !(e > b && b > -1) {
		return nil, py.Raise("AssertionError")
	}
	arglist := tok.body[b : e+1]
	call := strip(tok.body[e+1:])
	body, err := p.parseBody("end")
	if err != nil {
		return nil, err
	}
	return newCallNode("$caller"+arglist, strings.ReplaceAll(call, "%caller", "$caller"), nodesOf(body)), nil
}

// stopTag is body[-1].tagname: the AttributeError of None when the tokens
// ran out.
func stopTag(body []bodyItem) (*textToken, error) {
	last := body[len(body)-1]
	if last.stop == nil {
		return nil, py.Raise("AttributeError", py.String("'NoneType' object has no attribute 'tagname'"))
	}
	return last.stop, nil
}

func (p *textParser) parseIf(tok *textToken) (irNode, error) {
	body, err := p.parseBody("end", "else")
	if err != nil {
		return nil, err
	}
	stop, err := stopTag(body)
	if err != nil {
		return nil, err
	}
	if stop.tagname == "else" {
		p.pushed = append(p.pushed, stop)
	}
	return &ifNode{hier: newHier(nodesOf(body)), decl: tok.body}, nil
}

func (p *textParser) parseFor(tok *textToken) (irNode, error) {
	body, err := p.parseBody("end")
	if err != nil {
		return nil, err
	}
	return &forNode{hier: newHier(nodesOf(body)), decl: tok.body}, nil
}

func (p *textParser) parseSwitch(tok *textToken) (irNode, error) {
	body, err := p.parseBody("end")
	if err != nil {
		return nil, err
	}
	return &switchNode{hier: newHier(nodesOf(body)), decl: tok.body}, nil
}

// parseCase is _parse_case: the tag that ends a case (the next case, an
// else or the end) goes back to the switch's body, None too.
func (p *textParser) parseCase(tok *textToken) (irNode, error) {
	body, err := p.parseBody("case", "else", "end")
	if err != nil {
		return nil, err
	}
	p.pushed = append(p.pushed, body[len(body)-1].stop)
	return &caseNode{hier: newHier(nodesOf(body)), decl: tok.body}, nil
}

func (p *textParser) parseElse() (irNode, error) {
	body, err := p.parseBody("end")
	if err != nil {
		return nil, err
	}
	return &elseNode{hier: newHier(nodesOf(body))}, nil
}

// shlexFirst is shlex.split(body) with its first part (IndexError when
// there is none).
func shlexFirst(body string) ([]string, error) {
	parts, err := shlexSplit(body)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, py.Raise("IndexError", py.String("list index out of range"))
	}
	return parts, nil
}

func (p *textParser) parseExtends(tok *textToken) (irNode, error) {
	parts, err := shlexFirst(tok.body)
	if err != nil {
		return nil, err
	}
	if len(parts) != 1 {
		return nil, py.Raise("AssertionError")
	}
	p.isChild = true
	return &extendNode{tplName: parts[0]}, nil
}

func (p *textParser) parseImport(tok *textToken) (irNode, error) {
	parts, err := shlexFirst(tok.body)
	if err != nil {
		return nil, err
	}
	if len(parts) > 1 {
		if parts[1] != "as" {
			return nil, py.Raise("AssertionError")
		}
		if len(parts) < 3 {
			return nil, py.Raise("IndexError", py.String("list index out of range"))
		}
		return newImportNode(parts[0], &parts[2]), nil
	}
	return newImportNode(parts[0], nil), nil
}

func (p *textParser) parseInclude(tok *textToken) (irNode, error) {
	parts, err := shlexFirst(tok.body)
	if err != nil {
		return nil, err
	}
	if len(parts) != 1 {
		return nil, py.Raise("AssertionError")
	}
	return &includeNode{tplName: parts[0]}, nil
}

// parsePy is _parse_py: the code is the tag's own line, or the text up to
// %end; only text may make it up.
func (p *textParser) parsePy(tok *textToken) (irNode, error) {
	var text strings.Builder
	if body := strip(tok.body); body != "" {
		text.WriteString(body)
	} else {
		items, err := p.parseBody("end")
		if err != nil {
			return nil, err
		}
		for _, b := range items[:len(items)-1] {
			t, ok := b.node.(*textNode)
			if !ok {
				return nil, py.Raise("AssertionError")
			}
			text.WriteString(t.text)
		}
	}
	n, err := newPythonNode(text.String())
	if err != nil {
		return nil, err
	}
	if n.moduleLevel {
		p.modPy = append(p.modPy, n)
		return nil, nil
	}
	return n, nil
}

// parseBlock is _parse_block: a block function; a child template's block
// binds its parent's first and is not rendered in place.
func (p *textParser) parseBlock(tok *textToken) (irNode, error) {
	fname := "_kj_block_" + strip(tok.body)
	decl := fname + "()"
	items, err := p.parseBody("end")
	if err != nil {
		return nil, err
	}
	var body []irNode
	for _, b := range items[:len(items)-1] {
		if b.node != nil {
			body = append(body, b.node)
		}
	}
	if p.isChild {
		n, err := newPythonNode("parent_block=parent." + fname)
		if err != nil {
			return nil, err
		}
		p.setFunction(decl, append([]irNode{n}, body...))
		return nil, nil
	}
	p.setFunction(decl, body)
	e := &exprNode{text: decl}
	return e, nil
}
