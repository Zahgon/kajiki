// Ported from CPython 3.12 (Objects/exceptions.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The C-level fields and __init__ of the builtin exception classes that have them (SyntaxError,
// ImportError, OSError and their subclasses), the errno-based choice of an OSError subclass, and
// their __str__.

package py

import (
	"strings"
)

// excFieldNames are the member fields the builtin exception class base
// gives its instances, nil for a class without any.
func excFieldNames(base string) []string {
	switch {
	case subclass(base, "SyntaxError"):
		return []string{"msg", "filename", "lineno", "offset", "text", "end_lineno", "end_offset",
			"print_file_and_line"}
	case subclass(base, "ImportError"):
		return []string{"msg", "name", "path", "name_from"}
	case subclass(base, "OSError"):
		return []string{"errno", "strerror", "filename", "filename2", "characters_written"}
	case subclass(base, "UnicodeError") && base != "UnicodeError":
		return []string{"encoding", "object", "start", "end", "reason"}
	case subclass(base, "BaseExceptionGroup"):
		return []string{"message", "exceptions"}
	case subclass(base, "AttributeError"):
		return []string{"name", "obj"}
	case subclass(base, "NameError"):
		return []string{"name"}
	}
	return nil
}

// isExcField reports whether name is a member field of e's builtin class.
func (e *excV) isExcField(name string) bool {
	for _, f := range excFieldNames(excClass(e).builtinBase().name) {
		if f == name {
			return true
		}
	}
	return false
}

// field reads a member field: None when it was never set.
func (e *excV) field(name string) Value {
	if v, ok := e.fields[name]; ok {
		return v
	}
	return none
}

// setField sets a member field.
func (e *excV) setField(name string, v Value) {
	if e.fields == nil {
		e.fields = map[string]Value{}
	}
	e.fields[name] = v
}

// osErrorClasses maps errno values to the OSError subclass OSError(errno,
// ...) makes.
var osErrorClasses = map[int64]string{
	11: "BlockingIOError", 115: "BlockingIOError", 114: "BlockingIOError", 4: "InterruptedError",
	10: "ChildProcessError", 32: "BrokenPipeError", 108: "BrokenPipeError", 103: "ConnectionAbortedError",
	111: "ConnectionRefusedError", 104: "ConnectionResetError", 17: "FileExistsError",
	2: "FileNotFoundError", 21: "IsADirectoryError", 20: "NotADirectoryError", 1: "PermissionError",
	13: "PermissionError", 3: "ProcessLookupError", 110: "TimeoutError",
}

// constructExc is calling a builtin exception class: __new__ (which picks
// the OSError subclass from the errno) and __init__.
func constructExc(name string, args []Value, kw *dictV) (Value, error) {
	if name == "BaseExceptionGroup" || name == "ExceptionGroup" {
		return newExcGroup(classes[name], args, kw)
	}
	args = append([]Value(nil), args...)
	if name == "OSError" && len(args) >= 2 && len(args) <= 5 {
		if n, ok := asInt(args[0]); ok && n.IsInt64() {
			if sub, ok := osErrorClasses[n.Int64()]; ok {
				name = sub
			}
		}
	}
	e := &excV{Class: name, args: args}
	if err := initExc(e, args, kw); err != nil {
		return nil, err
	}
	return e, nil
}

// initExc is the __init__ of e's builtin exception class: args becomes
// e.args and the member fields are set from them.
func initExc(e *excV, args []Value, kw *dictV) error {
	base := excClass(e).builtinBase().name
	if subclass(base, "BaseExceptionGroup") {
		// BaseExceptionGroup.__init__ accepts what __new__ checked.
		e.args = append([]Value(nil), args...)
		return nil
	}
	switch {
	case subclass(base, "ImportError"):
		if err := excKeywords(e, kw, "name", "path", "name_from"); err != nil {
			return err
		}
	case subclass(base, "AttributeError"):
		if err := excKeywords(e, kw, "name", "obj"); err != nil {
			return err
		}
	case subclass(base, "NameError"):
		if err := excKeywords(e, kw, "name"); err != nil {
			return err
		}
	default:
		if err := noKw(excClass(e).name, kw); err != nil {
			return err
		}
	}
	e.args = append([]Value(nil), args...)
	e.ownStr = false
	switch {
	case subclass(base, "SyntaxError"):
		if err := initSyntaxError(e, args); err != nil {
			return err
		}
	case subclass(base, "ImportError"):
		if len(args) == 1 {
			e.setField("msg", args[0])
		}
	case subclass(base, "OSError"):
		initOSError(e, args)
	case subclass(base, "UnicodeError") && base != "UnicodeError":
		if err := initUnicodeError(e, base, args); err != nil {
			return err
		}
	}
	e.Message = excStr(base, e.args)
	if s, err := excBuiltinStr(e); err == nil {
		e.Message = s
	}
	return nil
}

// excKeywords reads the keyword arguments of the exception classes that
// take some (ImportError's name and path, AttributeError's name and obj,
// NameError's name) into their fields.
func excKeywords(e *excV, kw *dictV, allowed ...string) error {
	if kw == nil {
		return nil
	}
	for i, k := range kw.keys {
		s, _ := unsub(k).(strV)
		ok := false
		for _, a := range allowed {
			ok = ok || a == string(s)
		}
		if !ok {
			return typeErr("'%s' is an invalid keyword argument for %s()", string(s), excClass(e).name)
		}
		e.setField(string(s), kw.vals[i])
	}
	return nil
}

// initSyntaxError is SyntaxError_init: SyntaxError(msg) or
// SyntaxError(msg, (filename, lineno, offset, text[, end_lineno,
// end_offset])).
func initSyntaxError(e *excV, args []Value) error {
	if len(args) >= 1 {
		e.setField("msg", args[0])
	}
	if len(args) == 2 {
		info, err := toSlice(args[1])
		if err != nil {
			return err
		}
		if len(info) < 4 {
			return typeErr("function takes at least 4 arguments (%d given)", len(info))
		}
		if len(info) > 6 {
			return typeErr("function takes at most 6 arguments (%d given)", len(info))
		}
		names := []string{"filename", "lineno", "offset", "text", "end_lineno", "end_offset"}
		for i, v := range info {
			e.setField(names[i], v)
		}
	}
	return nil
}

// initOSError is OSError_init: OSError(errno, strerror[, filename[,
// winerror[, filename2]]]); args keeps only (errno, strerror) when a file
// name is given.
func initOSError(e *excV, args []Value) {
	if len(args) < 2 || len(args) > 5 {
		return
	}
	e.setField("errno", args[0])
	e.setField("strerror", args[1])
	if len(args) >= 3 {
		e.setField("filename", args[2])
		if len(args) == 5 {
			e.setField("filename2", args[4])
		}
		e.args = args[:2]
	}
}

// excFieldStr is __str__ of the classes with member fields; ok is false
// when the class uses BaseException.__str__.
func excFieldStr(e *excV, base string) (string, bool, error) {
	switch {
	case subclass(base, "BaseExceptionGroup"):
		return groupStr(e), true, nil
	case subclass(base, "SyntaxError"):
		s, err := syntaxErrorStr(e)
		return s, true, err
	case subclass(base, "ImportError"):
		if msg := e.field("msg"); !isNoneV(msg) {
			s, err := pyStr(msg)
			return s, true, err
		}
	case subclass(base, "OSError"):
		return osErrorStr(e)
	case subclass(base, "UnicodeError") && base != "UnicodeError" && e.fields != nil:
		s, err := unicodeErrorStr(e, base)
		return s, true, err
	}
	return "", false, nil
}

// initUnicodeError is UnicodeDecodeError(encoding, object, start, end,
// reason) and UnicodeEncodeError(encoding, object, start, end, reason).
func initUnicodeError(e *excV, base string, args []Value) error {
	if len(args) != 5 {
		return typeErr("function takes exactly 5 arguments (%d given)", len(args))
	}
	if _, ok := unsub(args[0]).(strV); !ok {
		return typeErr("argument 1 must be str, not %s", typeName(args[0]))
	}
	if subclass(base, "UnicodeDecodeError") {
		if _, ok := bytesOf(args[1]); !ok {
			return typeErr("argument 2 must be bytes-like object, not '%s'", typeName(args[1]))
		}
	} else if _, ok := unsub(args[1]).(strV); !ok {
		return typeErr("argument 2 must be str, not %s", typeName(args[1]))
	}
	for i, name := range []string{"encoding", "object", "start", "end", "reason"} {
		e.setField(name, args[i])
	}
	return nil
}

// unicodeErrorStr is UnicodeDecodeError.__str__ and
// UnicodeEncodeError.__str__.
func unicodeErrorStr(e *excV, base string) (string, error) {
	enc, err := pyStr(e.field("encoding"))
	if err != nil {
		return "", err
	}
	reason, err := pyStr(e.field("reason"))
	if err != nil {
		return "", err
	}
	start, _ := asInt(e.field("start"))
	end, _ := asInt(e.field("end"))
	if start == nil || end == nil {
		return reason, nil
	}
	s, en := start.Int64(), end.Int64()
	if subclass(base, "UnicodeDecodeError") {
		b, _ := bytesOf(e.field("object"))
		if en == s+1 && s >= 0 && s < int64(len(b)) {
			return "'" + enc + "' codec can't decode byte 0x" + hexByte(b[s]) + " in position " +
				itoa(int(s)) + ": " + reason, nil
		}
		return "'" + enc + "' codec can't decode bytes in position " + itoa(int(s)) + "-" + itoa(int(en-1)) +
			": " + reason, nil
	}
	text, _ := unsub(e.field("object")).(strV)
	rs := strRunes(string(text))
	if en == s+1 && s >= 0 && s < int64(len(rs)) {
		r := rs[s]
		esc := string(appendBackslashEscape(nil, r))
		return "'" + enc + "' codec can't encode character '" + esc + "' in position " + itoa(int(s)) + ": " + reason, nil
	}
	return "'" + enc + "' codec can't encode characters in position " + itoa(int(s)) + "-" + itoa(int(en-1)) +
		": " + reason, nil
}

func hexByte(b byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b>>4], digits[b&15]})
}

// osErrorStr is OSError_str.
func osErrorStr(e *excV) (string, bool, error) {
	errno, strerror := e.field("errno"), e.field("strerror")
	pieces := func(vs ...Value) ([]string, error) {
		out := make([]string, len(vs))
		for i, v := range vs {
			s, err := pyStr(v)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	}
	if f := e.field("filename"); !isNoneV(f) {
		fr, err := pyRepr(f)
		if err != nil {
			return "", true, err
		}
		p, err := pieces(errno, strerror)
		if err != nil {
			return "", true, err
		}
		if f2 := e.field("filename2"); !isNoneV(f2) {
			f2r, err := pyRepr(f2)
			if err != nil {
				return "", true, err
			}
			return "[Errno " + p[0] + "] " + p[1] + ": " + fr + " -> " + f2r, true, nil
		}
		return "[Errno " + p[0] + "] " + p[1] + ": " + fr, true, nil
	}
	if !isNoneV(errno) && !isNoneV(strerror) {
		p, err := pieces(errno, strerror)
		if err != nil {
			return "", true, err
		}
		return "[Errno " + p[0] + "] " + p[1], true, nil
	}
	return "", false, nil
}

// syntaxErrorStr is SyntaxError.__str__: str(msg), with the base name of
// the file and the line number when they are set (the line only when
// lineno is an int).
func syntaxErrorStr(e *excV) (string, error) {
	msg, err := pyStr(e.field("msg"))
	if err != nil {
		return "", err
	}
	file, haveFile := unsub(e.field("filename")).(strV)
	var line string
	haveLine := false
	if l, ok := e.field("lineno").(intV); ok {
		line, haveLine = l.v.String(), true
	}
	base := string(file)
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	switch {
	case haveFile && haveLine:
		return msg + " (" + base + ", line " + line + ")", nil
	case haveFile:
		return msg + " (" + base + ")", nil
	case haveLine:
		return msg + " (line " + line + ")", nil
	}
	return msg, nil
}
