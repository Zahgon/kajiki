// Ported from CPython 3.12 (Objects/object.c _dir_object, Objects/typeobject.c type___dir__
// and merge_class_dict, Objects/moduleobject.c module___dir__) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The builtin dir().

package py

func init() {
	builtinsTable["dir"] = newBuiltin("dir", builtinDir)
}

// builtinDir is dir([object]): the names of the local scope, or those
// type(object).__dir__ lists, sorted.
func builtinDir(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("dir", 0, 1, args, kw); err != nil {
		return nil, err
	}
	var names Value
	if len(args) == 0 {
		var locals Value = newDict()
		if c != nil && c.f != nil {
			locals = localsOf(c.f)
		}
		keys, err := toSlice(locals)
		if err != nil {
			return nil, err
		}
		names = newList(keys)
	} else {
		var err error
		if names, err = dirOf(args[0]); err != nil {
			return nil, err
		}
	}
	items, err := toSlice(names)
	if err != nil {
		return nil, err
	}
	l := newList(items)
	if _, err := methodsListSort(l, nil, nil); err != nil {
		return nil, err
	}
	return l, nil
}

// dirOf is type(v).__dir__(v): a user-defined __dir__, module.__dir__,
// type.__dir__ for a class, object.__dir__ otherwise.
func dirOf(v Value) (Value, error) {
	if k := userClassOf(v); k != nil {
		if m, ok := k.lookupUser("__dir__"); ok {
			return callValue(nil, bindDescriptor(m, v, k), nil, nil)
		}
	}
	names := newDict()
	switch x := v.(type) {
	case *moduleV:
		if f, ok := x.dict.getStr("__dir__"); ok {
			return callValue(nil, f, nil, nil)
		}
		return newList(append([]Value(nil), x.dict.keys...)), nil
	case *classV:
		if err := mergeClassDict(names, x); err != nil {
			return nil, err
		}
		return newList(append([]Value(nil), names.keys...)), nil
	case *instV:
		if x.dict != nil {
			for _, key := range x.dict.keys {
				if err := names.set(key, none); err != nil {
					return nil, err
				}
			}
		}
	case *excV:
		if x.attrs != nil {
			for _, key := range x.attrs.keys {
				if err := names.set(key, none); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := mergeClassDict(names, classOf(v)); err != nil {
		return nil, err
	}
	return newList(append([]Value(nil), names.keys...)), nil
}

// mergeClassDict is merge_class_dict: the names of k's __dict__ and those of
// its bases, recursively, added to names.
func mergeClassDict(names *dictV, k *classV) error {
	if k.isUser() {
		for _, key := range k.dict.keys {
			if err := names.set(key, none); err != nil {
				return err
			}
		}
		for _, b := range k.bases {
			if err := mergeClassDict(names, b); err != nil {
				return err
			}
		}
		return nil
	}
	if own, ok := typeDictNames[k.name]; ok {
		for _, n := range own {
			names.setStr(n, none)
		}
	}
	if k.base != nil && k.base != k {
		return mergeClassDict(names, k.base)
	}
	if k != objectClass {
		return mergeClassDict(names, objectClass)
	}
	return nil
}
