// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The exported constructors and conversions of Python values.

package py

import (
	"errors"
	"math/big"
)

// None returns Python's None.
func None() Value { return none }

// Bool returns True or False.
func Bool(b bool) Value { return pyBool(b) }

// Int returns an int.
func Int(i int64) Value { return mkInt(i) }

// BigInt returns an int of any size. The argument is copied.
func BigInt(i *big.Int) Value { return mkBig(new(big.Int).Set(i)) }

// Float returns a float.
func Float(f float64) Value { return floatV(f) }

// String returns a str holding s, which must be valid UTF-8.
func String(s string) Value { return strV(s) }

// Bytes returns a bytes object. The argument is copied.
func Bytes(b []byte) Value { return bytesV(b) }

// List returns a new list holding the items.
func List(items ...Value) Value { return newList(append([]Value(nil), items...)) }

// Tuple returns a tuple holding the items.
func Tuple(items ...Value) Value { return newTuple(append([]Value(nil), items...)) }

// Set returns a new set holding the items, or the TypeError Python raises for
// an unhashable item.
func Set(items ...Value) (Value, error) {
	s := newSet(false)
	for _, it := range items {
		if err := s.add(it); err != nil {
			return nil, toError(err)
		}
	}
	return s, nil
}

// NewDict returns a dict built as dict(zip(keys, values)) is, or the TypeError
// Python raises for an unhashable key.
func NewDict(keys, values []Value) (Value, error) {
	d := newDict()
	for i := range keys {
		if err := d.set(keys[i], values[i]); err != nil {
			return nil, toError(err)
		}
	}
	return d, nil
}

// EmptyDict is a new empty dict.
func EmptyDict() Value { return newDict() }

// StrDict is a new dict mapping each of the names, as a str, to the value at
// the same index.
func StrDict(names []string, values []Value) Value {
	d := newDict()
	for i, n := range names {
		d.setStr(n, values[i])
	}
	return d
}

// DictItems returns the keys and values of a dict in iteration order, and
// whether v is one.
func DictItems(v Value) (keys, values []Value, ok bool) {
	if x, isDict := v.(*dictV); isDict {
		return append([]Value(nil), x.keys...), append([]Value(nil), x.vals...), true
	}
	return nil, nil, false
}

// Repr returns Python's repr() of the value, or the exception repr() raises.
func Repr(v Value) (string, error) {
	s, err := pyRepr(v)
	return s, toError(err)
}

// Str returns Python's str() of the value, or the exception str() raises.
func Str(v Value) (string, error) {
	s, err := pyStr(v)
	return s, toError(err)
}

// TypeName returns type(v).__name__.
func TypeName(v Value) string { return typeName(v) }

// Keyword is one keyword argument of a call.
type Keyword struct {
	Name  string
	Value Value
}

// Error is a Python exception as a Go error. Class is type(e).__name__ and
// Text is str(e).
type Error struct {
	Class string
	Text  string
	exc   *excV
	// strFailure is the class of the exception str() of the exception
	// raised, when it raised one.
	strFailure string
}

// Error returns Text.
func (e *Error) Error() string { return e.Text }

// Value is the exception object.
func (e *Error) Value() Value { return e.exc }

// IsInstance reports whether the exception is an instance of the builtin
// exception class with the given name.
func (e *Error) IsInstance(class string) bool { return excIsInstance(e.exc, class) }

// ErrorIs reports whether err is a Python exception (an *Error, possibly
// wrapped) that is an instance of the builtin exception class named class,
// as `except class:` tests it.
func ErrorIs(err error, class string) bool {
	var pe *Error
	return errors.As(err, &pe) && pe.IsInstance(class)
}

// Raise returns the exception class(*args) for a Func or a hook to return,
// class being the name of a builtin exception class.
func Raise(class string, args ...Value) error {
	k := namedExcClass(class)
	if k == nil {
		return toError(newPyErr("TypeError", "exceptions must derive from BaseException"))
	}
	v, err := callValue(nil, k, append([]Value(nil), args...), nil)
	if err != nil {
		return toError(err)
	}
	if x, ok := v.(*excV); ok {
		return toError(x)
	}
	return toError(typeErr("exceptions must derive from BaseException"))
}

// toError converts an exception raised inside the package into an *Error.
func toError(err error) error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	x := asExc(err)
	text, terr := excText(x)
	e := &Error{Class: x.Class, Text: text, exc: x}
	if terr != nil {
		e.Text = "<exception str() failed>"
		e.strFailure = asExc(terr).Class
	}
	return e
}

// fromGoError turns an error returned by Go code called from Python into the
// exception it raises: an *Error raises its exception, any other error a
// RuntimeError holding its text.
func fromGoError(err error) error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok && e.exc != nil {
		return e.exc
	}
	if x, ok := err.(*excV); ok {
		return x
	}
	return newPyErr("RuntimeError", err.Error())
}

// ---------------------------------------------------------------- Object as a Python value

// ---------------------------------------------------------------- inspections

// AsString returns the text of a str (or of an instance of a str subclass,
// Markup included) and whether v is one.
func AsString(v Value) (string, bool) {
	s, ok := unsub(v).(strV)
	return string(s), ok
}

// IsNumber is isinstance(v, (int, float)), bool included.
func IsNumber(v Value) bool { return isNumber(v) }

// AsInt returns an int (or bool) as a big integer, and whether v is one.
func AsInt(v Value) (*big.Int, bool) {
	i, ok := asInt(v)
	if !ok {
		return nil, false
	}
	return new(big.Int).Set(i), true
}

// AsBool returns the value of a bool, and whether v is one.
func AsBool(v Value) (bool, bool) {
	b, ok := v.(boolV)
	return bool(b), ok
}

// AsFloat returns the value of a float, and whether v is one.
func AsFloat(v Value) (float64, bool) {
	f, ok := unsub(v).(floatV)
	return float64(f), ok
}

// IsNone reports whether v is None.
func IsNone(v Value) bool { return isNoneV(v) }

// IsCallable is callable(v).
func IsCallable(v Value) bool { return isCallable(v) }

// IsFunction is type(v) is types.FunctionType: a function made by def or
// lambda, or by Func.
func IsFunction(v Value) bool {
	switch v.(type) {
	case *funcV, *goFuncV:
		return true
	}
	return false
}

// IsDict is type(v) is dict.
func IsDict(v Value) bool {
	_, ok := v.(*dictV)
	return ok
}

// SeqItems returns the items of a list or tuple, and whether v is one.
func SeqItems(v Value) ([]Value, bool) {
	switch x := v.(type) {
	case *listV:
		return append([]Value(nil), x.items...), true
	case *tupleV:
		return append([]Value(nil), x.items...), true
	}
	return nil, false
}

// IsList is isinstance(v, list); IsTuple is isinstance(v, tuple).
func IsList(v Value) bool {
	_, ok := v.(*listV)
	return ok
}

// IsTuple is isinstance(v, tuple).
func IsTuple(v Value) bool {
	_, ok := v.(*tupleV)
	return ok
}

// Truth is bool(v).
func Truth(v Value) (bool, error) {
	t, err := truthy(v)
	return t, toError(err)
}

// Len is len(v).
func Len(v Value) (int, error) {
	n, err := pyLen(v)
	return n, toError(err)
}

// Equal is a == b.
func Equal(a, b Value) (bool, error) {
	eq, err := pyEq(a, b)
	return eq, toError(err)
}

// Hash is hash(v).
func Hash(v Value) (int64, error) {
	h, err := pyHash(v)
	return h, toError(err)
}

// GetAttr is getattr(v, name).
func GetAttr(v Value, name string) (Value, error) {
	a, err := pyGetattr(v, name)
	return a, toError(err)
}

// HasAttr is hasattr(v, name): false when looking the attribute up raises
// AttributeError, the error when it raises anything else.
func HasAttr(v Value, name string) (bool, error) {
	a, err := getattrOpt(v, name)
	return a != nil, toError(err)
}

// SetAttr is setattr(v, name, value).
func SetAttr(v Value, name string, value Value) error {
	return toError(pySetattr(v, name, value))
}

// GetItem is v[key].
func GetItem(v, key Value) (Value, error) {
	r, err := pyGetitem(v, key)
	return r, toError(err)
}

// SetItem is v[key] = value.
func SetItem(v, key, value Value) error { return toError(pySetitem(v, key, value)) }

// DelItem is del v[key].
func DelItem(v, key Value) error { return toError(pyDelitem(v, key)) }

// Contains is item in container.
func Contains(container, item Value) (bool, error) {
	in, err := pyContains(container, item)
	return in, toError(err)
}

// Call is fn(*args, **kwargs).
func Call(fn Value, args []Value, kwargs []Keyword) (Value, error) {
	var kw *dictV
	if len(kwargs) > 0 {
		kw = newDict()
		for _, k := range kwargs {
			kw.setStr(k.Name, k.Value)
		}
	}
	r, err := callValue(nil, fn, append([]Value(nil), args...), kw)
	return r, toError(err)
}

// NoAttribute raises the AttributeError of reading the attribute name of an
// object whose type is named typ.
func NoAttribute(typ, name string) error {
	return toError(attrErr(typ, name))
}

// Builtin is builtins.<name>, or nil when there is no such builtin.
func Builtin(name string) Value { return pyBuiltinName(name) }

// CallMethod is v.name(*args).
func CallMethod(v Value, name string, args ...Value) (Value, error) {
	fn, err := GetAttr(v, name)
	if err != nil {
		return nil, err
	}
	return Call(fn, args, nil)
}

// BinaryOp applies a binary operator given by its ast class name (Add, Sub,
// Mult, Div, FloorDiv, Mod, Pow, LShift, RShift, BitOr, BitXor, BitAnd).
func BinaryOp(op string, a, b Value) (Value, error) {
	r, err := binop(op, a, b)
	return r, toError(err)
}

// Iterator walks the items of an iterable, as a Python iterator does.
type Iterator struct {
	it iterator
}

// Next returns the next item; ok is false at the end.
func (i *Iterator) Next() (v Value, ok bool, err error) {
	v, ok, err = i.it.next()
	return v, ok, toError(err)
}

// Iter is iter(v).
func Iter(v Value) (*Iterator, error) {
	it, err := pyIter(v)
	if err != nil {
		return nil, toError(err)
	}
	return &Iterator{it: it}, nil
}

// ForEach is `for item in v: f(item)`: it stops at the first error, of the
// iteration or of f.
func ForEach(v Value, f func(item Value) error) error {
	it, err := Iter(v)
	if err != nil {
		return err
	}
	for {
		item, ok, err := it.Next()
		if err != nil || !ok {
			return err
		}
		if err := f(item); err != nil {
			return err
		}
	}
}

// ToList is list(v).
func ToList(v Value) ([]Value, error) {
	items, err := toSlice(v)
	return items, toError(err)
}

// ExceptionOf is the exception an error of another package stands for when
// it is raised in Python code: an instance of the builtin exception class
// named class whose args are (text,), made without running the class's
// __init__, so that str() of it is text. A class this package does not know
// becomes a RuntimeError.
func ExceptionOf(class, text string) *Error {
	k := namedExcClass(class)
	if k == nil {
		return toError(newPyErr("RuntimeError", text)).(*Error)
	}
	e := &excV{Class: k.name, args: []Value{strV(text)}, Message: text}
	if k.isUser() {
		e.cls = k
	}
	return toError(e).(*Error)
}

// Unpack is the n values of `t1, ..., tn = v`, with the errors of
// unpacking ("cannot unpack non-iterable ...", "not enough values to
// unpack ...", "too many values to unpack ...").
func Unpack(v Value, n int) ([]Value, error) {
	vals, err := unpackValues(v, n)
	return vals, toError(err)
}
