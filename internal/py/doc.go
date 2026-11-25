// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

// Package py is a CPython 3.12 runtime: the tokenizer and the PEG parser of
// CPython with every one of its error messages and positions, the checks of
// its compiler, an evaluator running the syntax trees with Python's scoping,
// exceptions and generators, CPython's values and their methods, and the
// standard modules Python code may import.
//
// # The interpreter
//
// Init starts (or restarts) the one interpreter of the process from a
// Config: the Go reader input() reads, the writers print() and warnings
// write to, and the number of frames of the launching program that count
// towards the recursion limit (1000, as in CPython). The package starts
// itself with an empty Config when used before Init. The sys module has
// sys.modules, the version, the recursion limit functions and
// sys.exception().
//
// # Compiling and running code
//
// Compile is compile(source, filename, mode) for "exec" and "eval": it
// raises the SyntaxError, IndentationError or TabError CPython 3.12 raises,
// with the same msg, lineno, offset, end_lineno, end_offset and text
// (Error.Syntax gives them), including the errors of the compiler's checks
// ('return' outside function, nonlocal and global errors, duplicate
// arguments, ...). Code.Exec and Code.Eval are exec(code, globals) and
// eval(code, globals). Parse is ast.parse for modes "exec" and "eval", and
// Node is a node of the tree it builds.
//
// # Tokenizing
//
// GenerateTokens is tokenize.generate_tokens(io.StringIO(source).readline):
// a TokenStream whose Next gives CPython's TokenInfo values one at a time,
// reading the source only as far as needed, and the TokenError,
// IndentationError or TabError tokenize raises.
//
// # Standard modules
//
// Python code can import math, re (on package pyre), string, itertools,
// functools, operator, collections (with collections.abc) and json, written
// in Go with CPython's results, reprs and error messages. Float results are
// those of CPython 3.12 on 64-bit ARM Linux: exp, exp2, log, log2, log10,
// pow, expm1, log1p and the hyperbolic functions and their inverses are the
// C library's (glibc 2.36) bit for bit, gamma, lgamma, hypot, dist, fsum
// and complex arithmetic are CPython's own, and the trigonometric
// functions, cbrt, erf and erfc are correctly rounded, which the C library
// is for nearly all arguments of the trigonometric ones.
//
// # Values
//
// Value is a Python value. The constructors are None, Bool, Int, BigInt,
// Float, Complex, String, Bytes, List, Tuple, Set, NewDict, EmptyDict and
// StrDict.
//
// The operations are Python's: Repr, Str, TypeName, Truth, Len, Equal, Hash,
// GetAttr, HasAttr, SetAttr, GetItem, SetItem, DelItem, Contains, Call,
// CallMethod, BinaryOp, Compare, RichCompare, Iter (an Iterator), ForEach,
// ToList and Unpack (`a, b = v`); TypeCheck is PyObject_TypeCheck;
// Builtin(name) is a builtin function, class or constant. The inspections
// are AsString (str and its subclasses), IsNumber, AsInt, AsFloat, AsBool,
// IsNone, IsCallable, IsFunction, IsDict, IsList, IsTuple, SeqItems and
// DictItems.
//
// # Generators
//
// A generator's body runs as a coroutine (iter.Pull2) and keeps its own
// exception state, as CPython's frames do. A generator object that nothing
// refers to any more while its body is paused has the body ended once it is
// collected, without running any of its Python code: CPython runs the
// finally blocks when the last reference goes away, which this runtime
// cannot tell, so they do not run rather than run at another point. A
// generator a for loop made itself (by calling a generator function or as a
// generator expression in its iterable) is closed when the loop is left
// early, as CPython's reference counting closes it there.
//
// # Functions and code objects
//
// FunctionCode is fn.__code__ as a *Code (one per code object, so == tells
// code objects apart); Code gives co_name, co_qualname, co_filename and
// co_firstlineno, and WithLines makes the code object CodeType would with
// another file name, first line and line table. FunctionType is
// types.FunctionType(code, globals, name, argdefs, closure) and
// RebindFunction re-binds a function to another globals dict. Traceback
// lists the frames an exception left with their code objects and lines.
// Dir is dir(v).
//
// # Functions and classes written in Go
//
// Func (and FuncQual, which sets __qualname__ and __module__) makes a
// Python function whose body is Go code; it binds as a method when it is a
// class attribute, receiving the instance as args[0]. NewFunction makes one
// that binds its arguments as a def with the parameters of a FuncSpec does,
// with the same TypeErrors. BuiltinFunc makes a
// builtin function, which does not bind. NewClass is type(name, bases, ns):
// ns is a dict holding __module__ (and optionally __qualname__, __doc__), the
// methods as Funcs, class attributes, and descriptors made by calling the
// builtins classmethod, staticmethod and property. NewInstance(cls, base)
// builds an instance, SetNative and Native attach Go state to it.
// NewIterator makes an iterator object from a Go function. NewModule and
// RegisterModule make modules written in Go, importable by Python code.
// EnterRecursiveCall counts Go code that stands for a Python frame towards
// the recursion limit; CallerGlobals and CallerLocals are globals() and
// locals() of the Python frame calling into Go.
//
// # Exceptions
//
// Every error the exported functions return for a Python exception is an
// *Error: Class is type(e).__name__, Text is str(e), Value the exception
// object, and IsInstance tests its class; ErrorIs tests an error. Raise
// makes class(*args) of a builtin exception class and RaiseValue raises any
// exception instance or class for Go code to return. ExceptionValue turns
// any error into the exception object (a RuntimeError holding the text of
// an error that is not a Python exception). Exceptions carry __cause__,
// __context__ and __suppress_context__; Traceback lists the Python frames
// an exception left, outermost first.
package py
