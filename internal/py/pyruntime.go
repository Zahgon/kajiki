// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Runtime objects other than plain data: builtin functions and methods, classes, dict
// views, iterator objects, and Python's subscription and attribute access on all of them.

package py

import (
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// callCtx is what a builtin receives besides its arguments: the frame of
// the Python code calling it, if any.
type callCtx struct {
	f *frame
}

// callable is a builtin function or a bound builtin method.
type callable struct {
	name string
	// typ is the Python type name: "builtin_function_or_method" for C
	// functions and methods, "function" for Python functions, "method" for a bound Python method.
	typ string
	// self is the bound object of a method, nil for a function.
	self Value
	// qual names a Python-defined function or method in its repr.
	qual string
	fn   func(c *callCtx, args []Value, kw *dictV) (Value, error)
}

func (c *callable) pyType() string { return c.typ }

func (c *callable) repr(st *reprState) (string, error) {
	switch {
	case c.typ == "function":
		return "<function " + c.qual + " at " + objAddr(c) + ">", nil
	case c.typ == "method":
		s, err := reprWith(c.self, st)
		if err != nil {
			return "", err
		}
		return "<bound method " + c.qual + " of " + s + ">", nil
	case c.typ == "method-wrapper" && c.self != nil:
		return "<method-wrapper '" + c.name + "' of " + typeName(c.self) + " object at " + objAddr(c.self) + ">", nil
	case c.self == nil && (c.typ == "wrapper_descriptor" || c.typ == "method_descriptor") && strings.Contains(c.qual, "."):
		return (&methodDescV{c}).repr(st)
	case c.self == nil:
		return "<built-in function " + c.name + ">", nil
	case isClass(c.self):
		return "<built-in method " + c.name + " of type object at " + objAddr(c.self) + ">", nil
	}
	return "<built-in method " + c.name + " of " + c.self.pyType() + " object at " + objAddr(c.self) + ">", nil
}

// eq is == of builtin methods: the same function bound to the same object
// (CPython compares the function and the identity of __self__).
func (c *callable) eq(other Value) (bool, error) {
	o, ok := other.(*callable)
	if !ok {
		return false, nil
	}
	if o == c {
		return true, nil
	}
	return c.self != nil && o.self != nil && o.typ == c.typ && o.name == c.name && o.qual == c.qual &&
		c.typ != "method" && sameObject(o.self, c.self), nil
}

func newBuiltin(name string, fn func(c *callCtx, args []Value, kw *dictV) (Value, error)) *callable {
	return &callable{name: name, typ: "builtin_function_or_method", fn: fn}
}

// classV is a class: a builtin type, an exception class, or a class made by
// Python code.
type classV struct {
	name string
	// tpName is the name error messages give the type when it differs
	// from name: the C name of a class written in Go ("collections.deque").
	tpName string
	// module names the class's module in its repr, empty for builtins.
	module string
	base   *classV
	// construct calls the class.
	construct func(c *callCtx, args []Value, kw *dictV) (Value, error)
	// The following are set for classes made by Python code: the qualified
	// name, the bases, the method resolution order, the namespace and the
	// names __slots__ lists (nil without __slots__).
	qual  string
	bases []*classV
	mro   []*classV
	dict  *dictV
	slots []string
	// meta is the metaclass of a class whose metaclass is a Python-defined
	// subclass of type, nil for the other classes.
	meta *classV
	// subclasses are the classes made with this one as a direct base, in
	// the order they were made (__subclasses__()).
	subclasses []*classV
}

func (*classV) pyType() string { return "type" }

func (k *classV) repr(*reprState) (string, error) {
	if k.meta != nil {
		if m, ok := k.meta.lookupUser("__repr__"); ok {
			return callStrMethod(m, k, "__repr__")
		}
	}
	mod := k.moduleName()
	if mod != "builtins" && mod != "" {
		return "<class '" + mod + "." + k.qualName() + "'>", nil
	}
	return "<class '" + k.name + "'>", nil
}

func isClass(v Value) bool {
	_, ok := v.(*classV)
	return ok
}

// subclassOf reports whether class k is other or derives from it.
func (k *classV) subclassOf(other *classV) bool {
	if other == objectClass {
		return true
	}
	for _, c := range k.mroList() {
		if c == other {
			return true
		}
	}
	return false
}

// callValue calls a value as Python calls it.
func callValue(c *callCtx, fn Value, args []Value, kw *dictV) (Value, error) {
	if kw == nil {
		kw = newDict()
	}
	switch f := fn.(type) {
	case *callable:
		if c == nil {
			c = &callCtx{}
		}
		return f.fn(c, args, kw)
	case *classV:
		if c == nil {
			c = &callCtx{}
		}
		if f.meta != nil {
			// type(f).__call__, when the metaclass defines it.
			if m, ok := f.meta.lookupUser("__call__"); ok {
				return callValue(c, bindDescriptor(m, f, f.meta), args, kw)
			}
		}
		if f.construct == nil {
			return nil, typeErr("cannot create '%s' instances", f.name)
		}
		return f.construct(c, args, kw)
	case *funcV:
		return f.call(args, kw)
	case callableObj:
		return f.pyCall(args, kw)
	case *methodDescV:
		return f.fn(c, args, kw)
	}
	return nil, typeErr("'%s' object is not callable", typeName(fn))
}

// isCallable is Python's callable().
func isCallable(v Value) bool {
	switch v.(type) {
	case *callable, *classV, *funcV, *methodDescV:
		return true
	case callableChecker:
		return v.(callableChecker).isCallable()
	case callableObj:
		return true
	}
	return false
}

// ---------------------------------------------------------------- identity

var (
	idMu    sync.Mutex
	idTable = map[any]uint64{}
)

// objID gives each object its identity number, used for id(), for hashing
// by identity and for the addresses printed in default reprs. An object held
// by pointer has its address, which, as in CPython, a later object may have
// once it is gone; the ints -5 to 256 and the strings of at most one
// character, of which CPython keeps one object each, have one number per
// value; any other value gets a number of its own.
func objID(v any) uint64 {
	switch x := v.(type) {
	case intV:
		if x.v.IsInt64() && x.v.Int64() >= -5 && x.v.Int64() <= 256 {
			return uint64(cachedIDBase + (x.v.Int64()+5)*0x20)
		}
	case strV:
		if strLen(string(x)) <= 1 {
			r := int64(-1)
			for _, c := range string(x) {
				r = int64(c)
			}
			return uint64(cachedIDBase + 0x4000 + (r+1)*0x40)
		}
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.UnsafePointer:
		return uint64(rv.Pointer())
	}
	idMu.Lock()
	defer idMu.Unlock()
	if id, ok := idTable[v]; ok {
		return id
	}
	id := uint64(0x7f3a00001000 + len(idTable)*0x30)
	idTable[v] = id
	return id
}

// cachedIDBase is where the identity numbers of the cached ints and strings
// start.
const cachedIDBase = 0x7f3900000000

// objAddr is the address CPython prints in a default repr.
func objAddr(v any) string {
	return fmt.Sprintf("0x%x", objID(v))
}

// ---------------------------------------------------------------- dict views

type dictViewV struct {
	d    *dictV
	kind string // keys, values or items
}

func (v *dictViewV) pyType() string { return "dict_" + v.kind }

func (v *dictViewV) items() []Value {
	switch v.kind {
	case "keys":
		return append([]Value(nil), v.d.keys...)
	case "values":
		return append([]Value(nil), v.d.vals...)
	}
	out := make([]Value, len(v.d.keys))
	for i := range v.d.keys {
		out[i] = newTuple([]Value{v.d.keys[i], v.d.vals[i]})
	}
	return out
}

func (v *dictViewV) length() (int, error) { return len(v.d.keys), nil }

func (v *dictViewV) iter() (iterator, error) {
	size, i := len(v.d.keys), 0
	return funcIter(func() (Value, bool, error) {
		if len(v.d.keys) != size {
			size = -1
			return nil, false, newPyErr("RuntimeError", "dictionary changed size during iteration")
		}
		if i >= len(v.d.keys) {
			return nil, false, nil
		}
		i++
		switch v.kind {
		case "keys":
			return v.d.keys[i-1], true, nil
		case "values":
			return v.d.vals[i-1], true, nil
		}
		return newTuple([]Value{v.d.keys[i-1], v.d.vals[i-1]}), true, nil
	}), nil
}

func (v *dictViewV) contains(item Value) (bool, error) {
	switch v.kind {
	case "keys":
		_, ok, err := v.d.get(item)
		return ok, err
	case "values":
		return seqContains(v.d.vals, item)
	}
	t, ok := item.(*tupleV)
	if !ok || len(t.items) != 2 {
		return false, nil
	}
	val, found, err := v.d.get(t.items[0])
	if err != nil || !found {
		return false, err
	}
	return pyEq(val, t.items[1])
}

// getAttr holds the view's mapping attribute, which simple_translate looks for.
func (v *dictViewV) getAttr(name string) (Value, error) {
	if name == "mapping" {
		return v.d, nil
	}
	return nil, nil
}

func (v *dictViewV) repr(st *reprState) (string, error) {
	if !st.enter(v) {
		return "...", nil
	}
	defer st.leave()
	s, err := seqRepr("[", "]", v.items(), false, st)
	if err != nil {
		return "", err
	}
	return v.pyType() + "(" + s + ")", nil
}

// ---------------------------------------------------------------- iterators

// iterObj is an iterator object: map, filter, zip, enumerate, reversed and
// the iterators of the builtin containers.
type iterObj struct {
	typ string
	it  iterator
}

func (o *iterObj) pyType() string { return o.typ }

func (o *iterObj) iter() (iterator, error) { return o.it, nil }

// ---------------------------------------------------------------- subscription

// pyGetitem is obj[key].
func pyGetitem(obj, key Value) (Value, error) {
	if x, ok := obj.(*instV); ok {
		return x.getItem(key)
	}
	obj = unsub(obj)
	switch x := obj.(type) {
	case getitemer:
		return x.getItem(key)
	case *listV:
		return seqGetitem("list", x.items, key, func(items []Value) Value { return newList(items) })
	case *tupleV:
		return seqGetitem("tuple", x.items, key, func(items []Value) Value { return newTuple(items) })
	case strV:
		if sl, ok := key.(*sliceV); ok {
			s, err := pySliceStr(string(x), sl.start, sl.stop, sl.step)
			return strV(s), err
		}
		rs := strRunes(string(x))
		i, ok, err := pyIndexNorm(len(rs), key)
		if err == errNotIndex {
			return nil, pyIndexTypeErr("str", key)
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, pyIndexRangeErr("str")
		}
		return strV(runesStr(rs[i : i+1])), nil
	case bytesV, *bytearrayV:
		b, _ := bytesOf(x)
		if sl, ok := key.(*sliceV); ok {
			start, stop, step, err := pySliceIndices(len(b), sl.start, sl.stop, sl.step)
			if err != nil {
				return nil, err
			}
			var out []byte
			for i, n := start, pySliceLen(start, stop, step); n > 0; i, n = i+step, n-1 {
				out = append(out, b[i])
			}
			if _, isArr := x.(*bytearrayV); isArr {
				return &bytearrayV{b: out}, nil
			}
			return bytesV(out), nil
		}
		i, ok, err := pyIndexNorm(len(b), key)
		if err == errNotIndex {
			return nil, typeErr("byte indices must be integers or slices, not %s", key.pyType())
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			if _, isArr := x.(*bytearrayV); isArr {
				return nil, newPyErr("IndexError", "bytearray index out of range")
			}
			return nil, newPyErr("IndexError", "index out of range")
		}
		return mkInt(int64(b[i])), nil
	case rangeV:
		if sl, ok := key.(*sliceV); ok {
			n := int(x.length())
			start, stop, step, err := pySliceIndices(n, sl.start, sl.stop, sl.step)
			if err != nil {
				return nil, err
			}
			return rangeV{start: x.at(int64(start)), stop: x.at(int64(stop)), step: x.step * int64(step)}, nil
		}
		i, ok, err := pyIndexNorm(int(x.length()), key)
		if err == errNotIndex {
			return nil, typeErr("range indices must be integers or slices, not %s", key.pyType())
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, newPyErr("IndexError", "range object index out of range")
		}
		return mkInt(x.at(int64(i))), nil
	case *dictV:
		v, ok, err := x.get(key)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, newExc("KeyError", []Value{key})
		}
		return v, nil
	case *classV:
		return classGetitem(x, key)
	}
	return nil, typeErr("'%s' object is not subscriptable", obj.pyType())
}

func seqGetitem(name string, items []Value, key Value, mk func([]Value) Value) (Value, error) {
	if sl, ok := key.(*sliceV); ok {
		out, err := pySliceSeq(items, sl.start, sl.stop, sl.step)
		if err != nil {
			return nil, err
		}
		return mk(out), nil
	}
	i, ok, err := pyIndexNorm(len(items), key)
	if err == errNotIndex {
		return nil, pyIndexTypeErr(name, key)
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, pyIndexRangeErr(name)
	}
	return items[i], nil
}

// pySetitem is obj[key] = value.
func pySetitem(obj, key, value Value) error {
	switch x := obj.(type) {
	case setitemer:
		return x.setItem(key, value)
	case *listV:
		if sl, ok := key.(*sliceV); ok {
			return listSetSlice(x, sl, value)
		}
		i, ok, err := pyIndexNorm(len(x.items), key)
		if err == errNotIndex {
			return pyIndexTypeErr("list", key)
		}
		if err != nil {
			return err
		}
		if !ok {
			return newPyErr("IndexError", "list assignment index out of range")
		}
		x.items[i] = value
		return nil
	case *dictV:
		return x.set(key, value)
	case *bytearrayV:
		if sl, ok := key.(*sliceV); ok {
			return bytearraySetSlice(x, sl, value)
		}
		i, ok, err := pyIndexNorm(len(x.b), key)
		if err == errNotIndex {
			return typeErr("bytearray indices must be integers or slices, not %s", key.pyType())
		}
		if err != nil {
			return err
		}
		if !ok {
			return newPyErr("IndexError", "bytearray index out of range")
		}
		n, isInt := asInt(value)
		if !isInt {
			return typeErr("'%s' object cannot be interpreted as an integer", value.pyType())
		}
		if !n.IsInt64() || n.Int64() < 0 || n.Int64() > 255 {
			return newPyErr("ValueError", "byte must be in range(0, 256)")
		}
		x.b[i] = byte(n.Int64())
		return nil
	}
	return typeErr("'%s' object does not support item assignment", obj.pyType())
}

// bytearraySetSlice is ba[slice] = value: the value is bytes-like or an
// iterable of ints in range(256).
func bytearraySetSlice(ba *bytearrayV, sl *sliceV, value Value) error {
	start, stop, step, err := pySliceIndices(len(ba.b), sl.start, sl.stop, sl.step)
	if err != nil {
		return err
	}
	var src []byte
	if b, ok := bytesOf(unsub(value)); ok {
		src = []byte(b)
	} else {
		_, isInt := asInt(value)
		_, isStr := unsub(value).(strV)
		if isInt || isStr {
			return typeErr("can assign only bytes, buffers, or iterables of ints in range(0, 256)")
		}
		items, err := toSlice(value)
		if err != nil {
			if isExc(err, "TypeError") {
				return typeErr("can assign only bytes, buffers, or iterables of ints in range(0, 256)")
			}
			return err
		}
		src = make([]byte, len(items))
		for i, it := range items {
			if src[i], err = byteValue(it); err != nil {
				return err
			}
		}
	}
	if step == 1 {
		if stop < start {
			stop = start
		}
		out := append([]byte(nil), ba.b[:start]...)
		out = append(out, src...)
		ba.b = append(out, ba.b[stop:]...)
		return nil
	}
	n := pySliceLen(start, stop, step)
	if len(src) != n {
		return valueErr("attempt to assign bytes of size %d to extended slice of size %d", len(src), n)
	}
	for i, j := start, 0; j < n; i, j = i+step, j+1 {
		ba.b[i] = src[j]
	}
	return nil
}

// listSetSlice is list[slice] = iterable.
func listSetSlice(l *listV, sl *sliceV, value Value) error {
	start, stop, step, err := pySliceIndices(len(l.items), sl.start, sl.stop, sl.step)
	if err != nil {
		return err
	}
	var items []Value
	if value == Value(l) {
		items = append([]Value(nil), l.items...)
	} else {
		items, err = toSlice(value)
		if err != nil {
			if isExc(err, "TypeError") {
				if step == 1 {
					return typeErr("can only assign an iterable")
				}
				return typeErr("must assign iterable to extended slice")
			}
			return err
		}
	}
	if step == 1 {
		if stop < start {
			stop = start
		}
		out := append([]Value(nil), l.items[:start]...)
		out = append(out, items...)
		l.items = append(out, l.items[stop:]...)
		return nil
	}
	n := pySliceLen(start, stop, step)
	if len(items) != n {
		return newPyErr("ValueError", "attempt to assign sequence of size "+strconv.Itoa(len(items))+" to extended slice of size "+strconv.Itoa(n))
	}
	for i, j := start, 0; j < n; i, j = i+step, j+1 {
		l.items[i] = items[j]
	}
	return nil
}

// pyDelitem is del obj[key].
func pyDelitem(obj, key Value) error {
	switch x := obj.(type) {
	case delitemer:
		return x.delItem(key)
	case *listV:
		if sl, ok := key.(*sliceV); ok {
			start, stop, step, err := pySliceIndices(len(x.items), sl.start, sl.stop, sl.step)
			if err != nil {
				return err
			}
			n := pySliceLen(start, stop, step)
			drop := map[int]bool{}
			for i, j := start, 0; j < n; i, j = i+step, j+1 {
				drop[i] = true
			}
			var out []Value
			for i, it := range x.items {
				if !drop[i] {
					out = append(out, it)
				}
			}
			x.items = out
			return nil
		}
		i, ok, err := pyIndexNorm(len(x.items), key)
		if err == errNotIndex {
			return pyIndexTypeErr("list", key)
		}
		if err != nil {
			return err
		}
		if !ok {
			return newPyErr("IndexError", "list assignment index out of range")
		}
		x.items = append(x.items[:i:i], x.items[i+1:]...)
		return nil
	case *dictV:
		found, err := x.del(key)
		if err != nil {
			return err
		}
		if !found {
			return newExc("KeyError", []Value{key})
		}
		return nil
	case *bytearrayV:
		if sl, ok := key.(*sliceV); ok {
			start, stop, step, err := pySliceIndices(len(x.b), sl.start, sl.stop, sl.step)
			if err != nil {
				return err
			}
			n := pySliceLen(start, stop, step)
			drop := map[int]bool{}
			for i, j := start, 0; j < n; i, j = i+step, j+1 {
				drop[i] = true
			}
			var out []byte
			for i, c := range x.b {
				if !drop[i] {
					out = append(out, c)
				}
			}
			x.b = out
			return nil
		}
		i, ok, err := pyIndexNorm(len(x.b), key)
		if err == errNotIndex {
			return typeErr("bytearray indices must be integers or slices, not %s", key.pyType())
		}
		if err != nil {
			return err
		}
		if !ok {
			return newPyErr("IndexError", "bytearray index out of range")
		}
		x.b = append(x.b[:i:i], x.b[i+1:]...)
		return nil
	}
	return typeErr("'%s' object doesn't support item deletion", obj.pyType())
}

// ---------------------------------------------------------------- attributes

// pyGetattr is getattr(obj, name) without a default: the attribute, or
// Python's AttributeError.
func pyGetattr(obj Value, name string) (Value, error) {
	v, err := getattrOf(obj, name)
	if err != nil {
		if x, ok := err.(*excV); ok && excIsInstance(x, "AttributeError") &&
			isNoneV(x.field("name")) && isNoneV(x.field("obj")) {
			// The lookup names the attribute and the object in the
			// AttributeError, as PyObject_GetAttr does.
			x.setField("name", strV(name))
			x.setField("obj", obj)
		}
	}
	return v, err
}

// getattrOf is pyGetattr without the name and obj of the AttributeError.
func getattrOf(obj Value, name string) (Value, error) {
	if ga, ok := obj.(attrGetter); ok {
		v, err := ga.getAttr(name)
		if v != nil || err != nil {
			return v, err
		}
	}
	switch x := obj.(type) {
	case *classV:
		if x.isUser() {
			return userClassGetattr(x, name)
		}
		if v, ok := classGetattr(x, name); ok {
			return v, nil
		}
	}
	if v, ok := attrOf(unsub(obj), name); ok {
		return v, nil
	}
	if v, ok := commonAttr(obj, name); ok {
		return v, nil
	}
	if k, ok := obj.(*classV); ok {
		return nil, typeAttrErr(k.name, name)
	}
	return nil, attrErr(typeName(obj), name)
}

// strSubclassAttr is an attribute of an instance of a str subclass whose
// value is s: the str methods, of which format and
// format_map hand back the instance itself when the format string has no
// replacement field, as CPython's string writer does.
func strSubclassAttr(self Value, s string, name string) (Value, bool) {
	m, ok := attrOf(strV(s), name)
	if !ok {
		return nil, false
	}
	if c, isCallable := m.(*callable); isCallable && c.self != nil {
		bound := *c
		bound.self = self
		m = &bound
	}
	if (name == "format" || name == "format_map") && s != "" && !strings.ContainsAny(s, "{}") {
		inner := m.(*callable)
		inner.self = strV(s)
		return &callable{name: name, typ: inner.typ, self: self,
			fn: func(c *callCtx, args []Value, kw *dictV) (Value, error) {
				if _, err := inner.fn(c, args, kw); err != nil {
					return nil, err
				}
				return self, nil
			}}, true
	}
	return m, true
}

// attrOf looks an attribute up on the value's type and the value.
func attrOf(obj Value, name string) (Value, bool) {
	switch x := obj.(type) {
	case *excV:
		if name == "args" {
			return newTuple(append([]Value(nil), x.args...)), true
		}
		return excMethod(x, name)
	case *dictV:
		return dictMethod(obj, x, name, false)
	case *setV:
		return setMethod(x, name)
	case *sliceV:
		switch name {
		case "start":
			return x.start, true
		case "stop":
			return x.stop, true
		case "step":
			return x.step, true
		case "indices":
			return sliceIndicesMethod(x), true
		}
		return nil, false
	case complexV:
		return complexMethod(complex128(x), name)
	case bytesV, *bytearrayV:
		return bytesMethod(obj, name)
	case *classV:
		if m, ok := objectClassMethod(x, name); ok {
			return m, true
		}
		return classAttr(x, name)
	}
	return builtinAttr(obj, name)
}

// pySetattr is setattr(obj, name, value).
func pySetattr(obj Value, name string, value Value) error {
	if sa, ok := obj.(attrSetter); ok {
		return sa.setAttr(name, value)
	}
	switch x := obj.(type) {
	case *excV:
		switch name {
		case "args":
			items, err := toSlice(value)
			if err != nil {
				return err
			}
			x.args = items
			x.Message = excStr(x.Class, items)
			return nil
		case "__cause__", "__context__", "__suppress_context__", "__traceback__":
			return x.setChainAttr(name, value)
		}
		if x.isExcField(name) {
			x.setField(name, value)
			return nil
		}
		if x.cls != nil {
			return instanceSetattr(x, x.cls, x.attrDict(), name, value)
		}
		x.attrDict().setStr(name, value)
		return nil
	case *classV:
		return classSetattr(x, name, value)
	}
	if _, ok := attrOf(obj, name); ok {
		return newPyErr("AttributeError", "'"+obj.pyType()+"' object attribute '"+name+"' is read-only")
	}
	return attrErr(obj.pyType(), name)
}

// pyDelattr is delattr(obj, name).
func pyDelattr(obj Value, name string) error {
	if da, ok := obj.(attrDeleter); ok {
		return da.delAttr(name)
	}
	if k, ok := obj.(*classV); ok {
		return classDelattr(k, name)
	}
	if _, ok := attrOf(obj, name); ok {
		return newPyErr("AttributeError", "'"+obj.pyType()+"' object attribute '"+name+"' is read-only")
	}
	return attrErr(obj.pyType(), name)
}

var bigOne = big.NewInt(1)

// getattrOpt is getattr(v, name, None) as hasattr sees it: nil when v has no
// such attribute, the error when looking it up raises anything else.
func getattrOpt(v Value, name string) (Value, error) {
	a, err := pyGetattr(v, name)
	if err != nil {
		if isExc(err, "AttributeError") {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}
