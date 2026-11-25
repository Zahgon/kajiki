// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/xml_template.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"errors"
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/entities"
	"github.com/jackrosenthal/kajiki-go/internal/expat"
	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// kajikiDTD is the doctype the parser puts in place of the template's own:
// with an external subset that is never read, expat reports the HTML
// entities it does not know as skipped instead of failing.
const kajikiDTD = `<!DOCTYPE kajiki SYSTEM "kajiki.dtd">`

// saxBufferSize is the number of characters xml.sax's expat reader reads
// from the source at a time (ExpatParser's default bufsize, 2**16 - 20).
const saxBufferSize = 1<<16 - 20

// xmlParser is _Parser: it reads a template into a minidom-like document
// through xml.sax's expat reader with namespace processing off, the
// template's doctype replaced by kajikiDTD while parsing.
type xmlParser struct {
	filename string
	source   string
	doc      *node
	els      []*node
	cdata    int
	expat    *expat.Parser
}

func newXMLParser(filename, source string) *xmlParser {
	p := &xmlParser{filename: filename, doc: newDocument()}
	dtd, position, rest := extractDTD(source)
	p.doc.dtd = dtd
	p.source = rest[:position] + kajikiDTD + rest[position:]
	return p
}

// parse is _Parser.parse: it returns the document, an XMLTemplateParseError
// for a document expat rejects, or the exception a handler raised (the
// KeyError of an unknown HTML entity).
func (p *xmlParser) parse() (*node, error) {
	if err := p.run(); err != nil {
		var xe *expat.Error
		if errors.As(err, &xe) {
			return nil, newParseError(expat.ErrorString(xe.Code), p.source, p.filename, xe.Lineno, xe.Offset)
		}
		return nil, err
	}
	p.doc.source = p.source
	p.doc.hasSource = true
	return p.doc, nil
}

// run feeds the source to expat as xml.sax's IncrementalParser does: the
// document starts with the first piece read, which is never empty, and
// close() ends it with a final empty piece.
func (p *xmlParser) run() error {
	ep := expat.NewParser()
	p.expat = ep
	ep.StartElementHandler = p.startElement
	ep.EndElementHandler = p.endElement
	ep.CharacterDataHandler = p.characters
	ep.ProcessingInstructionHandler = p.processingInstruction
	ep.CommentHandler = p.comment
	ep.StartCdataSectionHandler = p.startCDATA
	ep.EndCdataSectionHandler = p.endCDATA
	// Kajiki's startDTD only records a DocumentType nobody reads, and the
	// reader resolves no external entity (feature_external_ges is off): it
	// accepts the reference without reading anything.
	ep.StartDoctypeDeclHandler = func(string, *string, *string, bool) error { return nil }
	ep.ExternalEntityRefHandler = func(context, base, systemID, publicID *string) (int, error) { return 1, nil }
	ep.SkippedEntityHandler = p.skippedEntity
	ep.SetParamEntityParsing(expat.ParamEntityParsingUnlessStandalone)
	p.els = append(p.els, p.doc)
	runes := []rune(p.source)
	for start := 0; start < len(runes); start += saxBufferSize {
		end := min(start+saxBufferSize, len(runes))
		if err := ep.Parse(string(runes[start:end]), false); err != nil {
			return err
		}
	}
	return ep.Parse("", true)
}

func (p *xmlParser) line() int { return p.expat.CurrentLineNumber() }

func (p *xmlParser) top() *node { return p.els[len(p.els)-1] }

func (p *xmlParser) startElement(name string, attrs []string) error {
	el := createElement(name)
	el.lineno = p.line()
	for i := 0; i < len(attrs); i += 2 {
		el.setAttribute(attrs[i], attrs[i+1], false)
	}
	p.top().appendChild(el)
	p.els = append(p.els, el)
	return nil
}

func (p *xmlParser) endElement(string) error {
	p.els = p.els[:len(p.els)-1]
	return nil
}

// characters escapes the text outside CDATA sections with
// xml.sax.saxutils.escape, so the compiler sees entities as they would be
// written back.
func (p *xmlParser) characters(content string) error {
	escape := p.cdata == 0
	if escape {
		content = saxEscape(content)
	}
	n := createTextNode(content)
	n.lineno = p.line()
	n.escaped = escape
	p.top().appendChild(n)
	return nil
}

func (p *xmlParser) processingInstruction(target, data string) error {
	n := &node{kind: domPI, target: target, data: data, lineno: p.line()}
	p.top().appendChild(n)
	return nil
}

// skippedEntity resolves an HTML entity expat skipped through
// html.entities.html5, always asking for the name with its semicolon.
func (p *xmlParser) skippedEntity(name string, isParameterEntity bool) error {
	if isParameterEntity {
		name = "%" + name
	}
	if name != "" && !strings.HasSuffix(name, ";") {
		name += ";"
	}
	text, ok := entities.HTML5[name]
	if !ok {
		return py.Raise("KeyError", py.String(name))
	}
	return p.characters(text)
}

func (p *xmlParser) comment(text string) error {
	n := &node{kind: domComment, data: text, lineno: p.line()}
	p.top().appendChild(n)
	return nil
}

func (p *xmlParser) startCDATA() error {
	n := createTextNode("<![CDATA[")
	n.cdata = true
	n.lineno = p.line()
	p.top().appendChild(n)
	p.cdata++
	return nil
}

func (p *xmlParser) endCDATA() error {
	n := createTextNode("]]>")
	n.cdata = true
	n.lineno = p.line()
	p.top().appendChild(n)
	p.cdata--
	return nil
}

// saxEscape is xml.sax.saxutils.escape: &, < and > as entities.
func saxEscape(s string) string {
	if !strings.ContainsAny(s, "&<>") {
		return s
	}
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return strings.ReplaceAll(s, "<", "&lt;")
}
