// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/xml_template.py, ir.py, template.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/pyuni"
)

// The str methods Kajiki calls on template text, with Python's notion of
// white space (str.isspace).

func strip(s string) string  { return strings.TrimFunc(s, pyuni.IsSpace) }
func lstrip(s string) string { return strings.TrimLeftFunc(s, pyuni.IsSpace) }
func rstrip(s string) string { return strings.TrimRightFunc(s, pyuni.IsSpace) }

// isBlank is `not s.strip()`.
func isBlank(s string) bool { return strip(s) == "" }
