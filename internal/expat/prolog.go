// Ported from libexpat 2.8.5 (expat/lib/xmlparse.c, xmltok.c) to Go, 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Licensed under the MIT license.
// SPDX-License-Identifier: MIT

package expat

import (
	"strings"
	"unicode/utf8"
)

// Kinds of entity expansion.
const (
	entityInternal  = iota // in content or between declarations
	entityAttribute        // in an attribute value
	entityValue            // in an entity value
)

func prologInitProcessor(p *Parser, s, end int, nextPtr *int) int {
	p.initializeEncoding()
	p.processor = prologProcessor
	return prologProcessor(p, s, end, nextPtr)
}

func externalParEntInitProcessor(p *Parser, s, end int, nextPtr *int) int {
	p.initializeEncoding()
	p.dtd.paramEntityRead = true
	if p.prologState.inEntityValue {
		p.processor = entityValueInitProcessor
		return entityValueInitProcessor(p, s, end, nextPtr)
	}
	p.processor = externalParEntProcessor
	return externalParEntProcessor(p, s, end, nextPtr)
}

func entityValueInitProcessor(p *Parser, s, end int, nextPtr *int) int {
	start := s
	next := start
	p.eventPtr = start
	for {
		tok := p.encoding.tok(scanProlog, p.buf, start, end, &next)
		p.eventEndPtr = next
		if tok <= 0 {
			if !p.finalBuffer && tok != tokInvalid {
				*nextPtr = s
				return ErrNone
			}
			switch tok {
			case tokInvalid:
				return ErrInvalidToken
			case tokPartial:
				return ErrUnclosedToken
			case tokPartialChar:
				return ErrPartialChar
			}
			next := s
			return p.storeEntityValue(p.encoding, p.buf, s, end, &next)
		} else if tok == tokXMLDecl {
			if r := p.processXMLDecl(false, start, next); r != ErrNone {
				return r
			}
			if p.parsing == stateFinished {
				return ErrAborted
			}
			*nextPtr = next
			p.processor = entityValueProcessor
			return entityValueProcessor(p, next, end, nextPtr)
		} else if tok == tokBOM {
			*nextPtr = next
			s = next
		} else if tok == tokInstanceStart {
			*nextPtr = next
			return ErrSyntax
		}
		start = next
		p.eventPtr = start
	}
}

func externalParEntProcessor(p *Parser, s, end int, nextPtr *int) int {
	next := s
	tok := p.encoding.tok(scanProlog, p.buf, s, end, &next)
	if tok <= 0 {
		if !p.finalBuffer && tok != tokInvalid {
			*nextPtr = s
			return ErrNone
		}
		switch tok {
		case tokInvalid:
			return ErrInvalidToken
		case tokPartial:
			return ErrUnclosedToken
		case tokPartialChar:
			return ErrPartialChar
		}
	} else if tok == tokBOM {
		s = next
		tok = p.encoding.tok(scanProlog, p.buf, s, end, &next)
	}
	p.processor = prologProcessor
	return p.doProlog(p.encoding, p.buf, s, end, tok, next, nextPtr, !p.finalBuffer, true)
}

func entityValueProcessor(p *Parser, s, end int, nextPtr *int) int {
	start := s
	next := s
	enc := p.encoding
	for {
		tok := enc.tok(scanProlog, p.buf, start, end, &next)
		if tok <= 0 {
			if !p.finalBuffer && tok != tokInvalid {
				*nextPtr = s
				return ErrNone
			}
			switch tok {
			case tokInvalid:
				return ErrInvalidToken
			case tokPartial:
				return ErrUnclosedToken
			case tokPartialChar:
				return ErrPartialChar
			}
			next := s
			return p.storeEntityValue(enc, p.buf, s, end, &next)
		} else if tok == tokInstanceStart {
			*nextPtr = next
			return ErrSyntax
		}
		start = next
	}
}

func prologProcessor(p *Parser, s, end int, nextPtr *int) int {
	next := s
	tok := p.encoding.tok(scanProlog, p.buf, s, end, &next)
	return p.doProlog(p.encoding, p.buf, s, end, tok, next, nextPtr, !p.finalBuffer, true)
}

// loadExternalSubset calls ExternalEntityRefHandler for the external subset
// (or the foreign DTD) at the end of the doctype or at the start of the
// document element.
func (p *Parser) loadExternalSubset(atDoctype bool) int {
	d := p.dtd
	hadParamEntityRefs := d.hasParamEntityRefs
	d.hasParamEntityRefs = true
	if p.paramEntityParse != ParamEntityParsingNever && p.ExternalEntityRefHandler != nil {
		e := d.paramEntities[externalSubset]
		if e == nil {
			e = &entity{name: externalSubset}
			d.paramEntities[externalSubset] = e
		}
		d.paramEntityRead = false
		if p.externalEntityRef(nil, e.systemID, e.publicID) == 0 {
			return ErrExternalEntityHandling
		}
		if !d.paramEntityRead && (!atDoctype || p.doctypeSysid == nil) {
			d.hasParamEntityRefs = hadParamEntityRefs
		}
	}
	return ErrNone
}

// doProlog parses prolog or DTD tokens starting with tok at b[s:next].
func (p *Parser) doProlog(enc *encoding, b []byte, s, end, tok, next int, nextPtr *int, haveMore, allowClosingDoctype bool) int {
	d := p.dtd
	eventPP, eventEndPP := p.eventPointers(enc)
	mb := enc.minBPC
	for {
		handleDefault := true
		*eventPP = s
		*eventEndPP = next
		if tok <= 0 {
			if haveMore && tok != tokInvalid {
				*nextPtr = s
				return ErrNone
			}
			switch tok {
			case tokInvalid:
				*eventPP = next
				return ErrInvalidToken
			case tokPartial:
				return ErrUnclosedToken
			case tokPartialChar:
				return ErrPartialChar
			case -tokPrologS:
				tok = -tok
			case tokNone:
				if enc != p.encoding && !p.openInternal.betweenDecl {
					*nextPtr = s
					return ErrNone
				}
				if p.isParamEntity || enc != p.encoding {
					if p.prologState.tokenRole(tokNone, b, end, end, enc) == roleError {
						return ErrIncompletePE
					}
					*nextPtr = s
					return ErrNone
				}
				return ErrNoElements
			default:
				tok = -tok
				next = end
			}
		}
		role := p.prologState.tokenRole(tok, b, s, next, enc)
		switch role {
		case roleXMLDecl:
			if r := p.processXMLDecl(false, s, next); r != ErrNone {
				return r
			}
			enc = p.encoding
			handleDefault = false
		case roleDoctypeName:
			if p.StartDoctypeDeclHandler != nil {
				p.doctypeName = strPtr(enc.storeString(b, s, next))
				p.doctypePubid = nil
				handleDefault = false
			}
			p.doctypeSysid = nil
		case roleDoctypeInternalSubset:
			if p.StartDoctypeDeclHandler != nil {
				name, sysid, pubid := *p.doctypeName, p.doctypeSysid, p.doctypePubid
				p.event(func() error { return p.StartDoctypeDeclHandler(name, sysid, pubid, true) })
				p.doctypeName = nil
				handleDefault = false
			}
		case roleTextDecl:
			if r := p.processXMLDecl(true, s, next); r != ErrNone {
				return r
			}
			enc = p.encoding
			handleDefault = false
		case roleDoctypePublicID, roleEntityPublicID:
			checked := false
			if role == roleDoctypePublicID {
				p.useForeignDTD = false
				p.declEntity = d.paramEntities[externalSubset]
				if p.declEntity == nil {
					p.declEntity = &entity{name: externalSubset}
					d.paramEntities[externalSubset] = p.declEntity
				}
				d.hasParamEntityRefs = true
				if p.StartDoctypeDeclHandler != nil {
					if ok, bad := enc.isPublicId(b, s, next); !ok {
						*eventPP = bad
						return ErrPublicID
					}
					p.doctypePubid = strPtr(normalizePublicID(enc.storeString(b, s+mb, next-mb)))
					handleDefault = false
					checked = true
				}
			}
			if !checked {
				if ok, bad := enc.isPublicId(b, s, next); !ok {
					*eventPP = bad
					return ErrPublicID
				}
			}
			if d.keepProcessing && p.declEntity != nil {
				p.declEntity.publicID = strPtr(normalizePublicID(enc.storeString(b, s+mb, next-mb)))
			}
		case roleDoctypeClose:
			if !allowClosingDoctype {
				return ErrInvalidToken
			}
			if p.doctypeName != nil {
				name, sysid, pubid := *p.doctypeName, p.doctypeSysid, p.doctypePubid
				p.event(func() error { return p.StartDoctypeDeclHandler(name, sysid, pubid, false) })
				handleDefault = false
			}
			if p.doctypeSysid != nil || p.useForeignDTD {
				if r := p.loadExternalSubset(true); r != ErrNone {
					return r
				}
				p.useForeignDTD = false
			}
		case roleInstanceStart:
			if p.useForeignDTD {
				if r := p.loadExternalSubset(false); r != ErrNone {
					return r
				}
			}
			p.processor = contentProcessor
			return contentProcessor(p, s, end, nextPtr)
		case roleAttlistElementName:
			p.declElementType = p.getElementType(enc, b, s, next)
		case roleAttributeName:
			p.declAttributeID = p.getAttributeID(enc, b, s, next)
			p.declAttributeCdata = false
			p.declAttributeIsID = false
		case roleAttributeTypeCdata:
			p.declAttributeCdata = true
		case roleAttributeTypeID:
			p.declAttributeIsID = true
		case roleImpliedAttributeValue, roleRequiredAttributeValue:
			if d.keepProcessing {
				p.defineAttribute(p.declElementType, p.declAttributeID, p.declAttributeCdata, p.declAttributeIsID, nil)
			}
		case roleDefaultAttributeValue, roleFixedAttributeValue:
			if d.keepProcessing {
				v, r := p.storeAttributeValue(enc, p.declAttributeCdata, b, s+mb, next-mb, true)
				if r != ErrNone {
					return r
				}
				p.defineAttribute(p.declElementType, p.declAttributeID, p.declAttributeCdata, false, &v)
			}
		case roleEntityValue:
			if d.keepProcessing {
				r := p.callStoreEntityValue(enc, b, s+mb, next-mb)
				if p.declEntity != nil {
					p.declEntity.text = append([]byte{}, d.entityValuePool...)
					p.declEntity.hasText = true
				}
				d.entityValuePool = d.entityValuePool[:0]
				if r != ErrNone {
					return r
				}
			}
		case roleDoctypeSystemID, roleEntitySystemID:
			if role == roleDoctypeSystemID {
				p.useForeignDTD = false
				d.hasParamEntityRefs = true
				if p.StartDoctypeDeclHandler != nil {
					p.doctypeSysid = strPtr(enc.storeString(b, s+mb, next-mb))
					handleDefault = false
				} else {
					p.doctypeSysid = strPtr(externalSubset)
				}
				if p.declEntity == nil {
					p.declEntity = d.paramEntities[externalSubset]
					if p.declEntity == nil {
						p.declEntity = &entity{name: externalSubset}
						d.paramEntities[externalSubset] = p.declEntity
					}
					p.declEntity.publicID = nil
				}
			}
			if d.keepProcessing && p.declEntity != nil {
				p.declEntity.systemID = strPtr(enc.storeString(b, s+mb, next-mb))
			}
		case roleEntityNotationName:
			if d.keepProcessing && p.declEntity != nil {
				p.declEntity.notation = strPtr(enc.storeString(b, s, next))
			}
		case roleGeneralEntityName, roleParamEntityName:
			if role == roleGeneralEntityName && enc.predefinedEntityName(b, s, next) != 0 {
				p.declEntity = nil
				break
			}
			if !d.keepProcessing {
				p.declEntity = nil
				break
			}
			table := d.generalEntities
			if role == roleParamEntityName {
				table = d.paramEntities
			}
			name := enc.storeString(b, s, next)
			if _, exists := table[name]; exists {
				p.declEntity = nil
				break
			}
			p.declEntity = &entity{name: name, isParam: role == roleParamEntityName,
				isInternal: p.parentParser == nil && p.openInternal == nil}
			table[name] = p.declEntity
		case roleNotationPublicID:
			if ok, bad := enc.isPublicId(b, s, next); !ok {
				*eventPP = bad
				return ErrPublicID
			}
		case roleError:
			switch tok {
			case tokParamEntityRef:
				return ErrParamEntityRef
			case tokXMLDecl:
				return ErrMisplacedXMLPI
			}
			return ErrSyntax
		case roleIgnoreSect:
			if p.DefaultHandler != nil {
				p.reportDefault(enc, b, s, next)
			}
			handleDefault = false
			closed, r := p.doIgnoreSection(enc, b, &next, end, nextPtr, haveMore)
			if r != ErrNone {
				return r
			} else if !closed {
				p.processor = ignoreSectionProcessor
				return r
			}
		case roleGroupOpen:
			p.setGroupConnector(0)
		case roleGroupSequence:
			if p.groupConnector[p.prologState.level] == '|' {
				return ErrSyntax
			}
			p.groupConnector[p.prologState.level] = ','
		case roleGroupChoice:
			if p.groupConnector[p.prologState.level] == ',' {
				return ErrSyntax
			}
			p.groupConnector[p.prologState.level] = '|'
		case roleParamEntityRef, roleInnerParamEntityRef:
			r, handled := p.paramEntityRef(enc, b, s, next, role)
			if r != ErrNone {
				return r
			}
			if handled {
				handleDefault = false
			}
		case rolePI:
			p.reportProcessingInstruction(enc, b, s, next)
			handleDefault = false
		case roleComment:
			p.reportComment(enc, b, s, next)
			handleDefault = false
		case roleNone:
			if tok == tokBOM {
				handleDefault = false
			}
		case roleDoctypeNone:
			if p.StartDoctypeDeclHandler != nil {
				handleDefault = false
			}
		}
		if handleDefault && p.DefaultHandler != nil {
			p.reportDefault(enc, b, s, next)
		}
		switch {
		case p.parsing == stateFinished:
			return ErrAborted
		case p.reenter:
			*nextPtr = next
			return ErrNone
		}
		s = next
		next = s
		tok = enc.tok(scanProlog, b, s, end, &next)
	}
}

func (p *Parser) setGroupConnector(c byte) {
	for len(p.groupConnector) <= p.prologState.level {
		p.groupConnector = append(p.groupConnector, 0)
	}
	p.groupConnector[p.prologState.level] = c
}

// paramEntityRef handles a parameter entity reference in the DTD; handled
// reports whether the default handler is not to see it.
func (p *Parser) paramEntityRef(enc *encoding, b []byte, s, next, role int) (int, bool) {
	d := p.dtd
	mb := enc.minBPC
	d.hasParamEntityRefs = true
	if p.paramEntityParse == ParamEntityParsingNever {
		d.keepProcessing = d.standalone
		return ErrNone, false
	}
	name := enc.storeString(b, s+mb, next-mb)
	e := d.paramEntities[name]
	check := !d.hasParamEntityRefs
	if d.standalone {
		check = p.openInternal == nil
	}
	if p.prologState.documentEntity && check {
		if e == nil {
			return ErrUndefinedEntity, false
		} else if !e.isInternal {
			return ErrEntityDeclaredInPE, false
		}
	} else if e == nil {
		d.keepProcessing = d.standalone
		if role == roleParamEntityRef && p.SkippedEntityHandler != nil {
			p.event(func() error { return p.SkippedEntityHandler(name, true) })
			return ErrNone, true
		}
		return ErrNone, false
	}
	if e.open {
		return ErrRecursiveEntityRef, false
	}
	if e.hasText {
		p.processEntity(e, role == roleParamEntityRef, entityInternal)
		return ErrNone, true
	}
	if p.ExternalEntityRefHandler == nil {
		d.keepProcessing = d.standalone
		return ErrNone, false
	}
	d.paramEntityRead = false
	e.open = true
	status := p.externalEntityRef(nil, e.systemID, e.publicID)
	e.open = false
	if status == 0 {
		return ErrExternalEntityHandling, true
	}
	if !d.paramEntityRead {
		d.keepProcessing = d.standalone
	}
	return ErrNone, true
}

func ignoreSectionProcessor(p *Parser, s, end int, nextPtr *int) int {
	closed, r := p.doIgnoreSection(p.encoding, p.buf, &s, end, nextPtr, !p.finalBuffer)
	if r != ErrNone {
		return r
	}
	if closed {
		p.processor = prologProcessor
		return prologProcessor(p, s, end, nextPtr)
	}
	return r
}

// doIgnoreSection skips an IGNORE conditional section from *startPtr.
func (p *Parser) doIgnoreSection(enc *encoding, b []byte, startPtr *int, end int, nextPtr *int, haveMore bool) (bool, int) {
	next := *startPtr
	s := *startPtr
	eventPP, eventEndPP := p.eventPointers(enc)
	*eventPP = s
	tok := enc.tok(scanIgnoreSection, b, s, end, &next)
	*eventEndPP = next
	switch tok {
	case tokIgnoreSect:
		if p.DefaultHandler != nil {
			p.reportDefault(enc, b, s, next)
		}
		*startPtr = next
		*nextPtr = next
		if p.parsing == stateFinished {
			return true, ErrAborted
		}
		return true, ErrNone
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
		return false, ErrSyntax
	}
	*eventPP = next
	return false, ErrUnexpectedState
}

// processEntity starts expanding an internal entity.
func (p *Parser) processEntity(e *entity, betweenDecl bool, kind int) {
	list := &p.openInternal
	switch kind {
	case entityInternal:
		p.processor = internalEntityProcessor
	case entityAttribute:
		list = &p.openAttribute
	case entityValue:
		list = &p.openValue
	}
	e.open = true
	e.hasMore = true
	e.processed = 0
	*list = &openEntity{next: *list, entity: e, startTagLevel: p.tagLevel, betweenDecl: betweenDecl,
		internalEventPtr: -1, internalEventEnd: -1}
	if kind == entityInternal {
		p.reenter = true
	}
}

func internalEntityProcessor(p *Parser, s, end int, nextPtr *int) int {
	oe := p.openInternal
	if oe == nil {
		return ErrUnexpectedState
	}
	e := oe.entity
	if e.hasMore {
		textStart, textEnd := e.processed, len(e.text)
		next := textStart
		var r int
		if e.isParam {
			tok := p.internalEncoding.tok(scanProlog, e.text, textStart, textEnd, &next)
			r = p.doProlog(p.internalEncoding, e.text, textStart, textEnd, tok, next, &next, false, false)
		} else {
			r = p.doContent(oe.startTagLevel, p.internalEncoding, e.text, textStart, textEnd, &next, false)
		}
		if r != ErrNone {
			return r
		}
		if textEnd != next && p.parsing == stateParsing && p.reenter {
			e.processed = next
			return r
		}
		e.hasMore = false
		if !e.isParam && oe.startTagLevel != p.tagLevel {
			return ErrAsyncEntity
		}
		p.reenter = true
		return r
	}
	e.open = false
	p.openInternal = oe.next
	if p.openInternal == nil {
		if e.isParam {
			p.processor = prologProcessor
		} else {
			p.processor = contentProcessor
		}
	}
	p.reenter = true
	return ErrNone
}

// storeAttributeValue normalizes the attribute value b[ptr:end] in enc,
// expanding references; inProlog is set for a default value in an ATTLIST
// declaration.
func (p *Parser) storeAttributeValue(enc *encoding, isCdata bool, b []byte, ptr, end int, inProlog bool) (string, int) {
	var pool []byte
	next := ptr
	r := ErrNone
	for {
		if p.openAttribute == nil {
			r = p.appendAttributeValue(enc, isCdata, b, next, end, &pool, inProlog, &next)
		} else {
			oe := p.openAttribute
			e := oe.entity
			if e.hasMore {
				nextInEntity := e.processed
				r = p.appendAttributeValue(p.internalEncoding, isCdata, e.text, e.processed, len(e.text), &pool, inProlog, &nextInEntity)
				if r != ErrNone {
					break
				}
				if nextInEntity < len(e.text) {
					e.processed = nextInEntity
				} else {
					e.hasMore = false
				}
				continue
			}
			e.open = false
			p.openAttribute = oe.next
		}
		if r != ErrNone || (p.openAttribute == nil && end == next) {
			break
		}
	}
	if r != ErrNone {
		p.closeOpenEntities(&p.openAttribute)
		return "", r
	}
	if !isCdata && len(pool) > 0 && pool[len(pool)-1] == ' ' {
		pool = pool[:len(pool)-1]
	}
	return string(pool), ErrNone
}

// closeOpenEntities clears an expansion stack left by an error.
func (p *Parser) closeOpenEntities(list **openEntity) {
	for oe := *list; oe != nil; oe = oe.next {
		oe.entity.open = false
	}
	*list = nil
}

func (p *Parser) appendAttributeValue(enc *encoding, isCdata bool, b []byte, ptr, end int, pool *[]byte, inProlog bool, nextPtr *int) int {
	d := p.dtd
	mb := enc.minBPC
	lastIsSpace := func() bool { return len(*pool) == 0 || (*pool)[len(*pool)-1] == ' ' }
	for {
		next := ptr
		tok := enc.literalTok(attributeValueLiteral, b, ptr, end, &next)
		switch tok {
		case tokNone:
			*nextPtr = next
			return ErrNone
		case tokInvalid:
			if enc == p.encoding {
				p.eventPtr = next
			}
			return ErrInvalidToken
		case tokPartial:
			if enc == p.encoding {
				p.eventPtr = ptr
			}
			return ErrInvalidToken
		case tokCharRef:
			n := enc.charRefNumber(b, ptr)
			if n < 0 {
				if enc == p.encoding {
					p.eventPtr = ptr
				}
				return ErrBadCharRef
			}
			if !isCdata && n == 0x20 && lastIsSpace() {
				break
			}
			*pool = append(*pool, utf8.AppendRune(nil, rune(n))...)
		case tokDataChars:
			*pool = append(*pool, enc.storeString(b, ptr, next)...)
		case tokTrailingCr, tokAttributeValueS, tokDataNewline:
			if tok == tokTrailingCr {
				next = ptr + mb
			}
			if !isCdata && lastIsSpace() {
				break
			}
			*pool = append(*pool, ' ')
		case tokEntityRef:
			if ch := enc.predefinedEntityName(b, ptr+mb, next-mb); ch != 0 {
				*pool = append(*pool, ch)
				break
			}
			name := enc.storeString(b, ptr+mb, next-mb)
			e := d.generalEntities[name]
			var checkEntityDecl bool
			if inProlog {
				checkEntityDecl = p.prologState.documentEntity && !d.hasParamEntityRefs
				if d.standalone {
					checkEntityDecl = p.prologState.documentEntity && p.openInternal == nil
				}
			} else {
				checkEntityDecl = !d.hasParamEntityRefs || d.standalone
			}
			if checkEntityDecl {
				if e == nil {
					return ErrUndefinedEntity
				} else if !e.isInternal {
					return ErrEntityDeclaredInPE
				}
			} else if e == nil {
				break
			}
			if e.open {
				if enc == p.encoding {
					p.eventPtr = ptr
				}
				return ErrRecursiveEntityRef
			}
			if e.notation != nil {
				if enc == p.encoding {
					p.eventPtr = ptr
				}
				return ErrBinaryEntityRef
			}
			if !e.hasText {
				if enc == p.encoding {
					p.eventPtr = ptr
				}
				return ErrAttributeExternalEntityRef
			}
			p.processEntity(e, false, entityAttribute)
			*nextPtr = next
			return ErrNone
		default:
			if enc == p.encoding {
				p.eventPtr = ptr
			}
			return ErrUnexpectedState
		}
		ptr = next
	}
}

// callStoreEntityValue stores the entity value b[ptr:end] in the DTD's entity
// value pool, expanding the parameter entities it references.
func (p *Parser) callStoreEntityValue(enc *encoding, b []byte, ptr, end int) int {
	next := ptr
	r := ErrNone
	for {
		if p.openValue == nil {
			r = p.storeEntityValue(enc, b, next, end, &next)
		} else {
			oe := p.openValue
			e := oe.entity
			if e.hasMore {
				nextInEntity := e.processed
				r = p.storeEntityValue(p.internalEncoding, e.text, e.processed, len(e.text), &nextInEntity)
				if r != ErrNone {
					break
				}
				if nextInEntity != len(e.text) {
					e.processed = nextInEntity
				} else {
					e.hasMore = false
				}
				continue
			}
			e.open = false
			p.openValue = oe.next
		}
		if r != ErrNone || (p.openValue == nil && end == next) {
			break
		}
	}
	if r != ErrNone {
		p.closeOpenEntities(&p.openValue)
	}
	return r
}

// storeEntityValue appends the entity value text b[ptr:end] to the DTD's
// entity value pool, up to the end or to a parameter entity to expand.
func (p *Parser) storeEntityValue(enc *encoding, b []byte, ptr, end int, nextPtr *int) int {
	d := p.dtd
	pool := &d.entityValuePool
	mb := enc.minBPC
	old := p.prologState.inEntityValue
	p.prologState.inEntityValue = true
	defer func() { p.prologState.inEntityValue = old }()
	next := ptr
	defer func() { *nextPtr = next }()
	if ptr >= end {
		return ErrNone
	}
	for {
		next = ptr
		tok := enc.literalTok(entityValueLiteral, b, ptr, end, &next)
		switch tok {
		case tokParamEntityRef:
			if !p.isParamEntity && enc == p.encoding {
				p.eventPtr = ptr
				return ErrParamEntityRef
			}
			name := enc.storeString(b, ptr+mb, next-mb)
			e := d.paramEntities[name]
			if e == nil {
				d.keepProcessing = d.standalone
				return ErrNone
			}
			if e.open || e == p.declEntity {
				if enc == p.encoding {
					p.eventPtr = ptr
				}
				return ErrRecursiveEntityRef
			}
			if e.systemID != nil {
				if p.ExternalEntityRefHandler != nil {
					d.paramEntityRead = false
					e.open = true
					status := p.externalEntityRef(nil, e.systemID, e.publicID)
					e.open = false
					if status == 0 {
						return ErrExternalEntityHandling
					}
					if !d.paramEntityRead {
						d.keepProcessing = d.standalone
					}
				} else {
					d.keepProcessing = d.standalone
				}
			} else {
				p.processEntity(e, false, entityValue)
				return ErrNone
			}
		case tokNone:
			return ErrNone
		case tokEntityRef, tokDataChars:
			*pool = append(*pool, enc.storeString(b, ptr, next)...)
		case tokTrailingCr, tokDataNewline:
			if tok == tokTrailingCr {
				next = ptr + mb
			}
			*pool = append(*pool, '\n')
		case tokCharRef:
			n := enc.charRefNumber(b, ptr)
			if n < 0 {
				if enc == p.encoding {
					p.eventPtr = ptr
				}
				return ErrBadCharRef
			}
			*pool = append(*pool, utf8.AppendRune(nil, rune(n))...)
		case tokPartial:
			if enc == p.encoding {
				p.eventPtr = ptr
			}
			return ErrInvalidToken
		case tokInvalid:
			if enc == p.encoding {
				p.eventPtr = next
			}
			return ErrInvalidToken
		default:
			if enc == p.encoding {
				p.eventPtr = ptr
			}
			return ErrUnexpectedState
		}
		ptr = next
	}
}

// defineAttribute records an attribute declaration of an element type.
func (p *Parser) defineAttribute(et *elementType, id *attributeID, isCdata, isID bool, value *string) {
	if value != nil || isID {
		if _, found := et.defaultAttForName[id.name]; found {
			return
		}
		if isID && et.idAtt == nil && !id.xmlns {
			et.idAtt = id
		}
	}
	et.defaultAtts = append(et.defaultAtts, defaultAttribute{id: id, value: value, isCdata: isCdata})
	if !isCdata {
		id.maybeTokenized = true
	}
	if _, found := et.defaultAttForName[id.name]; !found {
		et.defaultAttForName[id.name] = len(et.defaultAtts) - 1
	}
}

// setElementTypePrefix sets the namespace prefix of a qualified element
// type name.
func (p *Parser) setElementTypePrefix(et *elementType) {
	if i := strings.IndexByte(et.name, ':'); i >= 0 {
		et.prefix = p.dtd.lookupPrefix(et.name[:i])
	}
}

// getElementType returns the element type named b[s:end], creating it.
func (p *Parser) getElementType(enc *encoding, b []byte, s, end int) *elementType {
	name := enc.storeString(b, s, end)
	if et, ok := p.dtd.elementTypes[name]; ok {
		return et
	}
	et := &elementType{name: name, defaultAttForName: map[string]int{}}
	p.dtd.elementTypes[name] = et
	p.setElementTypePrefix(et)
	return et
}

// getAttributeID returns the attribute named b[s:end], creating it (with its
// namespace prefix when namespaces are processed).
func (p *Parser) getAttributeID(enc *encoding, b []byte, s, end int) *attributeID {
	d := p.dtd
	name := enc.storeString(b, s, end)
	if id, ok := d.attributeIDs[name]; ok {
		return id
	}
	id := &attributeID{name: name}
	d.attributeIDs[name] = id
	if !p.ns {
		return id
	}
	switch {
	case name == "xmlns":
		id.prefix = &d.defaultPrefix
		id.xmlns = true
	case strings.HasPrefix(name, "xmlns:"):
		id.prefix = d.lookupPrefix(name[6:])
		id.xmlns = true
	default:
		if i := strings.IndexByte(name, ':'); i >= 0 {
			id.prefix = d.lookupPrefix(name[:i])
		}
	}
	return id
}

// normalizePublicID collapses white space in a public identifier.
func normalizePublicID(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case ' ', '\r', '\n':
			if sb.Len() > 0 && sb.String()[sb.Len()-1] != ' ' {
				sb.WriteByte(' ')
			}
		default:
			sb.WriteByte(c)
		}
	}
	return strings.TrimSuffix(sb.String(), " ")
}

// processXMLDecl handles the XML or text declaration b[s:next].
func (p *Parser) processXMLDecl(isGeneralTextEntity bool, s, next int) int {
	enc := p.encoding
	decl, ok := enc.parseXMLDecl(isGeneralTextEntity, p.buf, s, next, &p.eventPtr)
	if !ok {
		if isGeneralTextEntity {
			return ErrTextDecl
		}
		return ErrXMLDecl
	}
	if !isGeneralTextEntity && decl.standalone == 1 {
		p.dtd.standalone = true
		if p.paramEntityParse == ParamEntityParsingUnlessStandalone {
			p.paramEntityParse = ParamEntityParsingNever
		}
	}
	var storedEncName *string
	if decl.encodingName >= 0 {
		storedEncName = strPtr(enc.storeString(p.buf, decl.encodingName, decl.encodingName+enc.nameLength(p.buf, decl.encodingName)))
	}
	if p.XMLDeclHandler != nil {
		var version *string
		if decl.version >= 0 {
			version = strPtr(enc.storeString(p.buf, decl.version, decl.versionEnd-enc.minBPC))
		}
		p.event(func() error { return p.XMLDeclHandler(version, storedEncName, decl.standalone) })
	} else if p.DefaultHandler != nil {
		p.reportDefault(enc, p.buf, s, next)
	}
	return ErrNone
}

// xmlDecl is what parseXMLDecl found; positions are -1 when absent. The
// encoding name is reported to XMLDeclHandler only: the parser reads text
// whose encoding pyexpat has already declared, and expat ignores the
// declared encoding then.
type xmlDecl struct {
	version, versionEnd int
	encodingName        int
	standalone          int
}

// toASCII is the first character at ptr if it is ASCII, else -1.
func (e *encoding) toASCII(b []byte, ptr, end int) int {
	out, _ := e.toUTF8(b, &ptr, end, nil, 1)
	if len(out) == 0 {
		return -1
	}
	return int(int8(out[0]))
}

func isXMLSpace(c int) bool { return c == ' ' || c == '\r' || c == '\n' || c == '\t' }

// parsePseudoAttribute reads name="value" after white space. name is -1
// when only white space is left, and next is then ptr.
func (e *encoding) parsePseudoAttribute(b []byte, ptr, end int) (ok bool, name, nameEnd, val, next int) {
	mb := e.minBPC
	start := ptr
	if ptr == end {
		return true, -1, 0, 0, start
	}
	if !isXMLSpace(e.toASCII(b, ptr, end)) {
		return false, -1, 0, 0, ptr
	}
	for {
		ptr += mb
		if !isXMLSpace(e.toASCII(b, ptr, end)) {
			break
		}
	}
	if ptr == end {
		// As in expat, the position is left where the call started.
		return true, -1, 0, 0, start
	}
	name = ptr
	for {
		c := e.toASCII(b, ptr, end)
		if c == -1 {
			return false, name, 0, 0, ptr
		}
		if c == '=' {
			nameEnd = ptr
			break
		}
		if isXMLSpace(c) {
			nameEnd = ptr
			for {
				ptr += mb
				c = e.toASCII(b, ptr, end)
				if !isXMLSpace(c) {
					break
				}
			}
			if c != '=' {
				return false, name, 0, 0, ptr
			}
			break
		}
		ptr += mb
	}
	if ptr == name {
		return false, name, 0, 0, ptr
	}
	ptr += mb
	c := e.toASCII(b, ptr, end)
	for isXMLSpace(c) {
		ptr += mb
		c = e.toASCII(b, ptr, end)
	}
	if c != '"' && c != '\'' {
		return false, name, 0, 0, ptr
	}
	open := c
	ptr += mb
	val = ptr
	for ; ; ptr += mb {
		c = e.toASCII(b, ptr, end)
		if c == open {
			break
		}
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') && c != '.' && c != '-' && c != '_' {
			return false, name, nameEnd, val, ptr
		}
	}
	return true, name, nameEnd, val, ptr + mb
}

// checkXMLDeclVersionNum accepts versions "1." followed by digits.
func (e *encoding) checkXMLDeclVersionNum(b []byte, val, valEnd int) bool {
	mb := e.minBPC
	if valEnd-val < 2*mb || !e.nameMatchesASCII(b, val, val+2*mb, "1.") {
		return false
	}
	val += 2 * mb
	if val == valEnd {
		return false
	}
	for ; val != valEnd; val += mb {
		if c := e.toASCII(b, val, valEnd); c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parseXMLDecl is XmlParseXmlDecl for the declaration b[ptr:end]; on
// failure the offending position is stored in *badPtr.
func (e *encoding) parseXMLDecl(isGeneralTextEntity bool, b []byte, ptr, end int, badPtr *int) (xmlDecl, bool) {
	mb := e.minBPC
	decl := xmlDecl{version: -1, encodingName: -1, standalone: -1}
	ptr += 5 * mb
	end -= 2 * mb
	ok, name, nameEnd, val, next := e.parsePseudoAttribute(b, ptr, end)
	ptr = next
	if !ok || name < 0 {
		*badPtr = ptr
		return decl, false
	}
	if !e.nameMatchesASCII(b, name, nameEnd, "version") {
		if !isGeneralTextEntity {
			*badPtr = name
			return decl, false
		}
	} else {
		decl.version = val
		decl.versionEnd = ptr
		if val == ptr-mb {
			*badPtr = val
			return decl, false
		}
		if !e.checkXMLDeclVersionNum(b, val, ptr-mb) {
			*badPtr = val
			return decl, false
		}
		ok, name, nameEnd, val, next = e.parsePseudoAttribute(b, ptr, end)
		ptr = next
		if !ok {
			*badPtr = ptr
			return decl, false
		}
		if name < 0 {
			if isGeneralTextEntity {
				*badPtr = ptr
				return decl, false
			}
			return decl, true
		}
	}
	if e.nameMatchesASCII(b, name, nameEnd, "encoding") {
		c := e.toASCII(b, val, end)
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') {
			*badPtr = val
			return decl, false
		}
		decl.encodingName = val
		ok, name, nameEnd, val, next = e.parsePseudoAttribute(b, ptr, end)
		ptr = next
		if !ok {
			*badPtr = ptr
			return decl, false
		}
		if name < 0 {
			return decl, true
		}
	}
	if !e.nameMatchesASCII(b, name, nameEnd, "standalone") || isGeneralTextEntity {
		*badPtr = name
		return decl, false
	}
	switch {
	case e.nameMatchesASCII(b, val, ptr-mb, "yes"):
		decl.standalone = 1
	case e.nameMatchesASCII(b, val, ptr-mb, "no"):
		decl.standalone = 0
	default:
		*badPtr = val
		return decl, false
	}
	for isXMLSpace(e.toASCII(b, ptr, end)) {
		ptr += mb
	}
	if ptr != end {
		*badPtr = ptr
		return decl, false
	}
	return decl, true
}
