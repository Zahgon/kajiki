// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/xml_template.py, kajiki/markup_template.py, kajiki/html_utils.py) to Go,
// 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"slices"
	"sort"
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/py"
	"github.com/jackrosenthal/kajiki-go/internal/pyre"
	"github.com/jackrosenthal/kajiki-go/internal/pyuni"
)

// directive is one of markup_template.DIRECTIVES: the directive and the
// attribute of its element that holds the attribute form's value.
type directive struct {
	name  string
	attrs []string
}

// directives is QDIRECTIVES, in the order directives on one element nest.
var directives = []directive{
	{"py:def", []string{"function"}},
	{"py:call", []string{"function"}},
	{"py:case", []string{"value", "match"}},
	{"py:else", []string{""}},
	{"py:for", []string{"each"}},
	{"py:if", []string{"test"}},
	{"py:switch", []string{"test"}},
	{"py:with", []string{"vars"}},
	{"py:replace", []string{"value"}},
	{"py:block", []string{"name"}},
	{"py:extends", []string{"href"}},
}

func lookupDirective(name string) (directive, bool) {
	for _, d := range directives {
		if d.name == name {
			return d, true
		}
	}
	return directive{}, false
}

// The html_utils tables.
var (
	htmlEmptyAttrs      = []string{"checked", "disabled", "readonly", "multiple", "selected", "nohref", "ismap", "declare", "defer"}
	htmlOptionalEndTags = []string{"area", "base", "br", "col", "hr", "img", "input", "link", "meta", "param"}
	htmlRequiredEndTags = []string{"script"}
	htmlCDATATags       = []string{"script", "style"}
)

// xmlCompileOptions are the keyword arguments of XMLTemplate that shape the
// generated code.
type xmlCompileOptions struct {
	mode         string
	isFragment   bool
	autoblocks   []string
	cdataScripts bool
	stripText    bool
}

// xmlArgs are the arguments of XMLTemplate; "" stands for None in
// filename and mode, nil for None in autoblocks and baseGlobals.
type xmlArgs struct {
	source       *string
	filename     string
	mode         string
	isFragment   bool
	encoding     string
	autoblocks   []string
	cdataScripts bool
	stripText    bool
	baseGlobals  py.Value
}

// xmlTemplate is XMLTemplate: the template class for the source, read from
// filename with codecs.open when it is nil.
func xmlTemplate(a xmlArgs) (py.Value, error) {
	var source string
	if a.source != nil {
		source = *a.source
	} else {
		text, err := codecsOpenRead(a.filename, a.encoding)
		if err != nil {
			return nil, err
		}
		source = text
	}
	filename := a.filename
	if filename == "" {
		filename = "<string>"
	}
	tree, err := compileXMLTemplate(source, filename, xmlCompileOptions{mode: a.mode, isFragment: a.isFragment,
		autoblocks: a.autoblocks, cdataScripts: a.cdataScripts, stripText: a.stripText})
	if err != nil {
		return nil, err
	}
	return fromIR(tree, a.baseGlobals)
}

// compileXMLTemplate runs the front half of XMLTemplate: parse, transform
// and compile to the intermediate representation.
func compileXMLTemplate(source, filename string, opts xmlCompileOptions) (*templateNode, error) {
	doc, err := newXMLParser(filename, source).parse()
	if err != nil {
		return nil, err
	}
	if err := transformDocument(doc, opts.stripText); err != nil {
		return nil, err
	}
	return newCompiler(filename, doc, opts).compile()
}

// transformDocument is _DomTransformer.transform: directives expanded into
// elements, text nodes merged, white space around text split off into
// nodes of its own and, with strip_text, text stripped. Like the original,
// the expansion and the merging start at the document's first child, so
// they skip the document element when a comment precedes it.
func transformDocument(doc *node, stripText bool) error {
	if _, err := expandDirectives(doc.firstChild(), doc); err != nil {
		return err
	}
	mergeTextNodes(doc.firstChild())
	extractSpaces(doc)
	if stripText {
		stripTextNodes(doc)
	}
	return nil
}

// expandDirectives is _DomTransformer._expand_directives: a directive given
// as an attribute becomes an element wrapping its element, and a directive
// element becomes a py:nop wrapped by the same kind of element.
func expandDirectives(tree, parent *node) (*node, error) {
	if !tree.isElement() {
		return tree, nil
	}
	if d, ok := lookupDirective(tree.tagName); ok {
		for _, a := range d.attrs {
			v, none := tree.get(a)
			tree.setAttribute(tree.tagName, v, none)
		}
		tree.tagName = "py:nop"
	}
	if tree.tagName != "py:nop" && tree.has("py:extends") {
		value, none := tree.get("py:extends")
		el := createElement("py:extends")
		el.setAttribute("href", value, none)
		el.lineno = tree.lineno
		if err := tree.removeAttribute("py:extends"); err != nil {
			return nil, err
		}
		tree.children = slices.Insert(tree.children, 0, el)
	}
	for _, d := range directives {
		if !tree.has(d.name) {
			continue
		}
		value, none := tree.get(d.name)
		if err := tree.removeAttribute(d.name); err != nil {
			return nil, err
		}
		el := createElement(d.name)
		el.lineno = tree.lineno
		if len(d.attrs) > 1 {
			for _, a := range d.attrs {
				v, isNone := tree.get(a)
				el.setAttribute(a, v, isNone || !tree.has(a))
			}
			if d.name == "py:case" && tree.nodeName != "py:case" {
				at, err := caseAttribute(tree.parent)
				if err != nil {
					return nil, err
				}
				el.setAttribute(at, value, none)
			}
		} else if d.attrs[0] != "" {
			el.setAttribute(d.attrs[0], value, none)
		}
		parent.replaceChild(el, tree)
		el.appendChild(tree)
		if _, err := expandDirectives(tree, el); err != nil {
			return nil, err
		}
		return el, nil
	}
	for i := 0; i < len(tree.children); i++ {
		if _, err := expandDirectives(tree.children[i], tree); err != nil {
			return nil, err
		}
	}
	return tree, nil
}

// caseAttribute is the attribute a py:case attribute's value goes to on its
// element: "on" under a py:match (a tag or an attribute), "value"
// otherwise. The document has no attributes to look in: TypeError.
func caseAttribute(parent *node) (string, error) {
	if parent == nil {
		return "", parent.noAttribute("nodeName")
	}
	if parent.nodeName == "py:match" {
		return "on", nil
	}
	if parent.kind != domElement {
		return "", py.Raise("TypeError", py.String("argument of type 'NoneType' is not iterable"))
	}
	if parent.has("py:match") {
		return "on", nil
	}
	return "value", nil
}

// mergeTextNodes is _DomTransformer._merge_text_nodes: each run of text
// nodes (CDATA markers excepted) becomes one new node.
func mergeTextNodes(tree *node) {
	if !tree.isElement() {
		return
	}
	var merged *node
	for _, child := range slices.Clone(tree.children) {
		if child.kind != domText || child.cdata {
			merged = nil
			continue
		}
		if merged == nil {
			merged = createTextNode(child.data)
			merged.lineno = child.lineno
			merged.escaped = child.escaped
			tree.replaceChild(merged, child)
		} else {
			merged.data += child.data
			tree.removeChild(child)
		}
	}
	for _, child := range tree.children {
		if child.kind != domText {
			mergeTextNodes(child)
		}
	}
}

// extractSpaces is
// _DomTransformer._extract_nodes_leading_and_trailing_spaces: the white
// space around the text of a text node goes into nodes of its own, before
// and after it. The children are walked as a list being changed, so a node
// with surrounding white space is visited again, harmlessly.
func extractSpaces(tree *node) {
	for i := 0; i < len(tree.children); i++ {
		child := tree.children[i]
		if child.kind != domText {
			extractSpaces(child)
			continue
		}
		if child.cdata || isBlank(child.data) {
			continue
		}
		if l := lstrip(child.data); len(l) != len(child.data) {
			lead := child.data[:len(child.data)-len(l)]
			begin := createTextNode(lead)
			begin.lineno = child.lineno
			begin.escaped = child.escaped
			tree.insertBefore(begin, child)
			child.lineno += strings.Count(lead, "\n")
			child.data = l
		}
		if r := rstrip(child.data); len(r) != len(child.data) {
			end := createTextNode(child.data[len(r):])
			end.lineno = child.lineno + strings.Count(r, "\n")
			end.escaped = child.escaped
			tree.replaceChild(end, child)
			tree.insertBefore(child, end)
			child.data = r
		}
	}
}

// stripTextNodes is _DomTransformer._strip_text_nodes: text stripped, its
// line moved past the white space dropped at its start.
func stripTextNodes(tree *node) {
	for _, child := range tree.children {
		if child.kind != domText {
			stripTextNodes(child)
			continue
		}
		if child.cdata {
			continue
		}
		l := lstrip(child.data)
		child.lineno += strings.Count(child.data[:len(child.data)-len(l)], "\n")
		child.data = strip(child.data)
	}
}

// emitFn receives the IR nodes a compile step produces, in order; an error
// it returns stops the step.
type emitFn func(irNode) error

// collect is list() of a compile step.
func collect(step func(emitFn) error) ([]irNode, error) {
	var out []irNode
	err := step(func(n irNode) error {
		out = append(out, n)
		return nil
	})
	return out, err
}

// compiler is _Compiler: it compiles the transformed document into the
// intermediate representation. Its steps produce nodes one at a time, as
// the original's generators do, so the checks a step makes on the nodes of
// its children happen in the same order as there.
type compiler struct {
	filename     string
	doc          *node
	isFragment   bool
	functions    []string
	bodies       map[string][]irNode
	modPy        []*pythonNode
	autoblocks   []string
	cdataScripts bool
	inDef        bool
	isChild      bool
	mode         string
}

func newCompiler(filename string, doc *node, opts xmlCompileOptions) *compiler {
	c := &compiler{filename: filename, doc: doc, isFragment: opts.isFragment, autoblocks: opts.autoblocks,
		cdataScripts: opts.cdataScripts, bodies: map[string][]irNode{}}
	c.setFunction("__main__()", nil)
	switch dtd := matchingDoctype(doc.dtd); {
	case opts.mode != "":
		c.mode = opts.mode
	case dtd != nil:
		c.mode = dtd.renderingMode
	default:
		c.mode = "xml"
	}
	return c
}

// setFunction sets a function's body, keeping the place of a function
// defined before.
func (c *compiler) setFunction(decl string, body []irNode) {
	if _, ok := c.bodies[decl]; !ok {
		c.functions = append(c.functions, decl)
	}
	c.bodies[decl] = body
}

// compile is _Compiler.compile.
func (c *compiler) compile() (*templateNode, error) {
	var roots []*node
	for _, n := range c.doc.children {
		if n.kind != domComment {
			roots = append(roots, n)
		}
	}
	if len(roots) != 1 {
		return nil, newCompileError("expected a single root node in document", c.doc, c.filename, 0)
	}
	body, err := collect(func(emit emitFn) error { return c.compileNode(roots[0], emit) })
	if err != nil {
		return nil, err
	}
	if !c.isFragment && !c.isChild {
		dtd := c.doc.dtd
		if dtd == "" && c.mode == "html5" {
			dtd = "<!DOCTYPE html>"
		}
		if dtd != "" {
			t := &textNode{text: strip(dtd) + "\n"}
			t.lineno = 1
			body = slices.Insert(body, 0, irNode(t))
		}
	}
	c.setFunction("__main__()", body)
	defs := make([]irNode, len(c.functions))
	for i, decl := range c.functions {
		defs[i] = newDefNode(decl, c.bodies[decl])
	}
	tree := newTemplateNode(c.modPy, defs)
	tree.filename = c.filename
	return tree, nil
}

// annotated is the annotate decorator: the nodes a step for dom yields get
// dom's line unless they have one.
func (c *compiler) annotated(dom *node, emit emitFn) emitFn {
	return func(n irNode) error {
		if b := n.base(); b.lineno == 0 {
			b.lineno = dom.lineno
		}
		return emit(n)
	}
}

func (c *compiler) isAutoblock(n *node) (bool, error) {
	if !slices.Contains(c.autoblocks, n.tagName) {
		return false, nil
	}
	if n.has("py:autoblock") {
		v, _ := n.get("py:autoblock")
		switch pyuni.Lower(v) {
		case "false":
			return false, n.removeAttribute("py:autoblock")
		case "true":
		default:
			return false, py.Raise("ValueError",
				py.String("py:autoblock is evaluated at compile time and only accepts True/False constants"))
		}
	}
	return true, nil
}

// compileNode is _Compiler._compile_node: the step for a node, chosen by
// its class and, for py: elements, by the last part of the tag name.
func (c *compiler) compileNode(n *node, emit emitFn) error {
	switch n.kind {
	case domComment:
		return c.compileComment(n, emit)
	case domText:
		return c.compileText(n, emit)
	case domPI:
		return c.compilePI(n, emit)
	}
	auto, err := c.isAutoblock(n)
	if err != nil {
		return err
	}
	if auto {
		n.setAttribute("name", n.tagName, false)
		return c.compileBlock(n, emit)
	}
	if !strings.HasPrefix(n.tagName, "py:") {
		return c.compileXML(n, emit)
	}
	parts := strings.Split(n.tagName, ":")
	switch parts[len(parts)-1] {
	case "replace":
		return c.compileReplace(n, emit)
	case "pi":
		return c.compilePI(n, emit)
	case "import":
		return c.compileImport(n, emit)
	case "extends":
		return c.compileExtends(n, emit)
	case "include":
		return c.compileInclude(n, emit)
	case "block":
		return c.compileBlock(n, emit)
	case "def":
		return c.compileDef(n, emit)
	case "call":
		return c.compileCall(n, emit)
	case "text":
		return c.compileText(n, emit)
	case "comment":
		return c.compileComment(n, emit)
	case "for":
		return c.compileFor(n, emit)
	case "with":
		return c.compileWith(n, emit)
	case "switch":
		return c.compileSwitch(n, emit)
	case "match":
		return c.compileMatch(n, emit)
	case "case":
		return c.compileCase(n, emit)
	case "if":
		return c.compileIf(n, emit)
	case "else":
		return c.compileElse(n, emit)
	case "nop":
		return c.compileNop(n, emit)
	case "node":
		// _compile_node dispatches to itself for py:node, forever.
		return py.Raise("RecursionError", py.String("maximum recursion depth exceeded"))
	}
	return c.compileXML(n, emit)
}

// attribute is getAttribute on an element the compiler knows to be one.
func attribute(n *node, name string) string {
	v, _ := n.get(name)
	return v
}

// compileXML is _Compiler._compile_xml: an element with its attributes
// (py:strip, py:attrs and py:content applied) and its content.
func (c *compiler) compileXML(n *node, emit emitFn) error {
	emit = c.annotated(n, emit)
	guard, content, attrs := "", "", ""
	if n.has("py:strip") {
		guard = "False"
		if g := attribute(n, "py:strip"); g != "" {
			guard = "not (" + g + ")"
		}
		if err := n.removeAttribute("py:strip"); err != nil {
			return err
		}
	}
	if err := emit(&textNode{text: "<" + n.tagName, guard: guard}); err != nil {
		return err
	}
	sorted := slices.Clone(n.attrs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })
	for _, a := range sorted {
		tc := newTextCompiler(c, a.value, n.lineno, true, true)
		value, err := collect(tc.run)
		if err != nil {
			return err
		}
		switch a.name {
		case "py:content":
			content = attribute(n, "py:content")
			continue
		case "py:attrs":
			attrs = attribute(n, "py:attrs")
			continue
		}
		if err := emit(newAttrNode(a.name, value, guard, c.mode)); err != nil {
			return err
		}
	}
	if attrs != "" {
		if err := emit(&attrsNode{attrs: attrs, guard: guard, mode: c.mode}); err != nil {
			return err
		}
	}
	html := strings.HasPrefix(c.mode, "html")
	switch {
	case content != "":
		for _, x := range []irNode{&textNode{text: ">", guard: guard}, &exprNode{text: content},
			&textNode{text: "</" + n.tagName + ">", guard: guard}} {
			if err := emit(x); err != nil {
				return err
			}
		}
	case len(n.children) > 0:
		if err := emit(&textNode{text: ">", guard: guard}); err != nil {
			return err
		}
		var err error
		if c.cdataScripts && slices.Contains(htmlCDATATags, n.tagName) {
			err = c.compileScript(n, emit)
		} else {
			err = c.compileChildren(n, emit)
		}
		if err != nil {
			return err
		}
		if !(html && slices.Contains(htmlOptionalEndTags, n.tagName)) {
			return emit(&textNode{text: "</" + n.tagName + ">", guard: guard})
		}
	case slices.Contains(htmlRequiredEndTags, n.tagName):
		return emit(&textNode{text: "></" + n.tagName + ">", guard: guard})
	case html && slices.Contains(htmlOptionalEndTags, n.tagName):
		return emit(&textNode{text: ">", guard: guard})
	case html:
		return emit(&textNode{text: "></" + n.tagName + ">", guard: guard})
	default:
		return emit(&textNode{text: "/>", guard: guard})
	}
	return nil
}

// compileScript compiles the content of a script or style element: text
// only, unescaped back to what the template had (expressions included),
// in a CDATA wrapper in xml mode; CDATA sections the template has are
// dropped, their text kept as written.
func (c *compiler) compileScript(n *node, emit emitFn) error {
	if c.mode == "xml" {
		if err := emit(&textNode{text: "/*<![CDATA[*/"}); err != nil {
			return err
		}
	}
	for _, child := range n.children {
		if child.cdata {
			continue
		}
		if child.kind != domText {
			return py.Raise("AssertionError")
		}
		err := c.compileText(child, func(x irNode) error {
			if child.escaped {
				switch t := x.(type) {
				case *textNode:
					t.text = htmlUnescape(t.text)
				case *exprNode:
					t.text = htmlUnescape(t.text)
				}
			}
			return emit(x)
		})
		if err != nil {
			return err
		}
	}
	if c.mode == "xml" {
		return emit(&textNode{text: "/*]]>*/"})
	}
	return nil
}

// compileChildren compiles an element's content, CDATA markers as text.
func (c *compiler) compileChildren(n *node, emit emitFn) error {
	for _, cn := range n.children {
		var err error
		if cn.cdata {
			err = emit(&textNode{text: cn.data})
		} else {
			err = c.compileNode(cn, emit)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileReplace(n *node, emit emitFn) error {
	return c.annotated(n, emit)(&exprNode{text: attribute(n, "value")})
}

// dataOf is node.data, an AttributeError for the elements py:text,
// py:comment and py:pi tags are.
func dataOf(n *node) (string, error) {
	if n.kind == domElement || n.kind == domDocument {
		return "", n.noAttribute("data")
	}
	return n.data, nil
}

// compilePI is _Compiler._compile_pi: a code block, kept for the module
// when it is module level (and then without a line).
func (c *compiler) compilePI(n *node, emit emitFn) error {
	data, err := dataOf(n)
	if err != nil {
		return err
	}
	p, err := newPythonNode(strip(data))
	if err != nil {
		return err
	}
	if p.moduleLevel {
		c.modPy = append(c.modPy, p)
		return nil
	}
	return c.annotated(n, emit)(p)
}

func (c *compiler) compileImport(n *node, emit emitFn) error {
	var alias *string
	if n.has("alias") {
		a := attribute(n, "alias")
		alias = &a
	}
	return c.annotated(n, emit)(newImportNode(attribute(n, "href"), alias))
}

func (c *compiler) compileExtends(n *node, emit emitFn) error {
	emit = c.annotated(n, emit)
	c.isChild = true
	if err := emit(&extendNode{tplName: attribute(n, "href")}); err != nil {
		return err
	}
	return c.compileNop(n, emit)
}

func (c *compiler) compileInclude(n *node, emit emitFn) error {
	return c.annotated(n, emit)(&includeNode{tplName: attribute(n, "href")})
}

// compileBlock is _Compiler._compile_block: the block becomes a function;
// a child template's block starts by binding its parent's, any other
// renders in place.
func (c *compiler) compileBlock(n *node, emit emitFn) error {
	fname := "_kj_block_" + attribute(n, "name")
	decl := fname + "()"
	body, err := collect(func(e emitFn) error { return c.compileNop(n, e) })
	if err != nil {
		return err
	}
	if len(body) == 0 {
		body = []irNode{&passNode{}}
	}
	if c.isChild {
		p, err := newPythonNode("parent_block=parent." + fname)
		if err != nil {
			return err
		}
		body = slices.Insert(body, 0, irNode(p))
		c.setFunction(decl, body)
		return nil
	}
	c.setFunction(decl, body)
	return c.annotated(n, emit)(&exprNode{text: decl})
}

// compileDef is _Compiler._compile_def: a top-level py:def is a template
// function, one inside another a flattening inner function.
func (c *compiler) compileDef(n *node, emit emitFn) error {
	old := c.inDef
	c.inDef = true
	body, err := collect(func(e emitFn) error { return c.compileNop(n, e) })
	if err != nil {
		return err
	}
	c.inDef = old
	if c.inDef {
		d := newDefNode(attribute(n, "function"), body)
		d.inner = true
		return c.annotated(n, emit)(d)
	}
	c.setFunction(attribute(n, "function"), body)
	return nil
}

// compileCall is _Compiler._compile_call: the args of the element under the
// directive declare the caller's parameters.
func (c *compiler) compileCall(n *node, emit emitFn) error {
	emit = c.annotated(n, emit)
	if len(n.children) == 0 {
		return py.Raise("IndexError", py.String("list index out of range"))
	}
	first := n.children[0]
	hasArgs, err := first.hasAttribute("args")
	if err != nil {
		return err
	}
	defn := "$caller()"
	if hasArgs {
		defn = "$caller(" + attribute(first, "args") + ")"
	}
	body, err := collect(func(e emitFn) error { return c.compileNop(n, e) })
	if err != nil {
		return err
	}
	return emit(newCallNode(defn, strings.ReplaceAll(attribute(n, "function"), "%caller", "$caller"), body))
}

// compileText is _Compiler._compile_text: text with expressions; the text
// of script and style is never translated.
func (c *compiler) compileText(n *node, emit emitFn) error {
	emit = c.annotated(n, emit)
	plain := false
	if p := n.parent; p != nil {
		if !p.isElement() {
			return p.noAttribute("tagName")
		}
		plain = slices.Contains(htmlCDATATags, p.tagName)
	}
	data, err := dataOf(n)
	if err != nil {
		return err
	}
	return newTextCompiler(c, data, n.lineno, plain, false).run(emit)
}

// compileComment is _Compiler._compile_comment: "<!--!" comments are
// dropped, the others written back with a space around the text.
func (c *compiler) compileComment(n *node, emit emitFn) error {
	data, err := dataOf(n)
	if err != nil {
		return err
	}
	if strings.HasPrefix(data, "!") {
		return nil
	}
	return c.annotated(n, emit)(&textNode{text: "<!-- " + data + " -->"})
}

// body compiles an element's content for a directive's node.
func (c *compiler) body(n *node) ([]irNode, error) {
	return collect(func(e emitFn) error { return c.compileNop(n, e) })
}

func (c *compiler) compileFor(n *node, emit emitFn) error {
	body, err := c.body(n)
	if err != nil {
		return err
	}
	return c.annotated(n, emit)(&forNode{hier: newHier(body), decl: attribute(n, "each")})
}

func (c *compiler) compileWith(n *node, emit emitFn) error {
	body, err := c.body(n)
	if err != nil {
		return err
	}
	w, err := newWithNode(attribute(n, "vars"), body)
	if err != nil {
		return err
	}
	return c.annotated(n, emit)(w)
}

// isBlankText reports whether x is a text node (translatable or not) with
// only white space.
func isBlankText(x irNode) bool {
	switch t := x.(type) {
	case *textNode:
		return isBlank(t.text)
	case *translatableTextNode:
		return isBlank(t.text)
	}
	return false
}

// compileSwitch is _Compiler._compile_switch: only cases and an else, white
// space text between them dropped.
func (c *compiler) compileSwitch(n *node, emit emitFn) error {
	var body []irNode
	err := c.compileNop(n, func(x irNode) error {
		if isBlankText(x) {
			return nil
		}
		switch x.(type) {
		case *caseNode, *elseNode:
			body = append(body, x)
			return nil
		}
		return newCompileError("py:switch directive can only contain py:case and py:else nodes and cannot be "+
			"placed on a tag.", c.doc, c.filename, n.lineno)
	})
	if err != nil {
		return err
	}
	return c.annotated(n, emit)(&switchNode{hier: newHier(body), decl: attribute(n, "test")})
}

// compileMatch is _Compiler._compile_match: only match cases.
func (c *compiler) compileMatch(n *node, emit emitFn) error {
	var body []irNode
	err := c.compileNop(n, func(x irNode) error {
		if isBlankText(x) {
			return nil
		}
		if _, ok := x.(*matchCaseNode); ok {
			body = append(body, x)
			return nil
		}
		return newCompileError("py:match directive can only contain py:case nodes and cannot be placed on a tag.",
			c.doc, c.filename, n.lineno)
	})
	if err != nil {
		return err
	}
	return c.annotated(n, emit)(&matchNode{hier: newHier(body), decl: attribute(n, "on")})
}

func (c *compiler) compileCase(n *node, emit emitFn) error {
	if v := attribute(n, "value"); v != "" {
		body, err := c.body(n)
		if err != nil {
			return err
		}
		return c.annotated(n, emit)(&caseNode{hier: newHier(body), decl: v})
	}
	if m := attribute(n, "match"); m != "" {
		body, err := c.body(n)
		if err != nil {
			return err
		}
		return c.annotated(n, emit)(&matchCaseNode{hier: newHier(body), decl: m})
	}
	return newCompileError("case must have either value or match attribute, the former for py:switch, the latter "+
		"for py:match", c.doc, c.filename, n.lineno)
}

func (c *compiler) compileIf(n *node, emit emitFn) error {
	body, err := c.body(n)
	if err != nil {
		return err
	}
	return c.annotated(n, emit)(&ifNode{hier: newHier(body), decl: attribute(n, "test")})
}

// compileElse is _Compiler._compile_else: py:else belongs in a switch or
// right after a py:if.
func (c *compiler) compileElse(n *node, emit emitFn) error {
	if p := n.parent; p.tagNameOr("") != "py:nop" {
		inSwitch, err := p.hasAttribute("py:switch")
		if err != nil {
			return err
		}
		if !inSwitch && n.previous.tagNameOr("") != "py:if" {
			return newCompileError("py:else directive must be inside a py:switch or directly after py:if without "+
				"text or spaces in between", c.doc, c.filename, n.lineno)
		}
	}
	body, err := c.body(n)
	if err != nil {
		return err
	}
	return c.annotated(n, emit)(&elseNode{hier: newHier(body)})
}

func (c *compiler) compileNop(n *node, emit emitFn) error {
	emit = c.annotated(n, emit)
	for _, child := range n.children {
		if err := c.compileNode(child, emit); err != nil {
			return err
		}
	}
	return nil
}

// textCompiler is _TextCompiler: it splits text into literal text and
// expressions ($name, ${...}, $$ for $).
type textCompiler struct {
	c          *compiler
	source     []rune
	origLineno int
	lineno     int
	pos        int
	// plain selects ir.TextNode for the text; otherwise make_text_node,
	// translatable when there is more than white space.
	plain      bool
	inHTMLAttr bool
}

func newTextCompiler(c *compiler, source string, lineno int, plain, inHTMLAttr bool) *textCompiler {
	return &textCompiler{c: c, source: []rune(source), origLineno: lineno, plain: plain, inHTMLAttr: inHTMLAttr}
}

// textExpr is _TextCompiler._pattern.
var textExpr = pyre.MustCompile(`
    \$(?:
        (?P<expr_named>[_a-z][_a-z0-9.]*) | # $foo.bar
        {(?P<expr_braced>) | # ${....
        \$ # $$ -> $
    )`, pyre.VERBOSE|pyre.IGNORECASE|pyre.MULTILINE)

var (
	textExprNamed, _  = textExpr.GroupIndexOf("expr_named")
	textExprBraced, _ = textExpr.GroupIndexOf("expr_braced")
)

func (tc *textCompiler) text(s []rune) irNode {
	text := string(s)
	var n irNode
	if tc.plain || isBlank(text) {
		n = &textNode{text: text}
	} else {
		n = &translatableTextNode{text: text}
	}
	n.base().lineno = tc.origLineno + tc.lineno
	tc.lineno += strings.Count(text, "\n")
	return n
}

func (tc *textCompiler) expr(text string) irNode {
	n := &exprNode{text: text, safe: tc.inHTMLAttr}
	n.lineno = tc.origLineno + tc.lineno
	tc.lineno += strings.Count(text, "\n")
	return n
}

// run is _TextCompiler.__iter__. The matches are found in the whole text
// up front, so a "$" inside a braced expression is matched again after the
// expression, as in the original.
func (tc *textCompiler) run(emit emitFn) error {
	src := tc.source
	for _, m := range textExpr.FindIter(src, 0, len(src)) {
		start := m.Start(0)
		if start > tc.pos {
			if err := emit(tc.text(src[tc.pos:start])); err != nil {
				return err
			}
		}
		tc.pos = start
		var node irNode
		if _, ok := m.Group(textExprBraced); ok {
			tc.pos = m.End(0)
			var err error
			if node, err = tc.bracedExpr(); err != nil {
				return err
			}
		} else if name, ok := m.Group(textExprNamed); ok {
			tc.pos = m.End(0)
			node = tc.expr(name)
		} else {
			tc.pos = m.End(0)
			node = tc.text([]rune("$"))
		}
		if err := emit(node); err != nil {
			return err
		}
	}
	if tc.pos != len(src) {
		return emit(tc.text(src[min(tc.pos, len(src)):]))
	}
	return nil
}

// slice is source[from:to] with Python's clamping.
func (tc *textCompiler) slice(from, to int) []rune {
	from = max(0, min(from, len(tc.source)))
	to = max(from, min(to, len(tc.source)))
	return tc.source[from:to]
}

// bracedExpr is _TextCompiler._get_braced_expr: the expression is what
// precedes the "}" at which Python's compiler first fails on the rest of
// the text.
func (tc *textCompiler) bracedExpr() (irNode, error) {
	rest := string(tc.slice(tc.pos, len(tc.source)))
	tc.pos += runeLen(rest) - runeLen(lstrip(rest))
	rest = string(tc.slice(tc.pos, len(tc.source)))
	_, err := py.Compile(rest, "find_}", "eval")
	if err == nil {
		return nil, newCompileError("Braced expression not terminated", tc.c.doc, tc.c.filename, tc.lineno)
	}
	pe, ok := err.(*py.Error)
	if !ok || (pe.Class != "SyntaxError" && pe.Class != "IndentationError" && pe.Class != "TabError") {
		return nil, err
	}
	lineno, offset, err := syntaxErrorPosition(pe)
	if err != nil {
		return nil, err
	}
	end := tc.pos + offset
	for idx, line := range splitLines(rest) {
		if idx < lineno-1 {
			end += runeLen(line) + 1
		}
	}
	whole := []rune(rest)
	unclosed := string(whole[:max(len(whole)-1, 0)])
	upToEnd := tc.slice(tc.pos, end)
	if len(upToEnd) == 0 {
		return nil, py.Raise("IndexError", py.String("string index out of range"))
	}
	if upToEnd[len(upToEnd)-1] != '}' {
		return nil, newCompileError("Kajiki can't compile the python expression `"+unclosed+"`", tc.c.doc,
			tc.c.filename, tc.lineno)
	}
	text := string(tc.slice(tc.pos, end-1))
	if _, err := py.Compile(text, "check_validity", "eval"); err != nil {
		if ce, ok := err.(*py.Error); !ok || (ce.Class != "SyntaxError" && ce.Class != "IndentationError" &&
			ce.Class != "TabError") {
			return nil, err
		}
		return nil, newCompileError("Kajiki detected an invalid python expression `"+unclosed+"`", tc.c.doc,
			tc.c.filename, tc.lineno)
	}
	tc.pos = end
	return tc.expr(text), nil
}

// syntaxErrorPosition reads a SyntaxError's lineno and offset as
// _get_braced_expr uses them; None in either is the TypeError of the
// arithmetic on it.
func syntaxErrorPosition(e *py.Error) (lineno, offset int, err error) {
	read := func(name string) (int, bool, error) {
		v, err := py.GetAttr(e.Value(), name)
		if err != nil {
			return 0, false, err
		}
		i, ok := py.AsInt(v)
		if !ok {
			return 0, false, nil
		}
		return int(i.Int64()), true, nil
	}
	lineno, hasLine, err := read("lineno")
	if err != nil {
		return 0, 0, err
	}
	offset, hasOffset, err := read("offset")
	if err != nil {
		return 0, 0, err
	}
	if !hasLine {
		return 0, 0, py.Raise("TypeError", py.String("unsupported operand type(s) for -: 'NoneType' and 'int'"))
	}
	if !hasOffset {
		return 0, 0, py.Raise("TypeError", py.String("unsupported operand type(s) for +: 'int' and 'NoneType'"))
	}
	return lineno, offset, nil
}
