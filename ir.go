// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/ir.py, kajiki/util.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"fmt"
	"iter"
	"strings"
	"sync"

	"github.com/jackrosenthal/kajiki-go/internal/py"
	"github.com/jackrosenthal/kajiki-go/internal/pyre"
)

// The intermediate representation: a tree of nodes that iterates (as the
// Python nodes' __iter__ does) into a stream of nodes, each of which writes
// some lines of the generated Python module. Iteration is lazy and
// iterators are chained exactly as the generators they port are, because
// it has visible effects: optimize merges and rewrites text nodes as they
// pass, and attribute nodes draw generated names while the stream is read.

// irNode is a node of the intermediate representation.
type irNode interface {
	// base returns the fields every node has.
	base() *irBase
	// nodes is __iter__: the stream of nodes the node stands for.
	nodes() iter.Seq[irNode]
	// py is py(): the lines of Python the node writes, with indentation
	// changes where a node opens a block within its own lines.
	py() []pyItem
}

// irBase holds a node's template line: 0 when unknown (Python's 0 or
// None).
type irBase struct {
	lineno int
}

func (b *irBase) base() *irBase { return b }

func (b *irBase) line(text string) pyItem { return pyItem{line: &pyLine{lineno: b.lineno, text: text}} }

// self is Node.__iter__: the node alone.
func self(n irNode) iter.Seq[irNode] {
	return func(yield func(irNode) bool) { yield(n) }
}

// pyItem is what py() yields: a line, or an indentation change.
type pyItem struct {
	line   *pyLine
	indent int
}

// pyLine is a line of the generated module with the template line of the
// node that wrote it; text may hold newlines (a multi-line expression).
type pyLine struct {
	lineno int
	text   string
	indent int
}

func (l *pyLine) String() string { return strings.Repeat(" ", l.indent) + l.text }

// generatePython is generate_python: the lines of the module for the
// template node.
func generatePython(tree *templateNode) []*pyLine {
	var lines []*pyLine
	indent := 0
	for n := range tree.nodes() {
		switch n.(type) {
		case *indentNode:
			indent += 4
		case *dedentNode:
			indent -= 4
		}
		for _, item := range n.py() {
			if item.line == nil {
				indent += item.indent
				continue
			}
			l := *item.line
			l.indent += indent
			lines = append(lines, &l)
		}
	}
	return lines
}

// concat chains the streams of nodes in order, as flattener(map(flattener,
// body)) does.
func concat(body []irNode) iter.Seq[irNode] {
	return func(yield func(irNode) bool) {
		for _, b := range body {
			for n := range b.nodes() {
				if !yield(n) {
					return
				}
			}
		}
	}
}

// optimize merges each run of plain text nodes with the same guard into the
// first of them, which it changes, holding one node back to see the next.
func optimize(seq iter.Seq[irNode]) iter.Seq[irNode] {
	return func(yield func(irNode) bool) {
		var last irNode
		for n := range seq {
			if t, ok := n.(*textNode); ok {
				if lt, ok := last.(*textNode); ok && lt.guard == t.guard {
					lt.text += t.text
					continue
				}
			}
			if last != nil && !yield(last) {
				return
			}
			last = n
		}
		if last != nil {
			yield(last)
		}
	}
}

// hier is HierNode: a node with an indented body.
type hier struct {
	irBase
	body []irNode
}

func newHier(body []irNode) hier {
	var kept []irNode
	for _, b := range body {
		if b != nil {
			kept = append(kept, b)
		}
	}
	return hier{body: kept}
}

func (h *hier) bodyNodes() iter.Seq[irNode] { return optimize(concat(h.body)) }

// blockNodes is HierNode.__iter__ for the node n embedding h: n, an indent,
// the body and a dedent.
func blockNodes(n irNode, h *hier) iter.Seq[irNode] {
	return func(yield func(irNode) bool) {
		if !yield(n) || !yield(&indentNode{}) {
			return
		}
		for c := range h.bodyNodes() {
			if !yield(c) {
				return
			}
		}
		yield(&dedentNode{})
	}
}

// then is the stream seq followed by the nodes tail.
func then(seq iter.Seq[irNode], tail ...irNode) iter.Seq[irNode] {
	return func(yield func(irNode) bool) {
		for n := range seq {
			if !yield(n) {
				return
			}
		}
		for _, n := range tail {
			if !yield(n) {
				return
			}
		}
	}
}

// prepend is the node n followed by the stream seq.
func prepend(n irNode, seq iter.Seq[irNode]) iter.Seq[irNode] {
	return func(yield func(irNode) bool) {
		if !yield(n) {
			return
		}
		for c := range seq {
			if !yield(c) {
				return
			}
		}
	}
}

type indentNode struct{ irBase }

func (n *indentNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *indentNode) py() []pyItem            { return nil }

type dedentNode struct{ irBase }

func (n *dedentNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *dedentNode) py() []pyItem            { return nil }

// passNode is the body of an empty function: it yields "", since a
// template function must be a generator.
type passNode struct{ irBase }

func (n *passNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *passNode) py() []pyItem            { return []pyItem{n.line(`yield ""`)} }

// templateNode is the root: the module-level code, then the class whose
// methods are the template functions, then its conversion into a template
// class.
type templateNode struct {
	hier
	modPy    []*pythonNode
	filename string
}

func newTemplateNode(modPy []*pythonNode, defs []irNode) *templateNode {
	return &templateNode{hier: newHier(defs), modPy: modPy}
}

func (n *templateNode) nodes() iter.Seq[irNode] {
	return func(yield func(irNode) bool) {
		for _, m := range n.modPy {
			if !yield(m) {
				return
			}
		}
		for c := range then(blockNodes(n, &n.hier), &templateTail{}) {
			if !yield(c) {
				return
			}
		}
	}
}

func (n *templateNode) py() []pyItem { return []pyItem{n.line("class template:")} }

type templateTail struct{ irBase }

func (n *templateTail) nodes() iter.Seq[irNode] { return self(n) }
func (n *templateTail) py() []pyItem {
	return []pyItem{n.line("template = kajiki.Template(template)")}
}

// importNode is py:import: the template loaded as a fragment under alias.
type importNode struct {
	irBase
	tplName, alias string
}

func newImportNode(tplName string, alias *string) *importNode {
	if alias == nil {
		return &importNode{tplName: tplName, alias: defaultAliasFor(tplName)}
	}
	return &importNode{tplName: tplName, alias: *alias}
}

func (n *importNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *importNode) py() []pyItem {
	return []pyItem{n.line(fmt.Sprintf("%s = local.__kj__.import_(%s, %s, self.__globals__)",
		n.alias, pyre.StrRepr(n.tplName), pyre.StrRepr(n.alias)))}
}

// includeNode is py:include: the template's body rendered in place.
type includeNode struct {
	irBase
	tplName string
}

func (n *includeNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *includeNode) py() []pyItem {
	return []pyItem{n.line(fmt.Sprintf("yield local.__kj__.import_(%s, None, self.__globals__).__main__()",
		pyre.StrRepr(n.tplName)))}
}

// extendNode is py:extends: the parent template rendered with the child's
// blocks.
type extendNode struct {
	irBase
	tplName string
}

func (n *extendNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *extendNode) py() []pyItem {
	return []pyItem{n.line(fmt.Sprintf("yield local.__kj__.extend(%s).__main__()", pyre.StrRepr(n.tplName)))}
}

// defNode is a template function: exposed on the template class when it is
// top-level (DefNode), a flattening inner function otherwise
// (InnerDefNode).
type defNode struct {
	hier
	decl  string
	inner bool
}

func newDefNode(decl string, body []irNode) *defNode {
	return &defNode{hier: newHier(body), decl: decl}
}

func (n *defNode) nodes() iter.Seq[irNode] {
	return func(yield func(irNode) bool) {
		if !yield(n) || !yield(&indentNode{}) {
			return
		}
		empty := true
		for c := range n.bodyNodes() {
			if !yield(c) {
				return
			}
			empty = false
		}
		if empty && !yield(&passNode{}) {
			return
		}
		yield(&dedentNode{})
	}
}

func (n *defNode) py() []pyItem {
	prefix := "@kajiki.expose"
	if n.inner {
		prefix = "@__kj__.flattener.decorate"
	}
	return []pyItem{n.line(prefix), n.line("def " + n.decl + ":")}
}

// callNode is py:call: the caller function, then the call of the callee.
type callNode struct {
	hier
	decl, call string
}

func newCallNode(caller, callee string, body []irNode) *callNode {
	fname := genName()
	return &callNode{hier: newHier(body), decl: strings.ReplaceAll(caller, "$caller", fname),
		call: strings.ReplaceAll(callee, "$caller", fname)}
}

func (n *callNode) nodes() iter.Seq[irNode] {
	return then(blockNodes(n, &n.hier), &callTail{call: n.call})
}

func (n *callNode) py() []pyItem {
	return []pyItem{n.line("@__kj__.flattener.decorate"), n.line("def " + n.decl + ":")}
}

type callTail struct {
	irBase
	call string
}

func (n *callTail) nodes() iter.Seq[irNode] { return self(n) }
func (n *callTail) py() []pyItem            { return []pyItem{n.line("yield " + n.call)} }

// forNode is py:for.
type forNode struct {
	hier
	decl string
}

func (n *forNode) nodes() iter.Seq[irNode] { return blockNodes(n, &n.hier) }
func (n *forNode) py() []pyItem            { return []pyItem{n.line("for " + n.decl + ":")} }

// withAssignment matches the start of each assignment of py:with's vars:
// ";" or a line start, a target, and an "=" that is not "==".
var withAssignment = pyre.MustCompile(`(?:^|;)\s*([^;=]+)=(?!=)`, pyre.MULTILINE)

// withNode is py:with: the previous values of the names are saved, the
// assignments run in order, and the saved values are put back after the
// body (a name that was not a local becomes ()).
type withNode struct {
	hier
	assignments [][2]string
	names       []string
}

// newWithNode parses vars into assignments; vars without any is the
// AttributeError of reading the first match when there is none.
func newWithNode(vars string, body []irNode) (*withNode, error) {
	n := &withNode{hier: newHier(body)}
	text := []rune(vars)
	matches := withAssignment.FindIter(text, 0, len(text))
	if len(matches) == 0 {
		return nil, py.Raise("AttributeError", py.String("'NoneType' object has no attribute 'group'"))
	}
	for i, m := range matches {
		target, _ := m.Group(1)
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1].Start(0)
		}
		lhs := strip(target)
		n.assignments = append(n.assignments, [2]string{lhs, string(text[m.End(0):end])})
		n.names = append(n.names, lhs)
	}
	return n, nil
}

func (n *withNode) nodes() iter.Seq[irNode] {
	return prepend(n, then(n.bodyNodes(), &withTail{names: n.names}))
}

func (n *withNode) py() []pyItem {
	quoted := make([]string, len(n.names))
	for i, k := range n.names {
		quoted[i] = `"` + k + `"`
	}
	items := []pyItem{n.line("local.__kj__.push_with(locals(), [" + strings.Join(quoted, ",") + "])")}
	for _, a := range n.assignments {
		items = append(items, n.line(a[0]+" = "+a[1]))
	}
	return items
}

type withTail struct {
	irBase
	names []string
}

func (n *withTail) nodes() iter.Seq[irNode] { return self(n) }
func (n *withTail) py() []pyItem {
	return []pyItem{n.line("(" + strings.Join(n.names, ",") + ",) = local.__kj__.pop_with()")}
}

// switchNode is py:switch: its cases are elif branches after an "if
// False".
type switchNode struct {
	hier
	decl string
}

func (n *switchNode) nodes() iter.Seq[irNode] {
	return prepend(n, then(n.bodyNodes(), &switchTail{}))
}

func (n *switchNode) py() []pyItem {
	return []pyItem{n.line("local.__kj__.push_switch(" + n.decl + ")"), n.line("if False: pass")}
}

type switchTail struct{ irBase }

func (n *switchTail) nodes() iter.Seq[irNode] { return self(n) }
func (n *switchTail) py() []pyItem            { return []pyItem{n.line("local.__kj__.pop_switch()")} }

// caseNode is py:case value=: a branch of py:switch.
type caseNode struct {
	hier
	decl string
}

func (n *caseNode) nodes() iter.Seq[irNode] { return blockNodes(n, &n.hier) }
func (n *caseNode) py() []pyItem {
	return []pyItem{n.line("elif local.__kj__.case(" + n.decl + "):")}
}

// matchNode is py:match: a match statement, which opens its block in its
// own lines.
type matchNode struct {
	hier
	decl string
}

func (n *matchNode) nodes() iter.Seq[irNode] { return prepend(n, then(n.bodyNodes(), &dedentNode{})) }
func (n *matchNode) py() []pyItem {
	return []pyItem{n.line("match (" + n.decl + "):"), {indent: 4}}
}

// matchCaseNode is py:case match=: a case clause.
type matchCaseNode struct {
	hier
	decl string
}

func (n *matchCaseNode) nodes() iter.Seq[irNode] {
	return prepend(n, then(n.bodyNodes(), &dedentNode{}))
}
func (n *matchCaseNode) py() []pyItem {
	return []pyItem{n.line("case " + n.decl + ":"), {indent: 4}}
}

// ifNode is py:if.
type ifNode struct {
	hier
	decl string
}

func (n *ifNode) nodes() iter.Seq[irNode] { return blockNodes(n, &n.hier) }
func (n *ifNode) py() []pyItem            { return []pyItem{n.line("if " + n.decl + ":")} }

// elseNode is py:else.
type elseNode struct{ hier }

func (n *elseNode) nodes() iter.Seq[irNode] { return blockNodes(n, &n.hier) }
func (n *elseNode) py() []pyItem            { return []pyItem{n.line("else:")} }

// textNode outputs a literal text, under a guard expression when guard is
// not "".
type textNode struct {
	irBase
	text, guard string
}

func (n *textNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *textNode) py() []pyItem            { return guarded(&n.irBase, n.guard, "yield "+pyre.StrRepr(n.text)) }

func guarded(b *irBase, guard, s string) []pyItem {
	if guard != "" {
		return []pyItem{b.line("if " + guard + ": " + s)}
	}
	return []pyItem{b.line(s)}
}

// translatableTextNode is a text that goes through gettext when it holds
// more than white space. It is never merged with other text.
type translatableTextNode struct {
	irBase
	text, guard string
}

func (n *translatableTextNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *translatableTextNode) py() []pyItem {
	s := "yield " + pyre.StrRepr(n.text)
	if !isBlank(n.text) {
		s = "yield local.__kj__.gettext(" + pyre.StrRepr(n.text) + ")"
	}
	return guarded(&n.irBase, n.guard, s)
}

// exprNode outputs the value of an expression, escaped unless safe.
type exprNode struct {
	irBase
	text string
	safe bool
}

func (n *exprNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *exprNode) py() []pyItem {
	if n.safe {
		return []pyItem{n.line("yield " + n.text)}
	}
	return []pyItem{n.line("yield self.__kj__.escape(" + n.text + ")")}
}

// attrNode is one attribute of an element: a function computing the value,
// collected and rendered (dropped when it is None).
type attrNode struct {
	hier
	attr, guard, mode, genname string
}

func newAttrNode(attr string, value []irNode, guard, mode string) *attrNode {
	return &attrNode{hier: newHier(value), attr: attr, guard: guard, mode: mode, genname: genName()}
}

func (n *attrNode) nodes() iter.Seq[irNode] {
	if n.guard != "" {
		guardIf := &ifNode{hier: newHier([]irNode{newAttrNode(n.attr, n.body, "", n.mode)}), decl: n.guard}
		return guardIf.nodes()
	}
	return func(yield func(irNode) bool) {
		if !yield(n) || !yield(&indentNode{}) {
			return
		}
		if len(n.body) > 0 {
			for c := range n.bodyNodes() {
				if !yield(c) {
					return
				}
			}
		} else if !yield(&textNode{}) {
			return
		}
		if !yield(&dedentNode{}) {
			return
		}
		yield(&attrTail{p: n})
	}
}

func (n *attrNode) py() []pyItem { return []pyItem{n.line("def " + n.genname + "():")} }

type attrTail struct {
	irBase
	p *attrNode
}

func (n *attrTail) nodes() iter.Seq[irNode] { return self(n) }
func (n *attrTail) py() []pyItem {
	gen := n.p.genname
	x := genName()
	return []pyItem{
		n.line(gen + " = self.__kj__.collect(" + gen + "())"),
		n.line("for " + x + " in self.__kj__.render_attrs({" + pyre.StrRepr(n.p.attr) + ":" + gen + "}, " +
			pyre.StrRepr(n.p.mode) + "):"),
		n.line("    yield " + x),
	}
}

// attrsNode is py:attrs: the attributes of a mapping or pairs, rendered at
// run time.
type attrsNode struct {
	irBase
	attrs, guard, mode string
}

func (n *attrsNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *attrsNode) py() []pyItem {
	x := genName()
	body := []string{"for " + x + " in self.__kj__.render_attrs(" + n.attrs + ", " + pyre.StrRepr(n.mode) + "):",
		"    yield " + x}
	if n.guard == "" {
		return []pyItem{n.line(body[0]), n.line(body[1])}
	}
	items := []pyItem{n.line("if " + n.guard + ":")}
	for _, b := range body {
		item := n.line(b)
		item.line.indent = 4
		items = append(items, item)
	}
	return items
}

// pythonNode is a code block; module level when its text starts with "%".
type pythonNode struct {
	irBase
	lines       []string
	moduleLevel bool
}

// newPythonNode is PythonNode(TextNode(text)): the IndexError of an empty
// text, the AssertionError of a line that does not start with the first
// line's indentation.
func newPythonNode(text string) (*pythonNode, error) {
	if text == "" {
		return nil, py.Raise("IndexError", py.String("string index out of range"))
	}
	n := &pythonNode{}
	if text[0] == '%' {
		n.moduleLevel = true
		text = text[1:]
	}
	text = strings.TrimPrefix(text, "#\n")
	prefix, first := "", true
	for _, line := range splitLines(text) {
		if first {
			prefix = line[:len(line)-len(lstrip(line))]
			first = false
		}
		if !strings.HasPrefix(line, prefix) {
			return nil, py.Raise("AssertionError")
		}
		n.lines = append(n.lines, line[len(prefix):])
	}
	return n, nil
}

func (n *pythonNode) nodes() iter.Seq[irNode] { return self(n) }
func (n *pythonNode) py() []pyItem {
	items := make([]pyItem, len(n.lines))
	for i, l := range n.lines {
		items[i] = n.line(l)
	}
	return items
}

// The names of the generated functions and loop variables come from one
// generator per process, as NameGen's thread-local instance: "_kj_" first,
// then "_kj_" with a number that no name drawn before has.
var nameGen struct {
	sync.Mutex
	names map[string]bool
}

// genName is gen_name().
func genName() string {
	nameGen.Lock()
	defer nameGen.Unlock()
	if nameGen.names == nil {
		nameGen.names = map[string]bool{}
	}
	const hint = "_kj_"
	name := hint
	for k := len(nameGen.names); nameGen.names[name]; k++ {
		name = fmt.Sprintf("%s_%d", hint, k)
	}
	nameGen.names[name] = true
	return name
}

// defaultAliasFor is default_alias_for: the file name without directory and
// extension, as os.path.splitext(os.path.basename(name))[0] gives it.
func defaultAliasFor(name string) string {
	base := name[strings.LastIndexByte(name, '/')+1:]
	if dot := strings.LastIndexByte(base, '.'); dot > 0 && strings.Trim(base[:dot], ".") != "" {
		return base[:dot]
	}
	return base
}
