// Ported from libexpat 2.8.5 (expat/lib/xmltok_impl.c, xmltok.c, xmltok.h) to Go, 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Licensed under the MIT license.
// SPDX-License-Identifier: MIT

package expat

// Tokens. The scanners return a token and, through next, where the token
// ends; next is left alone where expat leaves it unset, so callers start it
// at the token's beginning.
const (
	tokTrailingRsqb = -5 // ] or ]] at the end of the scan
	tokNone         = -4 // empty input
	tokTrailingCr   = -3 // CR at the end of the scan
	tokPartialChar  = -2 // only part of a multi-unit character
	tokPartial      = -1 // only part of a token
	tokInvalid      = 0

	tokStartTagWithAtts     = 1
	tokStartTagNoAtts       = 2
	tokEmptyElementWithAtts = 3
	tokEmptyElementNoAtts   = 4
	tokEndTag               = 5
	tokDataChars            = 6
	tokDataNewline          = 7
	tokCdataSectOpen        = 8
	tokEntityRef            = 9
	tokCharRef              = 10

	tokPI                 = 11
	tokXMLDecl            = 12
	tokComment            = 13
	tokBOM                = 14
	tokPrologS            = 15
	tokDeclOpen           = 16
	tokDeclClose          = 17
	tokName               = 18
	tokNmtoken            = 19
	tokPoundName          = 20
	tokOr                 = 21
	tokPercent            = 22
	tokOpenParen          = 23
	tokCloseParen         = 24
	tokOpenBracket        = 25
	tokCloseBracket       = 26
	tokLiteral            = 27
	tokParamEntityRef     = 28
	tokInstanceStart      = 29
	tokNameQuestion       = 30
	tokNameAsterisk       = 31
	tokNamePlus           = 32
	tokCondSectOpen       = 33
	tokCondSectClose      = 34
	tokCloseParenQuestion = 35
	tokCloseParenAsterisk = 36
	tokCloseParenPlus     = 37
	tokComma              = 38
	tokAttributeValueS    = 39
	tokCdataSectClose     = 40
	tokPrefixedName       = 41
	tokIgnoreSect         = 42
)

// Scanner states.
const (
	scanProlog = iota
	scanContent
	scanCdataSection
	scanIgnoreSection
)

// Literal scanner types.
const (
	attributeValueLiteral = iota
	entityValueLiteral
)

func (e *encoding) hasChars(ptr, end, n int) bool { return end-ptr >= n*e.minBPC }

// tok is XmlTok: the scanner for state.
func (e *encoding) tok(state int, b []byte, ptr, end int, next *int) int {
	if e.kind == encInit {
		return e.initScan(state, b, ptr, end, next)
	}
	switch state {
	case scanProlog:
		return e.prologTok(b, ptr, end, next)
	case scanContent:
		return e.contentTok(b, ptr, end, next)
	case scanCdataSection:
		return e.cdataSectionTok(b, ptr, end, next)
	}
	return e.ignoreSectionTok(b, ptr, end, next)
}

// literalTok is XmlLiteralTok.
func (e *encoding) literalTok(typ int, b []byte, ptr, end int, next *int) int {
	if typ == attributeValueLiteral {
		return e.attributeValueTok(b, ptr, end, next)
	}
	return e.entityValueTok(b, ptr, end, next)
}

// initScan detects the encoding from the first bytes, stores it and scans
// the first token with it. The declared encoding is UTF-8, so only a byte
// order mark or a NUL among the first two bytes selects another one.
func (e *encoding) initScan(state int, b []byte, ptr, end int, next *int) int {
	if ptr >= end {
		return tokNone
	}
	if ptr+1 == end {
		switch b[ptr] {
		case 0xFE, 0xFF, 0xEF, 0x00, 0x3C:
			return tokPartial
		}
	} else {
		switch int(b[ptr])<<8 | int(b[ptr+1]) {
		case 0xFEFF:
			*next = ptr + 2
			*e.target = e.set.big2
			return tokBOM
		case 0x3C00:
			*e.target = e.set.little2
			return (*e.target).tok(state, b, ptr, end, next)
		case 0xFFFE:
			*next = ptr + 2
			*e.target = e.set.little2
			return tokBOM
		case 0xEFBB:
			if ptr+2 == end {
				return tokPartial
			}
			if b[ptr+2] == 0xBF {
				*next = ptr + 3
				*e.target = e.set.utf8
				return tokBOM
			}
		default:
			if b[ptr] == 0 {
				*e.target = e.set.big2
				return (*e.target).tok(state, b, ptr, end, next)
			} else if b[ptr+1] == 0 && state != scanContent {
				*e.target = e.set.little2
				return (*e.target).tok(state, b, ptr, end, next)
			}
		}
	}
	*e.target = e.set.utf8
	return (*e.target).tok(state, b, ptr, end, next)
}

// The INVALID_CASES of a scanner loop: it reports whether the character at
// *ptr ends the scan with tok, advancing *ptr over a valid multi-unit
// character.
func (e *encoding) invalidCase(b []byte, ptr *int, end, t int, next *int) (int, bool) {
	switch t {
	case btLead2, btLead3, btLead4:
		n := t - btLead2 + 2
		if end-*ptr < n {
			return tokPartialChar, true
		}
		if e.isInvalidChar(b, *ptr, n) {
			*next = *ptr
			return tokInvalid, true
		}
		*ptr += n
		return 0, false
	case btNonxml, btMalform, btTrail:
		*next = *ptr
		return tokInvalid, true
	}
	return 0, false
}

// nameCase is CHECK_NAME_CASES: handled reports whether t is a name
// character type; if so, tok >= -2 ends the scan with tok.
func (e *encoding) nameCase(b []byte, ptr *int, end, t int, next *int) (handled bool, tok int) {
	switch t {
	case btNonascii:
		if !e.isNameMin(b, *ptr) {
			*next = *ptr
			return true, tokInvalid
		}
		*ptr += e.minBPC
	case btNmstrt, btHex, btDigit, btName, btMinus:
		*ptr += e.minBPC
	case btLead2, btLead3, btLead4:
		n := t - btLead2 + 2
		if end-*ptr < n {
			return true, tokPartialChar
		}
		if e.isInvalidChar(b, *ptr, n) || !e.isNameChar(b, *ptr, n) {
			*next = *ptr
			return true, tokInvalid
		}
		*ptr += n
	default:
		return false, 0
	}
	return true, noTok
}

// nmstrtCase is CHECK_NMSTRT_CASES, with nameCase's results.
func (e *encoding) nmstrtCase(b []byte, ptr *int, end, t int, next *int) (handled bool, tok int) {
	switch t {
	case btNonascii:
		if !e.isNmstrtMin(b, *ptr) {
			*next = *ptr
			return true, tokInvalid
		}
		*ptr += e.minBPC
	case btNmstrt, btHex:
		*ptr += e.minBPC
	case btLead2, btLead3, btLead4:
		n := t - btLead2 + 2
		if end-*ptr < n {
			return true, tokPartialChar
		}
		if e.isInvalidChar(b, *ptr, n) || !e.isNmstrtChar(b, *ptr, n) {
			*next = *ptr
			return true, tokInvalid
		}
		*ptr += n
	default:
		return false, 0
	}
	return true, noTok
}

// noTok marks a name or name start case that consumed its character.
const noTok = -100

// scanComment: ptr is after "<!-".
func (e *encoding) scanComment(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if e.hasChars(ptr, end, 1) {
		if !e.charMatches(b, ptr, '-') {
			*next = ptr
			return tokInvalid
		}
		ptr += mb
		for e.hasChars(ptr, end, 1) {
			t := e.byteType(b, ptr)
			if tok, done := e.invalidCase(b, &ptr, end, t, next); done {
				return tok
			}
			switch t {
			case btLead2, btLead3, btLead4:
			case btMinus:
				ptr += mb
				if !e.hasChars(ptr, end, 1) {
					return tokPartial
				}
				if e.charMatches(b, ptr, '-') {
					ptr += mb
					if !e.hasChars(ptr, end, 1) {
						return tokPartial
					}
					if !e.charMatches(b, ptr, '>') {
						*next = ptr
						return tokInvalid
					}
					*next = ptr + mb
					return tokComment
				}
			default:
				ptr += mb
			}
		}
	}
	return tokPartial
}

// scanDecl: ptr is after "<!".
func (e *encoding) scanDecl(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if !e.hasChars(ptr, end, 1) {
		return tokPartial
	}
	switch e.byteType(b, ptr) {
	case btMinus:
		return e.scanComment(b, ptr+mb, end, next)
	case btLsqb:
		*next = ptr + mb
		return tokCondSectOpen
	case btNmstrt, btHex:
		ptr += mb
	default:
		*next = ptr
		return tokInvalid
	}
	for e.hasChars(ptr, end, 1) {
		switch e.byteType(b, ptr) {
		case btPercnt:
			if !e.hasChars(ptr, end, 2) {
				return tokPartial
			}
			switch e.byteType(b, ptr+mb) {
			case btS, btCr, btLf, btPercnt:
				*next = ptr
				return tokInvalid
			}
			*next = ptr
			return tokDeclOpen
		case btS, btCr, btLf:
			*next = ptr
			return tokDeclOpen
		case btNmstrt, btHex:
			ptr += mb
		default:
			*next = ptr
			return tokInvalid
		}
	}
	return tokPartial
}

// checkPiTarget reports whether the target b[ptr:end] is allowed and whether
// it is "xml" (an XML declaration).
func (e *encoding) checkPiTarget(b []byte, ptr, end int) (ok bool, tok int) {
	mb := e.minBPC
	if end-ptr != mb*3 {
		return true, tokPI
	}
	upper := false
	for _, want := range []byte("xml") {
		switch c := e.byteToASCII(b, ptr); c {
		case int(want):
		case int(want - 'a' + 'A'):
			upper = true
		default:
			return true, tokPI
		}
		ptr += mb
	}
	if upper {
		return false, tokPI
	}
	return true, tokXMLDecl
}

// scanPi: ptr is after "<?".
func (e *encoding) scanPi(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	target := ptr
	if !e.hasChars(ptr, end, 1) {
		return tokPartial
	}
	if handled, tok := e.nmstrtCase(b, &ptr, end, e.byteType(b, ptr), next); !handled {
		*next = ptr
		return tokInvalid
	} else if tok != noTok {
		return tok
	}
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if handled, tok := e.nameCase(b, &ptr, end, t, next); handled {
			if tok != noTok {
				return tok
			}
			continue
		}
		switch t {
		case btS, btCr, btLf:
			ok, tok := e.checkPiTarget(b, target, ptr)
			if !ok {
				*next = ptr
				return tokInvalid
			}
			ptr += mb
			for e.hasChars(ptr, end, 1) {
				t := e.byteType(b, ptr)
				if r, done := e.invalidCase(b, &ptr, end, t, next); done {
					return r
				}
				switch t {
				case btLead2, btLead3, btLead4:
				case btQuest:
					ptr += mb
					if !e.hasChars(ptr, end, 1) {
						return tokPartial
					}
					if e.charMatches(b, ptr, '>') {
						*next = ptr + mb
						return tok
					}
				default:
					ptr += mb
				}
			}
			return tokPartial
		case btQuest:
			ok, tok := e.checkPiTarget(b, target, ptr)
			if !ok {
				*next = ptr
				return tokInvalid
			}
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				return tokPartial
			}
			if e.charMatches(b, ptr, '>') {
				*next = ptr + mb
				return tok
			}
			*next = ptr
			return tokInvalid
		default:
			*next = ptr
			return tokInvalid
		}
	}
	return tokPartial
}

// scanCdataSection: ptr is after "<![".
func (e *encoding) scanCdataSection(b []byte, ptr, end int, next *int) int {
	if !e.hasChars(ptr, end, 6) {
		return tokPartial
	}
	for _, c := range []byte("CDATA[") {
		if !e.charMatches(b, ptr, c) {
			*next = ptr
			return tokInvalid
		}
		ptr += e.minBPC
	}
	*next = ptr
	return tokCdataSectOpen
}

// evenEnd shrinks end to whole code units; ok is false when none is left.
func (e *encoding) evenEnd(ptr, end int) (int, bool) {
	if e.minBPC > 1 {
		n := end - ptr
		if n&(e.minBPC-1) != 0 {
			n &^= e.minBPC - 1
			if n == 0 {
				return end, false
			}
			end = ptr + n
		}
	}
	return end, true
}

func (e *encoding) cdataSectionTok(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if ptr >= end {
		return tokNone
	}
	end, ok := e.evenEnd(ptr, end)
	if !ok {
		return tokPartial
	}
	t := e.byteType(b, ptr)
	switch t {
	case btRsqb:
		ptr += mb
		if !e.hasChars(ptr, end, 1) {
			return tokPartial
		}
		if !e.charMatches(b, ptr, ']') {
			break
		}
		ptr += mb
		if !e.hasChars(ptr, end, 1) {
			return tokPartial
		}
		if !e.charMatches(b, ptr, '>') {
			ptr -= mb
			break
		}
		*next = ptr + mb
		return tokCdataSectClose
	case btCr:
		ptr += mb
		if !e.hasChars(ptr, end, 1) {
			return tokPartial
		}
		if e.byteType(b, ptr) == btLf {
			ptr += mb
		}
		*next = ptr
		return tokDataNewline
	case btLf:
		*next = ptr + mb
		return tokDataNewline
	default:
		if tok, done := e.invalidCase(b, &ptr, end, t, next); done {
			return tok
		}
		if t != btLead2 && t != btLead3 && t != btLead4 {
			ptr += mb
		}
	}
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		switch t {
		case btLead2, btLead3, btLead4:
			n := t - btLead2 + 2
			if end-ptr < n || e.isInvalidChar(b, ptr, n) {
				*next = ptr
				return tokDataChars
			}
			ptr += n
		case btNonxml, btMalform, btTrail, btCr, btLf, btRsqb:
			*next = ptr
			return tokDataChars
		default:
			ptr += mb
		}
	}
	*next = ptr
	return tokDataChars
}

// scanEndTag: ptr is after "</".
func (e *encoding) scanEndTag(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if !e.hasChars(ptr, end, 1) {
		return tokPartial
	}
	if handled, tok := e.nmstrtCase(b, &ptr, end, e.byteType(b, ptr), next); !handled {
		*next = ptr
		return tokInvalid
	} else if tok != noTok {
		return tok
	}
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if handled, tok := e.nameCase(b, &ptr, end, t, next); handled {
			if tok != noTok {
				return tok
			}
			continue
		}
		switch t {
		case btS, btCr, btLf:
			for ptr += mb; e.hasChars(ptr, end, 1); ptr += mb {
				switch e.byteType(b, ptr) {
				case btS, btCr, btLf:
				case btGt:
					*next = ptr + mb
					return tokEndTag
				default:
					*next = ptr
					return tokInvalid
				}
			}
			return tokPartial
		case btColon:
			ptr += mb
		case btGt:
			*next = ptr + mb
			return tokEndTag
		default:
			*next = ptr
			return tokInvalid
		}
	}
	return tokPartial
}

// scanHexCharRef: ptr is after "&#x".
func (e *encoding) scanHexCharRef(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if e.hasChars(ptr, end, 1) {
		switch e.byteType(b, ptr) {
		case btDigit, btHex:
		default:
			*next = ptr
			return tokInvalid
		}
		for ptr += mb; e.hasChars(ptr, end, 1); ptr += mb {
			switch e.byteType(b, ptr) {
			case btDigit, btHex:
			case btSemi:
				*next = ptr + mb
				return tokCharRef
			default:
				*next = ptr
				return tokInvalid
			}
		}
	}
	return tokPartial
}

// scanCharRef: ptr is after "&#".
func (e *encoding) scanCharRef(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if e.hasChars(ptr, end, 1) {
		if e.charMatches(b, ptr, 'x') {
			return e.scanHexCharRef(b, ptr+mb, end, next)
		}
		if e.byteType(b, ptr) != btDigit {
			*next = ptr
			return tokInvalid
		}
		for ptr += mb; e.hasChars(ptr, end, 1); ptr += mb {
			switch e.byteType(b, ptr) {
			case btDigit:
			case btSemi:
				*next = ptr + mb
				return tokCharRef
			default:
				*next = ptr
				return tokInvalid
			}
		}
	}
	return tokPartial
}

// scanRef: ptr is after "&".
func (e *encoding) scanRef(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if !e.hasChars(ptr, end, 1) {
		return tokPartial
	}
	t := e.byteType(b, ptr)
	if t == btNum {
		return e.scanCharRef(b, ptr+mb, end, next)
	}
	if handled, tok := e.nmstrtCase(b, &ptr, end, t, next); !handled {
		*next = ptr
		return tokInvalid
	} else if tok != noTok {
		return tok
	}
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if handled, tok := e.nameCase(b, &ptr, end, t, next); handled {
			if tok != noTok {
				return tok
			}
			continue
		}
		if t == btSemi {
			*next = ptr + mb
			return tokEntityRef
		}
		*next = ptr
		return tokInvalid
	}
	return tokPartial
}

// scanAtts: ptr is after the first character of an attribute name.
func (e *encoding) scanAtts(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	hadColon := false
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if handled, tok := e.nameCase(b, &ptr, end, t, next); handled {
			if tok != noTok {
				return tok
			}
			continue
		}
		switch t {
		case btColon:
			if hadColon {
				*next = ptr
				return tokInvalid
			}
			hadColon = true
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				return tokPartial
			}
			if handled, tok := e.nmstrtCase(b, &ptr, end, e.byteType(b, ptr), next); !handled {
				*next = ptr
				return tokInvalid
			} else if tok != noTok {
				return tok
			}
			continue
		case btS, btCr, btLf, btEquals:
		default:
			*next = ptr
			return tokInvalid
		}
		if t != btEquals {
			for {
				ptr += mb
				if !e.hasChars(ptr, end, 1) {
					return tokPartial
				}
				t := e.byteType(b, ptr)
				if t == btEquals {
					break
				}
				switch t {
				case btS, btLf, btCr:
				default:
					*next = ptr
					return tokInvalid
				}
			}
		}
		// ptr is at '='.
		hadColon = false
		var open int
		for {
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				return tokPartial
			}
			open = e.byteType(b, ptr)
			if open == btQuot || open == btApos {
				break
			}
			switch open {
			case btS, btLf, btCr:
			default:
				*next = ptr
				return tokInvalid
			}
		}
		ptr += mb
		// In the attribute value.
		for {
			if !e.hasChars(ptr, end, 1) {
				return tokPartial
			}
			t := e.byteType(b, ptr)
			if t == open {
				break
			}
			if tok, done := e.invalidCase(b, &ptr, end, t, next); done {
				return tok
			}
			switch t {
			case btLead2, btLead3, btLead4:
			case btAmp:
				tok := e.scanRef(b, ptr+mb, end, &ptr)
				if tok <= 0 {
					if tok == tokInvalid {
						*next = ptr
					}
					return tok
				}
			case btLt:
				*next = ptr
				return tokInvalid
			default:
				ptr += mb
			}
		}
		ptr += mb
		if !e.hasChars(ptr, end, 1) {
			return tokPartial
		}
		switch e.byteType(b, ptr) {
		case btS, btCr, btLf:
		case btSol:
			return e.emptyElementEnd(b, ptr, end, next, tokEmptyElementWithAtts)
		case btGt:
			*next = ptr + mb
			return tokStartTagWithAtts
		default:
			*next = ptr
			return tokInvalid
		}
		// ptr is at the white space after the closing quote.
		for {
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				return tokPartial
			}
			t := e.byteType(b, ptr)
			if handled, tok := e.nmstrtCase(b, &ptr, end, t, next); handled {
				if tok != noTok {
					return tok
				}
				break
			}
			switch t {
			case btS, btCr, btLf:
				continue
			case btGt:
				*next = ptr + mb
				return tokStartTagWithAtts
			case btSol:
				return e.emptyElementEnd(b, ptr, end, next, tokEmptyElementWithAtts)
			default:
				*next = ptr
				return tokInvalid
			}
		}
	}
	return tokPartial
}

// emptyElementEnd checks the "/>" at ptr.
func (e *encoding) emptyElementEnd(b []byte, ptr, end int, next *int, tok int) int {
	ptr += e.minBPC
	if !e.hasChars(ptr, end, 1) {
		return tokPartial
	}
	if !e.charMatches(b, ptr, '>') {
		*next = ptr
		return tokInvalid
	}
	*next = ptr + e.minBPC
	return tok
}

// scanLt: ptr is after "<".
func (e *encoding) scanLt(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if !e.hasChars(ptr, end, 1) {
		return tokPartial
	}
	t := e.byteType(b, ptr)
	if handled, tok := e.nmstrtCase(b, &ptr, end, t, next); handled {
		if tok != noTok {
			return tok
		}
	} else {
		switch t {
		case btExcl:
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				return tokPartial
			}
			switch e.byteType(b, ptr) {
			case btMinus:
				return e.scanComment(b, ptr+mb, end, next)
			case btLsqb:
				return e.scanCdataSection(b, ptr+mb, end, next)
			}
			*next = ptr
			return tokInvalid
		case btQuest:
			return e.scanPi(b, ptr+mb, end, next)
		case btSol:
			return e.scanEndTag(b, ptr+mb, end, next)
		default:
			*next = ptr
			return tokInvalid
		}
	}
	hadColon := false
	// A start-tag.
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if handled, tok := e.nameCase(b, &ptr, end, t, next); handled {
			if tok != noTok {
				return tok
			}
			continue
		}
		switch t {
		case btColon:
			if hadColon {
				*next = ptr
				return tokInvalid
			}
			hadColon = true
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				return tokPartial
			}
			if handled, tok := e.nmstrtCase(b, &ptr, end, e.byteType(b, ptr), next); !handled {
				*next = ptr
				return tokInvalid
			} else if tok != noTok {
				return tok
			}
		case btS, btCr, btLf:
			ptr += mb
			for e.hasChars(ptr, end, 1) {
				t := e.byteType(b, ptr)
				if handled, tok := e.nmstrtCase(b, &ptr, end, t, next); handled {
					if tok != noTok {
						return tok
					}
					return e.scanAtts(b, ptr, end, next)
				}
				switch t {
				case btGt:
					*next = ptr + mb
					return tokStartTagNoAtts
				case btSol:
					return e.emptyElementEnd(b, ptr, end, next, tokEmptyElementNoAtts)
				case btS, btCr, btLf:
					ptr += mb
				default:
					*next = ptr
					return tokInvalid
				}
			}
			return tokPartial
		case btGt:
			*next = ptr + mb
			return tokStartTagNoAtts
		case btSol:
			return e.emptyElementEnd(b, ptr, end, next, tokEmptyElementNoAtts)
		default:
			*next = ptr
			return tokInvalid
		}
	}
	return tokPartial
}

func (e *encoding) contentTok(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if ptr >= end {
		return tokNone
	}
	end, ok := e.evenEnd(ptr, end)
	if !ok {
		return tokPartial
	}
	t := e.byteType(b, ptr)
	switch t {
	case btLt:
		return e.scanLt(b, ptr+mb, end, next)
	case btAmp:
		return e.scanRef(b, ptr+mb, end, next)
	case btCr:
		ptr += mb
		if !e.hasChars(ptr, end, 1) {
			return tokTrailingCr
		}
		if e.byteType(b, ptr) == btLf {
			ptr += mb
		}
		*next = ptr
		return tokDataNewline
	case btLf:
		*next = ptr + mb
		return tokDataNewline
	case btRsqb:
		ptr += mb
		if !e.hasChars(ptr, end, 1) {
			return tokTrailingRsqb
		}
		if e.charMatches(b, ptr, ']') {
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				return tokTrailingRsqb
			}
			if e.charMatches(b, ptr, '>') {
				*next = ptr
				return tokInvalid
			}
			ptr -= mb
		}
	default:
		if tok, done := e.invalidCase(b, &ptr, end, t, next); done {
			return tok
		}
		if t != btLead2 && t != btLead3 && t != btLead4 {
			ptr += mb
		}
	}
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		switch t {
		case btLead2, btLead3, btLead4:
			n := t - btLead2 + 2
			if end-ptr < n || e.isInvalidChar(b, ptr, n) {
				*next = ptr
				return tokDataChars
			}
			ptr += n
			continue
		case btRsqb:
			if e.hasChars(ptr, end, 2) {
				if !e.charMatches(b, ptr+mb, ']') {
					ptr += mb
					continue
				}
				if e.hasChars(ptr, end, 3) {
					if !e.charMatches(b, ptr+2*mb, '>') {
						ptr += mb
						continue
					}
					*next = ptr + 2*mb
					return tokInvalid
				}
			}
			*next = ptr
			return tokDataChars
		case btAmp, btLt, btNonxml, btMalform, btTrail, btCr, btLf:
			*next = ptr
			return tokDataChars
		default:
			ptr += mb
		}
	}
	*next = ptr
	return tokDataChars
}

// scanPercent: ptr is after "%".
func (e *encoding) scanPercent(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if !e.hasChars(ptr, end, 1) {
		return tokPartial
	}
	t := e.byteType(b, ptr)
	if handled, tok := e.nmstrtCase(b, &ptr, end, t, next); handled {
		if tok != noTok {
			return tok
		}
	} else {
		switch t {
		case btS, btLf, btCr, btPercnt:
			*next = ptr
			return tokPercent
		default:
			*next = ptr
			return tokInvalid
		}
	}
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if handled, tok := e.nameCase(b, &ptr, end, t, next); handled {
			if tok != noTok {
				return tok
			}
			continue
		}
		if t == btSemi {
			*next = ptr + mb
			return tokParamEntityRef
		}
		*next = ptr
		return tokInvalid
	}
	return tokPartial
}

// scanPoundName: ptr is after "#".
func (e *encoding) scanPoundName(b []byte, ptr, end int, next *int) int {
	if !e.hasChars(ptr, end, 1) {
		return tokPartial
	}
	if handled, tok := e.nmstrtCase(b, &ptr, end, e.byteType(b, ptr), next); !handled {
		*next = ptr
		return tokInvalid
	} else if tok != noTok {
		return tok
	}
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if handled, tok := e.nameCase(b, &ptr, end, t, next); handled {
			if tok != noTok {
				return tok
			}
			continue
		}
		switch t {
		case btCr, btLf, btS, btRpar, btGt, btPercnt, btVerbar:
			*next = ptr
			return tokPoundName
		}
		*next = ptr
		return tokInvalid
	}
	return -tokPoundName
}

// scanLit: ptr is after the opening quote of type open.
func (e *encoding) scanLit(open int, b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if tok, done := e.invalidCase(b, &ptr, end, t, next); done {
			return tok
		}
		switch t {
		case btLead2, btLead3, btLead4:
		case btQuot, btApos:
			ptr += mb
			if t != open {
				break
			}
			if !e.hasChars(ptr, end, 1) {
				return -tokLiteral
			}
			*next = ptr
			switch e.byteType(b, ptr) {
			case btS, btCr, btLf, btGt, btPercnt, btLsqb:
				return tokLiteral
			}
			return tokInvalid
		default:
			ptr += mb
		}
	}
	return tokPartial
}

func (e *encoding) prologTok(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	if ptr >= end {
		return tokNone
	}
	end, ok := e.evenEnd(ptr, end)
	if !ok {
		return tokPartial
	}
	var tok int
	t := e.byteType(b, ptr)
	switch t {
	case btQuot, btApos:
		return e.scanLit(t, b, ptr+mb, end, next)
	case btLt:
		ptr += mb
		if !e.hasChars(ptr, end, 1) {
			return tokPartial
		}
		switch e.byteType(b, ptr) {
		case btExcl:
			return e.scanDecl(b, ptr+mb, end, next)
		case btQuest:
			return e.scanPi(b, ptr+mb, end, next)
		case btNmstrt, btHex, btNonascii, btLead2, btLead3, btLead4:
			*next = ptr - mb
			return tokInstanceStart
		}
		*next = ptr
		return tokInvalid
	case btCr, btS, btLf:
		if t == btCr && ptr+mb == end {
			*next = end
			return -tokPrologS
		}
		for {
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				break
			}
			switch e.byteType(b, ptr) {
			case btS, btLf:
				continue
			case btCr:
				if ptr+mb != end {
					continue
				}
			}
			*next = ptr
			return tokPrologS
		}
		*next = ptr
		return tokPrologS
	case btPercnt:
		return e.scanPercent(b, ptr+mb, end, next)
	case btComma:
		*next = ptr + mb
		return tokComma
	case btLsqb:
		*next = ptr + mb
		return tokOpenBracket
	case btRsqb:
		ptr += mb
		if !e.hasChars(ptr, end, 1) {
			return -tokCloseBracket
		}
		if e.charMatches(b, ptr, ']') {
			if !e.hasChars(ptr, end, 2) {
				return tokPartial
			}
			if e.charMatches(b, ptr+mb, '>') {
				*next = ptr + 2*mb
				return tokCondSectClose
			}
		}
		*next = ptr
		return tokCloseBracket
	case btLpar:
		*next = ptr + mb
		return tokOpenParen
	case btRpar:
		ptr += mb
		if !e.hasChars(ptr, end, 1) {
			return -tokCloseParen
		}
		switch e.byteType(b, ptr) {
		case btAst:
			*next = ptr + mb
			return tokCloseParenAsterisk
		case btQuest:
			*next = ptr + mb
			return tokCloseParenQuestion
		case btPlus:
			*next = ptr + mb
			return tokCloseParenPlus
		case btCr, btLf, btS, btGt, btComma, btVerbar, btRpar:
			*next = ptr
			return tokCloseParen
		}
		*next = ptr
		return tokInvalid
	case btVerbar:
		*next = ptr + mb
		return tokOr
	case btGt:
		*next = ptr + mb
		return tokDeclClose
	case btNum:
		return e.scanPoundName(b, ptr+mb, end, next)
	case btLead2, btLead3, btLead4:
		n := t - btLead2 + 2
		if end-ptr < n {
			return tokPartialChar
		}
		if e.isInvalidChar(b, ptr, n) {
			*next = ptr
			return tokInvalid
		}
		if e.isNmstrtChar(b, ptr, n) {
			ptr += n
			tok = tokName
			break
		}
		if e.isNameChar(b, ptr, n) {
			ptr += n
			tok = tokNmtoken
			break
		}
		*next = ptr
		return tokInvalid
	case btNmstrt, btHex:
		tok = tokName
		ptr += mb
	case btDigit, btName, btMinus, btColon:
		tok = tokNmtoken
		ptr += mb
	case btNonascii:
		if e.isNmstrtMin(b, ptr) {
			ptr += mb
			tok = tokName
			break
		}
		if e.isNameMin(b, ptr) {
			ptr += mb
			tok = tokNmtoken
			break
		}
		*next = ptr
		return tokInvalid
	default:
		*next = ptr
		return tokInvalid
	}
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if handled, r := e.nameCase(b, &ptr, end, t, next); handled {
			if r != noTok {
				return r
			}
			continue
		}
		switch t {
		case btGt, btRpar, btComma, btVerbar, btLsqb, btPercnt, btS, btCr, btLf:
			*next = ptr
			return tok
		case btColon:
			ptr += mb
			switch tok {
			case tokName:
				if !e.hasChars(ptr, end, 1) {
					return tokPartial
				}
				tok = tokPrefixedName
				if handled, r := e.nameCase(b, &ptr, end, e.byteType(b, ptr), next); handled {
					if r != noTok {
						return r
					}
				} else {
					tok = tokNmtoken
				}
			case tokPrefixedName:
				tok = tokNmtoken
			}
		case btPlus, btAst, btQuest:
			if tok == tokNmtoken {
				*next = ptr
				return tokInvalid
			}
			*next = ptr + mb
			switch t {
			case btPlus:
				return tokNamePlus
			case btAst:
				return tokNameAsterisk
			}
			return tokNameQuestion
		default:
			*next = ptr
			return tokInvalid
		}
	}
	return -tok
}

func (e *encoding) attributeValueTok(b []byte, ptr, end int, next *int) int {
	return e.valueTok(b, ptr, end, next, false)
}

func (e *encoding) entityValueTok(b []byte, ptr, end int, next *int) int {
	return e.valueTok(b, ptr, end, next, true)
}

// valueTok is attributeValueTok, or entityValueTok when entity is set: they
// differ in '%' (a parameter entity reference in an entity value), '<' (an
// error in an attribute value) and white space (a separate token in an
// attribute value).
func (e *encoding) valueTok(b []byte, ptr, end int, next *int, entity bool) int {
	mb := e.minBPC
	if ptr >= end {
		return tokNone
	}
	if !e.hasChars(ptr, end, 1) {
		return tokPartial
	}
	start := ptr
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		switch {
		case t == btLead2 || t == btLead3 || t == btLead4:
			ptr += t - btLead2 + 2
			continue
		case t == btAmp:
			if ptr == start {
				return e.scanRef(b, ptr+mb, end, next)
			}
			*next = ptr
			return tokDataChars
		case t == btPercnt && entity:
			if ptr == start {
				tok := e.scanPercent(b, ptr+mb, end, next)
				if tok == tokPercent {
					return tokInvalid
				}
				return tok
			}
			*next = ptr
			return tokDataChars
		case t == btLt && !entity:
			*next = ptr
			return tokInvalid
		case t == btLf:
			if ptr == start {
				*next = ptr + mb
				return tokDataNewline
			}
			*next = ptr
			return tokDataChars
		case t == btCr:
			if ptr == start {
				ptr += mb
				if !e.hasChars(ptr, end, 1) {
					return tokTrailingCr
				}
				if e.byteType(b, ptr) == btLf {
					ptr += mb
				}
				*next = ptr
				return tokDataNewline
			}
			*next = ptr
			return tokDataChars
		case t == btS && !entity:
			if ptr == start {
				*next = ptr + mb
				return tokAttributeValueS
			}
			*next = ptr
			return tokDataChars
		}
		ptr += mb
	}
	*next = ptr
	return tokDataChars
}

func (e *encoding) ignoreSectionTok(b []byte, ptr, end int, next *int) int {
	mb := e.minBPC
	level := 0
	if mb > 1 {
		end = ptr + (end-ptr)&^(mb-1)
	}
	for e.hasChars(ptr, end, 1) {
		t := e.byteType(b, ptr)
		if tok, done := e.invalidCase(b, &ptr, end, t, next); done {
			return tok
		}
		switch t {
		case btLead2, btLead3, btLead4:
		case btLt:
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				return tokPartial
			}
			if e.charMatches(b, ptr, '!') {
				ptr += mb
				if !e.hasChars(ptr, end, 1) {
					return tokPartial
				}
				if e.charMatches(b, ptr, '[') {
					level++
					ptr += mb
				}
			}
		case btRsqb:
			ptr += mb
			if !e.hasChars(ptr, end, 1) {
				return tokPartial
			}
			if e.charMatches(b, ptr, ']') {
				ptr += mb
				if !e.hasChars(ptr, end, 1) {
					return tokPartial
				}
				if e.charMatches(b, ptr, '>') {
					ptr += mb
					if level == 0 {
						*next = ptr
						return tokIgnoreSect
					}
					level--
				}
			}
		default:
			ptr += mb
		}
	}
	return tokPartial
}

// isPublicId checks a public identifier literal b[ptr:end] (with its
// quotes), returning the offending position when it is not valid.
func (e *encoding) isPublicId(b []byte, ptr, end int) (bool, int) {
	mb := e.minBPC
	ptr += mb
	end -= mb
	for ; e.hasChars(ptr, end, 1); ptr += mb {
		switch e.byteType(b, ptr) {
		case btDigit, btHex, btMinus, btApos, btLpar, btRpar, btPlus, btComma, btSol, btEquals,
			btQuest, btCr, btLf, btSemi, btExcl, btAst, btPercnt, btNum, btColon:
			continue
		case btS:
			if e.charMatches(b, ptr, '\t') {
				return false, ptr
			}
			continue
		case btName, btNmstrt:
			if e.byteToASCII(b, ptr)&^0x7f == 0 {
				continue
			}
		}
		switch e.byteToASCII(b, ptr) {
		case 0x24, 0x40:
		default:
			return false, ptr
		}
	}
	return true, 0
}

// attribute is expat's ATTRIBUTE: where getAtts found a name and value.
type attribute struct {
	name, valuePtr, valueEnd int
	normalized               bool
}

// getAtts finds the attributes of a well-formed start-tag at ptr.
func (e *encoding) getAtts(b []byte, ptr int) []attribute {
	mb := e.minBPC
	const (
		other = iota
		inName
		inValue
	)
	state := inName
	var atts []attribute
	var cur attribute
	open := 0
	startName := func(p int) {
		if state == other {
			cur = attribute{name: p, normalized: true}
			state = inName
		}
	}
	for ptr += mb; ; ptr += mb {
		t := e.byteType(b, ptr)
		switch t {
		case btLead2, btLead3, btLead4:
			startName(ptr)
			ptr += t - btLead2 + 2 - mb
		case btNonascii, btNmstrt, btHex:
			startName(ptr)
		case btQuot, btApos:
			if state != inValue {
				cur.valuePtr = ptr + mb
				state = inValue
				open = t
			} else if open == t {
				state = other
				cur.valueEnd = ptr
				atts = append(atts, cur)
			}
		case btAmp:
			cur.normalized = false
		case btS:
			if state == inName {
				state = other
			} else if state == inValue && cur.normalized &&
				(ptr == cur.valuePtr || e.byteToASCII(b, ptr) != ' ' ||
					e.byteToASCII(b, ptr+mb) == ' ' || e.byteType(b, ptr+mb) == open) {
				cur.normalized = false
			}
		case btCr, btLf:
			if state == inName {
				state = other
			} else if state == inValue {
				cur.normalized = false
			}
		case btGt, btSol:
			if state != inValue {
				return atts
			}
		}
	}
}

// charRefNumber returns the code point of the character reference
// b[ptr:] ("&#...;"), or -1 when it is not allowed.
func (e *encoding) charRefNumber(b []byte, ptr int) int {
	mb := e.minBPC
	result := 0
	ptr += 2 * mb
	if e.charMatches(b, ptr, 'x') {
		for ptr += mb; !e.charMatches(b, ptr, ';'); ptr += mb {
			c := e.byteToASCII(b, ptr)
			switch {
			case c >= '0' && c <= '9':
				result = result<<4 | (c - '0')
			case c >= 'A' && c <= 'F':
				result = result<<4 + 10 + (c - 'A')
			case c >= 'a' && c <= 'f':
				result = result<<4 + 10 + (c - 'a')
			}
			if result >= 0x110000 {
				return -1
			}
		}
	} else {
		for ; !e.charMatches(b, ptr, ';'); ptr += mb {
			result = result*10 + (e.byteToASCII(b, ptr) - '0')
			if result >= 0x110000 {
				return -1
			}
		}
	}
	return checkCharRefNumber(result)
}

// predefinedEntityName returns the character of a predefined entity name
// b[ptr:end] (lt, gt, amp, quot, apos), or 0.
func (e *encoding) predefinedEntityName(b []byte, ptr, end int) byte {
	for _, p := range []struct {
		name string
		c    byte
	}{{"lt", '<'}, {"gt", '>'}, {"amp", '&'}, {"quot", '"'}, {"apos", '\''}} {
		if e.nameMatchesASCII(b, ptr, end, p.name) {
			return p.c
		}
	}
	return 0
}

// nameMatchesASCII reports whether b[ptr:end] is the ASCII string s.
func (e *encoding) nameMatchesASCII(b []byte, ptr, end int, s string) bool {
	mb := e.minBPC
	for i := 0; i < len(s); i, ptr = i+1, ptr+mb {
		if end-ptr < mb {
			return false
		}
		if !e.charMatches(b, ptr, s[i]) {
			return false
		}
	}
	return ptr == end
}

// nameLength is the length in bytes of the name at ptr.
func (e *encoding) nameLength(b []byte, ptr int) int {
	start := ptr
	for {
		switch t := e.byteType(b, ptr); t {
		case btLead2, btLead3, btLead4:
			ptr += t - btLead2 + 2
		case btNonascii, btNmstrt, btColon, btHex, btDigit, btName, btMinus:
			ptr += e.minBPC
		default:
			return ptr - start
		}
	}
}

// skipS skips white space at ptr.
func (e *encoding) skipS(b []byte, ptr int) int {
	for {
		switch e.byteType(b, ptr) {
		case btLf, btCr, btS:
			ptr += e.minBPC
		default:
			return ptr
		}
	}
}
