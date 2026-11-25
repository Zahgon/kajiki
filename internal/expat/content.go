// Ported from libexpat 2.8.5 (expat/lib/xmlparse.c) to Go, 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Licensed under the MIT license.
// SPDX-License-Identifier: MIT

package expat

import (
	"strings"
	"unicode/utf8"
)

func contentProcessor(p *Parser, s, end int, nextPtr *int) int {
	startTagLevel := 0
	if p.parentParser != nil {
		startTagLevel = 1
	}
	return p.doContent(startTagLevel, p.encoding, p.buf, s, end, nextPtr, !p.finalBuffer)
}

func externalEntityInitProcessor(p *Parser, s, end int, nextPtr *int) int {
	p.initializeEncoding()
	p.processor = externalEntityInitProcessor2
	return externalEntityInitProcessor2(p, s, end, nextPtr)
}

func externalEntityInitProcessor2(p *Parser, s, end int, nextPtr *int) int {
	next := s
	tok := p.encoding.tok(scanContent, p.buf, s, end, &next)
	switch tok {
	case tokBOM:
		if next == end && !p.finalBuffer {
			*nextPtr = next
			return ErrNone
		}
		s = next
	case tokPartial:
		if !p.finalBuffer {
			*nextPtr = s
			return ErrNone
		}
		p.eventPtr = s
		return ErrUnclosedToken
	case tokPartialChar:
		if !p.finalBuffer {
			*nextPtr = s
			return ErrNone
		}
		p.eventPtr = s
		return ErrPartialChar
	}
	p.processor = externalEntityInitProcessor3
	return externalEntityInitProcessor3(p, s, end, nextPtr)
}

func externalEntityInitProcessor3(p *Parser, s, end int, nextPtr *int) int {
	next := s
	p.eventPtr = s
	tok := p.encoding.tok(scanContent, p.buf, s, end, &next)
	p.eventEndPtr = next
	switch tok {
	case tokXMLDecl:
		if r := p.processXMLDecl(true, s, next); r != ErrNone {
			return r
		}
		if p.parsing == stateFinished {
			return ErrAborted
		}
		s = next
	case tokPartial:
		if !p.finalBuffer {
			*nextPtr = s
			return ErrNone
		}
		return ErrUnclosedToken
	case tokPartialChar:
		if !p.finalBuffer {
			*nextPtr = s
			return ErrNone
		}
		return ErrPartialChar
	}
	p.processor = externalEntityContentProcessor
	p.tagLevel = 1
	return externalEntityContentProcessor(p, s, end, nextPtr)
}

func externalEntityContentProcessor(p *Parser, s, end int, nextPtr *int) int {
	return p.doContent(1, p.encoding, p.buf, s, end, nextPtr, !p.finalBuffer)
}

// convertedData delivers the character data b[s:next] in enc to
// CharacterDataHandler, in pieces of at most dataBufSize bytes of UTF-8 when
// enc needs converting. cdata selects doCdataSection's event end updates.
func (p *Parser) convertedData(enc *encoding, b []byte, s, next int, cdata bool) {
	if enc.isUTF8 {
		p.characterData(b[s:next])
		return
	}
	eventPP, eventEndPP := p.eventPointers(enc)
	for {
		out, res := enc.toUTF8(b, &s, next, nil, dataBufSize)
		if cdata {
			*eventEndPP = next
		} else {
			*eventEndPP = s
		}
		p.characterData(out)
		if res == convertCompleted || res == convertInputIncomplete {
			return
		}
		*eventPP = s
	}
}

var newline = []byte{'\n'}

// doContent parses content in b[s:end]: the document element, an external
// parsed entity or an internal entity's replacement text.
func (p *Parser) doContent(startTagLevel int, enc *encoding, b []byte, s, end int, nextPtr *int, haveMore bool) int {
	d := p.dtd
	eventPP, eventEndPP := p.eventPointers(enc)
	*eventPP = s
	mb := enc.minBPC
	for {
		next := s
		tok := enc.tok(scanContent, b, s, end, &next)
		*eventEndPP = next
		switch tok {
		case tokTrailingCr:
			if haveMore {
				*nextPtr = s
				return ErrNone
			}
			*eventEndPP = end
			if p.CharacterDataHandler != nil {
				p.characterData(newline)
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, end)
			}
			if startTagLevel == 0 {
				return ErrNoElements
			}
			if p.tagLevel != startTagLevel {
				return ErrAsyncEntity
			}
			*nextPtr = end
			return ErrNone
		case tokNone:
			if haveMore {
				*nextPtr = s
				return ErrNone
			}
			if startTagLevel > 0 {
				if p.tagLevel != startTagLevel {
					return ErrAsyncEntity
				}
				*nextPtr = s
				return ErrNone
			}
			return ErrNoElements
		case tokInvalid:
			*eventPP = next
			return ErrInvalidToken
		case tokPartial:
			if haveMore {
				*nextPtr = s
				return ErrNone
			}
			return ErrUnclosedToken
		case tokPartialChar:
			if haveMore {
				*nextPtr = s
				return ErrNone
			}
			return ErrPartialChar
		case tokEntityRef:
			if ch := enc.predefinedEntityName(b, s+mb, next-mb); ch != 0 {
				if p.CharacterDataHandler != nil {
					p.characterData([]byte{ch})
				} else if p.DefaultHandler != nil {
					p.reportDefault(enc, b, s, next)
				}
				break
			}
			name := enc.storeString(b, s+mb, next-mb)
			e := d.generalEntities[name]
			if !d.hasParamEntityRefs || d.standalone {
				if e == nil {
					return ErrUndefinedEntity
				} else if !e.isInternal {
					return ErrEntityDeclaredInPE
				}
			} else if e == nil {
				if p.SkippedEntityHandler != nil {
					p.event(func() error { return p.SkippedEntityHandler(name, false) })
				} else if p.DefaultHandler != nil {
					p.reportDefault(enc, b, s, next)
				}
				break
			}
			if e.open {
				return ErrRecursiveEntityRef
			}
			if e.notation != nil {
				return ErrBinaryEntityRef
			}
			if e.hasText {
				if p.DefaultHandler != nil {
					if p.SkippedEntityHandler != nil {
						p.event(func() error { return p.SkippedEntityHandler(e.name, false) })
					} else {
						p.reportDefault(enc, b, s, next)
					}
					break
				}
				p.processEntity(e, false, entityInternal)
			} else if p.ExternalEntityRefHandler != nil {
				e.open = true
				context := p.getContext()
				e.open = false
				status := p.externalEntityRef(&context, e.systemID, e.publicID)
				if status == 0 {
					return ErrExternalEntityHandling
				}
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
		case tokStartTagNoAtts, tokStartTagWithAtts:
			t := &tag{parent: p.tagStack}
			p.tagStack = t
			rawLen := enc.nameLength(b, s+mb)
			t.rawName = append([]byte(nil), b[s+mb:s+mb+rawLen]...)
			p.tagLevel++
			name := enc.storeString(b, s+mb, s+mb+rawLen)
			expanded, atts, r := p.storeAtts(enc, b, s, name, &t.bindings)
			if r != ErrNone {
				return r
			}
			t.name = expanded
			if p.StartElementHandler != nil {
				p.event(func() error { return p.StartElementHandler(expanded, atts) })
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
		case tokEmptyElementNoAtts, tokEmptyElementWithAtts:
			rawName := s + mb
			name := enc.storeString(b, rawName, rawName+enc.nameLength(b, rawName))
			var bindings *binding
			expanded, atts, r := p.storeAtts(enc, b, s, name, &bindings)
			if r != ErrNone {
				p.freeBindings(bindings)
				return r
			}
			noElmHandlers := true
			if p.StartElementHandler != nil {
				p.event(func() error { return p.StartElementHandler(expanded, atts) })
				noElmHandlers = false
			}
			if p.EndElementHandler != nil {
				if p.StartElementHandler != nil {
					*eventPP = *eventEndPP
				}
				p.event(func() error { return p.EndElementHandler(expanded) })
				noElmHandlers = false
			}
			if noElmHandlers && p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
			p.freeBindings(bindings)
			if p.tagLevel == 0 && p.parsing != stateFinished {
				if p.parsing == stateParsing && p.reenter {
					p.processor = epilogProcessor
				} else {
					return epilogProcessor(p, next, end, nextPtr)
				}
			}
		case tokEndTag:
			if p.tagLevel == startTagLevel {
				return ErrAsyncEntity
			}
			t := p.tagStack
			rawName := s + mb*2
			n := enc.nameLength(b, rawName)
			if n != len(t.rawName) || string(b[rawName:rawName+n]) != string(t.rawName) {
				*eventPP = rawName
				return ErrTagMismatch
			}
			p.tagStack = t.parent
			p.tagLevel--
			if p.EndElementHandler != nil {
				name := t.name
				p.event(func() error { return p.EndElementHandler(name) })
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
			for t.bindings != nil {
				bd := t.bindings
				if p.EndNamespaceDeclHandler != nil {
					pr := bd.prefix.name
					p.event(func() error { return p.EndNamespaceDeclHandler(pr) })
				}
				t.bindings = bd.nextTagBinding
				bd.prefix.binding = bd.prevPrefixBinding
			}
			if p.tagLevel == 0 && p.parsing != stateFinished {
				if p.parsing == stateParsing && p.reenter {
					p.processor = epilogProcessor
				} else {
					return epilogProcessor(p, next, end, nextPtr)
				}
			}
		case tokCharRef:
			n := enc.charRefNumber(b, s)
			if n < 0 {
				return ErrBadCharRef
			}
			if p.CharacterDataHandler != nil {
				p.characterData(utf8.AppendRune(nil, rune(n)))
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
		case tokXMLDecl:
			return ErrMisplacedXMLPI
		case tokDataNewline:
			if p.CharacterDataHandler != nil {
				p.characterData(newline)
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
		case tokCdataSectOpen:
			if p.StartCdataSectionHandler != nil {
				p.event(p.StartCdataSectionHandler)
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
			closed, r := p.doCdataSection(enc, b, &next, end, nextPtr, haveMore)
			if r != ErrNone {
				return r
			} else if !closed {
				p.processor = cdataSectionProcessor
				return r
			}
		case tokTrailingRsqb:
			if haveMore {
				*nextPtr = s
				return ErrNone
			}
			if p.CharacterDataHandler != nil {
				if !enc.isUTF8 {
					out, _ := enc.toUTF8(b, &s, end, nil, dataBufSize)
					p.characterData(out)
				} else {
					p.characterData(b[s:end])
				}
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, end)
			}
			if startTagLevel == 0 {
				*eventPP = end
				return ErrNoElements
			}
			if p.tagLevel != startTagLevel {
				*eventPP = end
				return ErrAsyncEntity
			}
			*nextPtr = end
			return ErrNone
		case tokDataChars:
			if p.CharacterDataHandler != nil {
				p.convertedData(enc, b, s, next, false)
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
		case tokPI:
			p.reportProcessingInstruction(enc, b, s, next)
		case tokComment:
			p.reportComment(enc, b, s, next)
		default:
			if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
		}
		switch {
		case p.parsing == stateFinished:
			*eventPP = next
			return ErrAborted
		case p.reenter:
			*nextPtr = next
			return ErrNone
		}
		s = next
		*eventPP = s
	}
}

// freeBindings ends the namespace declarations of an empty element (or of
// a start-tag in error).
func (p *Parser) freeBindings(bindings *binding) {
	for bindings != nil {
		b := bindings
		if p.EndNamespaceDeclHandler != nil {
			pr := b.prefix.name
			p.event(func() error { return p.EndNamespaceDeclHandler(pr) })
		}
		bindings = bindings.nextTagBinding
		b.prefix.binding = b.prevPrefixBinding
	}
}

// storeAtts processes the attributes of the start-tag at attStr for the
// element called name: it checks and normalizes them, adds the defaults and,
// with namespace processing, declares the namespaces and expands the names.
// It returns the element name and the attribute list for the handler.
func (p *Parser) storeAtts(enc *encoding, b []byte, attStr int, name string, bindingsPtr **binding) (string, []string, int) {
	d := p.dtd
	et := d.elementTypes[name]
	if et == nil {
		et = &elementType{name: name, defaultAttForName: map[string]int{}}
		d.elementTypes[name] = et
		if p.ns {
			p.setElementTypePrefix(et)
		}
	}
	atts := enc.getAtts(b, attStr)
	var appAtts []string
	var appIDs []*attributeID
	nPrefixes := 0
	for i := range atts {
		cur := &atts[i]
		id := p.getAttributeID(enc, b, cur.name, cur.name+enc.nameLength(b, cur.name))
		if id.flag != 0 {
			if enc == p.encoding {
				p.eventPtr = cur.name
			}
			p.clearAttributeFlags(appIDs, *bindingsPtr)
			return "", nil, ErrDuplicateAttribute
		}
		id.flag = 1
		var value string
		if !cur.normalized {
			isCdata := true
			if id.maybeTokenized {
				if idx, ok := et.defaultAttForName[id.name]; ok {
					isCdata = et.defaultAtts[idx].isCdata
				}
			}
			v, r := p.storeAttributeValue(enc, isCdata, b, cur.valuePtr, cur.valueEnd, false)
			if r != ErrNone {
				p.clearAttributeFlags(appIDs, *bindingsPtr)
				id.flag = 0
				return "", nil, r
			}
			value = v
		} else {
			value = enc.storeString(b, cur.valuePtr, cur.valueEnd)
		}
		if id.prefix != nil {
			if id.xmlns {
				if r := p.addBinding(id.prefix, id, value, bindingsPtr); r != ErrNone {
					p.clearAttributeFlags(appIDs, *bindingsPtr)
					id.flag = 0
					return "", nil, r
				}
				continue
			}
			nPrefixes++
			id.flag = 2
		}
		appAtts = append(appAtts, id.name, value)
		appIDs = append(appIDs, id)
	}
	for _, da := range et.defaultAtts {
		if da.id.flag != 0 || da.value == nil {
			continue
		}
		if da.id.prefix != nil {
			if da.id.xmlns {
				if r := p.addBinding(da.id.prefix, da.id, *da.value, bindingsPtr); r != ErrNone {
					p.clearAttributeFlags(appIDs, *bindingsPtr)
					return "", nil, r
				}
				continue
			}
			da.id.flag = 2
			nPrefixes++
		} else {
			da.id.flag = 1
		}
		appAtts = append(appAtts, da.id.name, *da.value)
		appIDs = append(appIDs, da.id)
	}
	// Expand the prefixed names and check for duplicates among them.
	var expandedNames []string
	for i, id := range appIDs {
		if nPrefixes == 0 {
			break
		}
		if id.flag != 2 {
			continue
		}
		bd := id.prefix.binding
		if bd == nil {
			p.clearAttributeFlags(appIDs, *bindingsPtr)
			return "", nil, ErrUnboundPrefix
		}
		local := id.name[strings.IndexByte(id.name, ':')+1:]
		uriName := bd.uri + local
		for _, prev := range expandedNames {
			if prev == uriName {
				p.clearAttributeFlags(appIDs, *bindingsPtr)
				return "", nil, ErrDuplicateAttribute
			}
		}
		expandedNames = append(expandedNames, uriName)
		appAtts[2*i] = uriName
		id.flag = 0
		nPrefixes--
	}
	p.clearAttributeFlags(appIDs, *bindingsPtr)
	if appAtts == nil {
		appAtts = []string{}
	}
	if !p.ns {
		return name, appAtts, ErrNone
	}
	// Expand the element type name.
	var bd *binding
	localPart := name
	if et.prefix != nil {
		bd = et.prefix.binding
		if bd == nil {
			return "", nil, ErrUnboundPrefix
		}
		localPart = name[strings.IndexByte(name, ':')+1:]
	} else if d.defaultPrefix.binding != nil {
		bd = d.defaultPrefix.binding
	} else {
		return name, appAtts, ErrNone
	}
	return bd.uri + localPart, appAtts, ErrNone
}

// clearAttributeFlags resets the marks storeAtts sets on attribute names.
func (p *Parser) clearAttributeFlags(ids []*attributeID, bindings *binding) {
	for _, id := range ids {
		id.flag = 0
	}
	for ; bindings != nil; bindings = bindings.nextTagBinding {
		bindings.attID.flag = 0
	}
}

var rfc3986URIChars = func() [256]bool {
	var t [256]bool
	for _, c := range []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789%-._~:/?#[]@!$&'()*+,;=") {
		t[c] = true
	}
	return t
}()

const xmlnsNamespaceURI = "http://www.w3.org/2000/xmlns/"

// addBinding declares prefix as uri for the element whose bindings are at
// bindingsPtr; attID is nil for a binding that is not a declaration.
func (p *Parser) addBinding(pr *prefix, attID *attributeID, uri string, bindingsPtr **binding) int {
	if uri == "" && pr.name != nil {
		return ErrUndeclaringPrefix
	}
	mustBeXML := false
	if pr.name != nil && strings.HasPrefix(*pr.name, "xml") {
		if *pr.name == "xmlns" {
			return ErrReservedPrefixXMLNS
		}
		if *pr.name == "xml" {
			mustBeXML = true
		}
	}
	isXML := uri == xmlNamespaceURI
	isXMLNS := !mustBeXML && uri == xmlnsNamespaceURI
	for i := 0; i < len(uri); i++ {
		if uri[i] == p.namespaceSeparator && !rfc3986URIChars[uri[i]] {
			return ErrSyntax
		}
	}
	if mustBeXML != isXML {
		if mustBeXML {
			return ErrReservedPrefixXML
		}
		return ErrReservedNamespaceURI
	}
	if isXMLNS {
		return ErrReservedNamespaceURI
	}
	b := &binding{prefix: pr, attID: attID, uri: uri + string(p.namespaceSeparator), prevPrefixBinding: pr.binding}
	if uri == "" && pr == &p.dtd.defaultPrefix {
		pr.binding = nil
	} else {
		pr.binding = b
	}
	b.nextTagBinding = *bindingsPtr
	*bindingsPtr = b
	if attID != nil && p.StartNamespaceDeclHandler != nil {
		name := pr.name
		var u *string
		if pr.binding != nil {
			u = strPtr(uri)
		}
		p.event(func() error { return p.StartNamespaceDeclHandler(name, u) })
	}
	return ErrNone
}

func cdataSectionProcessor(p *Parser, s, end int, nextPtr *int) int {
	closed, r := p.doCdataSection(p.encoding, p.buf, &s, end, nextPtr, !p.finalBuffer)
	if r != ErrNone {
		return r
	}
	if closed {
		if p.parentParser != nil {
			p.processor = externalEntityContentProcessor
			return externalEntityContentProcessor(p, s, end, nextPtr)
		}
		p.processor = contentProcessor
		return contentProcessor(p, s, end, nextPtr)
	}
	return r
}

// doCdataSection parses a CDATA section's content from *startPtr; closed
// reports whether its end was reached, *startPtr then being after it.
func (p *Parser) doCdataSection(enc *encoding, b []byte, startPtr *int, end int, nextPtr *int, haveMore bool) (bool, int) {
	s := *startPtr
	eventPP, eventEndPP := p.eventPointers(enc)
	*eventPP = s
	for {
		next := s
		tok := enc.tok(scanCdataSection, b, s, end, &next)
		*eventEndPP = next
		switch tok {
		case tokCdataSectClose:
			if p.EndCdataSectionHandler != nil {
				p.event(p.EndCdataSectionHandler)
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
			*startPtr = next
			*nextPtr = next
			if p.parsing == stateFinished {
				return true, ErrAborted
			}
			return true, ErrNone
		case tokDataNewline:
			if p.CharacterDataHandler != nil {
				p.characterData(newline)
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
		case tokDataChars:
			if p.CharacterDataHandler != nil {
				p.convertedData(enc, b, s, next, true)
			} else if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
		case tokInvalid:
			*eventPP = next
			return false, ErrInvalidToken
		case tokPartialChar:
			if haveMore {
				*nextPtr = s
				return false, ErrNone
			}
			return false, ErrPartialChar
		case tokPartial, tokNone:
			if haveMore {
				*nextPtr = s
				return false, ErrNone
			}
			return false, ErrUnclosedCdataSection
		default:
			*eventPP = next
			return false, ErrUnexpectedState
		}
		if p.parsing == stateFinished {
			*eventPP = next
			return false, ErrAborted
		}
		s = next
		*eventPP = s
	}
}

func epilogProcessor(p *Parser, s, end int, nextPtr *int) int {
	p.processor = epilogProcessor
	p.eventPtr = s
	enc := p.encoding
	for {
		next := -1
		tok := enc.tok(scanProlog, p.buf, s, end, &next)
		p.eventEndPtr = next
		switch tok {
		case -tokPrologS:
			if p.DefaultHandler != nil {
				p.reportDefault(enc, p.buf, s, next)
				if p.parsing == stateFinished {
					return ErrAborted
				}
			}
			*nextPtr = next
			return ErrNone
		case tokNone:
			*nextPtr = s
			return ErrNone
		case tokPrologS:
			if p.DefaultHandler != nil {
				p.reportDefault(enc, p.buf, s, next)
			}
		case tokPI:
			p.reportProcessingInstruction(enc, p.buf, s, next)
		case tokComment:
			p.reportComment(enc, p.buf, s, next)
		case tokInvalid:
			p.eventPtr = next
			return ErrInvalidToken
		case tokPartial:
			if !p.finalBuffer {
				*nextPtr = s
				return ErrNone
			}
			return ErrUnclosedToken
		case tokPartialChar:
			if !p.finalBuffer {
				*nextPtr = s
				return ErrNone
			}
			return ErrPartialChar
		default:
			return ErrJunkAfterDocElement
		}
		if p.parsing == stateFinished {
			p.eventPtr = next
			return ErrAborted
		}
		s = next
		p.eventPtr = s
	}
}

// reportProcessingInstruction reports the PI b[start:end].
func (p *Parser) reportProcessingInstruction(enc *encoding, b []byte, start, end int) {
	if p.ProcessingInstructionHandler == nil {
		if p.DefaultHandler != nil {
			p.reportDefault(enc, b, start, end)
		}
		return
	}
	mb := enc.minBPC
	start += mb * 2
	tem := start + enc.nameLength(b, start)
	target := enc.storeString(b, start, tem)
	data := normalizeLines(enc.storeString(b, enc.skipS(b, tem), end-mb*2))
	p.event(func() error { return p.ProcessingInstructionHandler(target, data) })
}

// reportComment reports the comment b[start:end].
func (p *Parser) reportComment(enc *encoding, b []byte, start, end int) {
	if p.CommentHandler == nil {
		if p.DefaultHandler != nil {
			p.reportDefault(enc, b, start, end)
		}
		return
	}
	mb := enc.minBPC
	data := normalizeLines(enc.storeString(b, start+mb*4, end-mb*3))
	p.event(func() error { return p.CommentHandler(data) })
}

// normalizeLines turns CR and CR LF into LF.
func normalizeLines(s string) string {
	if strings.IndexByte(s, '\r') < 0 {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// storeString is poolStoreString: b[s:end] converted to UTF-8.
func (e *encoding) storeString(b []byte, s, end int) string {
	if e.isUTF8 {
		return string(b[s:end])
	}
	out, _ := e.toUTF8(b, &s, end, nil, -1)
	return string(out)
}

// externalEntityRef calls ExternalEntityRefHandler, returning its status (0
// on error).
func (p *Parser) externalEntityRef(context *string, systemID, publicID *string) int {
	status := 0
	p.event(func() error {
		var err error
		status, err = p.ExternalEntityRefHandler(context, nil, systemID, publicID)
		return err
	})
	return status
}
