// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Constructors and inspections of the builtin number and byte types for Go code.

package py

import "math/big"

// Complex returns a Python complex.
func Complex(c complex128) Value { return complexV(c) }

// AsComplex returns the value of a complex (an instance of a subclass of
// complex included), and whether v is one.
func AsComplex(v Value) (complex128, bool) {
	c, ok := unsub(v).(complexV)
	return complex128(c), ok
}

// AsBytes returns the contents of a bytes object (an instance of a subclass
// of bytes included), and whether v is one.
func AsBytes(v Value) ([]byte, bool) {
	b, ok := unsub(v).(bytesV)
	return []byte(b), ok
}

// NewBytearray returns a Python bytearray holding a copy of b.
func NewBytearray(b []byte) Value { return &bytearrayV{b: append([]byte(nil), b...)} }

// BuiltinFunc returns a builtin function running fn: its type is
// builtin_function_or_method, its repr <built-in function name>, and as a
// class attribute it does not bind to instances. A *Error fn returns is
// raised as that exception, any other error as a RuntimeError; a nil result
// is None.
func BuiltinFunc(name string, fn func(args []Value, kwargs []Keyword) (Value, error)) Value {
	return newBuiltin(name, func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
		r, err := fn(args, keywordsOf(kw))
		if err != nil {
			return nil, fromGoError(err)
		}
		if r == nil {
			return none, nil
		}
		return r, nil
	})
}

// SetNative attaches Go state to an instance of a class made by Python or
// Go code (see NewInstance), for the methods of a class written in Go.
func SetNative(v Value, state any) error {
	x, ok := v.(*instV)
	if !ok {
		return toError(typeErr("SetNative() needs an instance of a class made by Python or Go code, not %s", typeName(v)))
	}
	x.native = state
	return nil
}

// Native is the Go state SetNative attached to v, nil when there is none.
func Native(v Value) any {
	if x, ok := v.(*instV); ok {
		return x.native
	}
	return nil
}

// ---------------------------------------------------------------- arguments and conversions

// kwDict turns Keywords back into the dict of a call's keyword arguments.
func kwDict(kwargs []Keyword) *dictV {
	if len(kwargs) == 0 {
		return nil
	}
	d := newDict()
	for _, k := range kwargs {
		d.setStr(k.Name, k.Value)
	}
	return d
}

// NoKeywords is _PyArg_NoKeywords: "name() takes no keyword arguments" when
// there are keyword arguments.
func NoKeywords(name string, kwargs []Keyword) error {
	return toError(noKw(name, kwDict(kwargs)))
}

// CheckPositional is _PyArg_CheckPositional: "name expected N arguments,
// got M" outside min..max.
func CheckPositional(name string, n, min, max int) error {
	return toError(methodsCheckPositional(name, n, min, max))
}

// ArgsOne is the check of a function taking exactly one positional
// argument (METH_O): "name() takes exactly one argument (N given)".
func ArgsOne(name string, args []Value, kwargs []Keyword) error {
	return toError(methodsArgsOne(name, args, kwDict(kwargs)))
}

// ArgsNone is the check of a function taking no arguments (METH_NOARGS).
func ArgsNone(name string, args []Value, kwargs []Keyword) error {
	return toError(methodsArgsNone(name, args, kwDict(kwargs)))
}

// ParseArgs is _PyArg_UnpackKeywords for a function whose parameters are
// names, the first required of them required and the first maxpos
// positional: the arguments by parameter, nil for one not given.
func ParseArgs(name string, names []string, required, maxpos int, args []Value, kwargs []Keyword) ([]Value, error) {
	buf, err := methodsArgsKwRequired(name, names, required, maxpos, args, kwDict(kwargs))
	return buf, toError(err)
}

// Index is operator.index(v): an int, or the result of __index__.
func Index(v Value) (*big.Int, error) {
	n, err := methodsIndex(v)
	if err != nil {
		return nil, toError(err)
	}
	return new(big.Int).Set(n), nil
}

// Ssize is Index converted to a Go int, with CPython's OverflowError for an
// int that does not fit a Py_ssize_t.
func Ssize(v Value) (int, error) {
	n, err := methodsSsize(v)
	return n, toError(err)
}

// ToFloat is PyFloat_AsDouble: a float, an int, or the result of __float__
// or __index__; TypeError "must be real number, not X" otherwise.
func ToFloat(v Value) (float64, error) {
	f, err := toFloat(v)
	return f, toError(err)
}

// RichCompare evaluates the rich comparison a op b (op one of < <= > >= ==
// !=): whatever value the comparison gives.
func RichCompare(op string, a, b Value) (Value, error) {
	astOp := map[string]string{"<": "Lt", "<=": "LtE", ">": "Gt", ">=": "GtE", "==": "Eq", "!=": "NotEq"}[op]
	if astOp == "" {
		return nil, toError(newPyErr("SystemError", "unknown comparison "+op))
	}
	r, err := compareOp(astOp, a, b)
	return r, toError(err)
}

// Compare is the truth of RichCompare(op, a, b).
func Compare(op string, a, b Value) (bool, error) {
	r, err := RichCompare(op, a, b)
	if err != nil {
		return false, err
	}
	t, err := truthy(r)
	return t, toError(err)
}

// Format is format(v, spec).
func Format(v Value, spec string) (string, error) {
	s, err := formatValue(v, spec)
	return s, toError(err)
}

// NotImplemented is the NotImplemented singleton.
func NotImplemented() Value { return notImplementedV{} }

// IsNotImplemented reports whether v is NotImplemented.
func IsNotImplemented(v Value) bool { return isNotImplemented(v) }

// Is is the identity test a is b.
func Is(a, b Value) bool { return sameObject(a, b) }

// InplaceOp applies an augmented assignment's operator (ast class name, as
// for BinaryOp) to a and b: a.__iop__(b), falling back to the binary
// operator; the result is what the target is rebound to.
func InplaceOp(op string, a, b Value) (Value, error) {
	r, err := inplaceOp(op, a, b)
	return r, toError(err)
}

// UnaryOp applies a unary operator by its ast class name (USub, UAdd,
// Invert, Not).
func UnaryOp(op string, v Value) (Value, error) {
	r, err := unop(op, v)
	return r, toError(err)
}

// SetTypeName gives a class written in Go the name error messages show for
// its instances, as a C type's tp_name does ("collections.deque" for
// deque); __name__ stays the class's own name.
func SetTypeName(cls Value, name string) {
	if k, ok := cls.(*classV); ok {
		k.tpName = name
	}
}

// CompareOp evaluates one comparison operator by its ast class name (Eq,
// NotEq, Lt, LtE, Gt, GtE, Is, IsNot, In, NotIn) as a comparison
// expression does: the value it gives, with the VM's TypeErrors.
func CompareOp(op string, a, b Value) (Value, error) {
	if _, ok := comparisonOperators[op]; !ok {
		return nil, toError(newPyErr("SystemError", "unknown comparison "+op))
	}
	r, err := compareOp(op, a, b)
	return r, toError(err)
}

// OwnAttr is cls.__dict__[name]: what the class itself (not a base)
// defines for name, and whether it does. For a builtin class the names are
// those of CPython's type dict; the __hash__ of an unhashable type is None,
// and the value is nil for a slot the runtime does not expose as an
// attribute (reading it raises AttributeError).
func OwnAttr(cls Value, name string) (Value, bool) {
	k, ok := cls.(*classV)
	if !ok {
		return nil, false
	}
	if k.isUser() {
		return k.dict.getStr(name)
	}
	for _, n := range typeDictNames[k.name] {
		if n != name {
			continue
		}
		if name == "__hash__" && unhashableTypes[k.name] {
			return none, true
		}
		// A builtin class has no Python code that could raise anything else.
		v, ok := classAttr(k, name)
		if !ok {
			if v, ok = classGetattr(k, name); !ok {
				// The class defines name, but as a slot this runtime does
				// not expose as an attribute.
				return nil, true
			}
		}
		return v, true
	}
	return nil, false
}
