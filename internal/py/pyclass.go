// Ported from CPython 3.12 (Objects/typeobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Classes defined by Python code: the class statement's type creation, the
// method resolution order, instances and their attributes, bound methods,
// property, classmethod, staticmethod and super, and the special methods
// through which the value layer reaches the instances.

package py

import (
	"strings"
)

// isUser reports whether the class was made by a class statement (or by the
// Python source the package runs at start-up) rather than built in.
func (k *classV) isUser() bool { return k.dict != nil }

// mroList is the class's __mro__.
func (k *classV) mroList() []*classV {
	if k.mro != nil {
		return k.mro
	}
	var out []*classV
	for c := k; c != nil; c = c.base {
		out = append(out, c)
	}
	if out[len(out)-1] != objectClass {
		out = append(out, objectClass)
	}
	return out
}

// lookupUser finds name in the namespaces of the Python-defined classes of
// the MRO.
func (k *classV) lookupUser(name string) (Value, bool) {
	for _, c := range k.mroList() {
		if c.dict != nil {
			if v, ok := c.dict.getStr(name); ok {
				return v, true
			}
		}
	}
	return nil, false
}

// builtinBase is the first builtin class of the MRO.
func (k *classV) builtinBase() *classV {
	for _, c := range k.mroList() {
		if c.dict == nil {
			return c
		}
	}
	return objectClass
}

// qualName is __qualname__.
func (k *classV) qualName() string {
	if k.qual != "" {
		return k.qual
	}
	return k.name
}

// moduleName is __module__ ("builtins" for the builtin classes).
func (k *classV) moduleName() string {
	if k.dict != nil {
		if v, ok := k.dict.getStr("__module__"); ok {
			if s, ok := unsub(v).(strV); ok {
				return string(s)
			}
			return ""
		}
	}
	if k.module != "" {
		return k.module
	}
	return "builtins"
}

// isExcClass reports whether k derives from BaseException.
func isExcClass(k *classV) bool { return k.subclassOf(classes["BaseException"]) }

// c3Merge is the C3 linearization of a class with the given bases.
func c3Merge(k *classV, bases []*classV) ([]*classV, error) {
	seqs := make([][]*classV, 0, len(bases)+1)
	for _, b := range bases {
		seqs = append(seqs, append([]*classV(nil), b.mroList()...))
	}
	seqs = append(seqs, append([]*classV(nil), bases...))
	out := []*classV{k}
	for {
		empty := true
		for _, s := range seqs {
			if len(s) > 0 {
				empty = false
			}
		}
		if empty {
			return out, nil
		}
		var cand *classV
		for _, s := range seqs {
			if len(s) == 0 {
				continue
			}
			c := s[0]
			inTail := false
			for _, t := range seqs {
				for _, x := range t[min(1, len(t)):] {
					if x == c {
						inTail = true
					}
				}
			}
			if !inTail {
				cand = c
				break
			}
		}
		if cand == nil {
			// set_mro_error names the distinct heads of the sequences
			// left to merge.
			var names []string
			seen := map[*classV]bool{}
			for _, s := range seqs {
				if len(s) > 0 && !seen[s[0]] {
					seen[s[0]] = true
					names = append(names, s[0].name)
				}
			}
			return nil, typeErr("Cannot create a consistent method resolution\norder (MRO) for bases %s",
				strings.Join(names, ", "))
		}
		out = append(out, cand)
		for i, s := range seqs {
			if len(s) > 0 && s[0] == cand {
				seqs[i] = s[1:]
			}
		}
	}
}

// newUserClass is type(name, bases, ns) for a class statement.
func newUserClass(name string, bases []*classV, ns *dictV) (*classV, error) {
	if len(bases) == 0 {
		bases = []*classV{objectClass}
	}
	for i, b := range bases {
		for _, o := range bases[:i] {
			if o == b {
				return nil, typeErr("duplicate base class %s", b.name)
			}
		}
		if !b.isUser() && !acceptableBase(b) {
			name := b.name
			if b.module != "" {
				name = b.module + "." + name
			}
			return nil, typeErr("type '%s' is not an acceptable base type", name)
		}
	}
	slots, err := classSlots(ns, bases)
	if err != nil {
		return nil, err
	}
	// best_base: the solid bases of the bases must lie on one line of
	// inheritance.
	solid := objectClass
	for _, b := range bases {
		switch s := solidBase(b); {
		case solid.subclassOf(s):
		case s.subclassOf(solid):
			solid = s
		default:
			return nil, typeErr("multiple bases have instance lay-out conflict")
		}
	}
	k := &classV{name: name, bases: bases, dict: ns, base: bases[0], slots: slots}
	for _, s := range slots {
		if s == "__dict__" || s == "__weakref__" {
			ns.setStr(s, &dataDescV{name: s, owner: name})
		} else {
			ns.setStr(s, &dataDescV{name: s, owner: name, member: true})
		}
	}
	if slots == nil {
		// type_new_descriptors: the first class of a line without an
		// instance dict or weak references gets getset descriptors for
		// them.
		dictBase, weakBase := false, false
		for _, b := range bases {
			for _, c := range b.mroList() {
				if c.isUser() || isExcClass(c) {
					dictBase = true
				}
				if c.isUser() {
					weakBase = true
				}
			}
		}
		if !dictBase {
			ns.setStr("__dict__", &dataDescV{name: "__dict__", owner: name})
		}
		if !weakBase {
			ns.setStr("__weakref__", &dataDescV{name: "__weakref__", owner: name})
		}
	}
	if _, ok := ns.getStr("__doc__"); !ok {
		ns.setStr("__doc__", none)
	}
	if q, ok := ns.getStr("__qualname__"); ok {
		if s, ok := q.(strV); ok {
			k.qual = string(s)
		}
		ns.delStr("__qualname__")
	}
	mro, err := c3Merge(k, bases)
	if err != nil {
		return nil, err
	}
	k.mro = mro
	for _, b := range bases {
		b.subclasses = append(b.subclasses, k)
	}
	k.construct = func(c *callCtx, args []Value, kw *dictV) (Value, error) { return instantiate(c, k, args, kw) }
	return k, nil
}

// classSlots reads the __slots__ of a class namespace: nil when there is
// none, the names it lists otherwise (none for __slots__ = ()).
func classSlots(ns *dictV, bases []*classV) ([]string, error) {
	v, ok := ns.getStr("__slots__")
	if !ok {
		return nil, nil
	}
	items := []Value{v}
	if _, isStr := unsub(v).(strV); !isStr {
		var err error
		if items, err = toSlice(v); err != nil {
			return nil, err
		}
	}
	names := []string{}
	own := 0
	for _, it := range items {
		s, ok := unsub(it).(strV)
		if !ok {
			return nil, typeErr("__slots__ items must be strings, not '%s'", typeName(it))
		}
		if !isIdentifier(string(s)) {
			return nil, typeErr("__slots__ must be identifiers")
		}
		names = append(names, string(s))
		if s == "__dict__" || s == "__weakref__" {
			continue
		}
		own++
		if _, clash := ns.getStr(string(s)); clash {
			return nil, valueErr("%s in __slots__ conflicts with class variable", strRepr(string(s)))
		}
	}
	if b := bases[0].builtinBase(); own > 0 {
		switch b.name {
		case "int", "bytes", "tuple":
			return nil, typeErr("nonempty __slots__ not supported for subtype of '%s'", b.name)
		}
	}
	return names, nil
}

// layoutExceptions are the builtin exception classes whose instances have
// fields of their own, beyond those of their base class.
var layoutExceptions = map[string]bool{"BaseException": true, "BaseExceptionGroup": true, "StopIteration": true,
	"SystemExit": true, "ImportError": true, "OSError": true, "NameError": true, "AttributeError": true,
	"SyntaxError": true, "UnicodeEncodeError": true, "UnicodeDecodeError": true, "UnicodeTranslateError": true}

// solidBase is solid_base(k): the nearest class of k's line of bases whose
// instances have fields their base's lack: a builtin value type, an
// exception class with fields of its own, or a Python-defined class with
// __slots__.
func solidBase(k *classV) *classV {
	for ; k.isUser(); k = k.base {
		for _, s := range k.slots {
			if s != "__dict__" && s != "__weakref__" {
				return k
			}
		}
	}
	if isExcClass(k) {
		for !layoutExceptions[k.name] {
			k = k.base
		}
	}
	return k
}

// metaclassOf is type(k) for a class k: its Python-defined metaclass, or
// type.
func metaclassOf(k *classV) *classV {
	if k.meta != nil {
		return k.meta
	}
	return classes["type"]
}

// calculateMeta is _PyType_CalculateMetaclass: the most derived of meta and
// the classes of bases, which must all be its (non-strict) superclasses.
func calculateMeta(meta *classV, bases []Value) (*classV, error) {
	winner := meta
	for _, b := range bases {
		t := classOf(b)
		switch {
		case winner.subclassOf(t):
		case t.subclassOf(winner):
			winner = t
		default:
			return nil, typeErr("metaclass conflict: the metaclass of a derived class must be a (non-strict) " +
				"subclass of the metaclasses of all its bases")
		}
	}
	return winner, nil
}

// prepareNamespace is the namespace a class statement's body runs in: what
// meta.__prepare__(name, bases, **kw) returns when the metaclass is a
// Python-defined class that defines it, an empty dict otherwise (the
// __prepare__ of type).
func prepareNamespace(meta Value, name string, bases []Value, kw *dictV) (Value, error) {
	mk, ok := meta.(*classV)
	if !ok || !mk.isUser() {
		return newDict(), nil
	}
	prep, ok := mk.lookupUser("__prepare__")
	if !ok {
		return newDict(), nil
	}
	ns, err := callValue(nil, bindDescriptor(prep, nil, mk), []Value{strV(name), newTuple(bases)}, kw)
	if err != nil {
		return nil, err
	}
	if ok, err := isMapping(ns); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, typeErr("%s.__prepare__() must return a mapping, not %s", mk.name, typeName(ns))
	}
	return ns, nil
}

// typeNew is type.__new__(meta, name, bases, ns, **kw): a new class whose
// metaclass is meta (type or a Python-defined subclass of it). A metaclass
// of a base more derived than meta takes its place, its own __new__
// included. Keyword arguments reach object.__init_subclass__, which takes
// none.
func typeNew(c *callCtx, meta *classV, args []Value, kw *dictV) (Value, error) {
	if len(args) != 3 {
		return nil, typeErr("type.__new__() takes exactly 3 arguments (%d given)", len(args))
	}
	name, ok := unsub(args[0]).(strV)
	if !ok {
		return nil, typeErr("type.__new__() argument 1 must be str, not %s", typeName(args[0]))
	}
	bt, ok := args[1].(*tupleV)
	if !ok {
		return nil, typeErr("type.__new__() argument 2 must be tuple, not %s", typeName(args[1]))
	}
	ns, ok := unsub(args[2]).(*dictV)
	if !ok {
		return nil, typeErr("type.__new__() argument 3 must be dict, not %s", typeName(args[2]))
	}
	winner, err := calculateMeta(meta, bt.items)
	if err != nil {
		return nil, err
	}
	if winner != meta {
		if newFn, ok := winner.lookupUser("__new__"); ok {
			return callValue(c, bindDescriptor(newFn, nil, winner), append([]Value{winner}, args...), kw)
		}
		meta = winner
	}
	bases := make([]*classV, len(bt.items))
	for i, b := range bt.items {
		k, ok := b.(*classV)
		if !ok {
			return nil, typeErr("bases must be types")
		}
		bases[i] = k
	}
	dict := ns.copy()
	if _, ok := dict.getStr("__module__"); !ok && c != nil && c.f != nil {
		// The module of the calling code, from the __name__ of its globals.
		if mod, ok := c.f.vm.globals.getStr("__name__"); ok {
			dict.setStr("__module__", mod)
		}
	}
	k, err := newUserClass(string(name), bases, dict)
	if err != nil {
		return nil, err
	}
	if meta != classes["type"] {
		k.meta = meta
	}
	if err := setNames(k); err != nil {
		return nil, err
	}
	if err := initSubclass(k, kw); err != nil {
		return nil, err
	}
	return k, nil
}

// setNames calls __set_name__(k, name) on the attributes of a new class
// whose class defines it, as type.__new__ does.
func setNames(k *classV) error {
	keys := append([]Value(nil), k.dict.keys...)
	vals := append([]Value(nil), k.dict.vals...)
	for i, v := range vals {
		m, ok := userDescriptorMethod(v, "__set_name__")
		if !ok {
			if p, isProp := unsub(v).(*propertyV); isProp {
				if s, ok := unsub(keys[i]).(strV); ok {
					p.name = string(s)
				}
			}
			continue
		}
		if _, err := callValue(nil, m, []Value{k, keys[i]}, nil); err != nil {
			return err
		}
	}
	return nil
}

// initSubclass is super(k, k).__init_subclass__(**kw): the first
// __init_subclass__ after k in its method resolution order (implicitly a
// class method), or object's, which takes no keyword arguments.
func initSubclass(k *classV, kw *dictV) error {
	for _, b := range k.mroList()[1:] {
		if !b.isUser() {
			continue
		}
		m, ok := b.dict.getStr("__init_subclass__")
		if !ok {
			continue
		}
		if _, isCM := m.(*classmethodV); !isCM {
			m = &classmethodV{fn: m}
		}
		_, err := callValue(nil, bindDescriptor(m, nil, k), nil, kw)
		return err
	}
	if kw != nil && kw.Len() > 0 {
		return typeErr("%s.__init_subclass__() takes no keyword arguments", k.name)
	}
	return nil
}

// unacceptableBases are the builtin classes CPython does not let a class
// derive from (their types lack Py_TPFLAGS_BASETYPE).
var unacceptableBases = map[string]bool{"bool": true, "range": true, "slice": true, "NoneType": true,
	"ellipsis": true, "NotImplementedType": true, "UnionType": true}

// acceptableBase reports whether a class statement may name the builtin
// class k as a base. The classes of functions, methods, generators,
// iterators and views (see otherClass) are not acceptable either.
func acceptableBase(k *classV) bool {
	if unacceptableBases[k.name] {
		return false
	}
	otherMu.Lock()
	other := otherClasses[k.name] == k
	otherMu.Unlock()
	return !other
}

// initBased reports whether the builtin class k builds its value in
// __init__ (list, dict, set, bytearray, property): its __new__ makes an
// empty value whatever the arguments. The other builtin types build the
// value from the arguments in __new__.
func initBased(k *classV) bool {
	switch k.name {
	case "list", "dict", "set", "bytearray", "property":
		return true
	}
	return false
}

// newInstance is object.__new__(k), or the __new__ of the builtin type k
// derives from called with args: the instance without its __init__ run. For
// a metaclass, that is the class type.__new__ makes.
func newInstance(c *callCtx, k *classV, args []Value, kw *dictV) (Value, error) {
	if k.subclassOf(classes["BaseExceptionGroup"]) {
		return newExcGroup(k, args, nil)
	}
	if isExcClass(k) {
		e := &excV{Class: k.name, cls: k, args: append([]Value(nil), args...)}
		e.Message = excStr(k.builtinBase().name, e.args)
		return e, nil
	}
	b := k.builtinBase()
	switch b {
	case objectClass:
		return &instV{cls: k, dict: newDict()}, nil
	case classes["type"]:
		return typeNew(c, k, args, kw)
	}
	if b.construct == nil {
		return nil, typeErr("cannot create '%s' instances", b.name)
	}
	if initBased(b) {
		args, kw = nil, nil
	}
	base, err := b.construct(nil, args, kw)
	if err != nil {
		return nil, err
	}
	return &instV{cls: k, dict: newDict(), base: base}, nil
}

// instantiate is calling a Python-defined class: its __new__ (by default
// that of object, BaseException or the builtin type it derives from), then
// __init__ when __new__ returned an instance of the class.
func instantiate(c *callCtx, k *classV, args []Value, kw *dictV) (Value, error) {
	var self Value
	if newFn, ok := k.lookupUser("__new__"); ok {
		r, err := callValue(c, bindDescriptor(newFn, nil, k), append([]Value{k}, args...), kw)
		if err != nil {
			return nil, err
		}
		if !classOf(r).subclassOf(k) {
			return r, nil
		}
		self = r
	} else {
		r, err := newInstance(c, k, args, kw)
		if err != nil {
			return nil, err
		}
		self = r
	}
	init, ok := k.lookupUser("__init__")
	if !ok {
		b := k.builtinBase()
		if x, isExc := self.(*excV); isExc {
			return self, initExc(x, args, kw)
		}
		if initBased(b) {
			m, _ := builtinSlotMethod(b, "__init__")
			if _, err := callValue(nil, m, append([]Value{self}, args...), kw); err != nil {
				return nil, err
			}
			return self, nil
		}
		if b != objectClass {
			return self, nil
		}
		if _, hasNew := k.lookupUser("__new__"); hasNew {
			return self, nil
		}
		if len(args) > 0 || kw != nil && kw.Len() > 0 {
			return nil, typeErr("%s() takes no arguments", k.name)
		}
		return self, nil
	}
	r, err := callValue(nil, bindDescriptor(init, self, k), args, kw)
	if err != nil {
		return nil, err
	}
	if _, isNone := r.(noneV); !isNone {
		return nil, typeErr("__init__() should return None, not '%s'", typeName(r))
	}
	return self, nil
}

// reinit is the __init__ of an init-based builtin type (see initBased) on
// dst, a value of that type, given the value src built from the arguments:
// list, set and bytearray take src's content, dict adds src's items, a
// property takes src's functions.
func reinit(dst, src Value) error {
	switch d := dst.(type) {
	case *listV:
		d.items = src.(*listV).items
	case *dictV:
		s := src.(*dictV)
		for i, key := range s.keys {
			if err := d.set(key, s.vals[i]); err != nil {
				return err
			}
		}
	case *setV:
		d.clear()
		return d.merge(src.(*setV))
	case *bytearrayV:
		d.b = src.(*bytearrayV).b
	case *propertyV:
		*d = *src.(*propertyV)
	}
	return nil
}

// userClassOf is the Python-defined class of v, or nil: that of an instance,
// or the metaclass of a class.
func userClassOf(v Value) *classV {
	switch x := v.(type) {
	case *instV:
		return x.cls
	case *excV:
		return x.cls
	case *classV:
		return x.meta
	}
	return nil
}

// instV is an instance of a Python-defined class that does not derive from
// BaseException. An instance of a class deriving from a builtin type other
// than object holds a value of that type, base, which every operation the
// class does not define acts on, as the builtin type's slots do on the
// instance in CPython.
type instV struct {
	cls  *classV
	dict *dictV
	base Value
	// slotVals holds the values of the attributes the __slots__ of the
	// class and its bases declare, apart from the instance dictionary.
	slotVals map[string]Value
	// native is the Go state of an instance of a class written in Go
	// (SetNative).
	native any
}

func (o *instV) pyType() string {
	if o.cls.tpName != "" {
		return o.cls.tpName
	}
	return o.cls.name
}

// defaultRepr is object.__repr__: the qualified name with the module, or the
// plain name for a class of the builtins module.
func defaultRepr(v Value, k *classV) string {
	mod := k.moduleName()
	if mod != "" && mod != "builtins" {
		return "<" + mod + "." + k.qualName() + " object at " + objAddr(v) + ">"
	}
	return "<" + k.name + " object at " + objAddr(v) + ">"
}

// callStrMethod calls a __str__ or __repr__ and checks that it returned a str.
func callStrMethod(m Value, self Value, name string) (string, error) {
	r, err := callValue(nil, bindDescriptor(m, self, userClassOf(self)), nil, nil)
	if err != nil {
		return "", err
	}
	s, ok := unsub(r).(strV)
	if !ok {
		return "", typeErr("%s returned non-string (type %s)", name, typeName(r))
	}
	return string(s), nil
}

func (o *instV) repr(st *reprState) (string, error) {
	if m, ok := o.cls.lookupUser("__repr__"); ok {
		return callStrMethod(m, o, "__repr__")
	}
	if o.base != nil {
		return baseRepr(o.base, o.cls, st)
	}
	return defaultRepr(o, o.cls), nil
}

// baseRepr is the repr the builtin type of base gives an instance of its
// subclass k: set, frozenset, bytearray and Markup name the class of the
// instance, the other types do not.
func baseRepr(base Value, k *classV, st *reprState) (string, error) {
	switch b := base.(type) {
	case *setV:
		return b.reprAs(k.name, false, st)
	case *bytearrayV:
		return k.name + "(" + bytesRepr(string(b.b)) + ")", nil
	}
	return reprWith(base, st)
}

// builtinValue is the value of the builtin type that v, an instance of a
// Python-defined subclass of that type, holds, when its class does not
// override the special method name through which the type's operation is
// reached; otherwise v itself.
func builtinValue(v Value, name string) Value {
	if x, ok := v.(*instV); ok && x.base != nil {
		if _, overridden := x.cls.lookupUser(name); !overridden {
			return x.base
		}
	}
	return v
}

func (o *instV) str() (string, error) {
	if m, ok := o.cls.lookupUser("__str__"); ok {
		return callStrMethod(m, o, "__str__")
	}
	if o.base != nil {
		if _, hasRepr := o.cls.lookupUser("__repr__"); !hasRepr || hasOwnStrSlot(o.base) {
			return pyStr(o.base)
		}
	}
	return o.repr(nil)
}

// hasOwnStrSlot reports whether the builtin type of v defines __str__ itself
// rather than inheriting object.__str__, which calls repr(): str, bytes,
// bytearray (whose __str__ is the repr of the value itself) and the
// exceptions do.
func hasOwnStrSlot(v Value) bool {
	switch v.(type) {
	case strV, bytesV, *bytearrayV, *excV:
		return true
	}
	return false
}

// callSpecial calls the special method name of a Python-defined class on v.
// found is false when the class does not define it.
func callSpecial(v Value, name string, args ...Value) (Value, bool, error) {
	if userClassOf(v) == nil {
		return nil, false, nil
	}
	return callSpecialOf(v, name, args...)
}

func (o *instV) truth() (bool, error) {
	if t, ok, err := specialTruth(o); ok {
		return t, err
	}
	if o.base != nil {
		return truthy(o.base)
	}
	return true, nil
}

func (o *instV) length() (int, error) {
	if n, ok, err := specialLength(o); ok {
		return n, err
	}
	if o.base != nil {
		return pyLen(o.base)
	}
	return 0, typeErr("object of type '%s' has no len()", o.cls.name)
}

// specialTruth is bool(v) through the __bool__ or else the __len__ that the
// Python-defined class of v defines; ok is false when it defines neither.
func specialTruth(v Value) (t, ok bool, err error) {
	if r, ok, err := callSpecial(v, "__bool__"); ok {
		if err != nil {
			return false, true, err
		}
		b, isBool := r.(boolV)
		if !isBool {
			return false, true, typeErr("__bool__ should return bool, returned %s", typeName(r))
		}
		return bool(b), true, nil
	}
	n, ok, err := specialLength(v)
	return n > 0, ok, err
}

// specialLength is len(v) through the __len__ that the Python-defined class
// of v defines; ok is false when it does not.
func specialLength(v Value) (n int, ok bool, err error) {
	r, ok, err := callSpecial(v, "__len__")
	if !ok || err != nil {
		return 0, ok, err
	}
	i, isInt := asInt(unsub(r))
	if !isInt {
		return 0, true, typeErr("'%s' object cannot be interpreted as an integer", typeName(r))
	}
	if i.Sign() < 0 {
		return 0, true, valueErr("__len__() should return >= 0")
	}
	if !i.IsInt64() || i.Int64() > 1<<62 {
		return 0, true, newPyErr("OverflowError", "cannot fit 'int' into an index-sized integer")
	}
	return int(i.Int64()), true, nil
}

func (o *instV) iter() (iterator, error) {
	// The builtin type's iterator, unless the class defines __iter__: the
	// iteration through __getitem__ is only for types without one.
	if _, ok := o.cls.lookupUser("__iter__"); !ok && o.base != nil {
		return pyIter(o.base)
	}
	return userIter(o)
}

// definesIteration reports whether a Python-defined class makes its
// instances iterable itself, with __iter__ or __getitem__.
func definesIteration(k *classV) bool {
	if _, ok := k.lookupUser("__iter__"); ok {
		return true
	}
	_, ok := k.lookupUser("__getitem__")
	return ok
}

// userIter is iter() of an instance of a Python-defined class.
func userIter(o Value) (iterator, error) {
	k := userClassOf(o)
	if r, ok, err := callSpecial(o, "__iter__"); ok {
		if err != nil {
			return nil, err
		}
		return iterOfIterator(r)
	}
	if _, ok := k.lookupUser("__getitem__"); ok {
		i := int64(0)
		return funcIter(func() (Value, bool, error) {
			v, _, err := callSpecial(o, "__getitem__", mkInt(i))
			if err != nil {
				if isExc(err, "IndexError", "StopIteration") {
					return nil, false, nil
				}
				return nil, false, err
			}
			i++
			return v, true, nil
		}), nil
	}
	return nil, typeErr("'%s' object is not iterable", k.name)
}

// iterOfIterator walks the value __iter__ returned: a builtin iterator or an
// object whose class defines __next__.
func iterOfIterator(r Value) (iterator, error) {
	if k := userClassOf(r); k != nil {
		if _, ok := k.lookupUser("__next__"); !ok {
			return nil, typeErr("iter() returned non-iterator of type '%s'", typeName(r))
		}
		return funcIter(func() (Value, bool, error) {
			v, _, err := callSpecial(r, "__next__")
			if err != nil {
				if isExc(err, "StopIteration") {
					return nil, false, nil
				}
				return nil, false, err
			}
			return v, true, nil
		}), nil
	}
	if !isIterator(r) {
		return nil, typeErr("iter() returned non-iterator of type '%s'", typeName(r))
	}
	return pyIter(r)
}

// isIterator reports whether v has a __next__ method.
func isIterator(v Value) bool {
	switch v.(type) {
	case *iterObj, *genV:
		return true
	}
	if k := userClassOf(v); k != nil {
		_, ok := k.lookupUser("__next__")
		return ok
	}
	return false
}

func (o *instV) getItem(key Value) (Value, error) {
	r, ok, err := callSpecial(o, "__getitem__", key)
	if ok {
		return r, err
	}
	if o.base == nil {
		return nil, typeErr("'%s' object is not subscriptable", o.cls.name)
	}
	r, err = pyGetitem(o.base, key)
	if err != nil && isExc(err, "KeyError") {
		// dict.__getitem__ of a dict subclass asks __missing__ for a key
		// the dict does not hold.
		if _, isDict := unsub(o.base).(*dictV); isDict {
			if m, found, merr := callSpecial(o, "__missing__", key); found {
				return m, merr
			}
		}
	}
	return r, err
}

func (o *instV) setItem(key, value Value) error {
	_, ok, err := callSpecial(o, "__setitem__", key, value)
	if !ok {
		if o.base != nil {
			return pySetitem(o.base, key, value)
		}
		return typeErr("'%s' object does not support item assignment", o.cls.name)
	}
	return err
}

func (o *instV) delItem(key Value) error {
	_, ok, err := callSpecial(o, "__delitem__", key)
	if !ok {
		if o.base != nil {
			return pyDelitem(o.base, key)
		}
		return typeErr("'%s' object doesn't support item deletion", o.cls.name)
	}
	return err
}

func (o *instV) contains(item Value) (bool, error) {
	if r, ok, err := callSpecial(o, "__contains__", item); ok {
		if err != nil {
			return false, err
		}
		return truthy(r)
	}
	if o.base != nil {
		return pyContains(o.base, item)
	}
	return iterContains(o, item)
}

// iterContains is `item in v` for an object whose Python-defined class (or
// metaclass) has no __contains__: a search of what iterating it gives.
func iterContains(v, item Value) (bool, error) {
	it, err := userIter(v)
	if err != nil {
		if isExc(err, "TypeError") {
			return false, typeErr("argument of type '%s' is not iterable", userClassOf(v).name)
		}
		return false, err
	}
	for {
		v, more, err := it.next()
		if err != nil || !more {
			return false, err
		}
		if sameObject(v, item) {
			return true, nil
		}
		eq, err := pyEq(v, item)
		if err != nil || eq {
			return eq, err
		}
	}
}

func (o *instV) isCallable() bool {
	_, ok := o.cls.lookupUser("__call__")
	return ok || o.base != nil && isCallable(o.base)
}

func (o *instV) pyCall(args []Value, kw *dictV) (Value, error) {
	m, ok := o.cls.lookupUser("__call__")
	if !ok {
		if o.base != nil && isCallable(o.base) {
			return callValue(nil, o.base, args, kw)
		}
		return nil, typeErr("'%s' object is not callable", o.cls.name)
	}
	return callValue(nil, bindDescriptor(m, o, o.cls), args, kw)
}

func (o *instV) getAttr(name string) (Value, error) {
	return instanceGetattr(o, o.cls, o.dict, name, o.builtinAttr)
}

// builtinAttr is an attribute the builtin base value of the instance
// provides, nil when it has no base value.
func (o *instV) builtinAttr(name string) (Value, bool) {
	if o.base == nil {
		return nil, false
	}
	return baseAttr(o.base, name)
}

// baseAttr is an attribute the builtin base value of an instance provides:
// its methods, bound to the base value.
func baseAttr(base Value, name string) (Value, bool) {
	return attrOf(base, name)
}

func (o *instV) setAttr(name string, value Value) error {
	return instanceSetattr(o, o.cls, o.dict, name, value)
}

func (o *instV) delAttr(name string) error {
	return instanceDelattr(o, o.cls, o.dict, name)
}

// declaresSlot reports whether name is a slot that the __slots__ of the
// class k or of one of its bases declares.
func declaresSlot(k *classV, name string) bool {
	if name == "__dict__" || name == "__weakref__" {
		return false
	}
	for _, c := range k.mroList() {
		for _, s := range c.slots {
			if s == name {
				return true
			}
		}
	}
	return false
}

// slotted reports whether a class with __slots__ lets its instances have an
// attribute of that name.
func slotted(k *classV, name string) bool {
	for _, c := range k.mroList() {
		if c.dict == nil {
			continue
		}
		if c.slots == nil {
			return true
		}
		for _, s := range c.slots {
			if s == name || s == "__dict__" {
				return true
			}
		}
	}
	return false
}

// bindDescriptor is the __get__ of a class attribute found for an instance
// (self) or for the class itself (self nil).
func bindDescriptor(attr Value, self Value, owner *classV) Value {
	switch x := attr.(type) {
	case *funcV, *goFuncV:
		if self == nil {
			return x
		}
		return &methodV{fn: x, self: self}
	case *classmethodV:
		return &methodV{fn: x.fn, self: owner}
	case *staticmethodV:
		return x.fn
	case *propertyV:
		if self == nil {
			return x
		}
	case *instV:
		// An instance of a subclass of property, classmethod or
		// staticmethod binds as its base value does.
		switch x.base.(type) {
		case *classmethodV, *staticmethodV, *propertyV:
			return bindDescriptor(x.base, self, owner)
		}
	}
	return attr
}

// classGetattr is an attribute of a Python-defined class.
func classGetattr(k *classV, name string) (Value, bool) {
	switch name {
	case "__name__":
		return strV(k.name), true
	case "__qualname__":
		return strV(k.qualName()), true
	case "__module__":
		return strV(k.moduleName()), true
	case "__bases__":
		bases := k.bases
		if bases == nil && k.base != nil && k != objectClass {
			bases = []*classV{k.base}
		}
		items := make([]Value, len(bases))
		for i, b := range bases {
			items[i] = b
		}
		return newTuple(items), true
	case "__mro__":
		mro := k.mroList()
		items := make([]Value, len(mro))
		for i, b := range mro {
			items[i] = b
		}
		return newTuple(items), true
	case "__class__":
		return metaclassOf(k), true
	case "__dict__":
		if k.isUser() {
			return &mappingProxyV{d: k.dict}, true
		}
	case "__base__":
		if k == objectClass || k.base == nil {
			return none, true
		}
		return k.base, true
	case "__subclasses__":
		return newBuiltin("__subclasses__", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("__subclasses__", 0, 0, args, kw); err != nil {
				return nil, err
			}
			items := make([]Value, len(k.subclasses))
			for i, s := range k.subclasses {
				items[i] = s
			}
			return newList(items), nil
		}), true
	}
	if v, ok := k.lookupUser(name); ok {
		return bindDescriptor(v, nil, k), true
	}
	b := k.builtinBase()
	if v, ok := builtinClassAttr(b.name, k, name); ok {
		return v, true
	}
	if m, ok := objectClassMethod(k, name); ok {
		return m, true
	}
	return classAttr(b, name)
}

// The protocols of a class are those of type, unless its metaclass is a
// Python-defined class with the special methods.

func (k *classV) truth() (bool, error) {
	if k.meta != nil {
		if t, ok, err := specialTruth(k); ok {
			return t, err
		}
	}
	return true, nil
}

func (k *classV) length() (int, error) {
	if k.meta != nil {
		if n, ok, err := specialLength(k); ok {
			return n, err
		}
	}
	return 0, typeErr("object of type '%s' has no len()", metaclassOf(k).name)
}

func (k *classV) iter() (iterator, error) {
	if k.meta != nil {
		return userIter(k)
	}
	return nil, typeErr("'type' object is not iterable")
}

func (k *classV) contains(item Value) (bool, error) {
	if k.meta == nil {
		return false, typeErr("argument of type 'type' is not iterable")
	}
	if r, ok, err := callSpecial(k, "__contains__", item); ok {
		if err != nil {
			return false, err
		}
		return truthy(r)
	}
	return iterContains(k, item)
}

func (k *classV) getItem(key Value) (Value, error) {
	if r, ok, err := callSpecial(k, "__getitem__", key); ok {
		return r, err
	}
	if k.isUser() {
		if m, ok := k.lookupUser("__class_getitem__"); ok {
			if _, isCM := m.(*classmethodV); !isCM {
				m = &classmethodV{fn: m}
			}
			return callValue(nil, bindDescriptor(m, nil, k), []Value{key}, nil)
		}
	}
	return classGetitem(k, key)
}

func (k *classV) str() (string, error) {
	if k.meta != nil {
		if m, ok := k.meta.lookupUser("__str__"); ok {
			return callStrMethod(m, k, "__str__")
		}
	}
	return k.repr(nil)
}

// userClassGetattr is getattr(k, name) for a Python-defined class
// (type.__getattribute__): a property of its metaclass, then the attributes
// of the class and its bases, then those of the metaclass, bound to k.
func userClassGetattr(k *classV, name string) (Value, error) {
	var metaAttr Value
	if k.meta != nil {
		if m, ok := k.meta.lookupUser(name); ok {
			if p, isProp := unsub(m).(*propertyV); isProp {
				if p.fget == nil {
					return nil, newPyErr("AttributeError", "property '"+name+"' of '"+k.meta.name+"' object has no getter")
				}
				return callValue(nil, p.fget, []Value{k}, nil)
			}
			metaAttr = m
		}
	}
	if v, ok := k.lookupUser(name); ok {
		if _, isDesc := userDescriptorMethod(v, "__get__"); isDesc {
			return descriptorGet(v, nil, k)
		}
	}
	if name == "__module__" {
		// A class made by type() where the calling code has no __name__
		// has no module.
		if v, ok := k.dict.getStr(name); ok {
			return v, nil
		}
		if k.module != "" {
			return strV(k.module), nil
		}
		return nil, newPyErr("AttributeError", "__module__")
	}
	if v, ok := classGetattr(k, name); ok {
		return v, nil
	}
	if metaAttr != nil {
		return bindDescriptor(metaAttr, k, k.meta), nil
	}
	return nil, typeAttrErr(k.name, name)
}

// methodV is a function bound to an instance or, for a classmethod, a class.
type methodV struct {
	fn   Value
	self Value
}

func (*methodV) pyType() string { return "method" }

func (m *methodV) repr(st *reprState) (string, error) {
	s, err := reprWith(m.self, st)
	if err != nil {
		return "", err
	}
	qual := "?"
	if fn, ok := m.fn.(*funcV); ok {
		qual = fn.qual
	}
	return "<bound method " + qual + " of " + s + ">", nil
}

func (m *methodV) pyCall(args []Value, kw *dictV) (Value, error) {
	return callValue(nil, m.fn, append([]Value{m.self}, args...), kw)
}

func (m *methodV) getAttr(name string) (Value, error) {
	switch name {
	case "__self__":
		return m.self, nil
	case "__func__":
		return m.fn, nil
	}
	if ga, ok := m.fn.(attrGetter); ok {
		return ga.getAttr(name)
	}
	return nil, nil
}

func (m *methodV) eq(other Value) (bool, error) {
	o, ok := other.(*methodV)
	return ok && o.fn == m.fn && sameObject(o.self, m.self), nil
}

// propertyV is a property object.
type propertyV struct {
	fget, fset, fdel, doc Value
	// name is the attribute name __set_name__ gave the property (its
	// prop_name), "" before that.
	name string
}

func (*propertyV) pyType() string { return "property" }

func (p *propertyV) getAttr(name string) (Value, error) {
	mk := func(which string) Value {
		return newBuiltin(which, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(which, 1, 1, args, kw); err != nil {
				return nil, err
			}
			q := *p
			switch which {
			case "getter":
				q.fget = args[0]
			case "setter":
				q.fset = args[0]
			default:
				q.fdel = args[0]
			}
			return &q, nil
		})
	}
	opt := func(v Value) Value {
		if v == nil {
			return none
		}
		return v
	}
	switch name {
	case "getter", "setter", "deleter":
		return mk(name), nil
	case "__set_name__":
		return newBuiltin(name, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 2, 2, args, kw); err != nil {
				return nil, err
			}
			if s, ok := unsub(args[1]).(strV); ok {
				p.name = string(s)
			}
			return none, nil
		}), nil
	case "fget":
		return opt(p.fget), nil
	case "fset":
		return opt(p.fset), nil
	case "fdel":
		return opt(p.fdel), nil
	case "__doc__":
		return opt(p.doc), nil
	}
	return nil, nil
}

// classmethodV and staticmethodV wrap a function stored in a class.
type classmethodV struct{ fn Value }
type staticmethodV struct{ fn Value }

func (*classmethodV) pyType() string  { return "classmethod" }
func (*staticmethodV) pyType() string { return "staticmethod" }

func (c *classmethodV) getAttr(name string) (Value, error) {
	if name == "__func__" {
		return c.fn, nil
	}
	return nil, nil
}

func (s *staticmethodV) getAttr(name string) (Value, error) {
	if name == "__func__" {
		return s.fn, nil
	}
	return nil, nil
}

func (s *staticmethodV) repr(st *reprState) (string, error) {
	r, err := reprWith(s.fn, st)
	return "<staticmethod(" + r + ")>", err
}

func (c *classmethodV) repr(st *reprState) (string, error) {
	r, err := reprWith(c.fn, st)
	return "<classmethod(" + r + ")>", err
}

func (s *staticmethodV) pyCall(args []Value, kw *dictV) (Value, error) {
	return callValue(nil, s.fn, args, kw)
}

// superV is a super object: the attributes of the classes after start in
// the method resolution order of objType, bound to obj. obj is the class
// objType itself when the super object is bound to a class, an instance of
// objType otherwise; both are nil for an unbound super object.
type superV struct {
	start   *classV
	obj     Value
	objType *classV
}

func (*superV) pyType() string { return "super" }

// newSuper is super(start, obj): obj is a class deriving from start or an
// instance of one (supercheck).
func newSuper(start *classV, obj Value) (*superV, error) {
	if k, isClass := obj.(*classV); isClass && k.subclassOf(start) {
		return &superV{start: start, obj: obj, objType: k}, nil
	}
	if t := classOf(obj); t.subclassOf(start) {
		return &superV{start: start, obj: obj, objType: t}, nil
	}
	return nil, typeErr("super(type, obj): obj must be an instance or subtype of type")
}

func (s *superV) repr(*reprState) (string, error) {
	if s.obj == nil {
		return "<super: <class '" + s.start.name + "'>, NULL>", nil
	}
	return "<super: <class '" + s.start.name + "'>, <" + s.objType.name + " object>>", nil
}

func (s *superV) getAttr(name string) (Value, error) {
	switch name {
	case "__thisclass__":
		return s.start, nil
	case "__self__":
		if s.obj == nil {
			return none, nil
		}
		return s.obj, nil
	case "__self_class__":
		if s.objType == nil {
			return none, nil
		}
		return s.objType, nil
	case "__get__":
		// super.__get__(obj, type=None) binds an unbound super object to
		// obj; a bound one, or None, gives the super object back.
		return newBuiltin("__get__", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("__get__", 1, 2, args, kw); err != nil {
				return nil, err
			}
			if s.obj != nil || isNoneV(args[0]) {
				return s, nil
			}
			return newSuper(s.start, args[0])
		}), nil
	}
	if s.obj == nil {
		return nil, nil
	}
	objType := s.objType
	isClass := s.obj == Value(objType)
	mro := objType.mroList()
	i := 0
	for i < len(mro) && mro[i] != s.start {
		i++
	}
	for _, c := range mro[min(i+1, len(mro)):] {
		if c.dict != nil {
			if v, ok := c.dict.getStr(name); ok {
				if isClass {
					return bindDescriptor(v, nil, objType), nil
				}
				return bindDescriptor(v, s.obj, objType), nil
			}
			continue
		}
		m, ok := builtinSlotMethod(c, name)
		if !ok {
			// A method the builtin class defines, which acts on the value
			// of that type an instance of a subclass holds.
			if m, ok = classAttr(c, name); ok {
				if _, isMethod := m.(*methodDescV); !isMethod {
					return m, nil
				}
			}
		}
		if ok {
			if isClass && name != "__init_subclass__" {
				return m, nil
			}
			return &methodV{fn: m, self: s.obj}, nil
		}
	}
	return nil, nil
}

// builtinSlotMethod is the unbound special method name of a builtin class,
// for the ones Python-defined subclasses and metaclasses reach through
// super() or the class: __new__, __init__, type.__call__, and the __str__
// and __repr__ of object and the exceptions. Like the type dicts holding
// them, it gives the same descriptor object on each lookup.
func builtinSlotMethod(c *classV, name string) (Value, bool) {
	m, ok := makeBuiltinSlotMethod(c, name)
	if !ok {
		return nil, false
	}
	return canonicalDescriptor(m), true
}

// makeBuiltinSlotMethod builds the method builtinSlotMethod returns.
func makeBuiltinSlotMethod(c *classV, name string) (Value, bool) {
	if c == objectClass {
		if m, ok := objectMethod(name); ok {
			return m, true
		}
	}
	if c == classes["BaseException"] {
		if m, ok := baseExceptionAttr(name); ok {
			return m, true
		}
	}
	switch name {
	case "__getattribute__", "__setattr__", "__delattr__":
		return objectAttrSlot(name), true
	case "__eq__", "__ne__", "__lt__", "__le__", "__gt__", "__ge__", "__hash__":
		if c == objectClass {
			return objectCompareSlot(name), true
		}
	case "__init_subclass__":
		if c == objectClass {
			// object.__init_subclass__ is a class method taking no
			// arguments.
			return &callable{name: name, typ: "builtin_function_or_method", qual: "object.__init_subclass__",
				fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
					k, _ := args[0].(*classV)
					if kw != nil && kw.Len() > 0 || len(args) > 1 {
						name := "object"
						if k != nil {
							name = k.name
						}
						return nil, typeErr("%s.__init_subclass__() takes no keyword arguments", name)
					}
					return none, nil
				}}, true
		}
	case "__new__":
		return &callable{name: "__new__", typ: "builtin_function_or_method", qual: c.name + ".__new__",
			fn: func(cc *callCtx, args []Value, kw *dictV) (Value, error) {
				if len(args) == 0 {
					return nil, typeErr("%s.__new__(): not enough arguments", c.name)
				}
				k, ok := args[0].(*classV)
				if !ok {
					return nil, typeErr("%s.__new__(X): X is not a type object (%s)", c.name, typeName(args[0]))
				}
				if !k.subclassOf(c) {
					return nil, typeErr("%s.__new__(%s): %s is not a subtype of %s", c.name, k.name, k.name, c.name)
				}
				if c == objectClass && k.builtinBase() != objectClass {
					// The type's own __new__ builds its instances, as
					// tp_new_wrapper insists.
					static := k
					for static.isUser() {
						if _, ok := static.dict.getStr("__new__"); !ok {
							break
						}
						static = static.base
					}
					return nil, typeErr("object.__new__(%s) is not safe, use %s.__new__()", k.name, static.name)
				}
				if !k.isUser() {
					if k.construct == nil {
						return nil, typeErr("cannot create '%s' instances", k.name)
					}
					return k.construct(cc, args[1:], kw)
				}
				if c == objectClass && (len(args) > 1 || kw != nil && kw.Len() > 0) {
					if _, hasNew := k.lookupUser("__new__"); hasNew {
						return nil, typeErr("object.__new__() takes exactly one argument (the type to instantiate)")
					}
					if _, hasInit := k.lookupUser("__init__"); !hasInit {
						return nil, typeErr("%s() takes no arguments", k.name)
					}
				}
				return newInstance(cc, k, args[1:], kw)
			}}, true
	case "__call__":
		if c == classes["type"] {
			return &callable{name: "__call__", typ: "wrapper_descriptor", qual: "type.__call__",
				fn: func(cc *callCtx, args []Value, kw *dictV) (Value, error) {
					if len(args) == 0 {
						return nil, typeErr("descriptor '__call__' of 'type' object needs an argument")
					}
					k, ok := args[0].(*classV)
					if !ok {
						return nil, typeErr("descriptor '__call__' requires a 'type' object but received a '%s'", typeName(args[0]))
					}
					if k.construct == nil {
						return nil, typeErr("cannot create '%s' instances", k.name)
					}
					return k.construct(cc, args[1:], kw)
				}}, true
		}
	case "__init__":
		if c == classes["type"] {
			return &callable{name: "__init__", typ: "wrapper_descriptor", qual: "type.__init__",
				fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
					if len(args) == 0 {
						return nil, typeErr("descriptor '__init__' of 'type' object needs an argument")
					}
					if _, ok := args[0].(*classV); !ok {
						return nil, typeErr("descriptor '__init__' requires a 'type' object but received a '%s'", typeName(args[0]))
					}
					if n := len(args) - 1; n != 1 && n != 3 {
						return nil, typeErr("type.__init__() takes 1 or 3 arguments")
					}
					if len(args) == 2 && kw != nil && kw.Len() > 0 {
						return nil, typeErr("type.__init__() takes no keyword arguments")
					}
					return none, nil
				}}, true
		}
		if initBased(c) {
			return &callable{name: "__init__", typ: "wrapper_descriptor", qual: c.name + ".__init__",
				fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
					if len(args) == 0 {
						return nil, typeErr("descriptor '__init__' of '%s' object needs an argument", c.name)
					}
					if !classOf(args[0]).subclassOf(c) {
						return nil, typeErr("descriptor '__init__' requires a '%s' object but received a '%s'", c.name, typeName(args[0]))
					}
					src, err := c.construct(nil, args[1:], kw)
					if err != nil {
						return nil, err
					}
					return none, reinit(unsub(args[0]), src)
				}}, true
		}
		if isExcClass(c) {
			return &methodDescV{&callable{name: "__init__", typ: "wrapper_descriptor", qual: c.name + ".__init__",
				fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
					if len(args) == 0 {
						return nil, typeErr("descriptor '__init__' of '%s' object needs an argument", c.name)
					}
					e, ok := args[0].(*excV)
					if !ok {
						return nil, typeErr("descriptor '__init__' requires a '%s' object but received a '%s'", c.name, typeName(args[0]))
					}
					return none, initExc(e, args[1:], kw)
				}}}, true
		}
		if c == objectClass {
			return &callable{name: "__init__", typ: "wrapper_descriptor", qual: "object.__init__",
				fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
					if len(args) == 0 {
						return nil, typeErr("descriptor '__init__' of 'object' object needs an argument")
					}
					if len(args) > 1 || kw != nil && kw.Len() > 0 {
						if k := userClassOf(args[0]); k != nil {
							if _, ok := k.lookupUser("__init__"); ok {
								return nil, typeErr("object.__init__() takes exactly one argument (the instance to initialize)")
							}
						}
						return nil, typeErr("%s() takes no arguments", typeName(args[0]))
					}
					return none, nil
				}}, true
		}
	case "__str__", "__repr__":
		if isExcClass(c) || c == objectClass {
			return &callable{name: name, typ: "wrapper_descriptor", qual: c.name + "." + name,
				fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
					if err := posOnly(name, 1, 1, args, kw); err != nil {
						return nil, err
					}
					var s string
					switch x := args[0].(type) {
					case *excV:
						if name == "__str__" {
							str, err := builtinExcStr(c, x)
							if err != nil {
								return nil, err
							}
							s = str
						} else {
							r, err := excReprDefault(x, &reprState{})
							if err != nil {
								return nil, err
							}
							s = r
						}
					default:
						if k := userClassOf(x); k != nil {
							s = defaultRepr(x, k)
						} else {
							r, err := pyRepr(x)
							if err != nil {
								return nil, err
							}
							s = r
						}
					}
					return strV(s), nil
				}}, true
		}
	}
	return nil, false
}

// builtinExcStr is the __str__ the builtin class c gives the exception x
// (SyntaxError's, OSError's, ... or BaseException's), as super().__str__()
// reaches it from a subclass that overrides __str__.
func builtinExcStr(c *classV, x *excV) (string, error) {
	if c == objectClass {
		return x.Message, nil
	}
	if s, ok, err := excFieldStr(x, c.name); ok || err != nil {
		return s, err
	}
	if x.ownStr {
		return x.Message, nil
	}
	return excArgsStr(c.name, x.args)
}

// excClass is type(e) for an exception.
func excClass(e *excV) *classV {
	if e.cls != nil {
		return e.cls
	}
	return excClassOf(e.Class)
}

// excReprDefault is BaseException.__repr__.
func excReprDefault(x *excV, st *reprState) (string, error) {
	if len(x.args) == 1 {
		s, err := reprWith(x.args[0], st)
		if err != nil {
			return "", err
		}
		return x.Class + "(" + s + ")", nil
	}
	if len(x.args) == 0 {
		return x.Class + "()", nil
	}
	s, err := seqRepr("(", ")", x.args, true, st)
	if err != nil {
		return "", err
	}
	return x.Class + s, nil
}

// ---------------------------------------------------------------- operators

// binopNames maps an operator to its special method names.
var binopNames = map[string][2]string{
	"Add": {"__add__", "__radd__"}, "Sub": {"__sub__", "__rsub__"}, "Mult": {"__mul__", "__rmul__"},
	"Div": {"__truediv__", "__rtruediv__"}, "FloorDiv": {"__floordiv__", "__rfloordiv__"},
	"Mod": {"__mod__", "__rmod__"}, "Pow": {"__pow__", "__rpow__"}, "LShift": {"__lshift__", "__rlshift__"},
	"RShift": {"__rshift__", "__rrshift__"}, "BitAnd": {"__and__", "__rand__"}, "BitOr": {"__or__", "__ror__"},
	"BitXor": {"__xor__", "__rxor__"}, "MatMult": {"__matmul__", "__rmatmul__"},
	"DivMod": {"__divmod__", "__rdivmod__"},
}

// specialOf is type(v).name bound to v, when type(v) is a Python-defined
// class that defines it.
func specialOf(v Value, name string) (Value, bool) {
	if k := userClassOf(v); k != nil {
		m, ok := k.lookupUser(name)
		if !ok {
			if x, isInst := v.(*instV); isInst && x.base != nil {
				return baseSpecial(x.base, name)
			}
			return nil, false
		}
		if _, isNone := m.(noneV); isNone {
			return nil, false
		}
		return bindDescriptor(m, v, k), true
	}
	return nil, false
}

// baseSpecial is the special method name that an instance of a subclass of
// a builtin type inherits from the type, bound to the value base the
// instance holds, for the methods the operators look up on a class: the
// arithmetic operators of int, float and complex, which
// answer NotImplemented for an operand they cannot take.
func baseSpecial(base Value, name string) (Value, bool) {
	switch base.(type) {
	case intV, floatV, complexV:
	default:
		return nil, false
	}
	for op, names := range binopNames {
		if op == "MatMult" || name != names[0] && name != names[1] {
			continue
		}
		reflected := name == names[1]
		return newBuiltin(name, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 1, 1, args, kw); err != nil {
				return nil, err
			}
			other := unsub(args[0])
			if _, isComplex := other.(complexV); !isNumber(other) && !isComplex {
				return notImplementedV{}, nil
			}
			if reflected {
				return builtinBinop(op, other, base)
			}
			return builtinBinop(op, base, other)
		}), true
	}
	return nil, false
}

// hasNumberSlot reports whether the number slot of type(v) for op is one
// that Python code (or Markup) defines: the class has the operator or its
// reflection.
func hasNumberSlot(v Value, op string) bool {
	names := binopNames[op]
	if _, ok := specialOf(v, names[0]); ok {
		return true
	}
	_, ok := specialOf(v, names[1])
	return ok
}

// isNotImplemented reports whether v is NotImplemented.
func isNotImplemented(v Value) bool {
	_, ok := v.(notImplementedV)
	return ok
}

// callSpecialOf calls type(v).name(v, args...); found is false when the class
// does not define it.
func callSpecialOf(v Value, name string, args ...Value) (Value, bool, error) {
	m, ok := specialOf(v, name)
	if !ok {
		return nil, false, nil
	}
	r, err := callValue(nil, m, args, nil)
	return r, true, err
}

// slotBinary is SLOT1BINFULL: the number slot of a Python-defined class,
// called with (v, w), where the slot belongs to v's type, w's type or both.
func slotBinary(op string, v, w Value) (Value, error) {
	names := binopNames[op]
	tv, tw := classOf(v), classOf(w)
	doOther := tv != tw && hasNumberSlot(w, op)
	if hasNumberSlot(v, op) {
		if doOther && tw.subclassOf(tv) {
			if _, ok := specialOf(w, names[1]); ok {
				r, _, err := callSpecialOf(w, names[1], v)
				if err != nil || !isNotImplemented(r) {
					return r, err
				}
				doOther = false
			}
		}
		r, ok, err := callSpecialOf(v, names[0], w)
		if ok && (err != nil || !isNotImplemented(r)) {
			return r, err
		}
		if !ok {
			r = notImplementedV{}
		}
		if tv == tw {
			return r, nil
		}
	}
	if doOther {
		if r, ok, err := callSpecialOf(w, names[1], v); ok {
			return r, err
		}
	}
	return notImplementedV{}, nil
}

// builtinNumberSlot is the number slot of a builtin type facing an operand
// whose class defines the operator: only str % x and bytes % x do anything
// but answer NotImplemented. ok is false when the builtin type has no number
// slot for op at all (str and list concatenation and repetition are sequence
// operations, tried after the number slots).
func builtinNumberSlot(op string, v, w Value) (Value, bool, error) {
	switch unsub(v).(type) {
	case strV, bytesV:
		if op != "Mod" {
			return nil, false, nil
		}
		if _, isStr := v.(strV); isStr {
			r, err := pyMod(v, w)
			return r, true, err
		}
		if _, isBytes := v.(bytesV); isBytes {
			r, err := pyMod(v, w)
			return r, true, err
		}
		return notImplementedV{}, true, nil
	case *listV, *tupleV, *bytearrayV:
		return nil, false, nil
	}
	return notImplementedV{}, true, nil
}

// userBinop is binary_op1, followed by the sequence fallbacks, when an
// operand's type has a number slot defined in Python code (or by Markup).
// handled is false when neither does.
func userBinop(op string, a, b Value) (Value, bool, error) {
	sa, sb := hasNumberSlot(a, op), hasNumberSlot(b, op)
	if !sa && !sb {
		return nil, false, nil
	}
	ta, tb := classOf(a), classOf(b)
	if ta == tb {
		sb = false
	}
	var r Value = notImplementedV{}
	var err error
	if sa {
		r, err = slotBinary(op, a, b)
	} else {
		if br, has, berr := builtinNumberSlot(op, a, b); has {
			if berr != nil || !isNotImplemented(br) {
				return br, true, berr
			}
		}
		if sb {
			r, err = slotBinary(op, a, b)
		}
	}
	if err != nil || !isNotImplemented(r) {
		return r, true, err
	}
	switch op {
	case "Add":
		switch unsub(a).(type) {
		case strV, *listV, *tupleV, bytesV, *bytearrayV:
			// The sequence's concatenation slot takes an instance of a
			// subclass of its type on the right.
			r, err := builtinBinop(op, unsub(a), unsub(b))
			if err != nil && isExc(err, "TypeError") {
				// The message names the right operand's own class.
				return nil, true, concatErr(unsub(a), b)
			}
			return r, true, err
		}
	case "Mult":
		// The repetition of a sequence by an int, which may be an instance
		// of a subclass of int.
		for _, s := range []Value{unsub(a), unsub(b)} {
			switch s.(type) {
			case strV, *listV, *tupleV, bytesV, *bytearrayV:
				r, err := builtinBinop(op, unsub(a), unsub(b))
				return r, true, err
			}
		}
	}
	return nil, true, unsupported(binopSymbol(op), a, b)
}

// richNames maps a comparison to its special method and its reflection.
var richNames = map[string][2]string{
	"==": {"__eq__", "__eq__"}, "!=": {"__ne__", "__ne__"}, "<": {"__lt__", "__gt__"},
	"<=": {"__le__", "__ge__"}, ">": {"__gt__", "__lt__"}, ">=": {"__ge__", "__le__"},
}

// userRichMethod calls the comparison method name of v's class, with
// object's defaults (identity for __eq__, the negated __eq__ for __ne__).
func userRichMethod(v Value, name string, other Value) (Value, error) {
	k := userClassOf(v)
	if k != nil {
		if m, ok := k.lookupUser(name); ok {
			return callValue(nil, bindDescriptor(m, v, k), []Value{other}, nil)
		}
		if x, isInst := v.(*instV); isInst && x.base != nil {
			return userRichMethod(x.base, name, unsub(other))
		}
	}
	switch name {
	case "__eq__":
		if k == nil {
			return builtinRich("==", v, other)
		}
		if sameObject(v, other) {
			return trueV, nil
		}
		return notImplementedV{}, nil
	case "__ne__":
		if k == nil {
			return builtinRich("!=", v, other)
		}
		r, err := userRichMethod(v, "__eq__", other)
		if err != nil || isNotImplemented(r) {
			return r, err
		}
		t, err := truthy(r)
		return pyBool(!t), err
	}
	if k == nil {
		sym := map[string]string{"__lt__": "<", "__le__": "<=", "__gt__": ">", "__ge__": ">="}[name]
		return builtinRich(sym, v, other)
	}
	return notImplementedV{}, nil
}

// builtinRich is a builtin type's comparison slot: NotImplemented where the
// types cannot be compared.
func builtinRich(op string, a, b Value) (Value, error) {
	if op == "==" || op == "!=" {
		if userClassOf(b) != nil {
			if sameObject(a, b) {
				return pyBool(op == "=="), nil
			}
			return notImplementedV{}, nil
		}
		eq, err := pyEq(a, b)
		if err != nil {
			return nil, err
		}
		return pyBool(eq == (op == "==")), nil
	}
	if userClassOf(b) != nil {
		return notImplementedV{}, nil
	}
	r, err := pyCompare(op, a, b)
	if err != nil {
		if isExc(err, "TypeError") {
			return notImplementedV{}, nil
		}
		return nil, err
	}
	return pyBool(r), nil
}

// userRichCompare is do_richcompare when an operand's class is defined by
// Python code.
func userRichCompare(op string, a, b Value) (Value, error) {
	names := richNames[op]
	ta, tb := classOf(a), classOf(b)
	checkedReverse := false
	if ta != tb && tb.subclassOf(ta) && userClassOf(b) != nil {
		if _, ok := userClassOf(b).lookupUser(names[1]); ok {
			checkedReverse = true
			r, err := userRichMethod(b, names[1], a)
			if err != nil || !isNotImplemented(r) {
				return r, err
			}
		}
	}
	r, err := userRichMethod(a, names[0], b)
	if err != nil || !isNotImplemented(r) {
		return r, err
	}
	if !checkedReverse {
		r, err = userRichMethod(b, names[1], a)
		if err != nil || !isNotImplemented(r) {
			return r, err
		}
	}
	switch op {
	case "==":
		return pyBool(sameObject(a, b)), nil
	case "!=":
		return pyBool(!sameObject(a, b)), nil
	}
	return nil, typeErr("'%s' not supported between instances of '%s' and '%s'", op, typeName(a), typeName(b))
}

// userHash is hash() of an instance of a Python-defined class: __hash__, or
// unhashable when the class defines __eq__ without it, or the identity.
func userHash(v Value) (int64, bool, error) {
	k := userClassOf(v)
	if k == nil {
		return 0, false, nil
	}
	for _, c := range k.mroList() {
		if c.dict == nil {
			break
		}
		if h, ok := c.dict.getStr("__hash__"); ok {
			if _, isNone := h.(noneV); isNone {
				return 0, true, unhashable(v)
			}
			r, err := callValue(nil, bindDescriptor(h, v, k), nil, nil)
			if err != nil {
				return 0, true, err
			}
			i, ok := asInt(unsub(r))
			if !ok {
				return 0, true, typeErr("__hash__ method should return an integer")
			}
			// As slot_tp_hash: a result that fits a Py_hash_t is the hash
			// itself (-1 becoming -2), a larger one is hashed as an int.
			if i.IsInt64() {
				if h := i.Int64(); h != -1 {
					return h, true, nil
				}
				return -2, true, nil
			}
			return intHash(i), true, nil
		}
		if _, ok := c.dict.getStr("__eq__"); ok {
			return 0, true, unhashable(v)
		}
	}
	if x, isInst := v.(*instV); isInst && x.base != nil {
		h, err := pyHash(x.base)
		return h, true, err
	}
	return int64(objID(v) >> 4), true, nil
}

// userUnary applies -, + or ~ to an instance of a Python-defined class.
func userUnary(op string, v Value) (Value, bool, error) {
	name := map[string]string{"USub": "__neg__", "UAdd": "__pos__", "Invert": "__invert__"}[op]
	if name == "" || userClassOf(v) == nil {
		return nil, false, nil
	}
	r, ok, err := callSpecialOf(v, name)
	if !ok {
		sym := map[string]string{"USub": "-", "UAdd": "+", "Invert": "~"}[op]
		if x, isInst := v.(*instV); isInst && x.base != nil {
			r, err := unop(op, x.base)
			if err != nil && isExc(err, "TypeError") && strings.HasPrefix(asExc(err).Message, "bad operand type for unary ") {
				err = typeErr("bad operand type for unary %s: '%s'", sym, typeName(v))
			}
			return r, true, err
		}
		return nil, true, typeErr("bad operand type for unary %s: '%s'", sym, typeName(v))
	}
	return r, true, err
}

// userReversed is reversed() of an instance of a Python-defined class:
// __reversed__, or the sequence protocol (__len__ and __getitem__).
func userReversed(v Value, k *classV) (Value, error) {
	if r, ok, err := callSpecial(v, "__reversed__"); ok {
		return r, err
	}
	if x, isInst := v.(*instV); isInst && x.base != nil {
		// list and dict define __reversed__; the other types go through
		// the sequence protocol, with the class's own __getitem__ if any.
		_, ownGetitem := k.lookupUser("__getitem__")
		switch x.base.(type) {
		case *listV, *dictV:
			ownGetitem = false
		}
		if !ownGetitem {
			return builtinReversed(nil, []Value{x.base}, nil)
		}
	}
	if _, ok := k.lookupUser("__getitem__"); !ok {
		return nil, typeErr("'%s' object is not reversible", k.name)
	}
	n, err := pyLen(v)
	if err != nil {
		return nil, err
	}
	i := n - 1
	return &iterObj{typ: "reversed", it: funcIter(func() (Value, bool, error) {
		if i < 0 {
			return nil, false, nil
		}
		item, _, err := callSpecial(v, "__getitem__", mkInt(int64(i)))
		if err != nil {
			if isExc(err, "IndexError", "StopIteration") {
				i = -1
				return nil, false, nil
			}
			return nil, false, err
		}
		i--
		return item, true, nil
	})}, nil
}

// classSetattr is type.__setattr__: a data descriptor of the metaclass
// takes the value, the special names __name__, __qualname__ and __doc__
// update the class, anything else goes into the namespace of a class made
// by Python or Go code; builtin classes are immutable.
func classSetattr(k *classV, name string, value Value) error {
	if k.meta != nil {
		if m, ok := k.meta.lookupUser(name); ok {
			if p, isProp := unsub(m).(*propertyV); isProp {
				if p.fset == nil {
					return newPyErr("AttributeError", "property '"+name+"' of '"+k.meta.name+"' object has no setter")
				}
				_, err := callValue(nil, p.fset, []Value{k, value}, nil)
				return err
			}
			if sm, ok := specialOf(m, "__set__"); ok {
				_, err := callValue(nil, sm, []Value{k, value}, nil)
				return err
			}
		}
	}
	if !k.isUser() {
		return typeErr("cannot set '%s' attribute of immutable type '%s'", name, k.name)
	}
	switch name {
	case "__name__", "__qualname__":
		s, ok := value.(strV)
		if !ok {
			return typeErr("can only assign string to %s.%s, not '%s'", k.name, name, typeName(value))
		}
		if name == "__name__" {
			k.name = string(s)
		} else {
			k.qual = string(s)
		}
		return nil
	}
	k.dict.setStr(name, value)
	return nil
}

// classDelattr is type.__delattr__.
func classDelattr(k *classV, name string) error {
	if !k.isUser() {
		return typeErr("cannot set '%s' attribute of immutable type '%s'", name, k.name)
	}
	if _, ok := k.dict.getStr(name); !ok {
		return typeAttrErr(k.name, name)
	}
	k.dict.delStr(name)
	return nil
}

// mappingProxyV is a read-only view of a class namespace (type.__dict__).
type mappingProxyV struct{ d *dictV }

func (*mappingProxyV) pyType() string { return "mappingproxy" }

func (m *mappingProxyV) repr(st *reprState) (string, error) {
	r, err := reprWith(m.d, st)
	if err != nil {
		return "", err
	}
	return "mappingproxy(" + r + ")", nil
}

func (m *mappingProxyV) getItem(key Value) (Value, error) { return pyGetitem(m.d, key) }

// str is str() of the mapping, as mappingproxy.__str__ is.
func (m *mappingProxyV) str() (string, error) { return pyStr(m.d) }

func (m *mappingProxyV) contains(item Value) (bool, error) { return pyContains(m.d, item) }

func (m *mappingProxyV) iter() (iterator, error) { return pyIter(m.d) }

func (m *mappingProxyV) length() (int, error) { return m.d.Len(), nil }

func (m *mappingProxyV) getAttr(name string) (Value, error) {
	switch name {
	case "keys", "values", "items", "get", "copy", "__getitem__", "__contains__", "__len__", "__iter__":
		return pyGetattr(m.d, name)
	}
	return nil, nil
}

// objectCompareSlot is one of object's comparison slots or __hash__:
// __eq__ is identity, __ne__ inverts the type's __eq__, the orderings are
// NotImplemented, and __hash__ hashes by identity.
func objectCompareSlot(name string) *methodDescV {
	want := 2
	if name == "__hash__" {
		want = 1
	}
	return &methodDescV{&callable{name: name, typ: "wrapper_descriptor", qual: "object." + name,
		fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, want, want, args, kw); err != nil {
				return nil, err
			}
			self := args[0]
			switch name {
			case "__hash__":
				return mkInt(int64(objID(self) >> 4)), nil
			case "__eq__":
				if sameObject(self, args[1]) {
					return trueV, nil
				}
			case "__ne__":
				r, err := userRichMethod(self, "__eq__", args[1])
				if err != nil || isNotImplemented(r) {
					return r, err
				}
				t, err := truthy(r)
				return pyBool(!t), err
			}
			return notImplementedV{}, nil
		}}}
}
