// Ported from CPython 3.12 (Objects/object.c PyObject_GenericGetAttr and
// PyObject_GenericSetAttr, Objects/typeobject.c slot_tp_getattr_hook) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Attribute access on instances of classes made by Python or Go code: __getattribute__,
// __getattr__, __setattr__ and __delattr__ overrides, data and non-data descriptors (property,
// functions, classmethod, staticmethod, and objects whose class defines __get__, __set__ or
// __delete__), __slots__, and the instance __dict__.

package py

// slotStore is the storage of the __slots__ values of an instance.
func slotStore(self Value) map[string]Value {
	if o, ok := self.(*instV); ok {
		if o.slotVals == nil {
			o.slotVals = map[string]Value{}
		}
		return o.slotVals
	}
	return nil
}

// attrName is the name argument of getattr, setattr, delattr, hasattr and
// the attribute slot methods: a str (or an instance of a subclass).
func attrName(v Value) (string, error) {
	s, ok := unsub(v).(strV)
	if !ok {
		return "", typeErr("attribute name must be string, not '%s'", typeName(v))
	}
	return string(s), nil
}

// instanceGetattr is getattr(self, name) for an instance of class k with
// instance dictionary dict (nil without one): the class's
// __getattribute__ (object's generic one by default), then __getattr__
// when that raises AttributeError. builtin supplies the attributes of a
// builtin base value. The result is nil, nil when nothing provides the
// attribute.
func instanceGetattr(self Value, k *classV, dict *dictV, name string,
	builtin func(string) (Value, bool)) (Value, error) {
	var v Value
	var err error
	if ga, ok := k.lookupUser("__getattribute__"); ok {
		v, err = callValue(nil, bindDescriptor(ga, self, k), []Value{strV(name)}, nil)
	} else {
		v, err = objectGetattribute(self, k, dict, name, builtin)
	}
	if err == nil && v != nil {
		return v, nil
	}
	if err != nil && !isExc(err, "AttributeError") {
		return nil, err
	}
	if ga, ok := k.lookupUser("__getattr__"); ok {
		return callValue(nil, bindDescriptor(ga, self, k), []Value{strV(name)}, nil)
	}
	return nil, err
}

// objectGetattribute is object.__getattribute__: a data descriptor of the
// class, the instance dictionary, a non-data descriptor or plain class
// attribute, then the builtin base value's attributes. nil, nil when none
// has the attribute.
func objectGetattribute(self Value, k *classV, dict *dictV, name string,
	builtin func(string) (Value, bool)) (Value, error) {
	switch name {
	case "__class__":
		return k, nil
	case "__dict__":
		if dict != nil && slotted(k, "__dict__") {
			return dict, nil
		}
	case "__weakref__":
		if _, ok := k.lookupUser(name); ok {
			// No weak reference to the instance exists.
			return none, nil
		}
	}
	if declaresSlot(k, name) {
		if v, ok := slotStore(self)[name]; ok {
			return v, nil
		}
		return nil, nil
	}
	tattr, inType := k.lookupUser(name)
	if inType && isDataDescriptor(tattr) {
		return descriptorGet(tattr, self, k)
	}
	if dict != nil {
		if v, ok := dict.getStr(name); ok {
			return v, nil
		}
	}
	if inType {
		return descriptorGet(tattr, self, k)
	}
	if name == "__module__" {
		return strV(k.moduleName()), nil
	}
	if builtin != nil {
		if v, ok := builtin(name); ok {
			return v, nil
		}
	}
	if v, ok := objectInstanceAttr(self, k, name); ok {
		return v, nil
	}
	return nil, nil
}

// userDescriptorMethod is the special method name of the class of a value
// that is an instance of a class made by Python or Go code.
func userDescriptorMethod(attr Value, name string) (Value, bool) {
	k := userClassOf(attr)
	if k == nil {
		return nil, false
	}
	if _, isClass := attr.(*classV); isClass {
		return nil, false
	}
	m, ok := k.lookupUser(name)
	if !ok || isNoneV(m) {
		return nil, false
	}
	return bindDescriptor(m, attr, k), true
}

// isDataDescriptor reports whether a class attribute takes precedence
// over the instance dictionary: a property, or an object whose class
// defines __set__ or __delete__.
func isDataDescriptor(attr Value) bool {
	if _, ok := unsub(attr).(*propertyV); ok {
		return true
	}
	if _, ok := userDescriptorMethod(attr, "__set__"); ok {
		return true
	}
	_, ok := userDescriptorMethod(attr, "__delete__")
	return ok
}

// descriptorGet is attr.__get__(self, owner) for a class attribute found
// for an instance (self) or for the class itself (self nil).
func descriptorGet(attr Value, self Value, owner *classV) (Value, error) {
	if p, ok := unsub(attr).(*propertyV); ok {
		if self == nil {
			return attr, nil
		}
		if p.fget == nil {
			return nil, propertyError(p, owner, "getter")
		}
		return callValue(nil, p.fget, []Value{self}, nil)
	}
	if get, ok := userDescriptorMethod(attr, "__get__"); ok {
		var inst Value = none
		if self != nil {
			inst = self
		}
		return callValue(nil, get, []Value{inst, owner}, nil)
	}
	return bindDescriptor(attr, self, owner), nil
}

// propertyError is the AttributeError of a property used on an instance of
// class k without the function it needs (what is getter, setter or
// deleter): property_descr_get and property_descr_set name the property by
// the name __set_name__ gave it, and the class by its qualified name.
func propertyError(p *propertyV, k *classV, what string) error {
	if p.name != "" {
		return newPyErr("AttributeError", "property "+strRepr(p.name)+" of "+strRepr(k.qualName())+" object has no "+what)
	}
	return newPyErr("AttributeError", "property of "+strRepr(k.qualName())+" object has no "+what)
}

// instanceSetattr is setattr(self, name, value): the class's __setattr__
// when it defines one, else object.__setattr__.
func instanceSetattr(self Value, k *classV, dict *dictV, name string, value Value) error {
	if sa, ok := k.lookupUser("__setattr__"); ok {
		_, err := callValue(nil, bindDescriptor(sa, self, k), []Value{strV(name), value}, nil)
		return err
	}
	return objectSetattr(self, k, dict, name, value)
}

// objectSetattr is object.__setattr__: a data descriptor of the class takes
// the value, else a slot or the instance dictionary.
func objectSetattr(self Value, k *classV, dict *dictV, name string, value Value) error {
	if tattr, ok := k.lookupUser(name); ok {
		if p, ok := unsub(tattr).(*propertyV); ok {
			if p.fset == nil {
				return propertyError(p, k, "setter")
			}
			_, err := callValue(nil, p.fset, []Value{self, value}, nil)
			return err
		}
		if set, ok := userDescriptorMethod(tattr, "__set__"); ok {
			_, err := callValue(nil, set, []Value{self, value}, nil)
			return err
		}
	}
	if declaresSlot(k, name) {
		slotStore(self)[name] = value
		return nil
	}
	if name == "__class__" || name == "__dict__" {
		return typeErr("__class__ assignment only supported for mutable types or ModuleType subclasses")
	}
	if dict == nil || k.slots != nil && !slotted(k, name) {
		return newPyErr("AttributeError", "'"+k.name+"' object has no attribute '"+name+"'")
	}
	dict.setStr(name, value)
	return nil
}

// instanceDelattr is delattr(self, name): the class's __delattr__ when it
// defines one, else object.__delattr__.
func instanceDelattr(self Value, k *classV, dict *dictV, name string) error {
	if da, ok := k.lookupUser("__delattr__"); ok {
		_, err := callValue(nil, bindDescriptor(da, self, k), []Value{strV(name)}, nil)
		return err
	}
	return objectDelattr(self, k, dict, name)
}

// objectDelattr is object.__delattr__.
func objectDelattr(self Value, k *classV, dict *dictV, name string) error {
	if tattr, ok := k.lookupUser(name); ok {
		if p, ok := unsub(tattr).(*propertyV); ok {
			if p.fdel == nil {
				return propertyError(p, k, "deleter")
			}
			_, err := callValue(nil, p.fdel, []Value{self}, nil)
			return err
		}
		if del, ok := userDescriptorMethod(tattr, "__delete__"); ok {
			_, err := callValue(nil, del, []Value{self}, nil)
			return err
		}
	}
	if declaresSlot(k, name) {
		store := slotStore(self)
		if _, ok := store[name]; !ok {
			// A member descriptor names only the attribute.
			return newPyErr("AttributeError", name)
		}
		delete(store, name)
		return nil
	}
	if dict == nil {
		return attrErr(k.name, name)
	}
	if found, err := dict.del(strV(name)); err != nil {
		return err
	} else if !found {
		return attrErr(k.name, name)
	}
	return nil
}

// selfParts gives the class and instance dictionary of an instance for
// the object.__getattribute__/__setattr__/__delattr__ slot methods.
func selfParts(self Value) (*classV, *dictV, func(string) (Value, bool)) {
	switch x := self.(type) {
	case *instV:
		return x.cls, x.dict, x.builtinAttr
	case *excV:
		return excClass(x), x.attrDict(), func(n string) (Value, bool) { return excBuiltinAttr(x, n) }
	}
	return classOf(self), nil, func(n string) (Value, bool) { return attrOf(self, n) }
}

// objectAttrSlot is object.__getattribute__, __setattr__ or __delattr__
// as a function of the instance, for super() and explicit calls.
func objectAttrSlot(name string) *callable {
	return &callable{name: name, typ: "wrapper_descriptor", qual: "object." + name,
		fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) {
			want := 2
			if name == "__setattr__" {
				want = 3
			}
			if err := posOnly(name, want, want, args, kw); err != nil {
				return nil, err
			}
			attr, err := attrName(args[1])
			if err != nil {
				return nil, err
			}
			k, dict, builtin := selfParts(args[0])
			switch name {
			case "__getattribute__":
				v, err := objectGetattribute(args[0], k, dict, string(attr), builtin)
				if err != nil {
					return nil, err
				}
				if v == nil {
					return nil, attrErr(k.name, string(attr))
				}
				return v, nil
			case "__setattr__":
				if k.isUser() {
					return none, objectSetattr(args[0], k, dict, string(attr), args[2])
				}
				return none, pySetattr(args[0], string(attr), args[2])
			}
			if k.isUser() {
				return none, objectDelattr(args[0], k, dict, string(attr))
			}
			return none, pyDelattr(args[0], string(attr))
		}}
}
