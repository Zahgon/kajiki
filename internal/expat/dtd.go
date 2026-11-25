// Ported from libexpat 2.8.5 (expat/lib/xmlparse.c) to Go, 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Licensed under the MIT license.
// SPDX-License-Identifier: MIT

package expat

import "strings"

// entity is a general or parameter entity declared in the DTD.
type entity struct {
	name string
	// text is the replacement text in UTF-8; hasText is false for an
	// external entity.
	text       []byte
	hasText    bool
	systemID   *string
	publicID   *string
	notation   *string
	open       bool
	hasMore    bool
	isParam    bool
	isInternal bool
	processed  int
}

// prefix is a namespace prefix; name is nil for the default namespace.
type prefix struct {
	name    *string
	binding *binding
}

// binding is one namespace declaration in scope.
type binding struct {
	prefix            *prefix
	nextTagBinding    *binding
	prevPrefixBinding *binding
	attID             *attributeID
	// uri ends with the namespace separator.
	uri string
}

// attributeID is an attribute name; flag is the mark storeAtts keeps while
// checking an element's attributes (1: seen, 2: seen and prefixed).
type attributeID struct {
	name           string
	prefix         *prefix
	maybeTokenized bool
	xmlns          bool
	flag           byte
}

type defaultAttribute struct {
	id      *attributeID
	isCdata bool
	value   *string
}

type elementType struct {
	name              string
	prefix            *prefix
	idAtt             *attributeID
	defaultAtts       []defaultAttribute
	defaultAttForName map[string]int
}

// dtd is expat's DTD: the declarations a document entity and its external
// parameter entities share.
type dtd struct {
	generalEntities    map[string]*entity
	paramEntities      map[string]*entity
	elementTypes       map[string]*elementType
	attributeIDs       map[string]*attributeID
	prefixes           map[string]*prefix
	prefixOrder        []*prefix
	defaultPrefix      prefix
	keepProcessing     bool
	hasParamEntityRefs bool
	standalone         bool
	paramEntityRead    bool
	// entityValuePool collects the entity value being declared, which
	// external parameter entities referenced from it extend.
	entityValuePool []byte
}

func newDTD() *dtd {
	return &dtd{
		generalEntities: map[string]*entity{},
		paramEntities:   map[string]*entity{},
		elementTypes:    map[string]*elementType{},
		attributeIDs:    map[string]*attributeID{},
		prefixes:        map[string]*prefix{},
		keepProcessing:  true,
	}
}

// lookupPrefix returns the prefix called name, creating it.
func (d *dtd) lookupPrefix(name string) *prefix {
	if pr, ok := d.prefixes[name]; ok {
		return pr
	}
	n := name
	pr := &prefix{name: &n}
	d.prefixes[name] = pr
	d.prefixOrder = append(d.prefixOrder, pr)
	return pr
}

// copyFrom is dtdCopy: it copies the declarations of src, without namespace
// bindings.
func (d *dtd) copyFrom(src *dtd) {
	for _, pr := range src.prefixOrder {
		d.lookupPrefix(*pr.name)
	}
	for name, id := range src.attributeIDs {
		nid := &attributeID{name: name, maybeTokenized: id.maybeTokenized, xmlns: id.xmlns}
		if id.prefix != nil {
			if id.prefix == &src.defaultPrefix {
				nid.prefix = &d.defaultPrefix
			} else {
				nid.prefix = d.lookupPrefix(*id.prefix.name)
			}
		}
		d.attributeIDs[name] = nid
	}
	for name, et := range src.elementTypes {
		net := &elementType{name: name, defaultAttForName: map[string]int{}}
		if et.prefix != nil {
			net.prefix = d.lookupPrefix(*et.prefix.name)
		}
		if et.idAtt != nil {
			net.idAtt = d.attributeIDs[et.idAtt.name]
		}
		for _, da := range et.defaultAtts {
			net.defaultAtts = append(net.defaultAtts, defaultAttribute{id: d.attributeIDs[da.id.name],
				isCdata: da.isCdata, value: da.value})
		}
		for k, v := range et.defaultAttForName {
			net.defaultAttForName[k] = v
		}
		d.elementTypes[name] = net
	}
	copyEntities(d.paramEntities, src.paramEntities)
	copyEntities(d.generalEntities, src.generalEntities)
	d.keepProcessing = src.keepProcessing
	d.hasParamEntityRefs = src.hasParamEntityRefs
	d.standalone = src.standalone
	d.paramEntityRead = src.paramEntityRead
}

func copyEntities(dst, src map[string]*entity) {
	for name, e := range src {
		ne := *e
		ne.open, ne.hasMore, ne.processed = false, false, 0
		dst[name] = &ne
	}
}

// tag is an open element.
type tag struct {
	parent   *tag
	rawName  []byte
	name     string
	bindings *binding
}

// openEntity is an internal entity being expanded.
type openEntity struct {
	next             *openEntity
	entity           *entity
	startTagLevel    int
	betweenDecl      bool
	internalEventPtr int
	internalEventEnd int
}

// getContext describes the namespace bindings and open entities for an
// external entity parser.
func (p *Parser) getContext() string {
	d := p.dtd
	var sb strings.Builder
	needSep := false
	if d.defaultPrefix.binding != nil {
		sb.WriteByte('=')
		uri := d.defaultPrefix.binding.uri
		sb.WriteString(uri[:len(uri)-1])
		needSep = true
	}
	for _, pr := range d.prefixOrder {
		if pr.binding == nil {
			continue
		}
		if needSep {
			sb.WriteByte('\f')
		}
		sb.WriteString(*pr.name)
		sb.WriteByte('=')
		uri := pr.binding.uri
		sb.WriteString(uri[:len(uri)-1])
		needSep = true
	}
	for _, e := range d.generalEntities {
		if !e.open {
			continue
		}
		if needSep {
			sb.WriteByte('\f')
		}
		sb.WriteString(e.name)
		needSep = true
	}
	return sb.String()
}

// setContext restores the bindings and open entities getContext described,
// returning the error of a binding expat refuses.
func (p *Parser) setContext(context string) int {
	d := p.dtd
	for len(context) > 0 {
		item := context
		if i := strings.IndexByte(context, '\f'); i >= 0 {
			item, context = context[:i], context[i+1:]
		} else {
			context = ""
		}
		eq := strings.IndexByte(item, '=')
		if eq < 0 {
			if e, ok := d.generalEntities[item]; ok {
				e.open = true
			}
			continue
		}
		pr := &d.defaultPrefix
		if eq > 0 {
			pr = d.lookupPrefix(item[:eq])
		}
		if r := p.addBinding(pr, nil, item[eq+1:], &p.inheritedBindings); r != ErrNone {
			return r
		}
	}
	return ErrNone
}
