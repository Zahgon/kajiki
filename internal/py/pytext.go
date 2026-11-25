// Ported from CPython 3.12 (Objects/unicodeobject.c, Objects/unicodectype.c, Lib/posixpath.py) to
// Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The text side of str: lone surrogates in Go strings, code point access, the case mappings
// with the final sigma rule, repr() of str, bytes and float, and splitting on line breaks and
// whitespace. The character data comes from package pyuni.

package py

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackrosenthal/kajiki-go/internal/pyuni"
)

// isXIDStart is the XID_Start property: what may start an identifier, the
// underscore aside.
func isXIDStart(r rune) bool { return r != '_' && pyuni.IsIdentifierStart(r) }

// isASCII is str.isascii(); true for "".
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// isWordChar is what \w matches in a Python str pattern.
func isWordChar(r rune) bool { return r == '_' || pyuni.IsAlnum(r) }

// stripSpace is str.strip() with no argument: it removes the characters for
// which str.isspace() is true from both ends of s.
func stripSpace(s string) string { return strings.TrimFunc(s, pyuni.IsSpace) }

// isIdentifier is str.isidentifier(): XID_Start or '_' first, XID_Continue
// after, with no normalisation and no keyword check.
func isIdentifier(s string) bool {
	r, n := decodeRune(s)
	if n == 0 || r != '_' && !isXIDStart(r) {
		return false
	}
	for _, r := range strRunes(s[n:]) {
		if !pyuni.IsIdentifierContinue(r) {
			return false
		}
	}
	return true
}

// appendUpper appends str.upper() of r: the full mapping, so 'ß' is "SS".
func appendUpper(b []byte, r rune) []byte { return appendRunes(b, pyuni.UpperRune(r)) }

// appendLower appends str.lower() of r alone; appendLowerAt also applies the
// final sigma rule.
func appendLower(b []byte, r rune) []byte { return appendRunes(b, pyuni.LowerRune(r)) }

// appendTitle appends the full titlecase mapping of r, so 'ǆ' is 'ǅ'.
func appendTitle(b []byte, r rune) []byte { return appendRunes(b, pyuni.TitleRune(r)) }

// appendFold appends str.casefold() of r.
func appendFold(b []byte, r rune) []byte { return appendRunes(b, pyuni.CaseFoldRune(r)) }

// appendRunes appends the code points of a case mapping.
func appendRunes(b []byte, rs []rune) []byte {
	for _, r := range rs {
		b = appendRune(b, r)
	}
	return b
}

// appendLowerAt appends the lower mapping of rs[i], lowering U+03A3 to the
// final sigma U+03C2 in the Final_Sigma context as CPython's lower_ucs4 does.
func appendLowerAt(b []byte, rs []rune, i int) []byte {
	switch {
	case rs[i] != 'Σ':
		return appendLower(b, rs[i])
	case isFinalSigma(rs, i):
		return appendRune(b, 'ς')
	default:
		return appendRune(b, 'σ')
	}
}

// isFinalSigma reports whether rs[i] is in the Final_Sigma context: cased
// case-ignorable* Σ, not followed by case-ignorable* cased.
func isFinalSigma(rs []rune, i int) bool {
	j := i - 1
	for j >= 0 && pyuni.IsCaseIgnorable(rs[j]) {
		j--
	}
	if j < 0 || !pyuni.IsCased(rs[j]) {
		return false
	}
	j = i + 1
	for j < len(rs) && pyuni.IsCaseIgnorable(rs[j]) {
		j++
	}
	return j == len(rs) || !pyuni.IsCased(rs[j])
}

// strLower is str.lower(): the full lowercase mapping, so 'İ' becomes "i̇", with
// Σ lowered to ς at the end of a word.
func strLower(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	rs := strRunes(s)
	b := make([]byte, 0, len(s))
	for i := range rs {
		b = appendLowerAt(b, rs, i)
	}
	return string(b)
}

// Python strings may hold lone surrogates (chr(0xD800) is a valid str). Such
// a code point is kept in a Go string as its generalized UTF-8 encoding, the
// three bytes ED A0..BF 80..BF, which the functions below read and write.

// decodeRune is utf8.DecodeRuneInString that also decodes a lone surrogate.
func decodeRune(s string) (rune, int) {
	if isSurrogateAt(s, 0) {
		return 0xD000 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// isSurrogateAt reports whether s[i:] starts with an encoded lone surrogate.
func isSurrogateAt(s string, i int) bool {
	return i+3 <= len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2] >= 0x80 && s[i+2] <= 0xBF
}

// indexSurrogate returns the byte offset of the first lone surrogate in s,
// or -1.
func indexSurrogate(s string) int {
	for i := 0; i < len(s); i++ {
		if isSurrogateAt(s, i) {
			return i
		}
	}
	return -1
}

// appendRune is utf8.AppendRune that also encodes a lone surrogate.
func appendRune(b []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(b, 0xED, byte(0x80|(r>>6)&0x3F), byte(0x80|r&0x3F))
	}
	return utf8.AppendRune(b, r)
}

// strPrefix is s[:n] for a str: s cut to its first n code points, as the
// precision of "%.200R" cuts a repr.
func strPrefix(s string, n int) string {
	for i := 0; i < len(s); n-- {
		if n == 0 {
			return s[:i]
		}
		_, size := decodeRune(s[i:])
		i += size
	}
	return s
}

// strLen is len(s) for a str: its number of code points, lone surrogates
// included.
func strLen(s string) int {
	n := 0
	for i := 0; i < len(s); n++ {
		_, size := decodeRune(s[i:])
		i += size
	}
	return n
}

// strRunes is list(s): the code points of s, lone surrogates included.
func strRunes(s string) []rune {
	out := make([]rune, 0, len(s))
	for len(s) > 0 {
		r, n := decodeRune(s)
		out = append(out, r)
		s = s[n:]
	}
	return out
}

// runesStr joins code points into a string, the inverse of strRunes.
func runesStr(rs []rune) string {
	b := make([]byte, 0, len(rs))
	for _, r := range rs {
		b = appendRune(b, r)
	}
	return string(b)
}

// strRepr is repr(s) for a str.
func strRepr(s string) string {
	quote := reprQuote(s)
	b := []byte{quote}
	for len(s) > 0 {
		r, n := decodeRune(s)
		s = s[n:]
		switch {
		case isReprSpecial(r, quote):
			b = appendReprSpecial(b, r)
		case r >= 0x20 && r < 0x7F:
			b = append(b, byte(r))
		case r > 0x7F && pyuni.IsPrintable(r):
			b = appendRune(b, r)
		default:
			b = appendBackslashEscape(b, r)
		}
	}
	return string(append(b, quote))
}

// bytesRepr is repr(b) for a bytes object holding the bytes of s.
func bytesRepr(s string) string {
	quote := reprQuote(s)
	b := []byte{'b', quote}
	for i := 0; i < len(s); i++ {
		c := rune(s[i])
		switch {
		case isReprSpecial(c, quote):
			b = appendReprSpecial(b, c)
		case c >= 0x20 && c < 0x7F:
			b = append(b, byte(c))
		default:
			b = appendBackslashEscape(b, c)
		}
	}
	return string(append(b, quote))
}

// reprQuote is the quote repr() puts around s: a double quote when s holds a
// single quote and no double quote, else a single quote.
func reprQuote(s string) byte {
	if strings.IndexByte(s, '\'') >= 0 && strings.IndexByte(s, '"') < 0 {
		return '"'
	}
	return '\''
}

// isReprSpecial reports whether repr() writes r as a backslash and a letter
// (or the character itself) inside the given quotes.
func isReprSpecial(r rune, quote byte) bool {
	return r == rune(quote) || r == '\\' || r == '\t' || r == '\n' || r == '\r'
}

func appendReprSpecial(b []byte, r rune) []byte {
	switch r {
	case '\t':
		return append(b, '\\', 't')
	case '\n':
		return append(b, '\\', 'n')
	case '\r':
		return append(b, '\\', 'r')
	}
	return append(b, '\\', byte(r))
}

// appendBackslashEscape appends the backslash escape of r that repr(),
// ascii() and the backslashreplace error handler write: \xhh up to U+00FF,
// \uhhhh up to U+FFFF and \Uhhhhhhhh above, in lowercase hex.
func appendBackslashEscape(b []byte, r rune) []byte {
	letter, width := byte('x'), 2
	switch {
	case r > 0xFFFF:
		letter, width = 'U', 8
	case r > 0xFF:
		letter, width = 'u', 4
	}
	h := strconv.FormatInt(int64(r), 16)
	b = append(b, '\\', letter)
	for i := len(h); i < width; i++ {
		b = append(b, '0')
	}
	return append(b, h...)
}

// floatRepr is repr() of a float: the shortest digits that round-trip, in
// fixed notation for decimal exponents from -4 to 16 and in exponent notation
// otherwise.
func floatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	e := strconv.FormatFloat(f, 'e', -1, 64)
	sign := ""
	if e[0] == '-' {
		sign, e = "-", e[1:]
	}
	// e is d[.ddd]e±XX: the mantissa digits and a signed decimal exponent.
	mant, expText, _ := strings.Cut(e, "e")
	exp := 0
	for _, c := range expText[1:] {
		exp = exp*10 + int(c-'0')
	}
	if expText[0] == '-' {
		exp = -exp
	}
	digits := strings.Replace(mant, ".", "", 1)
	decpt := exp + 1
	if decpt > -4 && decpt <= 16 {
		switch {
		case decpt <= 0:
			return sign + "0." + strings.Repeat("0", -decpt) + digits
		case decpt >= len(digits):
			return sign + digits + strings.Repeat("0", decpt-len(digits)) + ".0"
		default:
			return sign + digits[:decpt] + "." + digits[decpt:]
		}
	}
	out := digits[:1]
	if len(digits) > 1 {
		out += "." + digits[1:]
	}
	expSign := "+"
	if exp < 0 {
		expSign, exp = "-", -exp
	}
	expDigits := strconv.Itoa(exp)
	if len(expDigits) < 2 {
		expDigits = "0" + expDigits
	}
	return sign + out + "e" + expSign + expDigits
}

// splitLines is str.splitlines(keepends): it breaks at every character
// pyuni.IsLineBreak accepts, reading "\r\n" as one break, and yields no empty last
// line for a trailing break.
func splitLines(s string, keepends bool) []string {
	out := []string{}
	for i, j := 0, 0; i < len(s); j = i {
		var r rune
		n := 0
		for i < len(s) {
			r, n = utf8.DecodeRuneInString(s[i:])
			if pyuni.IsLineBreak(r) {
				break
			}
			i += n
		}
		eol := i
		if i < len(s) {
			if r == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i += 2
			} else {
				i += n
			}
			if keepends {
				eol = i
			}
		}
		out = append(out, s[j:eol])
	}
	return out
}

// skipSpace returns the offset of the first character at or after i whose
// str.isspace() equals want, or len(s).
func skipSpace(s string, i int, want bool) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if pyuni.IsSpace(r) == want {
			break
		}
		i += n
	}
	return i
}

// skipSpaceBack is skipSpace from the right: it returns the end of the last
// character before end whose str.isspace() equals want, or 0.
func skipSpaceBack(s string, end int, want bool) int {
	for end > 0 {
		r, n := utf8.DecodeLastRuneInString(s[:end])
		if pyuni.IsSpace(r) == want {
			break
		}
		end -= n
	}
	return end
}

// splitWhitespace is str.split(None, maxsplit) (CPython's split_whitespace):
// once maxsplit splits are made, the rest, with leading whitespace skipped,
// is the last item. A negative maxsplit splits everywhere.
func splitWhitespace(s string, maxsplit int) []string {
	out := []string{}
	i := 0
	for ; maxsplit != 0; maxsplit-- {
		i = skipSpace(s, i, false)
		if i == len(s) {
			break
		}
		j := i
		i = skipSpace(s, i, true)
		out = append(out, s[j:i])
	}
	if i < len(s) {
		if i = skipSpace(s, i, false); i < len(s) {
			out = append(out, s[i:])
		}
	}
	return out
}

// rsplitWhitespace is str.rsplit(None, maxsplit) (CPython's
// rsplit_whitespace).
func rsplitWhitespace(s string, maxsplit int) []string {
	out := []string{}
	i := len(s)
	for ; maxsplit != 0; maxsplit-- {
		i = skipSpaceBack(s, i, false)
		if i == 0 {
			break
		}
		j := i
		i = skipSpaceBack(s, i, true)
		out = append(out, s[i:j])
	}
	if i > 0 {
		if i = skipSpaceBack(s, i, false); i > 0 {
			out = append(out, s[:i])
		}
	}
	slices.Reverse(out)
	return out
}

// basename is posixpath.basename(p): the part of p after its last '/'.
func basename(p string) string { return p[strings.LastIndexByte(p, '/')+1:] }

// chrOf is chr(i) for a code point given as an integer of any size.
func chrOf(i *big.Int) (string, error) {
	if !i.IsInt64() || i.Int64() > 1<<31-1 || i.Int64() < -1<<31 {
		return "", newPyErr("OverflowError", "Python int too large to convert to C int")
	}
	v := i.Int64()
	if v < 0 || v > 0x10FFFF {
		return "", newPyErr("ValueError", "chr() arg not in range(0x110000)")
	}
	return string(appendRune(nil, rune(v))), nil
}
