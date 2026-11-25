// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/template.py, kajiki/xml_template.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"fmt"
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/py"
	"github.com/jackrosenthal/kajiki-go/internal/pyuni"
)

// Error is an exception raised by Kajiki or by the Python a template runs.
// Every function of this package that fails returns an *Error.
type Error struct {
	// Class is type(e).__name__.
	Class string
	// Message is str(e).
	Message string
	// SyntaxMsg is set on a KajikiSyntaxError: the msg of the SyntaxError
	// Python raised compiling the template's generated code.
	SyntaxMsg string
	// Filename, Line and Column locate the error. On XMLTemplateParseError,
	// XMLTemplateCompileError and KajikiSyntaxError they are the exception's
	// filename, linenum and colnum. On any other exception they are the file
	// name and line of the innermost entry of the exception's traceback that
	// runs a function whose code Kajiki rewrote with the template's file name
	// and lines (the template body, a top-level py:def or %def, a block), with
	// Column 0; "" and 0 when the traceback has no such entry, and Line 0 when
	// that entry has no line.
	Filename string
	Line     int
	Column   int
}

// Error returns Message.
func (e *Error) Error() string { return e.Message }

// templateErrorText is str() of a KajikiTemplateError: the location, the
// message and the snippet of the source around the line.
func templateErrorText(msg, source, filename string, line int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s:%d] %s\n", filename, line, msg))
	lines := pyuni.SplitLines(source, false)
	at := line - 1
	for i := at - 2; i < at+2; i++ {
		if i < 0 || i >= len(lines) {
			continue
		}
		arrow := "   "
		if i == at {
			arrow = "-->"
		}
		b.WriteString(fmt.Sprintf("\t %s %s\n", arrow, lines[i]))
	}
	return b.String()
}

// The exception classes Kajiki defines, and the xml.dom one its DOM raises.
var (
	kajikiTemplateError     = exceptionClass("KajikiTemplateError", "kajiki.template", py.Builtin("Exception"))
	kajikiSyntaxError       = exceptionClass("KajikiSyntaxError", "kajiki.template", kajikiTemplateError)
	xmlTemplateError        = exceptionClass("XMLTemplateError", "kajiki.xml_template", kajikiTemplateError)
	xmlTemplateCompileError = exceptionClass("XMLTemplateCompileError", "kajiki.xml_template", xmlTemplateError)
	xmlTemplateParseError   = exceptionClass("XMLTemplateParseError", "kajiki.xml_template", xmlTemplateError)
	domException            = exceptionClass("DOMException", "xml.dom", py.Builtin("Exception"))
	notFoundErr             = exceptionClass("NotFoundErr", "xml.dom", domException)
)

// exceptionClass makes the class name of module deriving from base.
func exceptionClass(name, module string, base py.Value) py.Value {
	cls, err := py.NewClass(name, []py.Value{base}, py.StrDict([]string{"__module__"}, []py.Value{py.String(module)}))
	if err != nil {
		panic(err)
	}
	return cls
}

// newTemplateError is cls(msg, source, filename, line, col) for a class of
// the KajikiTemplateError family: str() is the location, the message and
// the snippet, and filename, linenum and colnum are attributes.
func newTemplateError(cls py.Value, msg, source, filename string, line, col int) error {
	e, err := py.Call(cls, []py.Value{py.String(templateErrorText(msg, source, filename, line))}, nil)
	if err != nil {
		return err
	}
	for _, a := range []struct {
		name  string
		value py.Value
	}{{"filename", py.String(filename)}, {"linenum", py.Int(int64(line))}, {"colnum", py.Int(int64(col))}} {
		if err := py.SetAttr(e, a.name, a.value); err != nil {
			return err
		}
	}
	return py.RaiseValue(e)
}

// newParseError is XMLTemplateParseError(msg, source, filename, line, col).
func newParseError(msg, source, filename string, line, col int) error {
	return newTemplateError(xmlTemplateParseError, msg, source, filename, line, col)
}

// newCompileError is XMLTemplateCompileError(msg, doc, filename, line): the
// snippet comes from the source the parser read, injected doctype included.
func newCompileError(msg string, doc *node, filename string, line int) error {
	return newTemplateError(xmlTemplateCompileError, msg, doc.source, filename, line, 0)
}

// newNotFoundErr is xml.dom.NotFoundErr().
func newNotFoundErr() error {
	e, err := py.Call(notFoundErr, nil, nil)
	if err != nil {
		return err
	}
	return py.RaiseValue(e)
}
