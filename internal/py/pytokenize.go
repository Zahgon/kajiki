// Ported from CPython 3.12 (Lib/tokenize.py, Python/Python-tokenize.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// tokenize.generate_tokens over the lines of a string: the C tokenizer in the mode of the tokenize
// module, producing CPython's TokenInfo tuples lazily, and its errors as tokenize raises them.

package py

import (
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// TokenInfo is tokenize.TokenInfo: the token's type (a number of the token
// module) and text, where it starts and ends as (row, column) with columns
// counting characters, and the physical line it is on.
type TokenInfo struct {
	Type       int
	String     string
	Start, End [2]int
	Line       string
}

// The numbers of the token module for the tokens whose internal numbers
// differ from them.
var tokenModuleTypes = map[int]int{
	tFSTRINGSTART: 61, tFSTRINGMIDDLE: 62, tFSTRINGEND: 63, tCOMMENT: 64, tNL: 65, tERRORTOKEN: 66,
}

// TokenStream is the generator tokenize.generate_tokens(readline) returns
// for readline = io.StringIO(source).readline. It reads the source a line
// at a time, as far as the tokens taken from it need.
type TokenStream struct {
	t    *tokenizer
	done bool
	// The line cache of tokenizeriterobject.
	lastLine       string
	lastLineno     int
	byteColOffDiff int
}

// GenerateTokens is tokenize.generate_tokens(io.StringIO(source).readline).
func GenerateTokens(source string) *TokenStream {
	return &TokenStream{t: newReadlineTokenizer(source)}
}

// Next returns the next token; ok is false at the end of the tokens. A
// tokenizer error ends the stream: a SyntaxError is raised as
// tokenize.TokenError(msg, (lineno, offset)) with "unterminated
// triple-quoted string literal" messages read "EOF in multi-line string",
// an IndentationError or TabError as it is.
func (s *TokenStream) Next() (tok TokenInfo, ok bool, err error) {
	if s.done {
		return TokenInfo{}, false, nil
	}
	tok, ok, x := s.next()
	if x != nil {
		s.done = true
		ensureInit()
		return TokenInfo{}, false, toError(tokenizeError(x))
	}
	if !ok {
		s.done = true
	}
	return tok, ok, nil
}

// tokenizeError is the exception generate_tokens raises for an error of
// the tokenizer.
func tokenizeError(x *excV) *excV {
	if x.Class != "SyntaxError" {
		return x
	}
	msg, _ := unsub(x.field("msg")).(strV)
	text := string(msg)
	if strings.Contains(text, "unterminated triple-quoted string literal") {
		text = "EOF in multi-line string"
	}
	e := newExc("TokenError", []Value{strV(text), newTuple([]Value{x.field("lineno"), x.field("offset")})})
	e.cls = tokenErrorClass()
	return e
}

var (
	tokenErrorOnce sync.Once
	tokenErrorK    *classV
)

// tokenErrorClass is tokenize.TokenError, a subclass of Exception.
func tokenErrorClass() *classV {
	tokenErrorOnce.Do(func() {
		ns := newDict()
		ns.setStr("__module__", strV("tokenize"))
		ns.setStr("__qualname__", strV("TokenError"))
		k, err := newUserClass("TokenError", []*classV{classes["Exception"]}, ns)
		if err != nil {
			panic("tokenize.TokenError: " + err.Error())
		}
		tokenErrorK = k
	})
	return tokenErrorK
}

// next is tokenizeriter_next: the next TokenInfo, or the exception.
func (s *TokenStream) next() (TokenInfo, bool, *excV) {
	t := s.t
	var rt rawToken
	typ := t.next(&rt)
	if typ == tERRORTOKEN {
		if t.exc != nil {
			x := t.exc.exceptionIn("<string>")
			t.exc = nil
			return TokenInfo{}, false, x
		}
		return TokenInfo{}, false, s.tokenizerError()
	}
	str := ""
	if rt.start >= 0 && rt.end >= 0 {
		str = t.buf[rt.start:rt.end]
		if !utf8.ValidString(str) {
			// A carriage return skipped before a character's first byte
			// leaves a token that is not UTF-8.
			if _, err := codecsDecode([]byte(str), "utf-8", "strict"); err != nil {
				return TokenInfo{}, false, asExc(fromLeaf(err))
			}
		}
	}
	trailing := typ == tENDMARKER || typ == tDEDENT && t.done == eEOF
	isString := typ == tSTRING || typ == tFSTRINGMIDDLE
	lineStart := t.lineStart
	if isString {
		lineStart = t.multiLineStart
	}
	line := ""
	lineChanged := true
	if !trailing {
		size := t.inp - lineStart
		if size >= 1 && t.implicitNewline {
			size--
		}
		if t.lineno != s.lastLineno {
			s.lastLine = t.buf[lineStart : lineStart+size]
			s.byteColOffDiff = 0
		} else {
			lineChanged = false
		}
		line = s.lastLine
	}
	lineno := t.lineno
	if isString {
		lineno = t.firstLineno
	}
	endLineno := t.lineno
	s.lastLineno = lineno

	col, endCol, byteOffset := -1, -1, -1
	if rt.start >= 0 && rt.start >= lineStart {
		byteOffset = rt.start - lineStart
		if lineChanged {
			col = charCountUTF8(line, 0, byteOffset)
			s.byteColOffDiff = byteOffset - col
		} else {
			col = byteOffset - s.byteColOffDiff
		}
	}
	if rt.end >= 0 && rt.end >= t.lineStart {
		endByte := rt.end - t.lineStart
		if lineno == endLineno {
			n := charCountUTF8(line, byteOffset, endByte)
			endCol = col + n
			s.byteColOffDiff += rt.end - rt.start - n
		} else {
			endCol = byteToCharOffset(t.buf[t.lineStart:], endByte)
			s.byteColOffDiff += endByte - endCol
		}
	}
	if trailing {
		lineno++
		endLineno = lineno
		col, endCol = 0, 0
	}
	switch {
	case typ > tDEDENT && typ < tOP:
		typ = tOP
	case typ == tASYNC || typ == tAWAIT:
		typ = tNAME
	case typ == tNEWLINE:
		if !t.implicitNewline {
			str = "\n"
			if t.buf[t.start] == '\r' {
				str = "\r\n"
			}
		}
		endCol++
	case typ == tNL:
		if t.implicitNewline {
			str = ""
		}
	}
	if typ == tENDMARKER {
		s.done = true
	}
	if mt, ok := tokenModuleTypes[typ]; ok {
		typ = mt
	}
	return TokenInfo{Type: typ, String: str, Start: [2]int{lineno, col}, End: [2]int{endLineno, endCol},
		Line: line}, true, nil
}

// charCountUTF8 is _PyPegen_byte_offset_to_character_offset_line: the
// number of characters in line from byte from to byte to, the bytes past
// the end of line counting one each.
func charCountUTF8(line string, from, to int) int {
	n := 0
	for from = max(from, 0); from < to; n++ {
		if from >= len(line) {
			from++
			continue
		}
		_, size := utf8.DecodeRuneInString(line[from:])
		from += size
	}
	return n
}

// tokenizerError is _tokenizer_error of the tokenize module: the exception
// for a failure the tokenizer did not raise itself.
func (s *TokenStream) tokenizerError() *excV {
	t := s.t
	class, msg := "SyntaxError", ""
	switch t.done {
	case eEOF:
		x := newExc("SyntaxError", []Value{strV("unexpected EOF in multi-line statement")})
		x.setField("msg", strV("unexpected EOF in multi-line statement"))
		x.setField("filename", strV("<string>"))
		x.setField("lineno", mkInt(int64(t.lineno)))
		x.setField("offset", mkInt(int64(max(t.inp-t.bufStart, 0))))
		x.Message = "unexpected EOF in multi-line statement (<string>, line " + strconv.Itoa(t.lineno) + ")"
		return x
	case eDEDENT:
		class, msg = "IndentationError", "unindent does not match any outer indentation level"
	case eTABSPACE:
		class, msg = "TabError", "inconsistent use of tabs and spaces in indentation"
	case eTOODEEP:
		class, msg = "IndentationError", "too many levels of indentation"
	case eLINECONT:
		msg = "unexpected character after line continuation character"
	default:
		msg = "unknown tokenization error"
	}
	errorLine := t.buf[t.bufStart : t.inp-1]
	e := &syntaxErr{msg: msg, line: t.lineno, offset: byteToCharOffset(errorLine, t.inp-t.bufStart), text: errorLine,
		hasText: true}
	if class != "SyntaxError" {
		e.class = class
	}
	x := e.exceptionIn("<string>")
	// The tuple of _tokenizer_error has no end position.
	x.args = []Value{strV(msg), newTuple([]Value{strV("<string>"), mkInt(int64(e.line)), mkInt(int64(e.offset)),
		strV(errorLine), none, none})}
	x.setField("end_lineno", none)
	x.setField("end_offset", none)
	return x
}
