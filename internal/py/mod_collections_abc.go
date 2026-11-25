// Ported from CPython 3.12 (Lib/_collections_abc.py, Lib/_py_abc.py, Lib/abc.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import ()

// abcImpl is the _abc_impl of a class made by ABCMeta: the classes
// registered as its virtual subclasses and its subclasses made by ABCMeta.
type abcImpl struct {
	registry   []Value
	subclasses []Value
}

func init() {
	register("abc", func(b *builder) {
		object := []Value{Builtin("object")}
		dataCls := b.newClass("_abc_data", object, &methods{})
		meta := b.class("ABCMeta", []Value{Builtin("type")}, abcMetaMethods(dataCls))
		abc, err := Call(meta, []Value{String("ABC"), Tuple(), StrDict(
			[]string{"__module__", "__qualname__", "__slots__"}, []Value{String("abc"), String("ABC"), Tuple()})}, nil)
		if err != nil {
			b.err = err
			return
		}
		b.set("ABC", abc)
		b.pyfn("abstractmethod", func(args []Value, kwargs []Keyword) (Value, error) {
			if err := CheckPositional("abstractmethod", len(args), 1, 1); err != nil {
				return nil, err
			}
			return args[0], SetAttr(args[0], "__isabstractmethod__", Bool(true))
		})
	})
	register("collections.abc", defineCollectionsABC)
}

// implOf is the _abc_impl of a class made by ABCMeta.
func implOf(cls Value) (*abcImpl, bool) {
	v, err := GetAttr(cls, "_abc_impl")
	if err != nil {
		return nil, false
	}
	impl, ok := Native(v).(*abcImpl)
	return impl, ok
}

func abcMetaMethods(dataCls Value) *methods {
	ms := &methods{}
	ms.method("__new__", func(args []Value, kwargs []Keyword) (Value, error) {
		if len(args) != 4 {
			return nil, raise("TypeError", "ABCMeta.__new__() missing required arguments: 'name', 'bases' and 'namespace'")
		}
		mcls := args[0]
		cls, err := CallSuper(mcls, mcls, "__new__", args, kwargs)
		if err != nil {
			return nil, err
		}
		data, err := instance(dataCls, &abcImpl{})
		if err != nil {
			return nil, err
		}
		if err := SetAttr(cls, "_abc_impl", data); err != nil {
			return nil, err
		}
		bases, _ := SeqItems(args[2])
		for _, base := range bases {
			if impl, ok := implOf(base); ok {
				impl.subclasses = append(impl.subclasses, cls)
			}
		}
		return cls, nil
	})
	ms.method("register", func(args []Value, kwargs []Keyword) (Value, error) {
		if len(args) != 2 {
			return nil, raise("TypeError", "register() takes 2 positional arguments but %d were given", len(args))
		}
		cls, sub := args[0], args[1]
		if !isClass(sub) {
			return nil, raise("TypeError", "Can only register classes")
		}
		if already, err := abcSubclassCheck(cls, sub); err != nil || already {
			return sub, err
		}
		if loop, err := abcSubclassCheck(sub, cls); err != nil {
			return nil, err
		} else if loop {
			return nil, raise("RuntimeError", "Refusing to create an inheritance cycle")
		}
		impl, ok := implOf(cls)
		if ok {
			impl.registry = append(impl.registry, sub)
		}
		return sub, nil
	})
	ms.method("__instancecheck__", func(args []Value, kwargs []Keyword) (Value, error) {
		r, err := abcSubclassCheck(args[0], TypeOf(args[1]))
		return Bool(r), err
	})
	ms.method("__subclasscheck__", func(args []Value, kwargs []Keyword) (Value, error) {
		if !isClass(args[1]) {
			return nil, raise("TypeError", "issubclass() arg 1 must be a class")
		}
		r, err := abcSubclassCheck(args[0], args[1])
		return Bool(r), err
	})
	return ms
}

// abcSubclassCheck is ABCMeta.__subclasscheck__(cls, sub): the class's
// __subclasshook__, then real inheritance, then the registered classes and
// the subclasses made by ABCMeta, recursively.
func abcSubclassCheck(cls, sub Value) (bool, error) {
	if hook, err := GetAttr(cls, "__subclasshook__"); err == nil {
		r, err := call(hook, sub)
		if err != nil {
			return false, err
		}
		if !IsNotImplemented(r) {
			return truth(r)
		}
	}
	if mro, err := GetAttr(sub, "__mro__"); err == nil {
		items, _ := SeqItems(mro)
		for _, k := range items {
			if Is(k, cls) {
				return true, nil
			}
		}
	}
	impl, ok := implOf(cls)
	if !ok {
		return false, nil
	}
	for _, group := range [][]Value{impl.registry, impl.subclasses} {
		for _, r := range group {
			ok, err := IsSubclass(sub, r)
			if err != nil || ok {
				return ok, err
			}
		}
	}
	return false, nil
}

// checkMethods is _check_methods(C, *methods): whether each method is
// defined, and not None, somewhere in C's MRO.
func checkMethods(c Value, names ...string) Value {
	mro, err := GetAttr(c, "__mro__")
	if err != nil {
		return NotImplemented()
	}
	classes, _ := SeqItems(mro)
	for _, name := range names {
		found := false
		for _, k := range classes {
			if v, ok := OwnAttr(k, name); ok {
				if v != nil && IsNone(v) {
					return NotImplemented()
				}
				found = true
				break
			}
		}
		if !found {
			return NotImplemented()
		}
	}
	return Bool(true)
}

// abcSpec describes one collections.abc class: its bases, the methods its
// __subclasshook__ checks (none: no hook), the builtin classes registered
// with it, and its mixin methods.
type abcSpec struct {
	name     string
	bases    []string
	hook     []string
	register []string
	mixins   func(ms *methods)
}

func defineCollectionsABC(b *builder) {
	abcMod, err := ImportModule("abc", "")
	if err != nil {
		b.err = err
		return
	}
	meta, err := GetAttr(abcMod, "ABCMeta")
	if err != nil {
		b.err = err
		return
	}
	specs := []abcSpec{
		{name: "Hashable", hook: []string{"__hash__"}},
		{name: "Awaitable", hook: []string{"__await__"}},
		{name: "Coroutine", bases: []string{"Awaitable"}, hook: []string{"__await__", "send", "throw", "close"}},
		{name: "AsyncIterable", hook: []string{"__aiter__"}},
		{name: "AsyncIterator", bases: []string{"AsyncIterable"}, hook: []string{"__anext__", "__aiter__"}},
		{name: "AsyncGenerator", bases: []string{"AsyncIterator"}, hook: []string{"__aiter__", "__anext__", "asend", "athrow", "aclose"}},
		{name: "Iterable", hook: []string{"__iter__"}},
		{name: "Iterator", bases: []string{"Iterable"}, hook: []string{"__iter__", "__next__"}, mixins: iteratorMixins},
		{name: "Reversible", bases: []string{"Iterable"}, hook: []string{"__reversed__", "__iter__"}},
		{name: "Generator", bases: []string{"Iterator"}, hook: []string{"__iter__", "__next__", "send", "throw", "close"}},
		{name: "Sized", hook: []string{"__len__"}},
		{name: "Container", hook: []string{"__contains__"}},
		{name: "Collection", bases: []string{"Sized", "Iterable", "Container"}, hook: []string{"__len__", "__iter__", "__contains__"}},
		{name: "Buffer", hook: []string{"__buffer__"}},
		{name: "Callable", hook: []string{"__call__"}},
		{name: "Set", bases: []string{"Collection"}, register: []string{"frozenset"}, mixins: setMixins},
		{name: "MutableSet", bases: []string{"Set"}, register: []string{"set"}, mixins: mutableSetMixins},
		{name: "Mapping", bases: []string{"Collection"}, mixins: mappingMixins},
		{name: "MappingView", bases: []string{"Sized"}},
		{name: "KeysView", bases: []string{"MappingView", "Set"}, register: []string{"dict_keys"}},
		{name: "ItemsView", bases: []string{"MappingView", "Set"}, register: []string{"dict_items"}},
		{name: "ValuesView", bases: []string{"MappingView", "Collection"}, register: []string{"dict_values"}},
		{name: "MutableMapping", bases: []string{"Mapping"}, register: []string{"dict"}, mixins: mutableMappingMixins},
		{name: "Sequence", bases: []string{"Reversible", "Collection"}, register: []string{"tuple", "str", "range"}, mixins: sequenceMixins},
		{name: "ByteString", bases: []string{"Sequence"}, register: []string{"bytes", "bytearray"}},
		{name: "MutableSequence", bases: []string{"Sequence"}, register: []string{"list", "bytearray"}, mixins: mutableSequenceMixins},
	}
	made := map[string]Value{}
	for _, spec := range specs {
		ms := &methods{}
		ms.add("__slots__", Tuple())
		if spec.hook != nil {
			name, hook := spec.name, spec.hook
			ms.classmethod("__subclasshook__", func(args []Value, kwargs []Keyword) (Value, error) {
				if !Is(args[0], made[name]) {
					return NotImplemented(), nil
				}
				return checkMethods(args[1], hook...), nil
			})
		}
		if spec.mixins != nil {
			spec.mixins(ms)
		}
		if ms.err != nil {
			b.err = ms.err
			return
		}
		bases := []Value{}
		for _, bn := range spec.bases {
			bases = append(bases, made[bn])
		}
		names := append([]string{"__module__", "__qualname__"}, ms.names...)
		values := append([]Value{String("collections.abc"), String(spec.name)}, ms.values...)
		cls, err := Call(meta, []Value{String(spec.name), Tuple(bases...), StrDict(names, values)}, nil)
		if err != nil {
			b.err = err
			return
		}
		made[spec.name] = cls
		b.set(spec.name, cls)
		for _, r := range spec.register {
			if _, err := CallMethod(cls, "register", builtinType(r)); err != nil {
				b.err = err
				return
			}
		}
	}
	if coll, err := ImportModule("collections", ""); err == nil {
		b.err = SetAttr(coll, "abc", b.mod)
	}
}

// builtinType is the builtin class of the given name, dict views included.
func builtinType(name string) Value {
	switch name {
	case "dict_keys":
		return TypeOf(mustCall(EmptyDict(), "keys"))
	case "dict_values":
		return TypeOf(mustCall(EmptyDict(), "values"))
	case "dict_items":
		return TypeOf(mustCall(EmptyDict(), "items"))
	}
	return Builtin(name)
}

// mustCall is v.name() for the builtin methods that cannot fail.
func mustCall(v Value, name string) Value {
	r, err := CallMethod(v, name)
	if err != nil {
		return None()
	}
	return r
}

func iteratorMixins(ms *methods) {
	ms.method("__iter__", func(args []Value, _ []Keyword) (Value, error) { return args[0], nil })
}

// mappingMixins are Mapping's methods built on __getitem__, __iter__ and
// __len__.
func mappingMixins(ms *methods) {
	ms.method("get", func(args []Value, kwargs []Keyword) (Value, error) {
		buf, err := ParseArgs("get", []string{"key", "default"}, 1, 2, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		v, err := GetItem(args[0], buf[0])
		if err != nil {
			if ErrorIs(err, "KeyError") {
				return orDefault(buf[1], None()), nil
			}
			return nil, err
		}
		return v, nil
	})
	ms.method("__contains__", func(args []Value, _ []Keyword) (Value, error) {
		if _, err := GetItem(args[0], args[1]); err != nil {
			if ErrorIs(err, "KeyError") {
				return Bool(false), nil
			}
			return nil, err
		}
		return Bool(true), nil
	})
	view := func(name string) {
		ms.method(name, func(args []Value, _ []Keyword) (Value, error) {
			d := EmptyDict()
			if err := iterate(args[0], func(k Value) error {
				v, err := GetItem(args[0], k)
				if err != nil {
					return err
				}
				return SetItem(d, k, v)
			}); err != nil {
				return nil, err
			}
			return CallMethod(d, name)
		})
	}
	view("keys")
	view("items")
	view("values")
	ms.method("__eq__", func(args []Value, _ []Keyword) (Value, error) {
		if !hasMappingMethods(args[1]) {
			return NotImplemented(), nil
		}
		a, err := call(Builtin("dict"), mustCall(args[0], "items"))
		if err != nil {
			return nil, err
		}
		b, err := call(Builtin("dict"), mustCall(args[1], "items"))
		if err != nil {
			return nil, err
		}
		return RichCompare("==", a, b)
	})
	ms.add("__reversed__", None())
	ms.add("__hash__", None())
}

func mutableMappingMixins(ms *methods) {
	ms.method("pop", func(args []Value, _ []Keyword) (Value, error) {
		if len(args) < 2 {
			return nil, raise("TypeError", "pop() missing 1 required positional argument: 'key'")
		}
		v, err := GetItem(args[0], args[1])
		if err != nil {
			if ErrorIs(err, "KeyError") && len(args) > 2 {
				return args[2], nil
			}
			return nil, err
		}
		return v, DelItem(args[0], args[1])
	})
	ms.method("popitem", func(args []Value, _ []Keyword) (Value, error) {
		it, err := Iter(args[0])
		if err != nil {
			return nil, err
		}
		k, ok, err := it.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, Raise("KeyError")
		}
		v, err := GetItem(args[0], k)
		if err != nil {
			return nil, err
		}
		return Tuple(k, v), DelItem(args[0], k)
	})
	ms.method("clear", func(args []Value, _ []Keyword) (Value, error) {
		keys, err := listOf(args[0])
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if err := DelItem(args[0], k); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	ms.method("update", func(args []Value, kwargs []Keyword) (Value, error) {
		self := args[0]
		if len(args) > 1 {
			other := args[1]
			if hasMappingMethods(other) {
				keys, err := listOf(mustCall(other, "keys"))
				if err != nil {
					return nil, err
				}
				for _, k := range keys {
					v, err := GetItem(other, k)
					if err != nil {
						return nil, err
					}
					if err := SetItem(self, k, v); err != nil {
						return nil, err
					}
				}
			} else if err := iterate(other, func(p Value) error {
				kv, err := listOf(p)
				if err != nil {
					return err
				}
				return SetItem(self, kv[0], kv[1])
			}); err != nil {
				return nil, err
			}
		}
		for _, k := range kwargs {
			if err := SetItem(self, String(k.Name), k.Value); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	ms.method("setdefault", func(args []Value, _ []Keyword) (Value, error) {
		dflt := Value(None())
		if len(args) > 2 {
			dflt = args[2]
		}
		v, err := GetItem(args[0], args[1])
		if err == nil {
			return v, nil
		}
		if !ErrorIs(err, "KeyError") {
			return nil, err
		}
		return dflt, SetItem(args[0], args[1], dflt)
	})
}

// sequenceMixins are Sequence's methods built on __getitem__ and __len__.
func sequenceMixins(ms *methods) {
	items := func(self Value) ([]Value, error) {
		var out []Value
		for i := 0; ; i++ {
			v, err := GetItem(self, Int(int64(i)))
			if err != nil {
				if ErrorIs(err, "IndexError") {
					return out, nil
				}
				return nil, err
			}
			out = append(out, v)
		}
	}
	ms.method("__iter__", func(args []Value, _ []Keyword) (Value, error) {
		all, err := items(args[0])
		if err != nil {
			return nil, err
		}
		return call(Builtin("iter"), List(all...))
	})
	ms.method("__contains__", func(args []Value, _ []Keyword) (Value, error) {
		all, err := items(args[0])
		if err != nil {
			return nil, err
		}
		for _, v := range all {
			eq := Is(v, args[1])
			if !eq {
				if eq, err = Compare("==", v, args[1]); err != nil {
					return nil, err
				}
			}
			if eq {
				return Bool(true), nil
			}
		}
		return Bool(false), nil
	})
	ms.method("__reversed__", func(args []Value, _ []Keyword) (Value, error) {
		n, err := Len(args[0])
		if err != nil {
			return nil, err
		}
		out := make([]Value, 0, n)
		for i := n - 1; i >= 0; i-- {
			v, err := GetItem(args[0], Int(int64(i)))
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return call(Builtin("iter"), List(out...))
	})
	ms.method("index", func(args []Value, _ []Keyword) (Value, error) {
		all, err := items(args[0])
		if err != nil {
			return nil, err
		}
		for i, v := range all {
			eq := Is(v, args[1])
			if !eq {
				if eq, err = Compare("==", v, args[1]); err != nil {
					return nil, err
				}
			}
			if eq {
				return Int(int64(i)), nil
			}
		}
		return nil, Raise("ValueError")
	})
	ms.method("count", func(args []Value, _ []Keyword) (Value, error) {
		all, err := items(args[0])
		if err != nil {
			return nil, err
		}
		n := 0
		for _, v := range all {
			eq := Is(v, args[1])
			if !eq {
				if eq, err = Compare("==", v, args[1]); err != nil {
					return nil, err
				}
			}
			if eq {
				n++
			}
		}
		return Int(int64(n)), nil
	})
}

func mutableSequenceMixins(ms *methods) {
	ms.method("append", func(args []Value, _ []Keyword) (Value, error) {
		n, err := Len(args[0])
		if err != nil {
			return nil, err
		}
		_, err = CallMethod(args[0], "insert", Int(int64(n)), args[1])
		return nil, err
	})
	ms.method("extend", func(args []Value, _ []Keyword) (Value, error) {
		values, err := listOf(args[1])
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			if _, err := CallMethod(args[0], "append", v); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	ms.method("pop", func(args []Value, _ []Keyword) (Value, error) {
		index := Value(Int(-1))
		if len(args) > 1 {
			index = args[1]
		}
		v, err := GetItem(args[0], index)
		if err != nil {
			return nil, err
		}
		return v, DelItem(args[0], index)
	})
	ms.method("remove", func(args []Value, _ []Keyword) (Value, error) {
		i, err := CallMethod(args[0], "index", args[1])
		if err != nil {
			return nil, err
		}
		return nil, DelItem(args[0], i)
	})
	ms.method("reverse", func(args []Value, _ []Keyword) (Value, error) {
		n, err := Len(args[0])
		if err != nil {
			return nil, err
		}
		for i := 0; i < n/2; i++ {
			a, err := GetItem(args[0], Int(int64(i)))
			if err != nil {
				return nil, err
			}
			b, err := GetItem(args[0], Int(int64(n-i-1)))
			if err != nil {
				return nil, err
			}
			if err := SetItem(args[0], Int(int64(i)), b); err != nil {
				return nil, err
			}
			if err := SetItem(args[0], Int(int64(n-i-1)), a); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	ms.method("clear", func(args []Value, _ []Keyword) (Value, error) {
		for {
			if _, err := CallMethod(args[0], "pop"); err != nil {
				if ErrorIs(err, "IndexError") {
					return nil, nil
				}
				return nil, err
			}
		}
	})
	ms.method("__iadd__", func(args []Value, _ []Keyword) (Value, error) {
		_, err := CallMethod(args[0], "extend", args[1])
		return args[0], err
	})
}

// setMixins are Set's comparisons and operators built on __contains__,
// __iter__ and __len__, producing instances of the class through
// _from_iterable (the class called with the iterable).
func setMixins(ms *methods) {
	fromIterable := func(self Value, items []Value) (Value, error) {
		return call(TypeOf(self), List(items...))
	}
	contains := func(c, v Value) (bool, error) { return Contains(c, v) }
	isSet := func(v Value) bool {
		has, _ := HasAttr(v, "__contains__")
		hasLen, _ := HasAttr(v, "__len__")
		return has && hasLen
	}
	subset := func(a, b Value) (bool, error) {
		na, err := Len(a)
		if err != nil {
			return false, err
		}
		nb, err := Len(b)
		if err != nil {
			return false, err
		}
		if na > nb {
			return false, nil
		}
		ok := true
		err = iterate(a, func(v Value) error {
			if !ok {
				return nil
			}
			in, err := contains(b, v)
			ok = in
			return err
		})
		return ok, err
	}
	compare := func(name string, f func(a, b Value) (bool, error)) {
		ms.method(name, func(args []Value, _ []Keyword) (Value, error) {
			if !isSet(args[1]) {
				return NotImplemented(), nil
			}
			r, err := f(args[0], args[1])
			return Bool(r), err
		})
	}
	lenOf := func(v Value) int {
		n, _ := Len(v)
		return n
	}
	compare("__le__", subset)
	compare("__ge__", func(a, b Value) (bool, error) { return subset(b, a) })
	compare("__lt__", func(a, b Value) (bool, error) {
		s, err := subset(a, b)
		return s && lenOf(a) < lenOf(b), err
	})
	compare("__gt__", func(a, b Value) (bool, error) {
		s, err := subset(b, a)
		return s && lenOf(a) > lenOf(b), err
	})
	compare("__eq__", func(a, b Value) (bool, error) {
		s, err := subset(a, b)
		return s && lenOf(a) == lenOf(b), err
	})
	filterItems := func(src Value, keep func(v Value) (bool, error)) ([]Value, error) {
		var out []Value
		err := iterate(src, func(v Value) error {
			k, err := keep(v)
			if k {
				out = append(out, v)
			}
			return err
		})
		return out, err
	}
	binary := func(name string, f func(a, b Value) ([]Value, error)) {
		ms.method(name, func(args []Value, _ []Keyword) (Value, error) {
			if has, _ := HasAttr(args[1], "__iter__"); !has {
				return NotImplemented(), nil
			}
			items, err := f(args[0], args[1])
			if err != nil {
				return nil, err
			}
			return fromIterable(args[0], items)
		})
	}
	and := func(a, b Value) ([]Value, error) {
		return filterItems(b, func(v Value) (bool, error) { return contains(a, v) })
	}
	or := func(a, b Value) ([]Value, error) {
		x, err := listOf(a)
		if err != nil {
			return nil, err
		}
		y, err := listOf(b)
		return append(x, y...), err
	}
	sub := func(a, b Value) ([]Value, error) {
		bs, err := call(Builtin("set"), b)
		if err != nil {
			return nil, err
		}
		return filterItems(a, func(v Value) (bool, error) {
			in, err := contains(bs, v)
			return !in, err
		})
	}
	binary("__and__", and)
	binary("__rand__", and)
	binary("__or__", or)
	binary("__ror__", or)
	binary("__sub__", sub)
	binary("__rsub__", func(a, b Value) ([]Value, error) { return sub(b, a) })
	xor := func(a, b Value) ([]Value, error) {
		x, err := sub(a, b)
		if err != nil {
			return nil, err
		}
		y, err := sub(b, a)
		return append(x, y...), err
	}
	binary("__xor__", xor)
	binary("__rxor__", xor)
	ms.method("isdisjoint", func(args []Value, _ []Keyword) (Value, error) {
		disjoint := true
		err := iterate(args[1], func(v Value) error {
			in, err := contains(args[0], v)
			if in {
				disjoint = false
			}
			return err
		})
		return Bool(disjoint), err
	})
	ms.add("__hash__", None())
}

func mutableSetMixins(ms *methods) {
	ms.method("remove", func(args []Value, _ []Keyword) (Value, error) {
		in, err := Contains(args[0], args[1])
		if err != nil {
			return nil, err
		}
		if !in {
			return nil, Raise("KeyError", args[1])
		}
		_, err = CallMethod(args[0], "discard", args[1])
		return nil, err
	})
	ms.method("pop", func(args []Value, _ []Keyword) (Value, error) {
		it, err := Iter(args[0])
		if err != nil {
			return nil, err
		}
		v, ok, err := it.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, Raise("KeyError")
		}
		_, err = CallMethod(args[0], "discard", v)
		return v, err
	})
	ms.method("clear", func(args []Value, _ []Keyword) (Value, error) {
		items, err := listOf(args[0])
		if err != nil {
			return nil, err
		}
		for _, v := range items {
			if _, err := CallMethod(args[0], "discard", v); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	inplace := func(name string, f func(self, v Value, other Value) error) {
		ms.method(name, func(args []Value, _ []Keyword) (Value, error) {
			items, err := listOf(args[1])
			if err != nil {
				return nil, err
			}
			for _, v := range items {
				if err := f(args[0], v, args[1]); err != nil {
					return nil, err
				}
			}
			return args[0], nil
		})
	}
	inplace("__ior__", func(self, v, _ Value) error {
		_, err := CallMethod(self, "add", v)
		return err
	})
	inplace("__isub__", func(self, v, _ Value) error {
		_, err := CallMethod(self, "discard", v)
		return err
	})
	inplace("__ixor__", func(self, v, _ Value) error {
		in, err := Contains(self, v)
		if err != nil {
			return err
		}
		if in {
			_, err = CallMethod(self, "discard", v)
		} else {
			_, err = CallMethod(self, "add", v)
		}
		return err
	})
	ms.method("__iand__", func(args []Value, _ []Keyword) (Value, error) {
		items, err := listOf(args[0])
		if err != nil {
			return nil, err
		}
		for _, v := range items {
			in, err := Contains(args[1], v)
			if err != nil {
				return nil, err
			}
			if !in {
				if _, err := CallMethod(args[0], "discard", v); err != nil {
					return nil, err
				}
			}
		}
		return args[0], nil
	})
}
