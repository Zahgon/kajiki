// Ported from libexpat 2.8.5 (expat/lib/xmltok.c, xmltok_ns.c) to Go, 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Licensed under the MIT license.
// SPDX-License-Identifier: MIT

package expat

// encKind selects how an encoding reads code units.
type encKind int

const (
	encUTF8    encKind = iota // UTF-8, also the internal encoding
	encLittle2                // UTF-16LE
	encBig2                   // UTF-16BE
	encInit                   // not yet detected: the first scan picks one
)

// encoding is expat's ENCODING: the byte type table and the code-unit
// layout the tokenizer works with, and the conversion to UTF-8.
type encoding struct {
	kind   encKind
	minBPC int
	// isUTF8 is set when the input bytes are already UTF-8 and are reported
	// without conversion.
	isUTF8 bool
	types  [256]byte
	// set is the family the encoding belongs to: the encodings of parsers
	// with or without namespace processing.
	set *encodingSet
	// target is, for an INIT_ENCODING, where the detected encoding is
	// stored.
	target **encoding
}

// Results of the conversion to UTF-8.
type convertResult int

const (
	convertCompleted convertResult = iota
	convertInputIncomplete
	convertOutputExhausted
)

// encodingSet holds the encodings of one kind of parser. Expat compiles its
// tokenizer twice: for a parser with namespace processing (xmltok_ns.c) the
// colon has a byte type of its own and the scanners check where it may
// appear in a name; without namespace processing it is an ordinary name
// start character.
type encodingSet struct {
	utf8, internalUTF8, little2, big2 *encoding
}

func newEncodingSet(ns bool) *encodingSet {
	set := &encodingSet{}
	enc := func(kind encKind, minBPC int, isUTF8 bool, high *[128]byte, internal bool) *encoding {
		e := &encoding{kind: kind, minBPC: minBPC, isUTF8: isUTF8, set: set}
		copy(e.types[:128], asciiTypes[:])
		if !ns {
			e.types[':'] = btNmstrt
		}
		if internal {
			e.types[0x0D] = btS
		}
		if high != nil {
			copy(e.types[128:], high[:])
		}
		return e
	}
	set.utf8 = enc(encUTF8, 1, true, &utf8Types, false)
	set.internalUTF8 = enc(encUTF8, 1, true, &utf8Types, true)
	set.little2 = enc(encLittle2, 2, false, &latin1Types, false)
	set.big2 = enc(encBig2, 2, false, &latin1Types, false)
	return set
}

var (
	nsEncodings    = newEncodingSet(true)
	plainEncodings = newEncodingSet(false)
)

// initEncoding is XmlInitEncoding (XmlInitEncodingNS for set nsEncodings)
// with the encoding name "utf-8", the protocol encoding pyexpat sets for
// text input: *target becomes an INIT_ENCODING that detects the actual
// encoding on the first scan and stores it in *target.
func initEncoding(target **encoding, set *encodingSet) {
	*target = &encoding{kind: encInit, minBPC: 1, target: target, set: set}
}

func unicodeByteType(hi, lo byte) int {
	switch {
	case hi >= 0xD8 && hi <= 0xDB:
		return btLead4
	case hi >= 0xDC && hi <= 0xDF:
		return btTrail
	case hi == 0xFF && (lo == 0xFF || lo == 0xFE):
		return btNonxml
	}
	return btNonascii
}

func (e *encoding) byteType(b []byte, p int) int {
	switch e.kind {
	case encLittle2:
		if b[p+1] == 0 {
			return int(e.types[b[p]])
		}
		return unicodeByteType(b[p+1], b[p])
	case encBig2:
		if b[p] == 0 {
			return int(e.types[b[p+1]])
		}
		return unicodeByteType(b[p], b[p+1])
	}
	return int(e.types[b[p]])
}

// byteToASCII is BYTE_TO_ASCII: the code unit as a C char (bytes above 0x7F
// are negative), or -1 for a 16-bit unit outside Latin-1.
func (e *encoding) byteToASCII(b []byte, p int) int {
	switch e.kind {
	case encLittle2:
		if b[p+1] == 0 {
			return int(int8(b[p]))
		}
		return -1
	case encBig2:
		if b[p] == 0 {
			return int(int8(b[p+1]))
		}
		return -1
	}
	return int(int8(b[p]))
}

func (e *encoding) charMatches(b []byte, p int, c byte) bool {
	switch e.kind {
	case encLittle2:
		return b[p+1] == 0 && b[p] == c
	case encBig2:
		return b[p] == 0 && b[p+1] == c
	}
	return b[p] == c
}

func utf8Naming2(pages *[256]byte, b []byte) bool {
	return namingBitmap[int(pages[(b[0]>>2)&7])<<3+int(b[0]&3)<<1+int((b[1]>>5)&1)]&(1<<(b[1]&0x1F)) != 0
}

func utf8Naming3(pages *[256]byte, b []byte) bool {
	return namingBitmap[int(pages[int(b[0]&0xF)<<4+int((b[1]>>2)&0xF)])<<3+int(b[1]&3)<<1+int((b[2]>>5)&1)]&(1<<(b[2]&0x1F)) != 0
}

// isNameChar is IS_NAME_CHAR for a multi-unit character of n bytes.
func (e *encoding) isNameChar(b []byte, p, n int) bool {
	if e.kind != encUTF8 {
		return false
	}
	switch n {
	case 2:
		return utf8Naming2(&namePages, b[p:])
	case 3:
		return utf8Naming3(&namePages, b[p:])
	}
	return false
}

// isNmstrtChar is IS_NMSTRT_CHAR for a multi-unit character of n bytes.
func (e *encoding) isNmstrtChar(b []byte, p, n int) bool {
	if e.kind != encUTF8 {
		return false
	}
	switch n {
	case 2:
		return utf8Naming2(&nmstrtPages, b[p:])
	case 3:
		return utf8Naming3(&nmstrtPages, b[p:])
	}
	return false
}

// isInvalidChar is IS_INVALID_CHAR for a multi-unit character of n bytes.
func (e *encoding) isInvalidChar(b []byte, p, n int) bool {
	switch e.kind {
	case encLittle2:
		return n == 4 && b[p+3]&0xFC != 0xDC
	case encBig2:
		return n == 4 && b[p+2]&0xFC != 0xDC
	case encUTF8:
		c := b[p:]
		switch n {
		case 2:
			return c[0] < 0xC2 || c[1]&0x80 == 0 || c[1]&0xC0 == 0xC0
		case 3:
			if c[2]&0x80 == 0 {
				return true
			}
			if c[0] == 0xEF && c[1] == 0xBF {
				if c[2] > 0xBD {
					return true
				}
			} else if c[2]&0xC0 == 0xC0 {
				return true
			}
			if c[0] == 0xE0 {
				return c[1] < 0xA0 || c[1]&0xC0 == 0xC0
			}
			if c[1]&0x80 == 0 {
				return true
			}
			if c[0] == 0xED {
				return c[1] > 0x9F
			}
			return c[1]&0xC0 == 0xC0
		case 4:
			if c[3]&0x80 == 0 || c[3]&0xC0 == 0xC0 || c[2]&0x80 == 0 || c[2]&0xC0 == 0xC0 {
				return true
			}
			if c[0] == 0xF0 {
				return c[1] < 0x90 || c[1]&0xC0 == 0xC0
			}
			if c[1]&0x80 == 0 {
				return true
			}
			if c[0] == 0xF4 {
				return c[1] > 0x8F
			}
			return c[1]&0xC0 == 0xC0
		}
	}
	return false
}

// isNameMin and isNmstrtMin classify a BT_NONASCII UTF-16 unit.
func (e *encoding) isNameMin(b []byte, p int) bool {
	switch e.kind {
	case encLittle2:
		return ucs2Naming(&namePages, int(b[p+1]), int(b[p]))
	case encBig2:
		return ucs2Naming(&namePages, int(b[p]), int(b[p+1]))
	}
	return false
}

func (e *encoding) isNmstrtMin(b []byte, p int) bool {
	switch e.kind {
	case encLittle2:
		return ucs2Naming(&nmstrtPages, int(b[p+1]), int(b[p]))
	case encBig2:
		return ucs2Naming(&nmstrtPages, int(b[p]), int(b[p+1]))
	}
	return false
}

// checkCharRefNumber returns -1 for code points XML does not allow.
func checkCharRefNumber(result int) int {
	switch result >> 8 {
	case 0xD8, 0xD9, 0xDA, 0xDB, 0xDC, 0xDD, 0xDE, 0xDF:
		return -1
	case 0:
		if result < 0x80 && asciiTypes[result] == btNonxml {
			return -1
		}
	case 0xFF:
		if result == 0xFFFE || result == 0xFFFF {
			return -1
		}
	}
	return result
}

// toUTF8 is XmlUtf8Convert: it appends the UTF-8 form of b[*from:fromLim] to
// out, stopping before out would exceed limit bytes (limit < 0: no limit).
func (e *encoding) toUTF8(b []byte, from *int, fromLim int, out []byte, limit int) ([]byte, convertResult) {
	room := func(n int) bool { return limit < 0 || len(out)+n <= limit }
	switch e.kind {
	case encUTF8, encInit:
		res := convertCompleted
		if limit >= 0 && fromLim-*from > limit-len(out) {
			fromLim = *from + limit - len(out)
			res = convertOutputExhausted
		}
		lim := trimToCompleteUTF8(b, *from, fromLim)
		if lim < fromLim && res == convertCompleted {
			res = convertInputIncomplete
		}
		out = append(out, b[*from:lim]...)
		*from = lim
		return out, res
	}
	// UTF-16.
	fromLim = *from + (fromLim-*from)&^1
	p := *from
	unit := func(i int) (lo, hi byte) {
		if e.kind == encLittle2 {
			return b[i], b[i+1]
		}
		return b[i+1], b[i]
	}
	for ; p < fromLim; p += 2 {
		lo, hi := unit(p)
		switch {
		case hi == 0 && lo < 0x80:
			if !room(1) {
				*from = p
				return out, convertOutputExhausted
			}
			out = append(out, lo)
		case hi < 0x08:
			if !room(2) {
				*from = p
				return out, convertOutputExhausted
			}
			out = append(out, lo>>6|hi<<2|0xC0, lo&0x3F|0x80)
		case hi >= 0xD8 && hi <= 0xDB:
			if !room(4) {
				*from = p
				return out, convertOutputExhausted
			}
			if fromLim-p < 4 {
				*from = p
				return out, convertInputIncomplete
			}
			plane := (int(hi&0x3)<<2 | int(lo>>6)&0x3) + 1
			lo2, hi2 := unit(p + 2)
			out = append(out, byte(plane>>2)|0xF0, (lo>>2)&0xF|byte(plane&0x3)<<4|0x80,
				(lo&0x3)<<4|(hi2&0x3)<<2|lo2>>6|0x80, lo2&0x3F|0x80)
			p += 2
		default:
			if !room(3) {
				*from = p
				return out, convertOutputExhausted
			}
			out = append(out, hi>>4|0xE0, (hi&0xF)<<2|lo>>6|0x80, lo&0x3F|0x80)
		}
	}
	*from = p
	if p < fromLim {
		return out, convertInputIncomplete
	}
	return out, convertCompleted
}

// trimToCompleteUTF8 is _INTERNAL_trim_to_complete_utf8_characters: it moves
// lim back so that b[from:lim] does not end inside a UTF-8 sequence.
func trimToCompleteUTF8(b []byte, from, lim int) int {
	walked := 0
	for ; lim > from; lim, walked = lim-1, walked+1 {
		prev := b[lim-1]
		switch {
		case prev&0xF8 == 0xF0:
			if walked+1 >= 4 {
				return lim + 3
			}
			walked = 0
		case prev&0xF0 == 0xE0:
			if walked+1 >= 3 {
				return lim + 2
			}
			walked = 0
		case prev&0xE0 == 0xC0:
			if walked+1 >= 2 {
				return lim + 1
			}
			walked = 0
		case prev&0x80 == 0:
			return lim
		}
	}
	return lim
}

// position is expat's POSITION: a 0-based line and column.
type position struct {
	line, column int
}

// updatePosition advances pos over b[ptr:end], counting characters.
func (e *encoding) updatePosition(b []byte, ptr, end int, pos *position) {
	if e.kind == encInit {
		e.set.utf8.updatePosition(b, ptr, end, pos)
		return
	}
	mb := e.minBPC
	for end-ptr >= mb {
		switch e.byteType(b, ptr) {
		case btLead2:
			ptr += 2
			pos.column++
		case btLead3:
			ptr += 3
			pos.column++
		case btLead4:
			ptr += 4
			pos.column++
		case btLf:
			pos.column = 0
			pos.line++
			ptr += mb
		case btCr:
			pos.line++
			ptr += mb
			if end-ptr >= mb && e.byteType(b, ptr) == btLf {
				ptr += mb
			}
			pos.column = 0
		default:
			ptr += mb
			pos.column++
		}
	}
}
