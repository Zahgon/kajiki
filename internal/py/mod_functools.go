// Ported from CPython 3.12 (Lib/functools.py, Modules/_functoolsmodule.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"strings"
)

// wrapperAssignments and wrapperUpdates are WRAPPER_ASSIGNMENTS and
// WRAPPER_UPDATES.
var (
	wrapperAssignments = []string{"__module__", "__name__", "__qualname__", "__doc__", "__annotations__", "__type_params__"}
	wrapperUpdates     = []string{"__dict__"}
)

func init() {
	register("functools", func(b *builder) {
		b.set("WRAPPER_ASSIGNMENTS", strTuple(wrapperAssignments))
		b.set("WRAPPER_UPDATES", strTuple(wrapperUpdates))
		b.fn("reduce", functoolsReduce)
		partial := b.cClass("partial", []Value{Builtin("object")}, partialMethods())
		updateWrapper := BuiltinFunc("update_wrapper", updateWrapper)
		b.set("update_wrapper", updateWrapper)
		b.pyfn("wraps", func(args []Value, kwargs []Keyword) (Value, error) {
			buf, err := ParseArgs("wraps", []string{"wrapped", "assigned", "updated"}, 1, 3, args, kwargs)
			if err != nil {
				return nil, err
			}
			kws := []Keyword{{Name: "wrapped", Value: buf[0]},
				{Name: "assigned", Value: orDefault(buf[1], strTuple(wrapperAssignments))},
				{Name: "updated", Value: orDefault(buf[2], strTuple(wrapperUpdates))}}
			return Call(partial, []Value{updateWrapper}, kws)
		})
		cacheInfo, err := makeNamedtuple("CacheInfo", []string{"hits", "misses", "maxsize", "currsize"}, nil,
			EmptyDict(), String("functools"))
		if err != nil {
			b.err = err
			return
		}
		lruWrapper := b.cClass("_lru_cache_wrapper", []Value{Builtin("object")}, lruWrapperMethods(cacheInfo))
		b.pyfn("lru_cache", func(args []Value, kwargs []Keyword) (Value, error) {
			return lruCache(lruWrapper, updateWrapper, args, kwargs)
		})
		b.pyfn("cache", func(args []Value, kwargs []Keyword) (Value, error) {
			if err := CheckPositional("cache", len(args), 1, 1); err != nil {
				return nil, err
			}
			return newLRUWrapper(lruWrapper, updateWrapper, args[0], -1, false)
		})
		b.pyfn("total_ordering", totalOrdering)
		keyWrapper := b.cClass("KeyWrapper", []Value{Builtin("object")}, keyWrapperMethods())
		b.fn("cmp_to_key", func(args []Value, kwargs []Keyword) (Value, error) {
			buf, err := ParseArgs("cmp_to_key", []string{"mycmp"}, 1, 1, args, kwargs)
			if err != nil {
				return nil, err
			}
			return instance(keyWrapper, &keyState{cmp: buf[0]})
		})
		b.class("cached_property", []Value{Builtin("object")}, cachedPropertyMethods())
		b.pyfn("singledispatch", func(args []Value, kwargs []Keyword) (Value, error) {
			if err := CheckPositional("singledispatch", len(args), 1, 1); err != nil {
				return nil, err
			}
			return singledispatch(updateWrapper, args[0])
		})
	})
}

func strTuple(list []string) Value {
	items := make([]Value, len(list))
	for i, s := range list {
		items[i] = String(s)
	}
	return Tuple(items...)
}

func orDefault(v, dflt Value) Value {
	if v == nil {
		return dflt
	}
	return v
}

// functoolsReduce is functools.reduce(function, iterable[, initial]).
func functoolsReduce(args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("reduce", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("reduce", len(args), 2, 3); err != nil {
		return nil, err
	}
	it, err := Iter(args[1])
	if err != nil {
		if ErrorIs(err, "TypeError") {
			return nil, raise("TypeError", "reduce() arg 2 must support iteration")
		}
		return nil, err
	}
	var result Value
	if len(args) == 3 {
		result = args[2]
	}
	for {
		v, ok, err := it.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if result == nil {
			result = v
			continue
		}
		if result, err = call(args[0], result, v); err != nil {
			return nil, err
		}
	}
	if result == nil {
		return nil, raise("TypeError", "reduce() of empty iterable with no initial value")
	}
	return result, nil
}

// partialState is a functools.partial: the function and the arguments it
// is called with first.
type partialState struct {
	fn       Value
	args     []Value
	keywords []Keyword
}

func partialMethods() *methods {
	ms := &methods{}
	ms.method("__new__", func(args []Value, kwargs []Keyword) (Value, error) {
		cls := args[0]
		if len(args) < 2 {
			return nil, raise("TypeError", "type 'partial' takes at least one argument")
		}
		f := args[1]
		if !IsCallable(f) {
			return nil, raise("TypeError", "the first argument must be callable")
		}
		st := &partialState{fn: f, args: append([]Value(nil), args[2:]...)}
		if inner, ok := Native(f).(*partialState); ok && Is(TypeOf(f), cls) {
			if d := InstanceDict(f); d != nil {
				if n, err := Len(d); err == nil && n == 0 {
					st.fn = inner.fn
					st.args = append(append([]Value(nil), inner.args...), st.args...)
					st.keywords = mergeKeywords(inner.keywords, nil)
				}
			}
		}
		st.keywords = mergeKeywords(st.keywords, kwargs)
		return instance(cls, st)
	})
	state := func(self Value) (*partialState, error) {
		return stateOf[*partialState](self, "functools.partial", "__call__")
	}
	ms.method("__call__", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := state(args[0])
		if err != nil {
			return nil, err
		}
		return Call(st.fn, append(append([]Value(nil), st.args...), args[1:]...), mergeKeywords(st.keywords, kwargs))
	})
	ms.property("func", func(self Value) (Value, error) {
		st, err := state(self)
		if err != nil {
			return nil, err
		}
		return st.fn, nil
	})
	ms.property("args", func(self Value) (Value, error) {
		st, err := state(self)
		if err != nil {
			return nil, err
		}
		return Tuple(st.args...), nil
	})
	ms.property("keywords", func(self Value) (Value, error) {
		st, err := state(self)
		if err != nil {
			return nil, err
		}
		names := make([]string, len(st.keywords))
		vals := make([]Value, len(st.keywords))
		for i, k := range st.keywords {
			names[i], vals[i] = k.Name, k.Value
		}
		return StrDict(names, vals), nil
	})
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		st, err := state(args[0])
		if err != nil {
			return nil, err
		}
		parts := []string{}
		r, err := repr(st.fn)
		if err != nil {
			return nil, err
		}
		parts = append(parts, r)
		for _, a := range st.args {
			s, err := repr(a)
			if err != nil {
				return nil, err
			}
			parts = append(parts, s)
		}
		for _, k := range st.keywords {
			s, err := repr(k.Value)
			if err != nil {
				return nil, err
			}
			parts = append(parts, k.Name+"="+s)
		}
		qual, err := GetAttr(TypeOf(args[0]), "__qualname__")
		if err != nil {
			return nil, err
		}
		mod, err := GetAttr(TypeOf(args[0]), "__module__")
		if err != nil {
			return nil, err
		}
		q, _ := AsString(qual)
		m, _ := AsString(mod)
		return String(m + "." + q + "(" + strings.Join(parts, ", ") + ")"), nil
	})
	return ms
}

// mergeKeywords is {**base, **extra} of two keyword argument lists.
func mergeKeywords(base, extra []Keyword) []Keyword {
	out := append([]Keyword(nil), base...)
	for _, k := range extra {
		replaced := false
		for i := range out {
			if out[i].Name == k.Name {
				out[i].Value = k.Value
				replaced = true
			}
		}
		if !replaced {
			out = append(out, k)
		}
	}
	return out
}

// updateWrapper is functools.update_wrapper(wrapper, wrapped,
// assigned=WRAPPER_ASSIGNMENTS, updated=WRAPPER_UPDATES).
func updateWrapper(args []Value, kwargs []Keyword) (Value, error) {
	buf, err := ParseArgs("update_wrapper", []string{"wrapper", "wrapped", "assigned", "updated"}, 2, 4, args, kwargs)
	if err != nil {
		return nil, err
	}
	wrapper, wrapped := buf[0], buf[1]
	assigned, err := listOf(orDefault(buf[2], strTuple(wrapperAssignments)))
	if err != nil {
		return nil, err
	}
	updated, err := listOf(orDefault(buf[3], strTuple(wrapperUpdates)))
	if err != nil {
		return nil, err
	}
	for _, a := range assigned {
		name, _ := AsString(a)
		v, err := GetAttr(wrapped, name)
		if err != nil {
			if ErrorIs(err, "AttributeError") {
				continue
			}
			return nil, err
		}
		if err := SetAttr(wrapper, name, v); err != nil {
			return nil, err
		}
	}
	for _, u := range updated {
		name, _ := AsString(u)
		dst, err := GetAttr(wrapper, name)
		if err != nil {
			return nil, err
		}
		src, err := GetAttr(wrapped, name)
		if err != nil {
			if !ErrorIs(err, "AttributeError") {
				return nil, err
			}
			src = EmptyDict()
		}
		if _, err := CallMethod(dst, "update", src); err != nil {
			return nil, err
		}
	}
	if err := SetAttr(wrapper, "__wrapped__", wrapped); err != nil {
		return nil, err
	}
	return wrapper, nil
}

// lruState is the state of an lru_cache wrapper: the cache (a dict whose
// order is the recency of use), its bound (-1 for none) and its counters.
type lruState struct {
	fn           Value
	cache        Value
	maxsize      int
	typed        bool
	hits, misses int
	kwdMark      Value
}

// lruCache is functools.lru_cache(maxsize=128, typed=False), which may also
// decorate a function directly.
func lruCache(wrapperCls, updateWrapperFn Value, args []Value, kwargs []Keyword) (Value, error) {
	buf, err := ParseArgs("lru_cache", []string{"maxsize", "typed"}, 0, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	typed := false
	if buf[1] != nil {
		if typed, err = truth(buf[1]); err != nil {
			return nil, err
		}
	}
	maxsize := 128
	switch m := orDefault(buf[0], Int(128)); {
	case IsNone(m):
		maxsize = -1
	case IsCallable(m) && !isIntValue(m):
		return newLRUWrapper(wrapperCls, updateWrapperFn, m, 128, typed)
	case isIntValue(m):
		n, err := Ssize(m)
		if err != nil {
			return nil, err
		}
		maxsize = max(n, 0)
	default:
		return nil, raise("TypeError", "Expected first argument to be an integer, a callable, or None")
	}
	return BuiltinFunc("decorating_function", func(args []Value, kwargs []Keyword) (Value, error) {
		if err := CheckPositional("decorating_function", len(args), 1, 1); err != nil {
			return nil, err
		}
		return newLRUWrapper(wrapperCls, updateWrapperFn, args[0], maxsize, typed)
	}), nil
}

func isIntValue(v Value) bool {
	_, ok := AsInt(v)
	return ok
}

// newLRUWrapper wraps f in a cache of maxsize entries (-1: unbounded).
func newLRUWrapper(wrapperCls, updateWrapperFn, f Value, maxsize int, typed bool) (Value, error) {
	mark, err := call(Builtin("object"))
	if err != nil {
		return nil, err
	}
	w, err := instance(wrapperCls, &lruState{fn: f, cache: EmptyDict(), maxsize: maxsize, typed: typed, kwdMark: mark})
	if err != nil {
		return nil, err
	}
	return call(updateWrapperFn, w, f)
}

// lruKey is _make_key: the arguments as one hashable key.
func (st *lruState) lruKey(args []Value, kwargs []Keyword) Value {
	if len(kwargs) == 0 && !st.typed && len(args) == 1 {
		switch TypeName(args[0]) {
		case "int", "str":
			return args[0]
		}
	}
	key := append([]Value(nil), args...)
	if len(kwargs) > 0 {
		key = append(key, st.kwdMark)
		for _, k := range kwargs {
			key = append(key, String(k.Name), k.Value)
		}
	}
	if st.typed {
		for _, a := range args {
			key = append(key, TypeOf(a))
		}
		for _, k := range kwargs {
			key = append(key, TypeOf(k.Value))
		}
	}
	return Tuple(key...)
}

func lruWrapperMethods(cacheInfo Value) *methods {
	ms := &methods{}
	state := func(self Value) (*lruState, error) {
		return stateOf[*lruState](self, "functools._lru_cache_wrapper", "__call__")
	}
	ms.method("__call__", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := state(args[0])
		if err != nil {
			return nil, err
		}
		callArgs := args[1:]
		if st.maxsize == 0 {
			st.misses++
			return Call(st.fn, callArgs, kwargs)
		}
		key := st.lruKey(callArgs, kwargs)
		if _, err := Hash(key); err != nil {
			return nil, err
		}
		if in, err := Contains(st.cache, key); err != nil {
			return nil, err
		} else if in {
			v, err := GetItem(st.cache, key)
			if err != nil {
				return nil, err
			}
			st.hits++
			if st.maxsize > 0 {
				if err := DelItem(st.cache, key); err != nil {
					return nil, err
				}
				if err := SetItem(st.cache, key, v); err != nil {
					return nil, err
				}
			}
			return v, nil
		}
		st.misses++
		result, err := Call(st.fn, callArgs, kwargs)
		if err != nil {
			return nil, err
		}
		if st.maxsize > 0 {
			if in, err := Contains(st.cache, key); err != nil || in {
				return result, err
			}
			if n, _ := Len(st.cache); n >= st.maxsize {
				keys, _, _ := DictItems(st.cache)
				if err := DelItem(st.cache, keys[0]); err != nil {
					return nil, err
				}
			}
		}
		return result, SetItem(st.cache, key, result)
	})
	ms.method("cache_info", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := state(args[0])
		if err != nil {
			return nil, err
		}
		var maxsize Value = None()
		if st.maxsize >= 0 {
			maxsize = Int(int64(st.maxsize))
		}
		n, err := Len(st.cache)
		if err != nil {
			return nil, err
		}
		return call(cacheInfo, Int(int64(st.hits)), Int(int64(st.misses)), maxsize, Int(int64(n)))
	})
	ms.method("cache_clear", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := state(args[0])
		if err != nil {
			return nil, err
		}
		st.hits, st.misses = 0, 0
		_, err = CallMethod(st.cache, "clear")
		return nil, err
	})
	ms.method("cache_parameters", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := state(args[0])
		if err != nil {
			return nil, err
		}
		var maxsize Value = None()
		if st.maxsize >= 0 {
			maxsize = Int(int64(st.maxsize))
		}
		return StrDict([]string{"maxsize", "typed"}, []Value{maxsize, Bool(st.typed)}), nil
	})
	ms.method("__get__", func(args []Value, kwargs []Keyword) (Value, error) {
		if len(args) < 2 || IsNone(args[1]) {
			return args[0], nil
		}
		return boundMethod(args[0], args[1])
	})
	return ms
}

// boundMethod is types.MethodType(f, obj): f called with obj first.
func boundMethod(f, obj Value) (Value, error) {
	return Func("bound", func(args []Value, kwargs []Keyword) (Value, error) {
		return Call(f, append([]Value{obj}, args...), kwargs)
	}), nil
}

// orderingRoots are the comparisons total_ordering can derive the others
// from.
var orderingRoots = []string{"__lt__", "__le__", "__gt__", "__ge__"}

// derived is one comparison total_ordering adds: its name, the name of the
// function CPython uses, and how it follows from the root's result.
type derived struct {
	name, fname string
	// negate takes not r; combine, when set, joins the result with
	// self == other (or) or self != other (and).
	negate  bool
	combine string
}

// orderingRules is total_ordering's _convert: the comparisons each root
// derives.
var orderingRules = map[string][]derived{
	"__lt__": {{"__gt__", "_gt_from_lt", true, "and!="}, {"__le__", "_le_from_lt", false, "or=="}, {"__ge__", "_ge_from_lt", true, ""}},
	"__le__": {{"__ge__", "_ge_from_le", true, "or=="}, {"__lt__", "_lt_from_le", false, "and!="}, {"__gt__", "_gt_from_le", true, ""}},
	"__gt__": {{"__lt__", "_lt_from_gt", true, "and!="}, {"__ge__", "_ge_from_gt", false, "or=="}, {"__le__", "_le_from_gt", true, ""}},
	"__ge__": {{"__le__", "_le_from_ge", true, "or=="}, {"__gt__", "_gt_from_ge", false, "and!="}, {"__lt__", "_lt_from_ge", true, ""}},
}

// definesOwn reports whether cls or one of its bases other than object
// defines the special method name.
func definesOwn(cls Value, name string) bool {
	mro, err := GetAttr(cls, "__mro__")
	if err != nil {
		return false
	}
	items, _ := SeqItems(mro)
	for _, k := range items {
		if Is(k, Builtin("object")) {
			continue
		}
		d, err := GetAttr(k, "__dict__")
		if err != nil {
			if has, herr := HasAttr(k, name); herr == nil && has {
				return true
			}
			continue
		}
		if in, err := Contains(d, String(name)); err == nil && in {
			return true
		}
	}
	return false
}

// totalOrdering is functools.total_ordering(cls).
func totalOrdering(args []Value, kwargs []Keyword) (Value, error) {
	if err := CheckPositional("total_ordering", len(args), 1, 1); err != nil {
		return nil, err
	}
	cls := args[0]
	roots := map[string]bool{}
	root := ""
	for _, op := range orderingRoots {
		if definesOwn(cls, op) {
			roots[op] = true
			if op > root {
				root = op
			}
		}
	}
	if root == "" {
		return nil, raise("ValueError", "must define at least one ordering operation: < > <= >=")
	}
	for _, d := range orderingRules[root] {
		if roots[d.name] {
			continue
		}
		d := d
		f := Func(d.fname, func(args []Value, kwargs []Keyword) (Value, error) {
			if len(args) != 2 {
				return nil, raise("TypeError", "%s() takes 2 positional arguments but %d were given", d.fname, len(args))
			}
			self, other := args[0], args[1]
			m, err := GetAttr(TypeOf(self), root)
			if err != nil {
				return nil, err
			}
			r, err := call(m, self, other)
			if err != nil || IsNotImplemented(r) {
				return r, err
			}
			t, err := truth(r)
			if err != nil {
				return nil, err
			}
			var first Value = r
			if d.negate {
				first, t = Bool(!t), !t
			}
			switch d.combine {
			case "or==":
				if t {
					return first, nil
				}
				return RichCompare("==", self, other)
			case "and!=":
				if !t {
					return first, nil
				}
				return RichCompare("!=", self, other)
			}
			return first, nil
		})
		if err := SetAttr(f, "__name__", String(d.name)); err != nil {
			return nil, err
		}
		if err := SetAttr(cls, d.name, f); err != nil {
			return nil, err
		}
	}
	return cls, nil
}

// keyState is a cmp_to_key wrapper: the comparison function and, once
// called, the wrapped object.
type keyState struct {
	cmp, obj Value
}

func keyWrapperMethods() *methods {
	ms := &methods{}
	ms.method("__call__", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := stateOf[*keyState](args[0], "functools.KeyWrapper", "__call__")
		if err != nil {
			return nil, err
		}
		buf, err := ParseArgs("K", []string{"obj"}, 1, 1, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		return instance(TypeOf(args[0]), &keyState{cmp: st.cmp, obj: buf[0]})
	})
	for _, op := range []string{"<", "<=", ">", ">=", "==", "!="} {
		op := op
		name := map[string]string{"<": "__lt__", "<=": "__le__", ">": "__gt__", ">=": "__ge__", "==": "__eq__", "!=": "__ne__"}[op]
		ms.method(name, func(args []Value, _ []Keyword) (Value, error) {
			a, err := stateOf[*keyState](args[0], "functools.KeyWrapper", name)
			if err != nil {
				return nil, err
			}
			b, ok := Native(args[1]).(*keyState)
			if !ok {
				return nil, raise("TypeError", "other argument must be K instance")
			}
			r, err := call(a.cmp, a.obj, b.obj)
			if err != nil {
				return nil, err
			}
			return RichCompare(op, r, Int(0))
		})
	}
	ms.property("obj", func(self Value) (Value, error) {
		st, err := stateOf[*keyState](self, "functools.KeyWrapper", "obj")
		if err != nil {
			return nil, err
		}
		if st.obj == nil {
			return nil, raise("AttributeError", "obj")
		}
		return st.obj, nil
	})
	ms.add("__hash__", None())
	return ms
}

func cachedPropertyMethods() *methods {
	ms := &methods{}
	ms.method("__init__", func(args []Value, kwargs []Keyword) (Value, error) {
		if len(args) != 2 {
			return nil, raise("TypeError", "cached_property.__init__() missing 1 required positional argument: 'func'")
		}
		if err := SetAttr(args[0], "func", args[1]); err != nil {
			return nil, err
		}
		if err := SetAttr(args[0], "attrname", None()); err != nil {
			return nil, err
		}
		doc, err := GetAttr(args[1], "__doc__")
		if err != nil {
			doc = None()
		}
		return nil, SetAttr(args[0], "__doc__", doc)
	})
	ms.method("__set_name__", func(args []Value, kwargs []Keyword) (Value, error) {
		cur, err := GetAttr(args[0], "attrname")
		if err != nil {
			return nil, err
		}
		if IsNone(cur) {
			return nil, SetAttr(args[0], "attrname", args[2])
		}
		if eq, err := Compare("==", args[2], cur); err != nil || !eq {
			if err != nil {
				return nil, err
			}
			return nil, raise("TypeError", "Cannot assign the same cached_property to two different names (%s and %s).",
				mustRepr(cur), mustRepr(args[2]))
		}
		return nil, nil
	})
	ms.method("__get__", func(args []Value, kwargs []Keyword) (Value, error) {
		self := args[0]
		if len(args) < 2 || IsNone(args[1]) {
			return self, nil
		}
		inst := args[1]
		name, err := GetAttr(self, "attrname")
		if err != nil {
			return nil, err
		}
		if IsNone(name) {
			return nil, raise("TypeError", "Cannot use cached_property instance without calling __set_name__ on it.")
		}
		d := InstanceDict(inst)
		if d == nil {
			n, _ := AsString(name)
			return nil, raise("TypeError", "No '__dict__' attribute on %s instance to cache %s property.",
				mustRepr(String(TypeName(inst))), mustRepr(String(n)))
		}
		if v, err := GetItem(d, name); err == nil {
			return v, nil
		}
		f, err := GetAttr(self, "func")
		if err != nil {
			return nil, err
		}
		v, err := call(f, inst)
		if err != nil {
			return nil, err
		}
		return v, SetItem(d, name, v)
	})
	return ms
}

// mustRepr is repr(v) for values whose repr cannot fail (strs).
func mustRepr(v Value) string {
	s, err := repr(v)
	if err != nil {
		return "?"
	}
	return s
}

// singledispatch is functools.singledispatch(func): a function dispatching
// on the class of its first argument to the implementations registered
// for that class or the nearest of its bases.
func singledispatch(updateWrapperFn, f Value) (Value, error) {
	registry := EmptyDict()
	if err := SetItem(registry, Builtin("object"), f); err != nil {
		return nil, err
	}
	funcname := "singledispatch function"
	if n, err := GetAttr(f, "__name__"); err == nil {
		if s, ok := AsString(n); ok {
			funcname = s
		}
	}
	dispatch := BuiltinFunc("dispatch", func(args []Value, kwargs []Keyword) (Value, error) {
		if err := CheckPositional("dispatch", len(args), 1, 1); err != nil {
			return nil, err
		}
		mro, err := GetAttr(args[0], "__mro__")
		if err != nil {
			return nil, err
		}
		classes, _ := SeqItems(mro)
		for _, c := range classes {
			if impl, err := GetItem(registry, c); err == nil {
				return impl, nil
			}
		}
		return f, nil
	})
	var register Value
	register = BuiltinFunc("register", func(args []Value, kwargs []Keyword) (Value, error) {
		if err := CheckPositional("register", len(args), 1, 2); err != nil {
			return nil, err
		}
		cls := args[0]
		if isClass(cls) {
			if len(args) == 1 {
				return BuiltinFunc("register", func(fargs []Value, _ []Keyword) (Value, error) {
					if err := CheckPositional("register", len(fargs), 1, 1); err != nil {
						return nil, err
					}
					return call(register, cls, fargs[0])
				}), nil
			}
			return args[1], SetItem(registry, cls, args[1])
		}
		impl := cls
		ann, err := GetAttr(impl, "__annotations__")
		keys, vals, ok := DictItems(ann)
		if err != nil || !ok || len(keys) == 0 {
			r := mustRepr(impl)
			return nil, raise("TypeError", "Invalid first argument to `register()`: %s. Use either `@register(some_class)` or plain `@register` on an annotated function.", r)
		}
		if !isClass(vals[0]) {
			return nil, raise("TypeError", "Invalid annotation for %s. %s is not a class.", mustRepr(keys[0]), mustRepr(vals[0]))
		}
		return impl, SetItem(registry, vals[0], impl)
	})
	wrapper := Func(funcname, func(args []Value, kwargs []Keyword) (Value, error) {
		if len(args) == 0 {
			return nil, raise("TypeError", "%s requires at least 1 positional argument", funcname)
		}
		impl, err := call(dispatch, TypeOf(args[0]))
		if err != nil {
			return nil, err
		}
		return Call(impl, args, kwargs)
	})
	for _, a := range []struct {
		name string
		v    Value
	}{{"register", register}, {"dispatch", dispatch}, {"registry", registry}} {
		if err := SetAttr(wrapper, a.name, a.v); err != nil {
			return nil, err
		}
	}
	return call(updateWrapperFn, wrapper, f)
}
