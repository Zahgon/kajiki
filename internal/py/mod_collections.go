// Ported from CPython 3.12 (Lib/collections/__init__.py, Modules/_collectionsmodule.c,
// Objects/odictobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

func init() {
	register("collections", func(b *builder) {
		dequeIter := b.newClass("_deque_iterator", []Value{Builtin("object")}, iterMethods("_deque_iterator"))
		b.cClass("deque", []Value{Builtin("object")}, dequeMethods(dequeIter))
		dict := Builtin("dict")
		b.cClass("defaultdict", []Value{dict}, defaultdictMethods())
		b.cClass("OrderedDict", []Value{dict}, orderedDictMethods())
		b.class("Counter", []Value{dict}, counterMethods())
		b.class("ChainMap", []Value{Builtin("object")}, chainMapMethods())
		b.pyfn("namedtuple", namedtuple)
		// collections is a package whose abc submodule the interpreter has
		// imported by the time a program runs.
		b.set("__path__", List())
		if b.err == nil {
			_, b.err = ImportModule("collections.abc", "")
		}
	})
}

// iterMethods is the namespace of an iterator class over *goIter states
// made by Go code.
func iterMethods(name string) *methods {
	ms := &methods{}
	addIterMethods(ms, name)
	return ms
}

// ---------------------------------------------------------------- deque

// dequeState is a deque: its items, its maximum length (-1 for none), and
// a count of the changes, which iterators check.
type dequeState struct {
	items  []Value
	maxlen int
	state  int
}

func dequeOf(self Value, method string) (*dequeState, error) {
	return stateOf[*dequeState](self, "collections.deque", method)
}

// push adds v at the right (or the left), dropping an item from the other
// end when the deque is full.
func (d *dequeState) push(v Value, left bool) {
	d.state++
	if d.maxlen == 0 {
		return
	}
	if d.maxlen > 0 && len(d.items) == d.maxlen {
		if left {
			d.items = d.items[:len(d.items)-1]
		} else {
			d.items = d.items[1:]
		}
	}
	if left {
		d.items = append([]Value{v}, d.items...)
	} else {
		d.items = append(d.items, v)
	}
}

// extend adds the items of an iterable at one end.
func (d *dequeState) extend(self, iterable Value, left bool) error {
	var items []Value
	var err error
	if Is(self, iterable) {
		items = append([]Value(nil), d.items...)
	} else if items, err = ToList(iterable); err != nil {
		return err
	}
	for _, v := range items {
		d.push(v, left)
	}
	return nil
}

// index normalises a deque index, with the IndexError of one out of range.
func (d *dequeState) index(v Value) (int, error) {
	n, ok := AsInt(v)
	if !ok {
		return 0, raise("TypeError", "sequence index must be integer, not '%s'", TypeName(v))
	}
	i := len(d.items)
	if n.IsInt64() {
		i = int(n.Int64())
		if i < 0 {
			i += len(d.items)
		}
	}
	if i < 0 || i >= len(d.items) {
		return 0, raise("IndexError", "deque index out of range")
	}
	return i, nil
}

// newDequeLike makes a new deque of self's class holding items, with
// self's maxlen.
func newDequeLike(self Value, d *dequeState, items []Value) (Value, error) {
	var maxlen Value = None()
	if d.maxlen >= 0 {
		maxlen = Int(int64(d.maxlen))
	}
	return Call(TypeOf(self), []Value{List(items...), maxlen}, nil)
}

// seqCompare is deque_richcompare: the first pair of items that differ
// decides, else the lengths do.
func seqCompare(op string, a, b []Value) (Value, error) {
	i, err := seqFirstDiff(a, b, func(x, y Value) (bool, error) { return Compare("==", x, y) })
	if err != nil {
		return nil, err
	}
	if i >= len(a) || i >= len(b) {
		return RichCompare(op, Int(int64(len(a))), Int(int64(len(b))))
	}
	switch op {
	case "==":
		return Bool(false), nil
	case "!=":
		return Bool(true), nil
	}
	return RichCompare(op, a[i], b[i])
}

func dequeMethods(iterCls Value) *methods {
	ms := &methods{}
	self := func(args []Value, name string) (*dequeState, error) { return dequeOf(args[0], name) }
	noArgs := func(name string, f func(d *dequeState, self Value) (Value, error)) {
		ms.method(name, func(args []Value, kwargs []Keyword) (Value, error) {
			d, err := self(args, name)
			if err != nil {
				return nil, err
			}
			if err := ArgsNone("deque."+name, args[1:], kwargs); err != nil {
				return nil, err
			}
			return f(d, args[0])
		})
	}
	oneArg := func(name string, f func(d *dequeState, self, v Value) (Value, error)) {
		ms.method(name, func(args []Value, kwargs []Keyword) (Value, error) {
			d, err := self(args, name)
			if err != nil {
				return nil, err
			}
			if err := ArgsOne("deque."+name, args[1:], kwargs); err != nil {
				return nil, err
			}
			return f(d, args[0], args[1])
		})
	}
	ms.method("__new__", func(args []Value, _ []Keyword) (Value, error) {
		return instance(args[0], &dequeState{maxlen: -1})
	})
	ms.method("__init__", func(args []Value, kwargs []Keyword) (Value, error) {
		d, err := self(args, "__init__")
		if err != nil {
			return nil, err
		}
		buf, err := ParseArgs("deque", []string{"iterable", "maxlen"}, 0, 2, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		d.maxlen = -1
		if buf[1] != nil && !IsNone(buf[1]) {
			n, err := Ssize(buf[1])
			if err != nil {
				return nil, err
			}
			if n < 0 {
				return nil, raise("ValueError", "maxlen must be non-negative")
			}
			d.maxlen = n
		}
		d.items = nil
		d.state++
		if buf[0] != nil {
			return nil, d.extend(args[0], buf[0], false)
		}
		return nil, nil
	})
	oneArg("append", func(d *dequeState, _, v Value) (Value, error) { d.push(v, false); return nil, nil })
	oneArg("appendleft", func(d *dequeState, _, v Value) (Value, error) { d.push(v, true); return nil, nil })
	oneArg("extend", func(d *dequeState, s, v Value) (Value, error) { return nil, d.extend(s, v, false) })
	oneArg("extendleft", func(d *dequeState, s, v Value) (Value, error) { return nil, d.extend(s, v, true) })
	pop := func(left bool) func(d *dequeState, _ Value) (Value, error) {
		return func(d *dequeState, _ Value) (Value, error) {
			if len(d.items) == 0 {
				return nil, raise("IndexError", "pop from an empty deque")
			}
			d.state++
			if left {
				v := d.items[0]
				d.items = d.items[1:]
				return v, nil
			}
			v := d.items[len(d.items)-1]
			d.items = d.items[:len(d.items)-1]
			return v, nil
		}
	}
	noArgs("pop", pop(false))
	noArgs("popleft", pop(true))
	noArgs("clear", func(d *dequeState, _ Value) (Value, error) { d.items = nil; d.state++; return nil, nil })
	noArgs("copy", func(d *dequeState, s Value) (Value, error) { return newDequeLike(s, d, d.items) })
	noArgs("__copy__", func(d *dequeState, s Value) (Value, error) { return newDequeLike(s, d, d.items) })
	noArgs("reverse", func(d *dequeState, _ Value) (Value, error) {
		for i, j := 0, len(d.items)-1; i < j; i, j = i+1, j-1 {
			d.items[i], d.items[j] = d.items[j], d.items[i]
		}
		d.state++
		return nil, nil
	})
	ms.method("rotate", func(args []Value, kwargs []Keyword) (Value, error) {
		d, err := self(args, "rotate")
		if err != nil {
			return nil, err
		}
		if err := NoKeywords("rotate", kwargs); err != nil {
			return nil, err
		}
		if err := CheckPositional("rotate", len(args)-1, 0, 1); err != nil {
			return nil, err
		}
		n := 1
		if len(args) == 2 {
			if n, err = Ssize(args[1]); err != nil {
				return nil, err
			}
		}
		if l := len(d.items); l > 1 {
			n %= l
			if n < 0 {
				n += l
			}
			d.items = append(append([]Value(nil), d.items[l-n:]...), d.items[:l-n]...)
		}
		d.state++
		return nil, nil
	})
	oneArg("count", func(d *dequeState, _, v Value) (Value, error) {
		n, err := seqScanCount(d.items, v, pyCompareEq)
		if err != nil {
			return nil, err
		}
		return Int(int64(n)), nil
	})
	ms.method("index", func(args []Value, kwargs []Keyword) (Value, error) {
		d, err := self(args, "index")
		if err != nil {
			return nil, err
		}
		if err := NoKeywords("index", kwargs); err != nil {
			return nil, err
		}
		if err := CheckPositional("index", len(args)-1, 1, 3); err != nil {
			return nil, err
		}
		start, stop := 0, len(d.items)
		bound := func(v Value) (int, error) {
			i, err := Ssize(v)
			if err != nil {
				return 0, err
			}
			if i < 0 {
				i = max(i+len(d.items), 0)
			}
			return min(i, len(d.items)), nil
		}
		if len(args) > 2 {
			if start, err = bound(args[2]); err != nil {
				return nil, err
			}
		}
		if len(args) > 3 {
			if stop, err = bound(args[3]); err != nil {
				return nil, err
			}
		}
		i, err := seqScan(d.items, args[1], start, stop, pyCompareEq)
		if err != nil {
			return nil, err
		}
		if i >= 0 {
			return Int(int64(i)), nil
		}
		r, err := Repr(args[1])
		if err != nil {
			return nil, err
		}
		return nil, raise("ValueError", "%s is not in deque", r)
	})
	ms.method("insert", func(args []Value, kwargs []Keyword) (Value, error) {
		d, err := self(args, "insert")
		if err != nil {
			return nil, err
		}
		if err := NoKeywords("insert", kwargs); err != nil {
			return nil, err
		}
		if err := CheckPositional("insert", len(args)-1, 2, 2); err != nil {
			return nil, err
		}
		i, err := Ssize(args[1])
		if err != nil {
			return nil, err
		}
		if d.maxlen >= 0 && len(d.items) >= d.maxlen {
			return nil, raise("IndexError", "deque already at its maximum size")
		}
		n := len(d.items)
		if i < 0 {
			i = max(i+n, 0)
		}
		i = min(i, n)
		d.items = append(d.items[:i], append([]Value{args[2]}, d.items[i:]...)...)
		d.state++
		return nil, nil
	})
	oneArg("remove", func(d *dequeState, _, v Value) (Value, error) {
		i, err := seqScan(d.items, v, 0, len(d.items), pyCompareEq)
		if err != nil {
			return nil, err
		}
		if i >= 0 {
			d.items = append(d.items[:i], d.items[i+1:]...)
			d.state++
			return nil, nil
		}
		return nil, raise("ValueError", "deque.remove(x): x not in deque")
	})
	ms.property("maxlen", func(s Value) (Value, error) {
		d, err := dequeOf(s, "maxlen")
		if err != nil {
			return nil, err
		}
		if d.maxlen < 0 {
			return None(), nil
		}
		return Int(int64(d.maxlen)), nil
	})
	noArgs("__len__", func(d *dequeState, _ Value) (Value, error) { return Int(int64(len(d.items))), nil })
	noArgs("__bool__", func(d *dequeState, _ Value) (Value, error) { return Bool(len(d.items) > 0), nil })
	oneArg("__getitem__", func(d *dequeState, _, k Value) (Value, error) {
		i, err := d.index(k)
		if err != nil {
			return nil, err
		}
		return d.items[i], nil
	})
	ms.method("__setitem__", func(args []Value, kwargs []Keyword) (Value, error) {
		d, err := self(args, "__setitem__")
		if err != nil {
			return nil, err
		}
		i, err := d.index(args[1])
		if err != nil {
			return nil, err
		}
		d.items[i] = args[2]
		return nil, nil
	})
	oneArg("__delitem__", func(d *dequeState, _, k Value) (Value, error) {
		i, err := d.index(k)
		if err != nil {
			return nil, err
		}
		d.items = append(d.items[:i], d.items[i+1:]...)
		d.state++
		return nil, nil
	})
	oneArg("__contains__", func(d *dequeState, _, v Value) (Value, error) {
		i, err := seqScan(d.items, v, 0, len(d.items), pyCompareEq)
		if err != nil {
			return nil, err
		}
		return Bool(i >= 0), nil
	})
	iterOver := func(reverse bool) func(d *dequeState, _ Value) (Value, error) {
		return func(d *dequeState, _ Value) (Value, error) {
			state, i := d.state, 0
			return instance(iterCls, &goIter{next: func() (Value, bool, error) {
				if d.state != state {
					return nil, false, raise("RuntimeError", "deque mutated during iteration")
				}
				if i >= len(d.items) {
					return nil, false, nil
				}
				i++
				if reverse {
					return d.items[len(d.items)-i], true, nil
				}
				return d.items[i-1], true, nil
			}})
		}
	}
	noArgs("__iter__", iterOver(false))
	noArgs("__reversed__", iterOver(true))
	noArgs("__repr__", func(d *dequeState, s Value) (Value, error) {
		items, err := Repr(List(d.items...))
		if err != nil {
			return nil, err
		}
		name := ClassName(TypeOf(s))
		if d.maxlen >= 0 {
			return String(name + "(" + items + ", maxlen=" + strconv.Itoa(d.maxlen) + ")"), nil
		}
		return String(name + "(" + items + ")"), nil
	})
	for _, op := range []string{"==", "!=", "<", "<=", ">", ">="} {
		op := op
		name := map[string]string{"==": "__eq__", "!=": "__ne__", "<": "__lt__", "<=": "__le__", ">": "__gt__", ">=": "__ge__"}[op]
		oneArg(name, func(d *dequeState, _, other Value) (Value, error) {
			o, ok := Native(other).(*dequeState)
			if !ok {
				return NotImplemented(), nil
			}
			return seqCompare(op, d.items, o.items)
		})
	}
	oneArg("__add__", func(d *dequeState, s, other Value) (Value, error) {
		o, ok := Native(other).(*dequeState)
		if !ok {
			return nil, raise("TypeError", "can only concatenate deque (not \"%s\") to deque", TypeName(other))
		}
		r, err := newDequeLike(s, d, d.items)
		if err != nil {
			return nil, err
		}
		rd, _ := Native(r).(*dequeState)
		for _, v := range o.items {
			rd.push(v, false)
		}
		return r, nil
	})
	oneArg("__iadd__", func(d *dequeState, s, other Value) (Value, error) {
		return s, d.extend(s, other, false)
	})
	repeat := func(d *dequeState, n Value) ([]Value, error) {
		k, err := Ssize(n)
		if err != nil {
			return nil, err
		}
		var out []Value
		for i := 0; i < k; i++ {
			out = append(out, d.items...)
		}
		return out, nil
	}
	oneArg("__mul__", func(d *dequeState, s, n Value) (Value, error) {
		items, err := repeat(d, n)
		if err != nil {
			return nil, err
		}
		return newDequeLike(s, d, items)
	})
	oneArg("__rmul__", func(d *dequeState, s, n Value) (Value, error) {
		items, err := repeat(d, n)
		if err != nil {
			return nil, err
		}
		return newDequeLike(s, d, items)
	})
	oneArg("__imul__", func(d *dequeState, s, n Value) (Value, error) {
		items, err := repeat(d, n)
		if err != nil {
			return nil, err
		}
		d.items = nil
		for _, v := range items {
			d.push(v, false)
		}
		return s, nil
	})
	ms.add("__hash__", None())
	return ms
}

// ---------------------------------------------------------------- defaultdict

// defaultFactory is the state of a defaultdict: its default_factory.
type defaultFactory struct{ factory Value }

func factoryOf(self Value) (*defaultFactory, error) {
	return stateOf[*defaultFactory](self, "collections.defaultdict", "default_factory")
}

func defaultdictMethods() *methods {
	ms := &methods{}
	ms.method("__init__", func(args []Value, kwargs []Keyword) (Value, error) {
		var factory Value = None()
		rest := args[1:]
		if len(rest) > 0 {
			factory = rest[0]
			rest = rest[1:]
			if !IsNone(factory) && !IsCallable(factory) {
				return nil, raise("TypeError", "first argument must be callable or None")
			}
		}
		if err := SetNative(args[0], &defaultFactory{factory: factory}); err != nil {
			return nil, err
		}
		return CallSuper(TypeOf(args[0]), args[0], "__init__", rest, kwargs)
	})
	ms.method("__missing__", func(args []Value, kwargs []Keyword) (Value, error) {
		if err := ArgsOne("__missing__", args[1:], kwargs); err != nil {
			return nil, err
		}
		f, err := factoryOf(args[0])
		if err != nil {
			return nil, err
		}
		if IsNone(f.factory) {
			return nil, Raise("KeyError", Tuple(args[1]))
		}
		v, err := call(f.factory)
		if err != nil {
			return nil, err
		}
		return v, SetItem(args[0], args[1], v)
	})
	ms.getset("default_factory",
		func(args []Value, _ []Keyword) (Value, error) {
			f, err := factoryOf(args[0])
			if err != nil {
				return nil, err
			}
			return f.factory, nil
		},
		func(args []Value, _ []Keyword) (Value, error) {
			f, err := factoryOf(args[0])
			if err != nil {
				return nil, err
			}
			f.factory = args[1]
			return nil, nil
		})
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		f, err := factoryOf(args[0])
		if err != nil {
			return nil, err
		}
		fr := "None"
		if !IsNone(f.factory) {
			if fr, err = Repr(f.factory); err != nil {
				return nil, err
			}
		}
		base, err := baseDictRepr(args[0])
		if err != nil {
			return nil, err
		}
		return String(ClassName(TypeOf(args[0])) + "(" + fr + ", " + base + ")"), nil
	})
	copyFn := func(args []Value, _ []Keyword) (Value, error) {
		f, err := factoryOf(args[0])
		if err != nil {
			return nil, err
		}
		return call(TypeOf(args[0]), f.factory, args[0])
	}
	ms.method("copy", copyFn)
	ms.method("__copy__", copyFn)
	ms.method("__or__", func(args []Value, _ []Keyword) (Value, error) {
		if !IsDict(baseOrSelf(args[1])) {
			return NotImplemented(), nil
		}
		n, err := copyFn(args[:1], nil)
		if err != nil {
			return nil, err
		}
		_, err = CallMethod(n, "update", args[1])
		return n, err
	})
	return ms
}

// baseDictRepr is dict.__repr__(v) for a dict or an instance of a subclass.
func baseDictRepr(v Value) (string, error) {
	base := v
	if b, ok := BaseValue(v); ok {
		base = b
	}
	return Repr(base)
}

// dictKeys is list(d) of a dict or an instance of a subclass, without
// calling an overridden __iter__.
func dictKeys(v Value) []Value {
	keys, _ := dictPairs(v)
	return keys
}

// dictPairs is the items of a dict or of an instance of a subclass.
func dictPairs(v Value) ([]Value, []Value) {
	base := v
	if b, ok := BaseValue(v); ok {
		base = b
	}
	keys, vals, _ := DictItems(base)
	return keys, vals
}

// ---------------------------------------------------------------- OrderedDict

func orderedDictMethods() *methods {
	ms := &methods{}
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		name := ClassName(TypeOf(args[0]))
		keys := dictKeys(args[0])
		if len(keys) == 0 {
			return String(name + "()"), nil
		}
		base, err := baseDictRepr(args[0])
		if err != nil {
			return nil, err
		}
		return String(name + "(" + base + ")"), nil
	})
	ms.method("move_to_end", func(args []Value, kwargs []Keyword) (Value, error) {
		buf, err := ParseArgs("move_to_end", []string{"key", "last"}, 1, 2, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		last := true
		if buf[1] != nil {
			if last, err = Truth(buf[1]); err != nil {
				return nil, err
			}
		}
		base, _ := BaseValue(args[0])
		v, err := GetItem(base, buf[0])
		if err != nil {
			return nil, err
		}
		if err := DelItem(base, buf[0]); err != nil {
			return nil, err
		}
		if last {
			return nil, SetItem(base, buf[0], v)
		}
		keys, vals := dictPairs(base)
		if _, err := CallMethod(base, "clear"); err != nil {
			return nil, err
		}
		if err := SetItem(base, buf[0], v); err != nil {
			return nil, err
		}
		for i, k := range keys {
			if err := SetItem(base, k, vals[i]); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	ms.method("popitem", func(args []Value, kwargs []Keyword) (Value, error) {
		buf, err := ParseArgs("popitem", []string{"last"}, 0, 1, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		last := true
		if buf[0] != nil {
			if last, err = Truth(buf[0]); err != nil {
				return nil, err
			}
		}
		keys := dictKeys(args[0])
		if len(keys) == 0 {
			return nil, Raise("KeyError", String("dictionary is empty"))
		}
		k := keys[0]
		if last {
			k = keys[len(keys)-1]
		}
		v, err := CallMethod(args[0], "pop", k)
		if err != nil {
			return nil, err
		}
		return Tuple(k, v), nil
	})
	ms.method("__eq__", func(args []Value, _ []Keyword) (Value, error) {
		if !IsDict(baseOrSelf(args[1])) {
			return NotImplemented(), nil
		}
		eq, err := Compare("==", baseOrSelf(args[0]), baseOrSelf(args[1]))
		if err != nil || !eq {
			return Bool(false), err
		}
		if ClassName(TypeOf(args[1])) != "OrderedDict" {
			if ok, err := IsInstance(args[1], TypeOf(args[0])); err != nil || !ok {
				return Bool(true), err
			}
		}
		ka, kb := dictKeys(args[0]), dictKeys(args[1])
		for i := range ka {
			same, err := Compare("==", ka[i], kb[i])
			if err != nil || !same {
				return Bool(false), err
			}
		}
		return Bool(true), nil
	})
	ms.method("__reversed__", func(args []Value, _ []Keyword) (Value, error) {
		return call(Builtin("reversed"), List(dictKeys(args[0])...))
	})
	copyFn := func(args []Value, _ []Keyword) (Value, error) { return call(TypeOf(args[0]), args[0]) }
	ms.method("copy", copyFn)
	ms.method("__copy__", copyFn)
	return ms
}

// baseOrSelf is the dict an instance of a dict subclass holds, or v.
func baseOrSelf(v Value) Value {
	if b, ok := BaseValue(v); ok {
		return b
	}
	return v
}

// ---------------------------------------------------------------- Counter

func counterMethods() *methods {
	ms := &methods{}
	ms.method("__init__", func(args []Value, kwargs []Keyword) (Value, error) {
		if len(args) > 2 {
			return nil, raise("TypeError", "Counter.__init__() takes from 1 to 2 positional arguments but %d were given", len(args))
		}
		if _, err := CallSuper(TypeOf(args[0]), args[0], "__init__", nil, nil); err != nil {
			return nil, err
		}
		return CallMethodKw(args[0], "update", args[1:], kwargs)
	})
	ms.method("__missing__", func(args []Value, _ []Keyword) (Value, error) { return Int(0), nil })
	ms.method("total", func(args []Value, _ []Keyword) (Value, error) {
		_, vals := dictPairs(args[0])
		var total Value = Int(0)
		for _, v := range vals {
			t, err := BinaryOp("Add", total, v)
			if err != nil {
				return nil, err
			}
			total = t
		}
		return total, nil
	})
	ms.method("most_common", func(args []Value, kwargs []Keyword) (Value, error) {
		buf, err := ParseArgs("most_common", []string{"n"}, 0, 1, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		pairs, err := counterSorted(args[0])
		if err != nil {
			return nil, err
		}
		if buf[0] == nil || IsNone(buf[0]) {
			return List(pairs...), nil
		}
		return nlargest(buf[0], pairs)
	})
	ms.method("elements", func(args []Value, _ []Keyword) (Value, error) {
		keys, vals := dictPairs(args[0])
		var out []Value
		for i, k := range keys {
			n, err := Index(vals[i])
			if err != nil {
				return nil, err
			}
			for j := int64(0); j < n.Int64(); j++ {
				out = append(out, k)
			}
		}
		return call(Builtin("iter"), List(out...))
	})
	ms.classmethod("fromkeys", func(args []Value, _ []Keyword) (Value, error) {
		return nil, raise("NotImplementedError", "Counter.fromkeys() is undefined.  Use Counter(iterable) instead.")
	})
	ms.method("update", func(args []Value, kwargs []Keyword) (Value, error) {
		return nil, counterUpdate(args, kwargs, false)
	})
	ms.method("subtract", func(args []Value, kwargs []Keyword) (Value, error) {
		return nil, counterUpdate(args, kwargs, true)
	})
	ms.method("copy", func(args []Value, _ []Keyword) (Value, error) { return call(TypeOf(args[0]), args[0]) })
	ms.method("__delitem__", func(args []Value, _ []Keyword) (Value, error) {
		if in, err := Contains(args[0], args[1]); err != nil || !in {
			return nil, err
		}
		return CallSuper(TypeOf(args[0]), args[0], "__delitem__", args[1:2], nil)
	})
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		name := ClassName(TypeOf(args[0]))
		keys := dictKeys(args[0])
		if len(keys) == 0 {
			return String(name + "()"), nil
		}
		pairs, err := counterSorted(args[0])
		var d Value
		if err == nil {
			ks := make([]Value, len(pairs))
			vs := make([]Value, len(pairs))
			for i, p := range pairs {
				items, _ := SeqItems(p)
				ks[i], vs[i] = items[0], items[1]
			}
			d, err = NewDict(ks, vs)
		}
		if err != nil {
			if !ErrorIs(err, "TypeError") {
				return nil, err
			}
			d = baseOrSelf(args[0])
		}
		s, err := Repr(d)
		if err != nil {
			return nil, err
		}
		return String(name + "(" + s + ")"), nil
	})
	for _, op := range []string{"==", "!=", "<=", "<", ">=", ">"} {
		op := op
		name := map[string]string{"==": "__eq__", "!=": "__ne__", "<": "__lt__", "<=": "__le__", ">": "__gt__", ">=": "__ge__"}[op]
		ms.method(name, func(args []Value, _ []Keyword) (Value, error) { return counterCompare(op, args[0], args[1]) })
	}
	for _, op := range []string{"+", "-", "|", "&"} {
		op := op
		name := map[string]string{"+": "__add__", "-": "__sub__", "|": "__or__", "&": "__and__"}[op]
		ms.method(name, func(args []Value, _ []Keyword) (Value, error) { return counterBinary(op, args[0], args[1]) })
		ms.method("__i"+name[2:], func(args []Value, _ []Keyword) (Value, error) {
			return counterInplace(op, args[0], args[1])
		})
	}
	ms.method("__pos__", func(args []Value, _ []Keyword) (Value, error) { return counterUnary(args[0], false) })
	ms.method("__neg__", func(args []Value, _ []Keyword) (Value, error) { return counterUnary(args[0], true) })
	return ms
}

// counterSorted is sorted(self.items(), key=itemgetter(1), reverse=True):
// a stable sort by count, largest first.
func counterSorted(self Value) ([]Value, error) {
	return sortByCount(self)
}

// nlargest is heapq.nlargest(n, items, key=itemgetter(1)) on the items
// already sorted by count, most common first, ties in insertion order: n is
// compared and used as heapq does, so a value that is not an integer fails
// as it does there.
func nlargest(n Value, sorted []Value) (Value, error) {
	one, err := Compare("==", n, Int(1))
	if err != nil {
		return nil, err
	}
	if one {
		if len(sorted) == 0 {
			return List(), nil
		}
		return List(sorted[0]), nil
	}
	size := Int(int64(len(sorted)))
	all, err := Compare(">=", n, size)
	if err != nil {
		return nil, err
	}
	if all {
		stop, err := Call(Builtin("slice"), []Value{n}, nil)
		if err != nil {
			return nil, err
		}
		return GetItem(List(sorted...), stop)
	}
	neg, err := UnaryOp("USub", n)
	if err != nil {
		return nil, err
	}
	if _, err := Call(Builtin("range"), []Value{Int(0), neg, Int(-1)}, nil); err != nil {
		return nil, err
	}
	k, err := Ssize(n)
	if err != nil {
		return nil, err
	}
	return List(sorted[:max(k, 0)]...), nil
}

// sortByCount is the items of a Counter sorted by count, most common
// first, ties in insertion order.
func sortByCount(self Value) ([]Value, error) {
	keys, vals := dictPairs(self)
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	var sortErr error
	sort.SliceStable(idx, func(a, b int) bool {
		if sortErr != nil {
			return false
		}
		less, err := Compare("<", vals[idx[b]], vals[idx[a]])
		if err != nil {
			sortErr = err
		}
		return less
	})
	if sortErr != nil {
		return nil, sortErr
	}
	out := make([]Value, len(idx))
	for i, j := range idx {
		out[i] = Tuple(keys[j], vals[j])
	}
	return out, nil
}

// counterUpdate is Counter.update (or subtract when sub is set).
func counterUpdate(args []Value, kwargs []Keyword, sub bool) error {
	self := args[0]
	if len(args) > 2 {
		return raise("TypeError", "expected at most 1 argument, got %d", len(args)-1)
	}
	add := func(k, n Value) error {
		cur, err := CallMethod(self, "get", k, Int(0))
		if err != nil {
			return err
		}
		op := "Add"
		if sub {
			op = "Sub"
		}
		r, err := BinaryOp(op, cur, n)
		if err != nil {
			return err
		}
		return SetItem(self, k, r)
	}
	if len(args) == 2 && !IsNone(args[1]) {
		src := args[1]
		mapping, err := hasMappingMethods(src)
		if err != nil {
			return err
		}
		if mapping {
			if !sub && len(dictKeys(self)) == 0 && IsDict(baseOrSelf(src)) {
				if _, err := CallSuper(TypeOf(self), self, "update", []Value{src}, nil); err != nil {
					return err
				}
			} else {
				items, err := CallMethod(src, "items")
				if err != nil {
					return err
				}
				if err := ForEach(items, func(p Value) error {
					kv, err := ToList(p)
					if err != nil {
						return err
					}
					return add(kv[0], kv[1])
				}); err != nil {
					return err
				}
			}
		} else if err := ForEach(src, func(k Value) error { return add(k, Int(1)) }); err != nil {
			return err
		}
	}
	for _, k := range kwargs {
		if err := add(String(k.Name), k.Value); err != nil {
			return err
		}
	}
	return nil
}

// hasMappingMethods is isinstance(v, collections.abc.Mapping) for the values the
// programs pass: dicts and objects with keys and items.
func hasMappingMethods(v Value) (bool, error) {
	if IsDict(baseOrSelf(v)) {
		return true, nil
	}
	for _, name := range []string{"keys", "items"} {
		if has, err := HasAttr(v, name); err != nil || !has {
			return false, err
		}
	}
	return true, nil
}

// isCounter is isinstance(v, collections.Counter): whether collections'
// Counter is in the method resolution order of v's class.
func isCounter(v Value) bool {
	for _, k := range classOf(v).mroList() {
		if k.name == "Counter" && k.moduleName() == "collections" {
			return true
		}
	}
	return false
}

// counterCompare is Counter's rich comparisons: multiset inclusion, with
// missing elements counting zero.
func counterCompare(op string, a, b Value) (Value, error) {
	if !isCounter(b) {
		return NotImplemented(), nil
	}
	check := func(cmp string) (bool, error) {
		for _, c := range []Value{a, b} {
			for _, e := range dictKeys(c) {
				x, err := GetItem(a, e)
				if err != nil {
					return false, err
				}
				y, err := GetItem(b, e)
				if err != nil {
					return false, err
				}
				ok, err := Compare(cmp, x, y)
				if err != nil || !ok {
					return false, err
				}
			}
		}
		return true, nil
	}
	switch op {
	case "==":
		r, err := check("==")
		return Bool(r), err
	case "!=":
		r, err := check("==")
		return Bool(!r), err
	case "<=", ">=":
		r, err := check(op)
		return Bool(r), err
	}
	weak, err := check(op + "=")
	if err != nil || !weak {
		return Bool(false), err
	}
	eq, err := check("==")
	return Bool(!eq), err
}

// counterBinary is Counter +, -, | and &: the result keeps positive
// counts only.
func counterBinary(op string, a, b Value) (Value, error) {
	if !isCounter(b) {
		return NotImplemented(), nil
	}
	// Counter.__add__ and its siblings build a plain Counter(), whatever the
	// type of self is (Lib/collections/__init__.py).
	result, err := newCounter()
	if err != nil {
		return nil, err
	}
	keepPositive := func(k, v Value) error {
		pos, err := Compare(">", v, Int(0))
		if err != nil || !pos {
			return err
		}
		return SetItem(result, k, v)
	}
	keysA, valsA := dictPairs(a)
	for i, k := range keysA {
		other, err := GetItem(b, k)
		if err != nil {
			return nil, err
		}
		var v Value
		switch op {
		case "+":
			v, err = BinaryOp("Add", valsA[i], other)
		case "-":
			v, err = BinaryOp("Sub", valsA[i], other)
		case "|":
			v = valsA[i]
			if less, e := Compare("<", valsA[i], other); e != nil {
				err = e
			} else if less {
				v = other
			}
		default:
			v = valsA[i]
			if less, e := Compare("<", other, valsA[i]); e != nil {
				err = e
			} else if less {
				v = other
			}
		}
		if err != nil {
			return nil, err
		}
		if err := keepPositive(k, v); err != nil {
			return nil, err
		}
	}
	if op == "&" {
		return result, nil
	}
	keysB, valsB := dictPairs(b)
	for i, k := range keysB {
		in, err := Contains(a, k)
		if err != nil {
			return nil, err
		}
		if in {
			continue
		}
		v := valsB[i]
		if op == "-" {
			if v, err = BinaryOp("Sub", Int(0), v); err != nil {
				return nil, err
			}
		}
		if err := keepPositive(k, v); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// newCounter makes an empty collections.Counter for a result.
func newCounter() (Value, error) {
	mod, err := ImportModule("collections", "")
	if err != nil {
		return nil, err
	}
	cls, err := GetAttr(mod, "Counter")
	if err != nil {
		return nil, err
	}
	return call(cls)
}

// counterInplace is Counter +=, -=, |= and &=: the counter itself updated,
// then its non-positive counts dropped.
func counterInplace(op string, a, b Value) (Value, error) {
	r, err := counterBinary(op, a, b)
	if err != nil || IsNotImplemented(r) {
		return r, err
	}
	base := baseOrSelf(a)
	if _, err := CallMethod(base, "clear"); err != nil {
		return nil, err
	}
	keys, vals := dictPairs(r)
	for i, k := range keys {
		if err := SetItem(base, k, vals[i]); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// counterUnary is +counter (positive counts) or -counter (negated negative
// counts).
func counterUnary(a Value, neg bool) (Value, error) {
	result, err := newCounter()
	if err != nil {
		return nil, err
	}
	keys, vals := dictPairs(a)
	for i, k := range keys {
		v := vals[i]
		if neg {
			if v, err = BinaryOp("Sub", Int(0), v); err != nil {
				return nil, err
			}
		}
		pos, err := Compare(">", v, Int(0))
		if err != nil {
			return nil, err
		}
		if pos {
			if err := SetItem(result, k, v); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// ---------------------------------------------------------------- ChainMap

func chainMapMethods() *methods {
	ms := &methods{}
	maps := func(self Value) ([]Value, error) {
		m, err := GetAttr(self, "maps")
		if err != nil {
			return nil, err
		}
		return ToList(m)
	}
	ms.method("__init__", func(args []Value, kwargs []Keyword) (Value, error) {
		if err := NoKeywords("ChainMap", kwargs); err != nil {
			return nil, err
		}
		list := args[1:]
		if len(list) == 0 {
			list = []Value{EmptyDict()}
		}
		return nil, SetAttr(args[0], "maps", List(list...))
	})
	ms.method("__getitem__", func(args []Value, _ []Keyword) (Value, error) {
		ms, err := maps(args[0])
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			v, err := GetItem(m, args[1])
			if err == nil {
				return v, nil
			}
			if !ErrorIs(err, "KeyError") {
				return nil, err
			}
		}
		return CallMethod(args[0], "__missing__", args[1])
	})
	ms.method("__missing__", func(args []Value, _ []Keyword) (Value, error) {
		return nil, Raise("KeyError", args[1])
	})
	ms.method("get", func(args []Value, _ []Keyword) (Value, error) {
		var dflt Value = None()
		if len(args) > 2 {
			dflt = args[2]
		}
		in, err := Contains(args[0], args[1])
		if err != nil {
			return nil, err
		}
		if !in {
			return dflt, nil
		}
		return GetItem(args[0], args[1])
	})
	keysOf := func(self Value) ([]Value, error) {
		ms, err := maps(self)
		if err != nil {
			return nil, err
		}
		d := EmptyDict()
		for i := len(ms) - 1; i >= 0; i-- {
			if err := ForEach(ms[i], func(k Value) error { return SetItem(d, k, None()) }); err != nil {
				return nil, err
			}
		}
		keys, _, _ := DictItems(d)
		return keys, nil
	}
	ms.method("__len__", func(args []Value, _ []Keyword) (Value, error) {
		keys, err := keysOf(args[0])
		return Int(int64(len(keys))), err
	})
	ms.method("__iter__", func(args []Value, _ []Keyword) (Value, error) {
		keys, err := keysOf(args[0])
		if err != nil {
			return nil, err
		}
		return call(Builtin("iter"), List(keys...))
	})
	ms.method("__contains__", func(args []Value, _ []Keyword) (Value, error) {
		ms, err := maps(args[0])
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			in, err := Contains(m, args[1])
			if err != nil || in {
				return Bool(in), err
			}
		}
		return Bool(false), nil
	})
	ms.method("__bool__", func(args []Value, _ []Keyword) (Value, error) {
		ms, err := maps(args[0])
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			t, err := Truth(m)
			if err != nil || t {
				return Bool(t), err
			}
		}
		return Bool(false), nil
	})
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		ms, err := maps(args[0])
		if err != nil {
			return nil, err
		}
		s, err := joinReprs(ms)
		if err != nil {
			return nil, err
		}
		return String(ClassName(TypeOf(args[0])) + "(" + s + ")"), nil
	})
	ms.method("new_child", func(args []Value, kwargs []Keyword) (Value, error) {
		ms, err := maps(args[0])
		if err != nil {
			return nil, err
		}
		var m Value
		if len(args) > 1 && !IsNone(args[1]) {
			m = args[1]
		} else {
			m = EmptyDict()
		}
		for _, k := range kwargs {
			if err := SetItem(m, String(k.Name), k.Value); err != nil {
				return nil, err
			}
		}
		return Call(TypeOf(args[0]), append([]Value{m}, ms...), nil)
	})
	ms.property("parents", func(self Value) (Value, error) {
		ms, err := maps(self)
		if err != nil {
			return nil, err
		}
		return Call(TypeOf(self), ms[1:], nil)
	})
	ms.method("copy", func(args []Value, _ []Keyword) (Value, error) {
		ms, err := maps(args[0])
		if err != nil {
			return nil, err
		}
		first, err := CallMethod(ms[0], "copy")
		if err != nil {
			return nil, err
		}
		return Call(TypeOf(args[0]), append([]Value{first}, ms[1:]...), nil)
	})
	ms.method("__setitem__", func(args []Value, _ []Keyword) (Value, error) {
		ms, err := maps(args[0])
		if err != nil {
			return nil, err
		}
		return nil, SetItem(ms[0], args[1], args[2])
	})
	ms.method("__delitem__", func(args []Value, _ []Keyword) (Value, error) {
		ms, err := maps(args[0])
		if err != nil {
			return nil, err
		}
		if err := DelItem(ms[0], args[1]); err != nil {
			if !ErrorIs(err, "KeyError") {
				return nil, err
			}
			r, rerr := Repr(args[1])
			if rerr != nil {
				return nil, rerr
			}
			return nil, Raise("KeyError", String("Key not found in the first mapping: "+r))
		}
		return nil, nil
	})
	for _, name := range []string{"keys", "items", "values"} {
		name := name
		ms.method(name, func(args []Value, _ []Keyword) (Value, error) {
			keys, err := keysOf(args[0])
			if err != nil {
				return nil, err
			}
			d := EmptyDict()
			for _, k := range keys {
				v, err := GetItem(args[0], k)
				if err != nil {
					return nil, err
				}
				if err := SetItem(d, k, v); err != nil {
					return nil, err
				}
			}
			return CallMethod(d, name)
		})
	}
	return ms
}

// ---------------------------------------------------------------- namedtuple

// namedtuple is collections.namedtuple(typename, field_names, *,
// rename=False, defaults=None, module=None).
func namedtuple(args []Value, kwargs []Keyword) (Value, error) {
	buf, err := ParseArgs("namedtuple", []string{"typename", "field_names", "rename", "defaults", "module"}, 2, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	typenameV, err := call(Builtin("str"), buf[0])
	if err != nil {
		return nil, err
	}
	typename, _ := AsString(typenameV)
	var fieldVals []Value
	if s, ok := AsString(buf[1]); ok {
		for _, f := range strings.Fields(strings.ReplaceAll(s, ",", " ")) {
			fieldVals = append(fieldVals, String(f))
		}
	} else {
		items, err := ToList(buf[1])
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			sv, err := call(Builtin("str"), it)
			if err != nil {
				return nil, err
			}
			fieldVals = append(fieldVals, sv)
		}
	}
	fields := make([]string, len(fieldVals))
	for i, v := range fieldVals {
		fields[i], _ = AsString(v)
	}
	rename := false
	if buf[2] != nil {
		if rename, err = Truth(buf[2]); err != nil {
			return nil, err
		}
	}
	if rename {
		seen := map[string]bool{}
		for i, name := range fields {
			if !isIdentifier(name) || isKeyword(name) || strings.HasPrefix(name, "_") || seen[name] {
				fields[i] = "_" + strconv.Itoa(i)
			}
			seen[name] = true
		}
	}
	for _, name := range append([]string{typename}, fields...) {
		r, err := Repr(String(name))
		if err != nil {
			return nil, err
		}
		if !isIdentifier(name) {
			return nil, raise("ValueError", "Type names and field names must be valid identifiers: %s", r)
		}
		if isKeyword(name) {
			return nil, raise("ValueError", "Type names and field names cannot be a keyword: %s", r)
		}
	}
	seen := map[string]bool{}
	for _, name := range fields {
		r, err := Repr(String(name))
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(name, "_") && !rename {
			return nil, raise("ValueError", "Field names cannot start with an underscore: %s", r)
		}
		if seen[name] {
			return nil, raise("ValueError", "Encountered duplicate field name: %s", r)
		}
		seen[name] = true
	}
	var defaults []Value
	fieldDefaults := EmptyDict()
	if buf[3] != nil && !IsNone(buf[3]) {
		if defaults, err = ToList(buf[3]); err != nil {
			return nil, err
		}
		if len(defaults) > len(fields) {
			return nil, raise("TypeError", "Got more default values than field names")
		}
		off := len(fields) - len(defaults)
		for i, d := range defaults {
			if err := SetItem(fieldDefaults, String(fields[off+i]), d); err != nil {
				return nil, err
			}
		}
	}
	module := buf[4]
	if module == nil || IsNone(module) {
		module = String("__main__")
		if g := CallerGlobals(); g != nil {
			// The caller's module name; globals without __name__ leave
			// '__main__', as the `or '__main__'` after
			// sys._getframemodulename(1) in namedtuple does.
			name, err := GetItem(g, String("__name__"))
			switch {
			case err == nil:
				module = name
			case !ErrorIs(err, "KeyError"):
				return nil, err
			}
		}
	}
	return makeNamedtuple(typename, fields, defaults, fieldDefaults, module)
}

// kwlist is keyword.kwlist, the names namedtuple refuses as field names.
var kwlist = []string{"False", "None", "True", "and", "as", "assert", "async", "await", "break", "class",
	"continue", "def", "del", "elif", "else", "except", "finally", "for", "from", "global", "if", "import",
	"in", "is", "lambda", "nonlocal", "not", "or", "pass", "raise", "return", "try", "while", "with", "yield"}

// isKeyword is keyword.iskeyword(s).
func isKeyword(s string) bool { return slices.Contains(kwlist, s) }

// makeNamedtuple builds the tuple subclass of namedtuple.
func makeNamedtuple(typename string, fields []string, defaults []Value, fieldDefaults, module Value) (Value, error) {
	argList := strings.Join(fields, ", ")
	if len(fields) == 1 {
		argList += ","
	}
	// The constructor is a Python function, as CPython builds it, so that
	// calling it binds arguments and reports errors as Python functions do.
	tupleNew, err := GetAttr(Builtin("tuple"), "__new__")
	if err != nil {
		return nil, err
	}
	src := "lambda _cls, " + strings.Join(fields, ", ") + ": _tuple_new(_cls, (" + argList + "))"
	if len(fields) == 0 {
		src = "lambda _cls: _tuple_new(_cls, ())"
	}
	code, err := compileSource(strV(src), "<string>", "eval")
	if err != nil {
		return nil, err
	}
	ns := newDict()
	ns.setStr("_tuple_new", tupleNew)
	ns.setStr("__builtins__", newDict())
	ns.setStr("__name__", strV("namedtuple_"+typename))
	newFn, err := runModuleCode(code, ns, ns)
	if err != nil {
		return nil, err
	}
	for _, attr := range []struct {
		name string
		v    Value
	}{{"__name__", String("__new__")}, {"__qualname__", String(typename + ".__new__")},
		{"__doc__", String("Create new instance of " + typename + "(" + strings.Join(fields, ", ") + ")")}} {
		if err := SetAttr(newFn, attr.name, attr.v); err != nil {
			return nil, err
		}
	}
	if defaults != nil {
		if err := SetAttr(newFn, "__defaults__", Tuple(defaults...)); err != nil {
			return nil, err
		}
	}
	fieldNames := make([]Value, len(fields))
	for i, f := range fields {
		fieldNames[i] = String(f)
	}
	nfields := len(fields)
	ms := &methods{}
	ms.add("__doc__", String(typename+"("+strings.Join(fields, ", ")+")"))
	ms.add("__slots__", Tuple())
	ms.add("_fields", Tuple(fieldNames...))
	ms.add("_field_defaults", fieldDefaults)
	ms.add("__new__", newFn)
	ms.classmethod("_make", func(args []Value, kwargs []Keyword) (Value, error) {
		if err := ArgsOne("_make", args[1:], kwargs); err != nil {
			return nil, err
		}
		result, err := call(tupleNew, args[0], args[1])
		if err != nil {
			return nil, err
		}
		n, err := Len(result)
		if err != nil {
			return nil, err
		}
		if n != nfields {
			return nil, raise("TypeError", "Expected %d arguments, got %d", nfields, n)
		}
		return result, nil
	})
	ms.method("_replace", func(args []Value, kwargs []Keyword) (Value, error) {
		if len(args) != 1 {
			return nil, raise("TypeError", "%s._replace() takes 1 positional argument but %d were given", typename, len(args))
		}
		values, ok := SeqItems(baseOrSelf(args[0]))
		if !ok {
			return nil, raise("TypeError", "_replace() needs a %s", typename)
		}
		out := append([]Value(nil), values...)
		used := map[string]bool{}
		for i, f := range fields {
			if v := kwarg(kwargs, f); v != nil {
				out[i] = v
				used[f] = true
			}
		}
		var extra []Value
		for _, k := range kwargs {
			if !used[k.Name] {
				extra = append(extra, String(k.Name))
			}
		}
		if len(extra) > 0 {
			r, err := Repr(List(extra...))
			if err != nil {
				return nil, err
			}
			return nil, raise("ValueError", "Got unexpected field names: %s", r)
		}
		return CallMethod(TypeOf(args[0]), "_make", Tuple(out...))
	})
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		values, _ := SeqItems(baseOrSelf(args[0]))
		parts := make([]string, len(values))
		for i, v := range values {
			r, err := Repr(v)
			if err != nil {
				return nil, err
			}
			name := "?"
			if i < len(fields) {
				name = fields[i]
			}
			parts[i] = name + "=" + r
		}
		return String(ClassName(TypeOf(args[0])) + "(" + strings.Join(parts, ", ") + ")"), nil
	})
	ms.method("_asdict", func(args []Value, _ []Keyword) (Value, error) {
		values, _ := SeqItems(baseOrSelf(args[0]))
		return NewDict(fieldNames, values)
	})
	ms.method("__getnewargs__", func(args []Value, _ []Keyword) (Value, error) {
		values, _ := SeqItems(baseOrSelf(args[0]))
		return Tuple(values...), nil
	})
	ms.add("__match_args__", Tuple(fieldNames...))
	for i, f := range fields {
		i := i
		get := BuiltinFunc(f, func(args []Value, _ []Keyword) (Value, error) {
			if len(args) != 1 {
				return nil, raise("TypeError", "expected 1 argument, got %d", len(args))
			}
			return GetItem(args[0], Int(int64(i)))
		})
		getter, err := call(Builtin("property"), get, None(), None(),
			String("Alias for field number "+strconv.Itoa(i)))
		if err != nil {
			return nil, err
		}
		ms.add(f, getter)
	}
	if ms.err != nil {
		return nil, ms.err
	}
	names := append([]string{"__module__", "__qualname__"}, ms.names...)
	values := append([]Value{module, String(typename)}, ms.values...)
	cls, err := NewClass(typename, []Value{Builtin("tuple")}, StrDict(names, values))
	if err != nil {
		return nil, err
	}
	for _, m := range []string{"_replace", "__repr__", "_asdict", "__getnewargs__"} {
		f, err := GetAttr(cls, m)
		if err != nil {
			return nil, err
		}
		if err := SetAttr(f, "__qualname__", String(typename+"."+m)); err != nil {
			return nil, err
		}
	}
	return cls, nil
}
