// Port of CPython 3.12's repr() of str and bytes (Objects/unicodeobject.c unicode_repr,
// Objects/bytesobject.c PyBytes_Repr; PSF License Version 2;
// Copyright (c) 2001-2024 Python Software Foundation).

package pyuni

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// ReprQuote is the quote repr() puts around the text s (the bytes of a
// bytes object or the UTF-8 of a str): a double quote when s holds a single
// quote and no double quote, else a single quote.
func ReprQuote(s string) byte {
	if strings.IndexByte(s, '\'') >= 0 && strings.IndexByte(s, '"') < 0 {
		return '"'
	}
	return '\''
}

// AppendRepr appends the code point r as repr() writes it between the
// quotes q: tab, newline and carriage return as \t, \n and \r, the quote and
// the backslash after a backslash, a printable character as itself (only a
// printable ASCII one when asciiOnly is set, as for bytes), and any other as
// AppendEscape writes it.
func AppendRepr(b []byte, r rune, q byte, asciiOnly bool) []byte {
	switch {
	case r == rune(q) || r == '\\':
		return append(b, '\\', byte(r))
	case r == '\t':
		return append(b, '\\', 't')
	case r == '\n':
		return append(b, '\\', 'n')
	case r == '\r':
		return append(b, '\\', 'r')
	case r >= 0x20 && r < 0x7F:
		return append(b, byte(r))
	case !asciiOnly && r > 0x7F && IsPrintable(r):
		return utf8.AppendRune(b, r)
	}
	return AppendEscape(b, r)
}

// AppendEscape appends the backslash escape of r that repr(), ascii() and
// the backslashreplace error handler write: \xhh up to U+00FF, \uhhhh up to
// U+FFFF and \Uhhhhhhhh above, in lowercase hex.
func AppendEscape(b []byte, r rune) []byte {
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

// Repr is repr() of the str holding the code points rs.
func Repr(rs []rune) string {
	q := byte('\'')
	single, double := false, false
	for _, r := range rs {
		single = single || r == '\''
		double = double || r == '"'
	}
	if single && !double {
		q = '"'
	}
	b := make([]byte, 0, len(rs)+2)
	b = append(b, q)
	for _, r := range rs {
		b = AppendRepr(b, r, q, false)
	}
	return string(append(b, q))
}

// ReprString is repr() of the str s, given in UTF-8.
func ReprString(s string) string { return Repr([]rune(s)) }
