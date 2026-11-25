// Ported from libexpat 2.8.5 (expat/lib/xmlparse.c) and CPython 3.12.15 (Modules/pyexpat.c) to Go,
// 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: MIT AND PSF-2.0

package expat

// Parameter entity parsing modes for SetParamEntityParsing.
const (
	ParamEntityParsingNever = iota
	ParamEntityParsingUnlessStandalone
	ParamEntityParsingAlways
)

const (
	contextBytes    = 1024 // XML_CONTEXT_BYTES
	initBufferSize  = 1024
	dataBufSize     = 1024 // the conversion buffer for character data
	charBufferSize  = 8192 // pyexpat's CHARACTER_DATA_BUFFER_SIZE
	externalSubset  = "#"
	xmlNamespaceURI = "http://www.w3.org/XML/1998/namespace"
)

// Parsing states.
const (
	stateInitialized = iota
	stateParsing
	stateFinished
)

type processor func(p *Parser, s, end int, nextPtr *int) int

// Parser emulates an xml.parsers.expat xmlparser fed with str data:
// libexpat 2.8.5's parsing (encoding detection, tokens, well-formedness, DTD
// processing, namespaces when enabled, positions, buffering) and pyexpat's
// handler layer (character data buffering, errors raised by handlers).
//
// Handlers are the pyexpat handler attributes; a nil handler is unset. A
// handler that returns an error stops parsing, and Parse returns that error.
// Setting DefaultHandler (XML_SetDefaultHandler) also stops internal
// entities from being expanded in content: their references are reported to
// it. Attributes always arrive ordered (ordered_attributes) as
// name, value, name, value..., including defaulted ones.
type Parser struct {
	StartElementHandler          func(name string, attrs []string) error
	EndElementHandler            func(name string) error
	CharacterDataHandler         func(data string) error
	ProcessingInstructionHandler func(target, data string) error
	CommentHandler               func(data string) error
	StartCdataSectionHandler     func() error
	EndCdataSectionHandler       func() error
	DefaultHandler               func(data string) error
	// SkippedEntityHandler gets the name of an entity reference expat does
	// not expand because the entity may be declared in an external subset
	// it did not read, and whether it is a parameter entity.
	SkippedEntityHandler func(name string, isParameterEntity bool) error
	// StartDoctypeDeclHandler gets the doctype name, system id, public id
	// (nil for None) and whether there is an internal subset.
	StartDoctypeDeclHandler func(name string, sysid, pubid *string, hasInternalSubset bool) error
	// StartNamespaceDeclHandler gets the prefix (nil for the default
	// namespace) and the URI (nil when the default namespace is undeclared).
	StartNamespaceDeclHandler func(prefix, uri *string) error
	EndNamespaceDeclHandler   func(prefix *string) error
	// XMLDeclHandler gets the version (nil for a text declaration without
	// one), the encoding (nil when absent) and standalone (-1, 0 or 1).
	XMLDeclHandler func(version, encoding *string, standalone int) error
	// ExternalEntityRefHandler gets the context (nil for an external
	// parameter entity or the external subset), base, system id and public
	// id; returning 0 is an error in processing the reference.
	ExternalEntityRefHandler func(context, base, systemID, publicID *string) (int, error)

	// The pyexpat layer.
	pyErr      error
	textBuffer []byte
	bufferText bool

	// The expat parser.
	ns                 bool
	namespaceSeparator byte
	encodings          *encodingSet
	encoding           *encoding
	internalEncoding   *encoding
	processor          processor
	errorCode          int
	parsing            int
	finalBuffer        bool
	reenter            bool
	handlerDepth       int

	buf                     []byte
	hasBuffer               bool
	bufferPtr, bufferEnd    int
	eventPtr, eventEndPtr   int
	positionPtr             int
	position                position
	lastBufferRequestSize   int
	partialTokenBytesBefore int

	prologState      prologState
	dtd              *dtd
	parentParser     *Parser
	isParamEntity    bool
	useForeignDTD    bool
	paramEntityParse int

	tagLevel           int
	tagStack           *tag
	inheritedBindings  *binding
	openInternal       *openEntity
	openAttribute      *openEntity
	openValue          *openEntity
	groupConnector     []byte
	declElementType    *elementType
	declAttributeID    *attributeID
	declAttributeCdata bool
	declAttributeIsID  bool
	declEntity         *entity
	doctypeName        *string
	doctypeSysid       *string
	doctypePubid       *string
}

// NewParser is xml.parsers.expat.ParserCreate(): a parser without namespace
// processing, which reports names as they are written.
func NewParser() *Parser {
	p := newParser(false, 0, nil)
	p.dtd = newDTD()
	return p
}

// NewParserNS is xml.parsers.expat.ParserCreate(namespace_separator=sep):
// element and attribute names in a namespace are reported as the namespace
// URI, sep and the local name.
func NewParserNS(sep byte) *Parser {
	p := newParser(true, sep, nil)
	p.dtd = newDTD()
	return p
}

func newParser(ns bool, sep byte, d *dtd) *Parser {
	p := &Parser{ns: ns, namespaceSeparator: sep, encodings: plainEncodings, dtd: d,
		eventPtr: -1, eventEndPtr: -1, positionPtr: -1}
	if ns {
		p.encodings = nsEncodings
	}
	p.internalEncoding = p.encodings.internalUTF8
	p.processor = prologInitProcessor
	p.prologState.init()
	initEncoding(&p.encoding, p.encodings)
	return p
}

// SetBufferText sets buffer_text: when on, character data is collected and
// reported in as few CharacterDataHandler calls as the buffer allows.
func (p *Parser) SetBufferText(on bool) error {
	if on {
		p.bufferText = true
		return nil
	}
	ok := p.flushCharacterBuffer()
	p.bufferText = false
	if !ok {
		return p.pyErr
	}
	return nil
}

// SetParamEntityParsing is XML_SetParamEntityParsing; it reports success.
func (p *Parser) SetParamEntityParsing(flag int) bool {
	if p.parsing != stateInitialized {
		return false
	}
	p.paramEntityParse = flag
	return true
}

// UseForeignDTD is xmlparser.UseForeignDTD(flag).
func (p *Parser) UseForeignDTD(flag bool) error {
	if p.parsing != stateInitialized {
		return p.errorAt(ErrCantChangeFeatureOnceParsing)
	}
	p.useForeignDTD = flag
	return nil
}

// CurrentLineNumber is xmlparser.CurrentLineNumber (1-based).
func (p *Parser) CurrentLineNumber() int {
	p.updateEventPosition()
	return p.position.line + 1
}

// CurrentColumnNumber is xmlparser.CurrentColumnNumber (0-based, in
// characters as expat counts them).
func (p *Parser) CurrentColumnNumber() int {
	p.updateEventPosition()
	return p.position.column
}

func (p *Parser) updateEventPosition() {
	if p.eventPtr >= 0 && p.eventPtr >= p.positionPtr {
		p.encoding.updatePosition(p.buf, p.positionPtr, p.eventPtr, &p.position)
		p.positionPtr = p.eventPtr
	}
}

// errorAt builds the ExpatError for code at the current position.
func (p *Parser) errorAt(code int) *Error {
	return NewError(code, p.CurrentLineNumber(), p.CurrentColumnNumber())
}

// Parse is xmlparser.Parse(data, isfinal) with str data: the text is parsed
// as UTF-8, the encoding pyexpat declares for it, so an encoding named in
// the XML declaration is checked for syntax and otherwise ignored.
func (p *Parser) Parse(data string, isFinal bool) error {
	ok := p.xmlParse([]byte(data), isFinal)
	return p.parseResult(ok)
}

// parseResult is pyexpat's get_parse_result.
func (p *Parser) parseResult(ok bool) error {
	if p.pyErr != nil {
		return p.pyErr
	}
	if !ok {
		return p.errorAt(p.errorCode)
	}
	p.flushCharacterBuffer()
	return p.pyErr
}

// ExternalEntityParserCreate is xmlparser.ExternalEntityParserCreate(context):
// a parser for an external entity that shares this parser's handlers, and
// its DTD when context is nil (an external parameter entity). The context is
// the one ExternalEntityRefHandler received; a context whose namespace
// bindings expat refuses gives the ExpatError of that binding.
func (p *Parser) ExternalEntityParserCreate(context *string) (*Parser, error) {
	var d *dtd
	if context == nil {
		d = p.dtd
	}
	np := newParser(p.ns, p.namespaceSeparator, d)
	if d == nil {
		np.dtd = newDTD()
	}
	np.StartElementHandler = p.StartElementHandler
	np.EndElementHandler = p.EndElementHandler
	np.CharacterDataHandler = p.CharacterDataHandler
	np.ProcessingInstructionHandler = p.ProcessingInstructionHandler
	np.SkippedEntityHandler = p.SkippedEntityHandler
	np.CommentHandler = p.CommentHandler
	np.StartCdataSectionHandler = p.StartCdataSectionHandler
	np.EndCdataSectionHandler = p.EndCdataSectionHandler
	np.DefaultHandler = p.DefaultHandler
	np.StartDoctypeDeclHandler = p.StartDoctypeDeclHandler
	np.StartNamespaceDeclHandler = p.StartNamespaceDeclHandler
	np.EndNamespaceDeclHandler = p.EndNamespaceDeclHandler
	np.XMLDeclHandler = p.XMLDeclHandler
	np.ExternalEntityRefHandler = p.ExternalEntityRefHandler
	np.bufferText = p.bufferText
	np.parentParser = p
	np.paramEntityParse = p.paramEntityParse
	np.prologState.inEntityValue = p.prologState.inEntityValue
	if context != nil {
		np.dtd.copyFrom(p.dtd)
		if r := np.setContext(*context); r != ErrNone {
			return nil, NewError(r, 1, 0)
		}
		np.processor = externalEntityInitProcessor
	} else {
		np.isParamEntity = true
		np.prologState.initExternalEntity()
		np.processor = externalParEntInitProcessor
	}
	return np, nil
}

// xmlParse is XML_Parse.
func (p *Parser) xmlParse(data []byte, isFinal bool) bool {
	if p.handlerDepth > 0 {
		return false
	}
	switch p.parsing {
	case stateFinished:
		p.errorCode = ErrFinished
		return false
	case stateInitialized:
		if p.parentParser == nil {
			p.startParsing()
		}
	}
	p.parsing = stateParsing
	if p.getBuffer(len(data)) < 0 {
		return false
	}
	copy(p.buf[p.bufferEnd:], data)
	return p.parseBuffer(len(data), isFinal)
}

// startParsing binds the xml prefix, as expat's implicit context does for a
// parser with namespace processing.
func (p *Parser) startParsing() {
	if p.ns {
		p.setContext("xml=" + xmlNamespaceURI)
	}
}

// getBuffer is XML_GetBuffer: it makes room for n more bytes at bufferEnd
// and returns bufferEnd.
func (p *Parser) getBuffer(n int) int {
	if p.parsing == stateFinished {
		p.errorCode = ErrFinished
		return -1
	}
	p.lastBufferRequestSize = n
	if n > len(p.buf)-p.bufferEnd || !p.hasBuffer {
		needed := n + p.bufferEnd - p.bufferPtr
		parsed := p.bufferPtr
		keep := parsed
		if keep > contextBytes {
			keep = contextBytes
		}
		needed += keep
		if p.hasBuffer && needed <= len(p.buf) {
			if keep < parsed {
				offset := parsed - keep
				copy(p.buf, p.buf[offset:p.bufferEnd])
				p.bufferEnd -= offset
				p.bufferPtr -= offset
			}
		} else {
			size := len(p.buf)
			if size == 0 {
				size = initBufferSize
			}
			for {
				size *= 2
				if size >= needed {
					break
				}
			}
			nb := make([]byte, size)
			if p.hasBuffer {
				copy(nb, p.buf[p.bufferPtr-keep:p.bufferEnd])
				p.bufferEnd = p.bufferEnd - p.bufferPtr + keep
				p.bufferPtr = keep
			} else {
				p.bufferEnd, p.bufferPtr = 0, 0
			}
			p.buf = nb
			p.hasBuffer = true
		}
		p.eventPtr, p.eventEndPtr = -1, -1
		p.positionPtr = -1
	}
	return p.bufferEnd
}

// parseBuffer is XML_ParseBuffer for n bytes already placed at bufferEnd.
func (p *Parser) parseBuffer(n int, isFinal bool) bool {
	switch p.parsing {
	case stateFinished:
		p.errorCode = ErrFinished
		return false
	case stateInitialized:
		if p.parentParser == nil {
			p.startParsing()
		}
	}
	p.parsing = stateParsing
	start := p.bufferPtr
	p.positionPtr = start
	p.bufferEnd += n
	p.finalBuffer = isFinal
	p.errorCode = p.callProcessor(start, p.bufferEnd, &p.bufferPtr)
	if p.errorCode != ErrNone {
		p.eventEndPtr = p.eventPtr
		p.processor = errorProcessor
		return false
	}
	if p.parsing == stateParsing && isFinal {
		p.parsing = stateFinished
		return true
	}
	p.encoding.updatePosition(p.buf, p.positionPtr, p.bufferPtr, &p.position)
	p.positionPtr = p.bufferPtr
	return true
}

// callProcessor runs the processor, deferring the reparse of a partial token
// until enough new data has arrived.
func (p *Parser) callProcessor(start, end int, endPtr *int) int {
	haveNow := end - start
	if !p.finalBuffer {
		hadBefore := p.partialTokenBytesBefore
		available := p.bufferPtr
		available -= min(available, contextBytes)
		available += len(p.buf) - p.bufferEnd
		enough := haveNow >= 2*hadBefore || p.lastBufferRequestSize > available
		if !enough {
			*endPtr = start
			return ErrNone
		}
	}
	var ret int
	*endPtr = start
	for {
		ret = p.processor(p, *endPtr, end, endPtr)
		if p.parsing != stateParsing {
			p.reenter = false
		}
		if !p.reenter {
			break
		}
		p.reenter = false
		if ret != ErrNone {
			return ret
		}
	}
	if ret == ErrNone {
		if *endPtr == start {
			p.partialTokenBytesBefore = haveNow
		} else {
			p.partialTokenBytesBefore = 0
		}
	}
	return ret
}

func errorProcessor(p *Parser, s, end int, nextPtr *int) int { return p.errorCode }

// initializeEncoding sets up the encoding the parser was given: UTF-8,
// which the first scan may still find to be UTF-16 from a byte order mark
// or a NUL.
func (p *Parser) initializeEncoding() {
	initEncoding(&p.encoding, p.encodings)
}
