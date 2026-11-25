// Ported from CPython 3.12.15 (Modules/pyexpat.c) and libexpat 2.8.5 (expat/lib/xmlparse.c) to Go,
// 2026-11-22.
// Copyright 2001 Python Software Foundation.
// Copyright (c) 2000-2026 Expat development team
// SPDX-License-Identifier: PSF-2.0 AND MIT

package expat

// fail records the first error a handler returned and stops the parser, as
// pyexpat does when a handler raises.
func (p *Parser) fail(err error) {
	if err == nil || p.pyErr != nil {
		return
	}
	p.pyErr = err
	if p.parsing == stateParsing {
		p.parsing = stateFinished
	}
}

// event calls a handler the way pyexpat's handler wrappers do: nothing
// after an error, and buffered character data is delivered first.
func (p *Parser) event(call func() error) {
	if p.pyErr != nil || !p.flushCharacterBuffer() {
		return
	}
	p.handlerDepth++
	err := call()
	p.handlerDepth--
	p.fail(err)
}

// flushCharacterBuffer delivers the buffered character data; it reports
// whether that succeeded.
func (p *Parser) flushCharacterBuffer() bool {
	if len(p.textBuffer) == 0 {
		return true
	}
	data := string(p.textBuffer)
	p.textBuffer = p.textBuffer[:0]
	return p.callCharacterHandler(data)
}

// callCharacterHandler calls CharacterDataHandler; it reports whether there
// was one and it succeeded.
func (p *Parser) callCharacterHandler(data string) bool {
	if p.CharacterDataHandler == nil {
		return false
	}
	p.handlerDepth++
	err := p.CharacterDataHandler(data)
	p.handlerDepth--
	p.fail(err)
	return err == nil
}

// characterData is pyexpat's character data handler: data is UTF-8 that
// expat reports. With buffer_text on, the data is collected and delivered
// when the buffer would overflow or another event comes.
func (p *Parser) characterData(data []byte) {
	if p.pyErr != nil {
		return
	}
	if !p.bufferText {
		p.callCharacterHandler(string(data))
		return
	}
	if len(p.textBuffer)+len(data) > charBufferSize {
		if !p.flushCharacterBuffer() || p.CharacterDataHandler == nil {
			return
		}
	}
	if len(data) > charBufferSize {
		p.callCharacterHandler(string(data))
		p.textBuffer = p.textBuffer[:0]
		return
	}
	p.textBuffer = append(p.textBuffer, data...)
}

// reportDefault passes markup b[s:end] in enc to DefaultHandler, through the
// conversion buffer when enc is not UTF-8.
func (p *Parser) reportDefault(enc *encoding, b []byte, s, end int) {
	if enc.isUTF8 {
		text := string(b[s:end])
		p.event(func() error { return p.DefaultHandler(text) })
		return
	}
	eventPP, eventEndPP := p.eventPointers(enc)
	for {
		out, res := enc.toUTF8(b, &s, end, nil, dataBufSize)
		*eventEndPP = s
		text := string(out)
		p.event(func() error { return p.DefaultHandler(text) })
		*eventPP = s
		if res == convertCompleted || res == convertInputIncomplete {
			return
		}
	}
}

// eventPointers returns where the position of the current event is kept:
// the parser's for its own input, the open entity's for entity text.
func (p *Parser) eventPointers(enc *encoding) (*int, *int) {
	if enc == p.encoding {
		return &p.eventPtr, &p.eventEndPtr
	}
	return &p.openInternal.internalEventPtr, &p.openInternal.internalEventEnd
}

// strPtr returns a pointer to a copy of s.
func strPtr(s string) *string { return &s }
