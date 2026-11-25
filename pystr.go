// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/xml_template.py, ir.py, template.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"strings"
	"unicode/utf8"

	"github.com/jackrosenthal/kajiki-go/internal/pyuni"
)

// The str methods Kajiki calls on template text, with Python's notion of
// white space (str.isspace) and line boundaries (str.splitlines).

func strip(s string) string  { return strings.TrimFunc(s, pyuni.IsSpace) }
func lstrip(s string) string { return strings.TrimLeftFunc(s, pyuni.IsSpace) }
func rstrip(s string) string { return strings.TrimRightFunc(s, pyuni.IsSpace) }

// isBlank is `not s.strip()`.
func isBlank(s string) bool { return strip(s) == "" }

// splitLines is str.splitlines().
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !pyuni.IsLineBreak(r) {
			i += size
			continue
		}
		lines = append(lines, s[start:i])
		i += size
		if r == '\r' && i < len(s) && s[i] == '\n' {
			i++
		}
		start = i
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// runeLen is len() of a str.
func runeLen(s string) int { return utf8.RuneCountInString(s) }
