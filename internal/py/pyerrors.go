// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Python exceptions: the builtin classes, their hierarchy, and exception instances as Python
// values.

package py

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// excV is a Python exception instance. It is both a Value (it can be bound
// by `except ... as e`, printed and inspected) and a Go error (it is what a
// Python operation raises).
type excV struct {
	Class string
	// Message is the text of the exception for Go code: str() of it as it
	// was raised (see excStr). Python code gets str() from excText.
	Message string
	// ownStr is set when Message is the str() the exception's class
	// formats itself (a codec error), rather than one derived from args.
	ownStr bool
	args   []Value
	// attrs holds attributes set on the instance (nil until one is).
	attrs *dictV
	// cls is the class of an exception whose class is not a builtin one: a
	// class defined by Python code or by Go code.
	cls *classV
	// cause, context and suppressContext are __cause__, __context__ and
	// __suppress_context__ (nil for None).
	cause           Value
	context         *excV
	suppressContext bool
	// tb holds the traceback entries, innermost first; tbFrame is the frame
	// the last one was added for. tbObj is __traceback__ as last made, for
	// the first tbObjLen entries.
	tb       []tbEntry
	tbFrame  *frame
	tbObj    *tracebackV
	tbObjLen int
	// fields holds the member fields of the builtin classes that have them
	// (SyntaxError.msg, OSError.errno, ...); see excFieldNames.
	fields map[string]Value
}

// excText is str() of an exception: its class's __str__ when Python code
// defines one, else BaseException.__str__.
func excText(e *excV) (string, error) {
	if e.cls != nil {
		if m, ok := e.cls.lookupUser("__str__"); ok {
			return callStrMethod(m, e, "__str__")
		}
	}
	return excBuiltinStr(e)
}

// excBuiltinStr is __str__ of the builtin exception class e derives from.
func excBuiltinStr(e *excV) (string, error) {
	return builtinExcStr(excClass(e).builtinBase(), e)
}

// excArgsStr is BaseException.__str__ (KeyError.__str__ for KeyError): the
// str of the single argument, or the repr of the args.
func excArgsStr(class string, args []Value) (string, error) {
	switch len(args) {
	case 0:
		return "", nil
	case 1:
		if subclass(class, "KeyError") {
			return pyRepr(args[0])
		}
		return pyStr(args[0])
	}
	return pyRepr(newTuple(args))
}

func (e *excV) Error() string { return e.Class + ": " + e.Message }

// attrDict is the instance dictionary of the exception.
func (e *excV) attrDict() *dictV {
	if e.attrs == nil {
		e.attrs = newDict()
	}
	return e.attrs
}

// getAttr is attribute access on an exception: args, then for a class
// defined in Python the class and instance attributes, then the instance
// dictionary and BaseException's attributes.
func (e *excV) getAttr(name string) (Value, error) {
	if e.isExcField(name) {
		return e.field(name), nil
	}
	if name == "args" {
		return newTuple(append([]Value(nil), e.args...)), nil
	}
	if e.cls != nil {
		return instanceGetattr(e, e.cls, e.attrDict(), name, func(n string) (Value, bool) {
			return excBuiltinAttr(e, n)
		})
	}
	if v, ok := excMethod(e, name); ok {
		return v, nil
	}
	return nil, nil
}

// excBuiltinAttr is an attribute BaseException (or a builtin subclass)
// defines for its instances.
func excBuiltinAttr(e *excV, name string) (Value, bool) {
	if v, ok := excGroupAttr(e, name); ok {
		return v, true
	}
	switch name {
	case "__traceback__":
		return e.traceback(), true
	case "__dict__":
		return e.attrDict(), true
	case "add_note":
		return methodsBound(e, name, func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly("add_note", 1, 1, args, kw); err != nil {
				return nil, err
			}
			if _, ok := unsub(args[0]).(strV); !ok {
				return nil, typeErr("note must be a str, not '%s'", typeName(args[0]))
			}
			notes, ok := e.attrDict().getStr("__notes__")
			if !ok {
				l := newList(nil)
				e.attrDict().setStr("__notes__", l)
				notes = l
			}
			l, isList := notes.(*listV)
			if !isList {
				return nil, typeErr("Cannot add note: __notes__ is not a list")
			}
			l.items = append(l.items, args[0])
			return none, nil
		}), true
	case "__cause__":
		if e.cause == nil {
			return none, true
		}
		return e.cause, true
	case "__context__":
		if e.context == nil {
			return none, true
		}
		return e.context, true
	case "__suppress_context__":
		return pyBool(e.suppressContext), true
	case "__class__":
		return excClass(e), true
	case "code":
		if subclass(excClass(e).builtinBase().name, "SystemExit") {
			switch len(e.args) {
			case 0:
				return none, true
			case 1:
				return e.args[0], true
			}
			return newTuple(append([]Value(nil), e.args...)), true
		}
	case "value":
		if subclass(excClass(e).builtinBase().name, "StopIteration") {
			if len(e.args) > 0 {
				return e.args[0], true
			}
			return none, true
		}
	case "with_traceback":
		return methodsBound(e, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("with_traceback", args, kw); err != nil {
				return nil, err
			}
			if err := e.setTraceback(args[0]); err != nil {
				return nil, err
			}
			return e, nil
		}), true
	}
	return nil, false
}

// newPyErr returns an exception raised with a single message argument.
func newPyErr(class, msg string) *excV {
	return &excV{Class: class, Message: msg, args: []Value{strV(msg)}}
}

// newExc returns an exception built as Class(*args) is in Python.
func newExc(class string, args []Value) *excV {
	e := &excV{Class: class, args: args}
	e.Message = excStr(class, args)
	return e
}

// excStr is the Message of an exception built from args: the str() of the
// args. Building an exception does not call str() in Python, so a str() that
// raises is not an error here; the Message is then the text CPython's
// traceback display shows for such an exception (PyErr_Display's
// "<exception str() failed>"), and excText raises the error when Python
// code asks for str().
func excStr(class string, args []Value) string {
	s, err := excArgsStr(class, args)
	if err != nil {
		return "<exception str() failed>"
	}
	return s
}

// excParents is the class hierarchy of the builtin exceptions.
var excParents = map[string]string{
	"BaseException":             "",
	"Exception":                 "BaseException",
	"KeyboardInterrupt":         "BaseException",
	"SystemExit":                "BaseException",
	"GeneratorExit":             "BaseException",
	"ArithmeticError":           "Exception",
	"FloatingPointError":        "ArithmeticError",
	"OverflowError":             "ArithmeticError",
	"ZeroDivisionError":         "ArithmeticError",
	"AssertionError":            "Exception",
	"AttributeError":            "Exception",
	"BufferError":               "Exception",
	"EOFError":                  "Exception",
	"ImportError":               "Exception",
	"ModuleNotFoundError":       "ImportError",
	"LookupError":               "Exception",
	"IndexError":                "LookupError",
	"KeyError":                  "LookupError",
	"MemoryError":               "Exception",
	"NameError":                 "Exception",
	"UnboundLocalError":         "NameError",
	"OSError":                   "Exception",
	"ReferenceError":            "Exception",
	"RuntimeError":              "Exception",
	"NotImplementedError":       "RuntimeError",
	"RecursionError":            "RuntimeError",
	"StopIteration":             "Exception",
	"SyntaxError":               "Exception",
	"IndentationError":          "SyntaxError",
	"TabError":                  "IndentationError",
	"SystemError":               "Exception",
	"TypeError":                 "Exception",
	"ValueError":                "Exception",
	"UnicodeError":              "ValueError",
	"UnicodeDecodeError":        "UnicodeError",
	"UnicodeEncodeError":        "UnicodeError",
	"UnicodeTranslateError":     "UnicodeError",
	"Warning":                   "Exception",
	"BytesWarning":              "Warning",
	"DeprecationWarning":        "Warning",
	"ImportWarning":             "Warning",
	"RuntimeWarning":            "Warning",
	"SyntaxWarning":             "Warning",
	"UnicodeWarning":            "Warning",
	"UserWarning":               "Warning",
	"FutureWarning":             "Warning",
	"PendingDeprecationWarning": "Warning",
	"ResourceWarning":           "Warning",
	"EncodingWarning":           "Warning",
	"StopAsyncIteration":        "Exception",
	"BaseExceptionGroup":        "BaseException",
	"ExceptionGroup":            "BaseExceptionGroup",
	"BlockingIOError":           "OSError",
	"ChildProcessError":         "OSError",
	"ConnectionError":           "OSError",
	"BrokenPipeError":           "ConnectionError",
	"ConnectionAbortedError":    "ConnectionError",
	"ConnectionRefusedError":    "ConnectionError",
	"ConnectionResetError":      "ConnectionError",
	"FileExistsError":           "OSError",
	"FileNotFoundError":         "OSError",
	"InterruptedError":          "OSError",
	"IsADirectoryError":         "OSError",
	"NotADirectoryError":        "OSError",
	"PermissionError":           "OSError",
	"ProcessLookupError":        "OSError",
	"TimeoutError":              "OSError",
}

// excAliases are the builtin names bound to another exception class.
var excAliases = map[string]string{"EnvironmentError": "OSError", "IOError": "OSError"}

func subclass(class, of string) bool {
	for c := class; c != ""; c = excParents[c] {
		if c == of {
			return true
		}
	}
	return false
}

// isExc reports whether err is a Python exception of one of the classes.
func isExc(err error, classes ...string) bool {
	var e *excV
	if !errors.As(err, &e) {
		return false
	}
	for _, c := range classes {
		if subclass(e.Class, c) {
			return true
		}
	}
	return false
}

// asExc returns err as an exception, wrapping a foreign Go error as
// SystemError (it would be a bug in this package).
func asExc(err error) *excV {
	var e *excV
	if errors.As(err, &e) {
		return e
	}
	return newPyErr("SystemError", err.Error())
}

func typeErr(format string, args ...any) *excV {
	return newPyErr("TypeError", fmt.Sprintf(format, args...))
}

func valueErr(format string, args ...any) *excV {
	return newPyErr("ValueError", fmt.Sprintf(format, args...))
}

// attrErr is the AttributeError of reading the attribute name of an object of
// type typ.
func attrErr(typ, name string) *excV {
	return newPyErr("AttributeError", "'"+typ+"' object has no attribute '"+name+"'")
}

// typeAttrErr is the AttributeError of reading the attribute name of the
// class named typ.
func typeAttrErr(typ, name string) *excV {
	return newPyErr("AttributeError", "type object '"+typ+"' has no attribute '"+name+"'")
}

// fromLeaf turns the failure of a codec into the exception object: a
// UnicodeDecodeError or UnicodeEncodeError keeps its attributes. Other errors
// pass through.
func fromLeaf(err error) error {
	if e, ok := err.(*codecError); ok {
		switch e.Class {
		case "UnicodeDecodeError":
			x := newExc(e.Class, []Value{strV(e.Encoding), bytesV(e.Object), mkInt(int64(e.Start)),
				mkInt(int64(e.End)), strV(e.Reason)})
			x.Message, x.ownStr = e.Msg, true
			setCodecAttrs(x, e, bytesV(e.Object))
			return x
		case "UnicodeEncodeError":
			x := newExc(e.Class, []Value{strV(e.Encoding), strV(e.Text), mkInt(int64(e.Start)),
				mkInt(int64(e.End)), strV(e.Reason)})
			x.Message, x.ownStr = e.Msg, true
			setCodecAttrs(x, e, strV(e.Text))
			return x
		}
	}
	if e, ok := err.(*codecError); ok {
		return newPyErr(e.Class, e.Msg)
	}
	return err
}

// setChainAttr sets __cause__, __context__, __suppress_context__ or
// __traceback__ of an exception, with BaseException's checks.
func (e *excV) setChainAttr(name string, value Value) error {
	switch name {
	case "__cause__", "__context__":
		var x *excV
		if !isNoneV(value) {
			ex, ok := value.(*excV)
			if !ok {
				what := "cause"
				if name == "__context__" {
					what = "context"
				}
				return typeErr("exception %s must be None or derive from BaseException", what)
			}
			x = ex
		}
		if name == "__context__" {
			e.context = x
			return nil
		}
		e.cause = nil
		if x != nil {
			e.cause = x
		}
		e.suppressContext = true
	case "__suppress_context__":
		t, err := truthy(value)
		if err != nil {
			return err
		}
		e.suppressContext = t
	case "__traceback__":
		return e.setTraceback(value)
	}
	return nil
}

// maxStrDigits is sys.get_int_max_str_digits(), the longest decimal string
// int() converts and the longest decimal string str() makes of an int.
const maxStrDigits = 4300

// digitLimitErr is the ValueError int() raises for a decimal string of
// digits digits, more than maxStrDigits.
func digitLimitErr(digits int) *excV {
	return newPyErr("ValueError", "Exceeds the limit ("+strconv.Itoa(maxStrDigits)+" digits) for integer string "+
		"conversion: value has "+strconv.Itoa(digits)+" digits; use sys.set_int_max_str_digits() to increase the limit")
}

// quoteList is the list of names CPython's argument errors show: each name in
// single quotes, joined as 'a', 'a' and 'b', or 'a', 'b', and 'c'.
func quoteList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = "'" + n + "'"
	}
	switch len(q) {
	case 0:
		return ""
	case 1:
		return q[0]
	case 2:
		return q[0] + " and " + q[1]
	}
	return strings.Join(q[:len(q)-1], ", ") + ", and " + q[len(q)-1]
}

// plural is the "s" of a count in CPython's messages: "" for 1, else "s".
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// tooManyPositionalMsg is the message of the TypeError CPython's
// too_many_positional raises: fn takes min to max positional arguments (min
// equals max when none has a default) but given were passed, together with
// kwonly keyword-only ones.
func tooManyPositionalMsg(fn string, min, max, given, kwonly int) string {
	takes, pl := strconv.Itoa(max), plural(max)
	if min < max {
		takes, pl = "from "+strconv.Itoa(min)+" to "+strconv.Itoa(max), "s"
	}
	extra := ""
	if kwonly > 0 {
		extra = " positional argument" + plural(given) + " (and " + strconv.Itoa(kwonly) + " keyword-only argument" +
			plural(kwonly) + ")"
	}
	verb := "were"
	if given == 1 && kwonly == 0 {
		verb = "was"
	}
	return fn + "() takes " + takes + " positional argument" + pl + " but " + strconv.Itoa(given) + extra + " " +
		verb + " given"
}
