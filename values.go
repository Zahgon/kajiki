// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/util.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"math/big"

	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// Value is a Python value. It is implemented only by the values this package
// creates.
type Value interface {
	pyValue()
}

// value is a Value: a value of the Python runtime.
type value struct{ v py.Value }

func (value) pyValue() {}

// pyOf is the Python value of v; a nil Value is None.
func pyOf(v Value) py.Value {
	if pv, ok := v.(value); ok {
		return pv.v
	}
	return py.None()
}

func pyValues(vs []Value) []py.Value {
	out := make([]py.Value, len(vs))
	for i, v := range vs {
		out[i] = pyOf(v)
	}
	return out
}

// None returns None.
func None() Value { return value{py.None()} }

// Bool returns True or False.
func Bool(b bool) Value { return value{py.Bool(b)} }

// Int returns an int.
func Int(i int64) Value { return value{py.Int(i)} }

// BigInt returns an int of any size. The argument is copied.
func BigInt(i *big.Int) Value { return value{py.BigInt(i)} }

// Float returns a float.
func Float(f float64) Value { return value{py.Float(f)} }

// String returns a str holding s, which must be valid UTF-8.
func String(s string) Value { return value{py.String(s)} }

// Bytes returns a bytes object. The argument is copied.
func Bytes(b []byte) Value { return value{py.Bytes(b)} }

// List returns a new list holding the items.
func List(items ...Value) Value { return value{py.List(pyValues(items)...)} }

// Tuple returns a tuple holding the items.
func Tuple(items ...Value) Value { return value{py.Tuple(pyValues(items)...)} }

// Set returns a new set holding the items, or the TypeError Python raises for
// an unhashable item.
func Set(items ...Value) (Value, error) {
	s, err := py.Set(pyValues(items)...)
	if err != nil {
		return nil, newError(err)
	}
	return value{s}, nil
}

// NewDict returns dict(zip(keys, values)), or the TypeError Python raises for an
// unhashable key.
func NewDict(keys, values []Value) (Value, error) {
	n := min(len(keys), len(values))
	d, err := py.NewDict(pyValues(keys[:n]), pyValues(values[:n]))
	if err != nil {
		return nil, newError(err)
	}
	return value{d}, nil
}

// Literal returns kajiki.util.literal(text).
func Literal(text string) Value {
	v, err := py.Call(literal, []py.Value{py.String(text)}, nil)
	if err != nil {
		panic(err)
	}
	return value{v}
}

// Var is one variable of a template's context or of its base_globals.
type Var struct {
	Name  string
	Value Value
}

// varsDict is dict(vars): a later variable of the same name wins.
func varsDict(vars []Var) py.Value {
	d := py.EmptyDict()
	for _, v := range vars {
		if err := py.SetItem(d, py.String(v.Name), pyOf(v.Value)); err != nil {
			panic(err)
		}
	}
	return d
}
