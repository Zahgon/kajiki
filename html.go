// Ported from CPython 3.12.15 (Lib/html/__init__.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package kajiki

import (
	"math/big"
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/entities"
	"github.com/jackrosenthal/kajiki-go/internal/pyre"
)

// charref is html._charref: a numeric or named character reference.
var charref = pyre.MustCompile(`&(#[0-9]+;?|#[xX][0-9a-fA-F]+;?|[^\t\n\f <&#;]{1,32};?)`, 0)

// invalidCharrefs is html._invalid_charrefs: the numeric references HTML
// maps to other characters.
var invalidCharrefs = map[int64]string{
	0x00: "�", 0x0d: "\r", 0x80: "€", 0x81: "\x81", 0x82: "‚", 0x83: "ƒ", 0x84: "„",
	0x85: "…", 0x86: "†", 0x87: "‡", 0x88: "ˆ", 0x89: "‰", 0x8a: "Š", 0x8b: "‹",
	0x8c: "Œ", 0x8d: "\x8d", 0x8e: "Ž", 0x8f: "\x8f", 0x90: "\x90", 0x91: "‘", 0x92: "’",
	0x93: "“", 0x94: "”", 0x95: "•", 0x96: "–", 0x97: "—", 0x98: "˜", 0x99: "™",
	0x9a: "š", 0x9b: "›", 0x9c: "œ", 0x9d: "\x9d", 0x9e: "ž", 0x9f: "Ÿ",
}

// invalidCodepoint is membership in html._invalid_codepoints: the code
// points a numeric reference drops.
func invalidCodepoint(n int64) bool {
	switch {
	case n >= 0x1 && n <= 0x8, n == 0xb, n >= 0xe && n <= 0x1f, n >= 0x7f && n <= 0x9f, n >= 0xfdd0 && n <= 0xfdef:
		return true
	}
	return n >= 0xfffe && n <= 0x10ffff && n&0xfffe == 0xfffe
}

// htmlUnescape is html.unescape: the character references in s replaced
// by the characters they stand for, as HTML5 resolves them.
func htmlUnescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	text := []rune(s)
	var b strings.Builder
	last := 0
	for _, m := range charref.FindIter(text, 0, len(text)) {
		b.WriteString(string(text[last:m.Start(0)]))
		ref, _ := m.Group(1)
		b.WriteString(replaceCharref(ref))
		last = m.End(0)
	}
	b.WriteString(string(text[last:]))
	return b.String()
}

// replaceCharref is html._replace_charref for the reference ref (without
// its "&").
func replaceCharref(ref string) string {
	if ref[0] == '#' {
		digits, base := ref[1:], 10
		if digits[0] == 'x' || digits[0] == 'X' {
			digits, base = digits[1:], 16
		}
		num, _ := new(big.Int).SetString(strings.TrimRight(digits, ";"), base)
		if !num.IsInt64() || num.Int64() > 0x10FFFF {
			return "�"
		}
		n := num.Int64()
		if r, ok := invalidCharrefs[n]; ok {
			return r
		}
		switch {
		case n >= 0xD800 && n <= 0xDFFF:
			return "�"
		case invalidCodepoint(n):
			return ""
		}
		return string(rune(n))
	}
	if r, ok := entities.HTML5[ref]; ok {
		return r
	}
	name := []rune(ref)
	for x := len(name) - 1; x > 1; x-- {
		if r, ok := entities.HTML5[string(name[:x])]; ok {
			return r + string(name[x:])
		}
	}
	return "&" + ref
}
