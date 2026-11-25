// Ported from CPython 3.12 (Lib/warnings.py, Python/_warnings.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The warnings the parser and the compiler issue, shown as the default filters show them.

package py

import (
	"fmt"
	"io"
)

// warnExplicit is PyErr_WarnExplicitObject under the default filters: the
// warning of the category (SyntaxWarning, DeprecationWarning, ...) issued for
// line lineno of the code compiled from filename is written to the
// interpreter's warning output in the format of warnings.formatwarning,
// with the line of the file named, when linecache can read it.
func warnExplicit(category, message, filename string, lineno int) {
	if category == "DeprecationWarning" {
		// The default filters ignore DeprecationWarning outside __main__.
		return
	}
	ensureInit()
	text := fmt.Sprintf("%s:%d: %s: %s\n", filename, lineno, category, message)
	if line, ok := fileLine(filename, lineno); ok {
		if line = stripSpace(line); line != "" {
			text += "  " + line + "\n"
		}
	}
	// warnings._showwarnmsg_impl ignores the OSError of a failed write.
	_, _ = io.WriteString(interp.stderr, text)
}
