// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/xml_template.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"slices"
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// nodeKind is the class of a DOM node.
type nodeKind int

const (
	domDocument nodeKind = iota
	domElement
	domText
	domComment
	domPI
)

// className is the Python class name of the minidom node kind, as
// AttributeError messages show it.
func (k nodeKind) className() string {
	switch k {
	case domDocument:
		return "Document"
	case domElement:
		return "Element"
	case domText:
		return "Text"
	case domComment:
		return "Comment"
	}
	return "ProcessingInstruction"
}

// attr is an attribute value; none marks the None a directive element gets
// for a missing attribute (see expandDirectives).
type attr struct {
	name  string
	value string
	none  bool
}

// node is the subset of xml.dom.minidom the template parser builds and the
// transformer and compiler walk, with minidom's own semantics where Kajiki
// depends on them: an element's tagName can be changed while its nodeName
// keeps the name it was created with, childNodes is a plain list that can
// be changed without updating the sibling links, and previousSibling and
// nextSibling are maintained only by appendChild, insertBefore,
// replaceChild and removeChild.
type node struct {
	kind                   nodeKind
	parent, previous, next *node
	children               []*node

	// Element.
	tagName, nodeName string
	attrs             []attr

	// Text, Comment and ProcessingInstruction.
	data   string
	target string

	// The attributes Kajiki's parser sets: the source line, whether the
	// text was escaped when it was read (false inside CDATA sections), and
	// cdata for the text nodes that stand for the CDATA section markers.
	lineno  int
	escaped bool
	cdata   bool

	// Document: the doctype Kajiki extracted and the source it parsed.
	dtd       string
	source    string
	hasSource bool
}

func newDocument() *node { return &node{kind: domDocument} }

// createElement is Document.createElement.
func createElement(tagName string) *node {
	return &node{kind: domElement, tagName: tagName, nodeName: tagName}
}

// createTextNode is Document.createTextNode.
func createTextNode(data string) *node { return &node{kind: domText, data: data} }

// noAttribute is the AttributeError of reading the attribute name of a
// node that does not have it; n nil is None.
func (n *node) noAttribute(name string) error {
	class := "NoneType"
	if n != nil {
		class = n.kind.className()
	}
	return py.Raise("AttributeError", py.String("'"+class+"' object has no attribute '"+name+"'"))
}

// firstChild is Node.firstChild.
func (n *node) firstChild() *node {
	if len(n.children) == 0 {
		return nil
	}
	return n.children[0]
}

// isElement is isinstance(getattr(n, "tagName", None), str).
func (n *node) isElement() bool { return n != nil && n.kind == domElement }

// tagNameOr is getattr(n, "tagName", def).
func (n *node) tagNameOr(def string) string {
	if n.isElement() {
		return n.tagName
	}
	return def
}

func (n *node) attrIndex(name string) int {
	return slices.IndexFunc(n.attrs, func(a attr) bool { return a.name == name })
}

// hasAttribute is Element.hasAttribute.
func (n *node) hasAttribute(name string) (bool, error) {
	if !n.isElement() {
		return false, n.noAttribute("hasAttribute")
	}
	return n.attrIndex(name) >= 0, nil
}

// has is hasAttribute on a node known to be an element.
func (n *node) has(name string) bool { return n.attrIndex(name) >= 0 }

// get is Element.getAttribute on a node known to be an element: "" for a
// missing attribute; the second result reports a None value.
func (n *node) get(name string) (string, bool) {
	if i := n.attrIndex(name); i >= 0 {
		return n.attrs[i].value, n.attrs[i].none
	}
	return "", false
}

// setAttribute is Element.setAttribute: an existing attribute keeps its
// place; a new one goes last, replacing the attribute with the same local
// name (the part after the first colon) if there is one, since minidom's
// namespace map keys attributes by (namespace, local name) and every
// attribute here has no namespace.
func (n *node) setAttribute(name, value string, none bool) {
	if i := n.attrIndex(name); i >= 0 {
		n.attrs[i].value, n.attrs[i].none = value, none
		return
	}
	local := localName(name)
	if i := slices.IndexFunc(n.attrs, func(a attr) bool { return localName(a.name) == local }); i >= 0 {
		n.attrs = slices.Delete(n.attrs, i, i+1)
	}
	n.attrs = append(n.attrs, attr{name: name, value: value, none: none})
}

// localName is Attr.localName of an attribute made by setAttribute.
func localName(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// removeAttribute is Element.removeAttribute.
func (n *node) removeAttribute(name string) error {
	i := n.attrIndex(name)
	if i < 0 {
		return newNotFoundErr()
	}
	n.attrs = slices.Delete(n.attrs, i, i+1)
	return nil
}

// appendChild is Node.appendChild.
func (n *node) appendChild(c *node) {
	if c.parent != nil {
		c.parent.removeChild(c)
	}
	if len(n.children) > 0 {
		last := n.children[len(n.children)-1]
		c.previous = last
		last.next = c
	}
	n.children = append(n.children, c)
	c.parent = n
	c.next = nil
}

func (n *node) indexOf(c *node) int { return slices.Index(n.children, c) }

// insertBefore is Node.insertBefore with a child ref of n.
func (n *node) insertBefore(c, ref *node) {
	if c.parent != nil {
		c.parent.removeChild(c)
	}
	i := n.indexOf(ref)
	n.children = slices.Insert(n.children, i, c)
	c.next = ref
	ref.previous = c
	if i > 0 {
		prev := n.children[i-1]
		prev.next = c
		c.previous = prev
	} else {
		c.previous = nil
	}
	c.parent = n
}

// replaceChild is Node.replaceChild with a child old of n.
func (n *node) replaceChild(c, old *node) {
	if c == old {
		return
	}
	if c.parent != nil {
		c.parent.removeChild(c)
	}
	n.children[n.indexOf(old)] = c
	c.parent = n
	old.parent = nil
	c.next = old.next
	c.previous = old.previous
	old.next = nil
	old.previous = nil
	if c.previous != nil {
		c.previous.next = c
	}
	if c.next != nil {
		c.next.previous = c
	}
}

// removeChild is Node.removeChild (Document.removeChild for the document)
// with a child c of n.
func (n *node) removeChild(c *node) {
	i := n.indexOf(c)
	n.children = slices.Delete(n.children, i, i+1)
	if n.kind != domDocument {
		if c.next != nil {
			c.next.previous = c.previous
		}
		if c.previous != nil {
			c.previous.next = c.next
		}
	}
	c.next, c.previous = nil, nil
	c.parent = nil
}
