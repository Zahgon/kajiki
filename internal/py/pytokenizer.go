// Ported from CPython 3.12 (Parser/tokenizer.c, Parser/token.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The tokenizer half of ast.parse: a port of CPython's tokenizer for source
// given as a string, including the f-string modes of PEP 701. It runs lazily,
// one token per call, as the parser asks for them, so its errors and warnings
// come in the order CPython issues them. Positions follow CPython: lines
// count from 1 and columns are UTF-8 byte offsets within the line.

package py

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackrosenthal/kajiki-go/internal/pyuni"
)

// Token types, named as in CPython's Grammar/Tokens.
const (
	tENDMARKER = iota
	tNAME
	tNUMBER
	tSTRING
	tNEWLINE
	tINDENT
	tDEDENT
	tLPAR
	tRPAR
	tLSQB
	tRSQB
	tCOLON
	tCOMMA
	tSEMI
	tPLUS
	tMINUS
	tSTAR
	tSLASH
	tVBAR
	tAMPER
	tLESS
	tGREATER
	tEQUAL
	tDOT
	tPERCENT
	tLBRACE
	tRBRACE
	tEQEQUAL
	tNOTEQUAL
	tLESSEQUAL
	tGREATEREQUAL
	tTILDE
	tCIRCUMFLEX
	tLEFTSHIFT
	tRIGHTSHIFT
	tDOUBLESTAR
	tPLUSEQUAL
	tMINEQUAL
	tSTAREQUAL
	tSLASHEQUAL
	tPERCENTEQUAL
	tAMPEREQUAL
	tVBAREQUAL
	tCIRCUMFLEXEQUAL
	tLEFTSHIFTEQUAL
	tRIGHTSHIFTEQUAL
	tDOUBLESTAREQUAL
	tDOUBLESLASH
	tDOUBLESLASHEQUAL
	tAT
	tATEQUAL
	tRARROW
	tELLIPSIS
	tCOLONEQUAL
	tEXCLAMATION
	tOP
	tAWAIT
	tASYNC
	tFSTRINGSTART
	tFSTRINGMIDDLE
	tFSTRINGEND
	tCOMMENT
	tNL
	tERRORTOKEN
)

// The tokenizer's states (tok->done), as in CPython's errcode.h.
const (
	eOK = iota
	eEOF
	eERROR
	eTABSPACE
	eTOODEEP
	eDEDENT
	eEOFS
	eEOLS
	eLINECONT
)

// CPython's limits on nesting.
const (
	tabSize         = 8
	maxIndent       = 100 // indentation levels
	maxLevel        = 200 // open brackets
	maxFStringLevel = 150 // nested f-strings
	maxExprNesting  = 3   // replacement fields nested in format specs
)

const eof = -1

// syntaxErr is a failure of compile(): CPython's message and exception class
// ("" for SyntaxError; IndentationError and TabError are its subclasses, and
// the parser also fails with MemoryError), with the location the SyntaxError
// reports: the lines and the 1-based character offsets of the start and the
// end, and the text of the line (hasText false for None).
type syntaxErr struct {
	msg                              string
	class                            string
	line, offset, endLine, endOffset int
	text                             string
	hasText                          bool
	// leaf is an error raised as it is rather than as a SyntaxError.
	leaf error
}

// syntax reports whether the error is a SyntaxError (or a subclass).
func (e *syntaxErr) syntax() bool {
	return e.leaf == nil && (e.class == "" || e.class == "IndentationError" || e.class == "TabError")
}

func (e *syntaxErr) Error() string { return e.msg }

// exceptionIn is the exception compile() raises for source compiled with the
// given file name. A SyntaxError's str() is "msg (file, line N)" with the base
// name of the file; without a text of its own, the error has the line of the
// file named, when it can be read, as PyErr_ProgramTextObject finds it.
func (e *syntaxErr) exceptionIn(filename string) *excV {
	class := e.class
	switch {
	case e.leaf != nil:
		if x, ok := fromLeaf(e.leaf).(*excV); ok {
			return x
		}
		return newPyErr("SystemError", e.leaf.Error())
	case !e.syntax():
		return newPyErr(class, e.msg)
	case class == "":
		class = "SyntaxError"
	}
	var text Value = none
	if e.hasText {
		text = strV(e.text)
	} else if line, ok := fileLine(filename, e.line); ok {
		text = strV(line)
	}
	info := []Value{strV(filename), mkInt(int64(e.line)), mkInt(int64(e.offset)), text, mkInt(int64(e.endLine)),
		mkInt(int64(e.endOffset))}
	x := &excV{Class: class, args: []Value{strV(e.msg), newTuple(info)}}
	x.setField("msg", strV(e.msg))
	for i, name := range []string{"filename", "lineno", "offset", "text", "end_lineno", "end_offset"} {
		x.setField(name, info[i])
	}
	x.Message = e.msg + " (" + basename(filename) + ", line " + strconv.Itoa(e.line) + ")"
	return x
}

// fileLine is line lineno (from 1) of the file, with its newline, as
// linecache reads it; ok is false when the file or the line is missing.
func fileLine(filename string, lineno int) (string, bool) {
	if filename == "" || strings.HasPrefix(filename, "<") || lineno < 1 {
		return "", false
	}
	// linecache.updatecache gives no lines for a file it cannot read
	// (`except OSError: return []`), which is what a read error is.
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", false
	}
	lines := strings.SplitAfter(string(data), "\n")
	if lineno > len(lines) || lines[lineno-1] == "" {
		return "", false
	}
	return lines[lineno-1], true
}

// tokMode is one entry of the tokenizer's mode stack (tokenizer_mode): the
// regular mode at the bottom, one f-string mode per f-string being read.
type tokMode struct {
	fstring bool // TOK_FSTRING_MODE rather than TOK_REGULAR_MODE

	curlyDepth     int // curly_bracket_depth
	exprStartDepth int // curly_bracket_expr_start_depth

	quote          byte
	quoteSize      int
	raw            bool
	start          int // offset of the f-string's prefix
	multiLineStart int // offset of the line the f-string starts on
	lineStart      int // the line number the f-string starts on

	// The text of the replacement field being read, for the debug text of
	// f"{expr=}": it runs from exprStart to exprEnd (-1 while open).
	exprStart, exprEnd int
	debug              bool
	inFormatSpec       bool
}

// rawToken is a token as the tokenizer returns it (struct token).
type rawToken struct {
	level                          int
	lineno, col, endLineno, endCol int
	start, end                     int // the token's text, or -1
	meta                           *string
}

// tokenizer is CPython's tok_state for string input.
type tokenizer struct {
	buf       string // the source, line ends translated to \n
	cur, inp  int    // next character; end of the lines read so far
	start     int    // start of the token being read, or -1
	lineStart int
	// bufStart is where tok->buf points: the start of the line read last
	// outside a token, the line a token spanning several lines starts on.
	bufStart int
	// multiLineStart is the offset of the line a string literal starts on.
	multiLineStart int

	lineno, firstLineno          int
	colOffset, startingColOffset int

	done int
	// exc is the SyntaxError the tokenizer raised, until the parser takes it.
	exc *syntaxErr

	atbol       bool
	pendin      int
	indent      int
	indstack    [maxIndent]int
	altindstack [maxIndent]int

	level            int
	parenstack       [maxLevel]byte
	parenlinenostack [maxLevel]int
	parencolstack    [maxLevel]int

	modes   [maxFStringLevel]tokMode
	modeIdx int

	// filename is the name of the source, for the warnings issued.
	filename string

	// extraTokens is tok_extra_tokens, the mode of the tokenize module:
	// comments and blank lines are tokens, and brackets need not match.
	extraTokens bool
	// implicitNewline is set while the line read last is the final one and
	// had no newline, one being supplied (implicit_newline).
	implicitNewline bool
	// syntheticNewline is the offset of that supplied newline, or -1.
	syntheticNewline int
	// commentNewline is set after a comment on a line of its own, whose
	// newline is an NL token.
	commentNewline bool
}

// newTokenizer prepares src as _PyTokenizer_FromUTF8 does: \r\n and \r
// become \n, and for exec input a final \n is added if missing.
func newTokenizer(src, filename string, execInput bool) *tokenizer {
	s := strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	if execInput && s != "" && s[len(s)-1] != '\n' {
		s += "\n"
	}
	return &tokenizer{buf: s, start: -1, colOffset: -1, startingColOffset: -1, atbol: true, filename: filename,
		syntheticNewline: -1}
}

// newReadlineTokenizer is the tokenizer the tokenize module runs over the
// lines readline returns (_PyTokenizer_FromReadline with extra tokens): the
// source is read as it is, \r\n included, and a last line without a newline
// gets one.
func newReadlineTokenizer(src string) *tokenizer {
	t := &tokenizer{buf: src, start: -1, colOffset: -1, startingColOffset: -1, atbol: true, filename: "<string>",
		extraTokens: true, syntheticNewline: -1}
	if src != "" && src[len(src)-1] != '\n' {
		t.syntheticNewline = len(src)
		t.buf += "\n"
	}
	return t
}

func (t *tokenizer) mode() *tokMode { return &t.modes[t.modeIdx] }

func (t *tokenizer) insideFString() bool { return t.modeIdx > 0 }

// nextc returns the next character, reading the next line when the current
// one is used up (tok_nextc with tok_underflow_string), or eof.
func (t *tokenizer) nextc() int {
	for {
		if t.cur != t.inp {
			t.colOffset++
			c := t.buf[t.cur]
			t.cur++
			return int(c)
		}
		if t.done != eOK {
			return eof
		}
		end := strings.IndexByte(t.buf[t.inp:], '\n')
		if end >= 0 {
			end += t.inp + 1
		} else {
			end = len(t.buf)
			if end == t.inp {
				t.done = eEOF
				t.cur = t.inp
				if t.extraTokens && t.start == -1 && !t.insideFString() {
					// The readline tokenizer empties its buffer before
					// asking for a line.
					t.bufStart = t.cur
				}
				return eof
			}
		}
		if t.start == -1 && !(t.extraTokens && t.insideFString()) {
			t.bufStart = t.cur
		}
		t.lineStart = t.cur
		t.lineno++
		t.colOffset = 0
		t.inp = end
		t.implicitNewline = end-1 == t.syntheticNewline
	}
}

// backup puts c back (tok_backup).
func (t *tokenizer) backup(c int) {
	if c != eof {
		t.cur--
		t.colOffset--
	}
}

// syntaxError raises a SyntaxError at the current position (syntaxerror):
// its offset counts the characters of the line read so far.
func (t *tokenizer) syntaxError(format string, args ...any) int {
	return t.syntaxErrorRange(-1, -1, format, args...)
}

// syntaxErrorRange raises a SyntaxError on the current line
// (_syntaxerror_range): col and endCol are the offsets it reports, -1 for
// the current position, and it stops the tokenizer. It returns ERRORTOKEN.
func (t *tokenizer) syntaxErrorRange(col, endCol int, format string, args ...any) int {
	if t.done == eERROR {
		return tERRORTOKEN
	}
	if col == -1 {
		col = utf8.RuneCountInString(t.buf[t.lineStart:t.cur])
	}
	if endCol == -1 {
		endCol = col
	}
	end := strings.IndexByte(t.buf[t.lineStart:], '\n')
	if end < 0 {
		end = len(t.buf) - t.lineStart
	}
	t.exc = &syntaxErr{msg: fmt.Sprintf(format, args...), line: t.lineno, offset: col, endLine: t.lineno,
		endOffset: endCol, text: t.buf[t.lineStart : t.lineStart+end], hasText: true}
	t.done = eERROR
	return tERRORTOKEN
}

// indentError is a tab/space inconsistency (E_TABSPACE).
func (t *tokenizer) indentError() int {
	t.done = eTABSPACE
	t.cur = t.inp
	return tERRORTOKEN
}

func isPotentialIdentifierStart(c int) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 128
}

func isPotentialIdentifierChar(c int) bool {
	return isPotentialIdentifierStart(c) || c >= '0' && c <= '9'
}

// lookahead reports whether the next characters are word followed by a
// character that cannot continue a name, reading nothing.
func (t *tokenizer) lookahead(word string) bool {
	i := 0
	res := false
	for {
		c := t.nextc()
		if i == len(word) {
			res = !isPotentialIdentifierChar(c)
		} else if c == int(word[i]) {
			i++
			continue
		}
		t.backup(c)
		for i > 0 {
			i--
			t.backup(int(word[i]))
		}
		return res
	}
}

// verifyEndOfNumber checks the character c after a number literal: a
// keyword that may follow a number in valid code ("1if x else y") is
// accepted with a SyntaxWarning, any other name character is an error.
func (t *tokenizer) verifyEndOfNumber(c int, kind string) bool {
	if t.extraTokens {
		// The tokenize module is more liberal than the parser.
		return true
	}
	r := false
	switch c {
	case 'a':
		r = t.lookahead("nd")
	case 'e':
		r = t.lookahead("lse")
	case 'f':
		r = t.lookahead("or")
	case 'i':
		c2 := t.nextc()
		r = c2 == 'f' || c2 == 'n' || c2 == 's'
		t.backup(c2)
	case 'o':
		r = t.lookahead("r")
	case 'n':
		r = t.lookahead("ot")
	}
	if r {
		t.backup(c)
		warnExplicit("SyntaxWarning", "invalid "+kind+" literal", t.filename, t.lineno)
		t.nextc()
	} else if c < 128 && isPotentialIdentifierChar(c) {
		t.backup(c)
		t.syntaxError("invalid %s literal", kind)
		return false
	}
	return true
}

// verifyIdentifier checks that the name just read follows PEP 3131.
func (t *tokenizer) verifyIdentifier() bool {
	if t.extraTokens {
		return true
	}
	s := t.buf[t.start:t.cur]
	for i, r := range s {
		ok := isIdentContinue(r)
		if i == 0 {
			ok = isIdentStart(r)
		}
		if ok {
			continue
		}
		// The error points just past the first invalid character.
		if end := i + len(string(r)); end < len(s) {
			t.cur = t.start + end
		}
		if pyuni.IsPrintable(r) {
			t.syntaxError("invalid character '%c' (U+%04X)", r, r)
		} else {
			t.syntaxError("invalid non-printable character U+%04X", r)
		}
		return false
	}
	return true
}

// isIdentStart reports whether r may start a name: '_' or XID_Start.
func isIdentStart(r rune) bool { return r == '_' || isXIDStart(r) }

// isIdentContinue reports whether r may continue a name (XID_Continue).
func isIdentContinue(r rune) bool { return pyuni.IsIdentifierContinue(r) }

// decimalTail reads digits with single underscores between them and
// returns the character after them, or 0 after an error.
func (t *tokenizer) decimalTail() int {
	for {
		c := t.nextc()
		for isASCIIDigit(rune(c)) {
			c = t.nextc()
		}
		if c != '_' {
			return c
		}
		c = t.nextc()
		if !isASCIIDigit(rune(c)) {
			t.backup(c)
			t.syntaxError("invalid decimal literal")
			return 0
		}
	}
}

// continuationLine reads the newline after a backslash and returns the
// first character of the next line (put back), or -1 after an error.
func (t *tokenizer) continuationLine() int {
	c := t.nextc()
	if c == '\r' {
		c = t.nextc()
	}
	if c != '\n' {
		t.done = eLINECONT
		return -1
	}
	c = t.nextc()
	if c == eof {
		t.done = eEOF
		t.cur = t.inp
		return -1
	}
	t.backup(c)
	return c
}

// next returns the next token (_PyTokenizer_Get).
func (t *tokenizer) next(tok *rawToken) int {
	*tok = rawToken{start: -1, end: -1}
	if t.mode().fstring {
		return t.fstringMode(tok)
	}
	return t.normalMode(tok)
}

// makeToken fills tok (token_setup): string literal parts start on the line
// their literal starts on.
func (t *tokenizer) makeToken(tok *rawToken, typ, start, end int) int {
	tok.level = t.level
	if typ == tSTRING || typ == tFSTRINGMIDDLE {
		tok.lineno = t.firstLineno
	} else {
		tok.lineno = t.lineno
	}
	tok.endLineno = t.lineno
	tok.col, tok.endCol = -1, -1
	tok.start, tok.end = start, end
	if start >= 0 {
		tok.col = t.startingColOffset
		tok.endCol = t.colOffset
	}
	return typ
}

func (t *tokenizer) errorToken(tok *rawToken) int { return t.makeToken(tok, tERRORTOKEN, -1, -1) }

// normalMode is tok_get_normal_mode.
func (t *tokenizer) normalMode(tok *rawToken) int {
	cur := t.mode()
nextline:
	t.start = -1
	t.startingColOffset = -1
	blankline := false

	// Get the indentation level.
	if t.atbol {
		col, altcol, contLineCol := 0, 0, 0
		t.atbol = false
		var c int
		for {
			c = t.nextc()
			switch c {
			case ' ':
				col++
				altcol++
				continue
			case '\t':
				col = (col/tabSize + 1) * tabSize
				altcol++
				continue
			case '\f':
				col, altcol = 0, 0
				continue
			case '\\':
				// Indentation cannot be split over physical lines: the
				// first backslash after whitespace fixes the level.
				if contLineCol == 0 {
					contLineCol = col
				}
				if c = t.continuationLine(); c == -1 {
					return t.errorToken(tok)
				}
				continue
			}
			break
		}
		t.backup(c)
		if c == '#' || c == '\n' || c == '\r' {
			// Lines with only whitespace and comments do not affect
			// the indentation.
			blankline = true
		}
		if !blankline && t.level == 0 {
			if contLineCol != 0 {
				col, altcol = contLineCol, contLineCol
			}
			switch {
			case col == t.indstack[t.indent]:
				if altcol != t.altindstack[t.indent] {
					return t.makeToken(tok, t.indentError(), -1, -1)
				}
			case col > t.indstack[t.indent]:
				if t.indent+1 >= maxIndent {
					t.done = eTOODEEP
					t.cur = t.inp
					return t.errorToken(tok)
				}
				if altcol <= t.altindstack[t.indent] {
					return t.makeToken(tok, t.indentError(), -1, -1)
				}
				t.pendin++
				t.indent++
				t.indstack[t.indent] = col
				t.altindstack[t.indent] = altcol
			default:
				for t.indent > 0 && col < t.indstack[t.indent] {
					t.pendin--
					t.indent--
				}
				if col != t.indstack[t.indent] {
					t.done = eDEDENT
					t.cur = t.inp
					return t.errorToken(tok)
				}
				if altcol != t.altindstack[t.indent] {
					return t.makeToken(tok, t.indentError(), -1, -1)
				}
			}
		}
	}

	t.start = t.cur
	t.startingColOffset = t.colOffset

	// Return pending indents and dedents.
	if t.pendin != 0 {
		if t.pendin < 0 {
			t.pendin++
			if t.extraTokens {
				return t.makeToken(tok, tDEDENT, t.cur, t.cur)
			}
			return t.makeToken(tok, tDEDENT, -1, -1)
		}
		t.pendin--
		if t.extraTokens {
			return t.makeToken(tok, tINDENT, t.bufStart, t.cur)
		}
		return t.makeToken(tok, tINDENT, -1, -1)
	}

again:
	t.start = -1
	// Skip spaces.
	c := t.nextc()
	for c == ' ' || c == '\t' || c == '\f' {
		c = t.nextc()
	}

	// Set the start of the current token.
	t.start = t.cur - 1
	t.startingColOffset = t.colOffset - 1

	// Skip a comment; the tokenize module sees it as a token.
	if c == '#' {
		for c != eof && c != '\n' && c != '\r' {
			c = t.nextc()
		}
		if t.extraTokens {
			t.backup(c)
			t.commentNewline = blankline
			return t.makeToken(tok, tCOMMENT, t.start, t.cur)
		}
	}

	if c == eof {
		if t.level != 0 {
			return t.errorToken(tok)
		}
		if t.done == eEOF {
			return t.makeToken(tok, tENDMARKER, -1, -1)
		}
		return t.errorToken(tok)
	}

	// An identifier, or the prefix of a string literal.
	if isPotentialIdentifierStart(c) {
		sawB, sawR, sawU, sawF := false, false, false, false
		for {
			switch {
			case !sawB && !sawU && !sawF && (c == 'b' || c == 'B'):
				sawB = true
			case !sawB && !sawU && !sawR && !sawF && (c == 'u' || c == 'U'):
				sawU = true
			case !sawR && !sawU && (c == 'r' || c == 'R'):
				sawR = true
			case !sawF && !sawB && !sawU && (c == 'f' || c == 'F'):
				sawF = true
			default:
				goto name
			}
			c = t.nextc()
			if c == '"' || c == '\'' {
				if sawF {
					return t.fstringStart(tok, c)
				}
				return t.stringLiteral(tok, c)
			}
		}
	name:
		nonascii := false
		for isPotentialIdentifierChar(c) {
			if c >= 128 {
				nonascii = true
			}
			c = t.nextc()
		}
		t.backup(c)
		if nonascii && !t.verifyIdentifier() {
			return t.errorToken(tok)
		}
		switch t.buf[t.start:t.cur] {
		case "async":
			return t.makeToken(tok, tASYNC, t.start, t.cur)
		case "await":
			return t.makeToken(tok, tAWAIT, t.start, t.cur)
		}
		return t.makeToken(tok, tNAME, t.start, t.cur)
	}

	if c == '\r' {
		c = t.nextc()
	}

	// A newline.
	if c == '\n' {
		t.atbol = true
		if blankline || t.level > 0 {
			if t.extraTokens {
				t.commentNewline = false
				return t.makeToken(tok, tNL, t.start, t.cur)
			}
			goto nextline
		}
		if t.commentNewline && t.extraTokens {
			t.commentNewline = false
			return t.makeToken(tok, tNL, t.start, t.cur)
		}
		return t.makeToken(tok, tNEWLINE, t.start, t.cur-1)
	}

	// A period, or a number starting with one.
	if c == '.' {
		c = t.nextc()
		if isASCIIDigit(rune(c)) {
			return t.numberToken(tok, t.decimalRest(c, true))
		} else if c == '.' {
			c = t.nextc()
			if c == '.' {
				return t.makeToken(tok, tELLIPSIS, t.start, t.cur)
			}
			t.backup(c)
			t.backup('.')
		} else {
			t.backup(c)
		}
		return t.makeToken(tok, tDOT, t.start, t.cur)
	}

	if isASCIIDigit(rune(c)) {
		return t.numberToken(tok, t.number(c))
	}

	if c == '"' || c == '\'' {
		return t.stringLiteral(tok, c)
	}

	// A line continuation.
	if c == '\\' {
		if t.continuationLine() == -1 {
			return t.errorToken(tok)
		}
		goto again
	}

	// Punctuation that ends or splits a replacement field of an f-string.
	if (c == ':' || c == '}' || c == '!' || c == '{') && t.insideFString() && cur.exprStartDepth >= 0 {
		// The '{' case runs before curly_bracket_depth is incremented.
		cursor := cur.curlyDepth
		if c != '{' {
			cursor--
		}
		cursorValid := cursor == 0 || cursor == 1 && (cur.debug || cur.inFormatSpec)
		if cursorValid {
			t.updateFStringExpr(c)
			if c != '{' {
				t.setFStringExpr(tok, c)
			}
		}
		if c == ':' && cursor == cur.exprStartDepth {
			cur.fstring = true
			cur.inFormatSpec = true
			return t.makeToken(tok, tCOLON, t.start, t.cur)
		}
	}

	// Two- and three-character operators.
	c2 := t.nextc()
	if typ := twoChars(c, c2); typ != tOP {
		c3 := t.nextc()
		if typ3 := threeChars(c, c2, c3); typ3 != tOP {
			typ = typ3
		} else {
			t.backup(c3)
		}
		return t.makeToken(tok, typ, t.start, t.cur)
	}
	t.backup(c2)

	// Keep track of bracket nesting.
	switch c {
	case '(', '[', '{':
		if t.level >= maxLevel {
			return t.makeToken(tok, t.syntaxError("too many nested parentheses"), -1, -1)
		}
		t.parenstack[t.level] = byte(c)
		t.parenlinenostack[t.level] = t.lineno
		t.parencolstack[t.level] = t.start - t.lineStart
		t.level++
		if t.insideFString() {
			cur.curlyDepth++
		}
	case ')', ']', '}':
		if t.insideFString() && cur.curlyDepth == 0 && c == '}' {
			return t.makeToken(tok, t.syntaxError("f-string: single '}' is not allowed"), -1, -1)
		}
		if !t.extraTokens && t.level == 0 {
			return t.makeToken(tok, t.syntaxError("unmatched '%c'", c), -1, -1)
		}
		if t.level > 0 {
			t.level--
			opening := int(t.parenstack[t.level])
			if !t.extraTokens && closing[opening] != byte(c) {
				// A bracket closing the '{' of a replacement field is
				// unmatched rather than mismatched.
				if t.insideFString() && opening == '{' && cur.curlyDepth-1 == cur.exprStartDepth {
					return t.makeToken(tok, t.syntaxError("f-string: unmatched '%c'", c), -1, -1)
				}
				if t.parenlinenostack[t.level] != t.lineno {
					return t.makeToken(tok, t.syntaxError("closing parenthesis '%c' does not match opening parenthesis '%c' on line %d",
						c, opening, t.parenlinenostack[t.level]), -1, -1)
				}
				return t.makeToken(tok, t.syntaxError("closing parenthesis '%c' does not match opening parenthesis '%c'",
					c, opening), -1, -1)
			}
		}
		if t.insideFString() {
			cur.curlyDepth--
			if cur.curlyDepth < 0 {
				return t.makeToken(tok, t.syntaxError("f-string: unmatched '%c'", c), -1, -1)
			}
			if c == '}' && cur.curlyDepth == cur.exprStartDepth {
				cur.exprStartDepth--
				cur.fstring = true
				cur.inFormatSpec = false
				cur.debug = false
			}
		}
	}

	if !pyuni.IsPrintable(rune(c)) {
		return t.makeToken(tok, t.syntaxError("invalid non-printable character U+%04X", c), -1, -1)
	}

	if c == '=' && cur.exprStartDepth >= 0 {
		cur.debug = true
	}

	return t.makeToken(tok, oneChar(c), t.start, t.cur)
}

// closing is the closing bracket of each opening one.
var closing = map[int]byte{'(': ')', '[': ']', '{': '}'}

// numberToken finishes a number literal: typ is NUMBER or ERRORTOKEN.
func (t *tokenizer) numberToken(tok *rawToken, typ int) int {
	if typ == tERRORTOKEN {
		return t.errorToken(tok)
	}
	return t.makeToken(tok, tNUMBER, t.start, t.cur)
}

// number reads a number literal whose first digit c has been read. It
// returns NUMBER, with the character after the literal put back, or
// ERRORTOKEN.
func (t *tokenizer) number(c int) int {
	if c != '0' {
		c = t.decimalTail()
		if c == 0 {
			return tERRORTOKEN
		}
		if c == '.' {
			return t.decimalRest(t.nextc(), true)
		}
		return t.decimalRest(c, false)
	}
	c = t.nextc()
	switch c {
	case 'x', 'X':
		c = t.nextc()
		for {
			if c == '_' {
				c = t.nextc()
			}
			if !isHexDigit(rune(c)) {
				t.backup(c)
				return t.syntaxError("invalid hexadecimal literal")
			}
			for c = t.nextc(); isHexDigit(rune(c)); c = t.nextc() {
			}
			if c != '_' {
				break
			}
		}
		if !t.verifyEndOfNumber(c, "hexadecimal") {
			return tERRORTOKEN
		}
	case 'o', 'O', 'b', 'B':
		kind, isBaseDigit := "octal", func(c int) bool { return c >= '0' && c <= '7' }
		if c == 'b' || c == 'B' {
			kind, isBaseDigit = "binary", func(c int) bool { return c == '0' || c == '1' }
		}
		c = t.nextc()
		for {
			if c == '_' {
				c = t.nextc()
			}
			if !isBaseDigit(c) {
				if isASCIIDigit(rune(c)) {
					return t.syntaxError("invalid digit '%c' in %s literal", c, kind)
				}
				t.backup(c)
				return t.syntaxError("invalid %s literal", kind)
			}
			for c = t.nextc(); isBaseDigit(c); c = t.nextc() {
			}
			if c != '_' {
				break
			}
		}
		if isASCIIDigit(rune(c)) {
			return t.syntaxError("invalid digit '%c' in %s literal", c, kind)
		}
		if !t.verifyEndOfNumber(c, kind) {
			return tERRORTOKEN
		}
	default:
		// Maybe an old-style octal literal; "0" alone is allowed.
		nonzero := false
		for {
			if c == '_' {
				c = t.nextc()
				if !isASCIIDigit(rune(c)) {
					t.backup(c)
					return t.syntaxError("invalid decimal literal")
				}
			}
			if c != '0' {
				break
			}
			c = t.nextc()
		}
		zerosEnd := t.cur
		if isASCIIDigit(rune(c)) {
			nonzero = true
			if c = t.decimalTail(); c == 0 {
				return tERRORTOKEN
			}
		}
		switch {
		case c == '.':
			return t.decimalRest(t.nextc(), true)
		case c == 'e' || c == 'E' || c == 'j' || c == 'J':
			return t.decimalRest(c, false)
		case nonzero && !t.extraTokens:
			t.backup(c)
			return t.syntaxErrorRange(t.start+1-t.lineStart, zerosEnd-t.lineStart,
				"leading zeros in decimal integer literals are not permitted; use an 0o prefix for octal integers")
		}
		if !t.verifyEndOfNumber(c, "decimal") {
			return tERRORTOKEN
		}
	}
	t.backup(c)
	return tNUMBER
}

// decimalRest reads the rest of a decimal literal from c: its fraction
// (when afterDot, c follows the '.'), exponent and imaginary suffix.
func (t *tokenizer) decimalRest(c int, afterDot bool) int {
	if afterDot && isASCIIDigit(rune(c)) {
		if c = t.decimalTail(); c == 0 {
			return tERRORTOKEN
		}
	}
	if c == 'e' || c == 'E' {
		e := c
		c = t.nextc()
		if c == '+' || c == '-' {
			c = t.nextc()
			if !isASCIIDigit(rune(c)) {
				t.backup(c)
				return t.syntaxError("invalid decimal literal")
			}
		} else if !isASCIIDigit(rune(c)) {
			t.backup(c)
			if !t.verifyEndOfNumber(e, "decimal") {
				return tERRORTOKEN
			}
			t.backup(e)
			return tNUMBER
		}
		if c = t.decimalTail(); c == 0 {
			return tERRORTOKEN
		}
	}
	if c == 'j' || c == 'J' {
		c = t.nextc()
		if !t.verifyEndOfNumber(c, "imaginary") {
			return tERRORTOKEN
		}
	} else if !t.verifyEndOfNumber(c, "decimal") {
		return tERRORTOKEN
	}
	t.backup(c)
	return tNUMBER
}

// stringLiteral reads a string or bytes literal whose opening quote has
// been read.
func (t *tokenizer) stringLiteral(tok *rawToken, quote int) int {
	quoteSize, endQuoteSize := 1, 0
	t.firstLineno = t.lineno
	t.multiLineStart = t.lineStart

	// Find the quote size and the start of the body.
	c := t.nextc()
	if c == quote {
		c = t.nextc()
		if c == quote {
			quoteSize = 3
		} else {
			endQuoteSize = 1 // an empty string
		}
	}
	if c != quote {
		t.backup(c)
	}

	for endQuoteSize != quoteSize {
		c = t.nextc()
		if c == eof || quoteSize == 1 && c == '\n' {
			// Report the error at the opening quote.
			t.cur = t.start + 1
			t.lineStart = t.multiLineStart
			detected := t.lineno
			t.lineno = t.firstLineno
			if t.insideFString() {
				// A quote matching the enclosing f-string's means a
				// replacement field is missing its '}'.
				if m := t.mode(); int(m.quote) == quote && m.quoteSize == quoteSize {
					return t.makeToken(tok, t.syntaxError("f-string: expecting '}'"), -1, -1)
				}
			}
			if quoteSize == 3 {
				t.syntaxError("unterminated triple-quoted string literal (detected at line %d)", detected)
				if c != '\n' {
					t.done = eEOFS
				}
				return t.errorToken(tok)
			}
			t.syntaxError("unterminated string literal (detected at line %d)", detected)
			if c != '\n' {
				t.done = eEOLS
			}
			return t.errorToken(tok)
		}
		if c == quote {
			endQuoteSize++
		} else {
			endQuoteSize = 0
			if c == '\\' {
				// The escaped character is skipped.
				if c = t.nextc(); c == '\r' {
					t.nextc()
				}
			}
		}
	}
	return t.makeToken(tok, tSTRING, t.start, t.cur)
}

// fstringStart reads the quotes opening an f-string and enters f-string
// mode.
func (t *tokenizer) fstringStart(tok *rawToken, quote int) int {
	quoteSize := 1
	t.firstLineno = t.lineno
	t.multiLineStart = t.lineStart

	afterQuote := t.nextc()
	if afterQuote == quote {
		afterAfterQuote := t.nextc()
		if afterAfterQuote == quote {
			quoteSize = 3
		} else {
			t.backup(afterAfterQuote)
			t.backup(afterQuote)
		}
	}
	if afterQuote != quote {
		t.backup(afterQuote)
	}

	if t.modeIdx+1 >= maxFStringLevel {
		return t.makeToken(tok, t.syntaxError("too many nested f-strings"), -1, -1)
	}
	t.modeIdx++
	m := t.mode()
	m.fstring = true
	m.quote = byte(quote)
	m.quoteSize = quoteSize
	m.start = t.start
	m.multiLineStart = t.lineStart
	m.lineStart = t.lineno
	m.exprStart, m.exprEnd = -1, -1
	m.debug = false
	switch t.buf[t.start] {
	case 'f', 'F':
		m.raw = t.buf[t.start+1] == 'r' || t.buf[t.start+1] == 'R'
	default:
		m.raw = true
	}
	m.curlyDepth = 0
	m.exprStartDepth = -1
	return t.makeToken(tok, tFSTRINGSTART, t.start, t.cur)
}

// updateFStringExpr records where the text of a replacement field starts
// ('{') and ends ('}', '!' or ':').
func (t *tokenizer) updateFStringExpr(c int) {
	m := t.mode()
	switch c {
	case '{':
		m.exprStart = t.cur
		m.exprEnd = -1
	case '}', '!':
		m.exprEnd = t.start
	case ':':
		if m.exprEnd == -1 {
			m.exprEnd = t.start
		}
	}
}

// setFStringExpr attaches the text of a debug replacement field
// (f"{expr=}"), without comments, to the token ending it.
func (t *tokenizer) setFStringExpr(tok *rawToken, c int) {
	m := t.mode()
	if !m.debug || tok.meta != nil || m.exprStart < 0 {
		return
	}
	text := t.buf[m.exprStart:m.exprEnd]
	if strings.IndexByte(text, '#') >= 0 {
		var b strings.Builder
		for i := 0; i < len(text); i++ {
			if text[i] != '#' {
				b.WriteByte(text[i])
				continue
			}
			for i < len(text) && text[i] != '\n' {
				i++
			}
			if i < len(text) {
				b.WriteByte('\n')
			}
		}
		text = b.String()
	}
	tok.meta = &text
}

// fstringMode is tok_get_fstring_mode: the literal text of an f-string up
// to a replacement field or the closing quotes.
func (t *tokenizer) fstringMode(tok *rawToken) int {
	cur := t.mode()
	endQuoteSize := 0
	unicodeEscape := false

	t.start = t.cur
	t.firstLineno = t.lineno
	t.startingColOffset = t.colOffset

	// A '{' starting a replacement field is read in the regular mode.
	startChar := t.nextc()
	if startChar == '{' {
		peek1 := t.nextc()
		t.backup(peek1)
		t.backup(startChar)
		if peek1 != '{' {
			cur.exprStartDepth++
			if cur.exprStartDepth >= maxExprNesting {
				return t.makeToken(tok, t.syntaxError("f-string: expressions nested too deeply"), -1, -1)
			}
			cur.fstring = false
			return t.normalMode(tok)
		}
	} else {
		t.backup(startChar)
	}

	// The closing quotes.
	for i := 0; i < cur.quoteSize; i++ {
		q := t.nextc()
		if q != int(cur.quote) {
			t.backup(q)
			goto middle
		}
	}
	cur.exprStart, cur.exprEnd = -1, -1
	t.modeIdx--
	return t.makeToken(tok, tFSTRINGEND, t.start, t.cur)

middle:
	t.multiLineStart = t.lineStart
	for endQuoteSize != cur.quoteSize {
		c := t.nextc()
		if t.done == eERROR {
			return t.errorToken(tok)
		}
		inFormatSpec := cur.inFormatSpec && cur.exprStartDepth >= 0

		if c == eof || cur.quoteSize == 1 && c == '\n' {
			// A newline ends a format spec, which returns to the
			// regular mode.
			if inFormatSpec && c == '\n' {
				t.backup(c)
				cur.fstring = false
				cur.inFormatSpec = false
				return t.makeToken(tok, tFSTRINGMIDDLE, t.start, t.cur)
			}
			// Report the error at the start of the f-string.
			t.cur = cur.start + 1
			t.lineStart = cur.multiLineStart
			detected := t.lineno
			t.lineno = cur.lineStart
			if cur.quoteSize == 3 {
				t.syntaxError("unterminated triple-quoted f-string literal (detected at line %d)", detected)
				if c != '\n' {
					t.done = eEOFS
				}
				return t.errorToken(tok)
			}
			return t.makeToken(tok, t.syntaxError("unterminated f-string literal (detected at line %d)", detected), -1, -1)
		}

		if c == int(cur.quote) {
			endQuoteSize++
			continue
		}
		endQuoteSize = 0

		switch c {
		case '{':
			t.updateFStringExpr(c)
			peek := t.nextc()
			if peek != '{' || inFormatSpec {
				t.backup(peek)
				t.backup(c)
				cur.exprStartDepth++
				if cur.exprStartDepth >= maxExprNesting {
					return t.makeToken(tok, t.syntaxError("f-string: expressions nested too deeply"), -1, -1)
				}
				cur.fstring = false
				cur.inFormatSpec = false
				return t.makeToken(tok, tFSTRINGMIDDLE, t.start, t.cur)
			}
			return t.makeToken(tok, tFSTRINGMIDDLE, t.start, t.cur-1)
		case '}':
			if unicodeEscape {
				return t.makeToken(tok, tFSTRINGMIDDLE, t.start, t.cur)
			}
			peek := t.nextc()
			// A format spec cannot use doubled braces, and the
			// tokenizer can only be in one past the top bracket level.
			if peek == '}' && !inFormatSpec && cur.curlyDepth == 0 {
				return t.makeToken(tok, tFSTRINGMIDDLE, t.start, t.cur-1)
			}
			t.backup(peek)
			t.backup(c)
			cur.fstring = false
			return t.makeToken(tok, tFSTRINGMIDDLE, t.start, t.cur)
		case '\\':
			peek := t.nextc()
			if peek == '\r' {
				peek = t.nextc()
			}
			// A backslash before a brace leaves the brace to the
			// loop.
			if peek == '{' || peek == '}' {
				if !cur.raw {
					warnExplicit("SyntaxWarning", fmt.Sprintf("invalid escape sequence '\\%c'", peek), t.filename,
						t.lineno)
				}
				t.backup(peek)
				continue
			}
			if !cur.raw && peek == 'N' {
				// A named unicode escape (\N{BULLET}).
				if peek = t.nextc(); peek == '{' {
					unicodeEscape = true
				} else {
					t.backup(peek)
				}
			}
		}
	}

	// Put the closing quotes back for the FSTRING_END that follows.
	for i := 0; i < cur.quoteSize; i++ {
		t.backup(int(cur.quote))
	}
	return t.makeToken(tok, tFSTRINGMIDDLE, t.start, t.cur)
}

// oneChar is _PyToken_OneChar.
func oneChar(c int) int {
	switch c {
	case '%':
		return tPERCENT
	case '&':
		return tAMPER
	case '(':
		return tLPAR
	case ')':
		return tRPAR
	case '*':
		return tSTAR
	case '+':
		return tPLUS
	case ',':
		return tCOMMA
	case '-':
		return tMINUS
	case '.':
		return tDOT
	case '/':
		return tSLASH
	case ':':
		return tCOLON
	case ';':
		return tSEMI
	case '<':
		return tLESS
	case '=':
		return tEQUAL
	case '>':
		return tGREATER
	case '@':
		return tAT
	case '[':
		return tLSQB
	case ']':
		return tRSQB
	case '^':
		return tCIRCUMFLEX
	case '{':
		return tLBRACE
	case '|':
		return tVBAR
	case '}':
		return tRBRACE
	case '~':
		return tTILDE
	case '!':
		return tEXCLAMATION
	}
	return tOP
}

// twoChars is _PyToken_TwoChars.
func twoChars(c1, c2 int) int {
	switch string([]byte{byte(c1), byte(c2)}) {
	case "!=", "<>":
		return tNOTEQUAL
	case "%=":
		return tPERCENTEQUAL
	case "&=":
		return tAMPEREQUAL
	case "**":
		return tDOUBLESTAR
	case "*=":
		return tSTAREQUAL
	case "+=":
		return tPLUSEQUAL
	case "-=":
		return tMINEQUAL
	case "->":
		return tRARROW
	case "//":
		return tDOUBLESLASH
	case "/=":
		return tSLASHEQUAL
	case ":=":
		return tCOLONEQUAL
	case "<<":
		return tLEFTSHIFT
	case "<=":
		return tLESSEQUAL
	case "==":
		return tEQEQUAL
	case ">=":
		return tGREATEREQUAL
	case ">>":
		return tRIGHTSHIFT
	case "@=":
		return tATEQUAL
	case "^=":
		return tCIRCUMFLEXEQUAL
	case "|=":
		return tVBAREQUAL
	}
	return tOP
}

// threeChars is _PyToken_ThreeChars.
func threeChars(c1, c2, c3 int) int {
	if c3 == eof {
		return tOP
	}
	switch string([]byte{byte(c1), byte(c2), byte(c3)}) {
	case "**=":
		return tDOUBLESTAREQUAL
	case "...":
		return tELLIPSIS
	case "//=":
		return tDOUBLESLASHEQUAL
	case "<<=":
		return tLEFTSHIFTEQUAL
	case ">>=":
		return tRIGHTSHIFTEQUAL
	}
	return tOP
}
