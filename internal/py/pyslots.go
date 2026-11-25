// Ported from CPython 3.12 (Objects/typeobject.c slot wrappers, Objects/longobject.c,
// Objects/floatobject.c, Objects/complexobject.c, Objects/listobject.c, Objects/dictobject.c,
// Objects/setobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The special methods of the builtin types reached as attributes: (5).__add__, list.__iadd__,
// float.__round__. Each one performs the operation its slot performs, returning NotImplemented
// where the slot does for an operand of another type.

package py

import (
	"math"
	"math/big"
)

// Special methods every builtin type inherits from object.
var objectSlots = []string{"__eq__", "__ne__", "__lt__", "__le__", "__gt__", "__ge__", "__hash__", "__repr__",
	"__str__", "__format__"}

var (
	intSlots = []string{"__abs__", "__add__", "__and__", "__bool__", "__ceil__", "__divmod__", "__float__",
		"__floor__", "__floordiv__", "__index__", "__int__", "__invert__", "__lshift__", "__mod__", "__mul__",
		"__neg__", "__or__", "__pos__", "__pow__", "__radd__", "__rand__", "__rdivmod__", "__rfloordiv__",
		"__rlshift__", "__rmod__", "__rmul__", "__ror__", "__round__", "__rpow__", "__rrshift__", "__rshift__",
		"__rsub__", "__rtruediv__", "__rxor__", "__sub__", "__truediv__", "__trunc__", "__xor__"}
	floatSlots = []string{"__abs__", "__add__", "__bool__", "__ceil__", "__divmod__", "__float__", "__floor__",
		"__floordiv__", "__int__", "__mod__", "__mul__", "__neg__", "__pos__", "__pow__", "__radd__", "__rdivmod__",
		"__rfloordiv__", "__rmod__", "__rmul__", "__round__", "__rpow__", "__rsub__", "__rtruediv__", "__sub__",
		"__truediv__", "__trunc__"}
	complexSlots = []string{"__abs__", "__add__", "__bool__", "__complex__", "__mul__", "__neg__", "__pos__",
		"__pow__", "__radd__", "__rmul__", "__rpow__", "__rsub__", "__rtruediv__", "__sub__", "__truediv__"}
	strSlots   = []string{"__add__", "__contains__", "__getitem__", "__iter__", "__len__", "__mod__", "__mul__", "__rmod__", "__rmul__"}
	bytesSlots = []string{"__add__", "__bytes__", "__contains__", "__getitem__", "__iter__", "__len__", "__mod__",
		"__mul__", "__rmod__", "__rmul__"}
	bytearraySlots = []string{"__add__", "__contains__", "__delitem__", "__getitem__", "__iadd__", "__imul__",
		"__iter__", "__len__", "__mod__", "__mul__", "__rmod__", "__rmul__", "__setitem__"}
	listSlots = []string{"__add__", "__contains__", "__delitem__", "__getitem__", "__iadd__", "__imul__", "__iter__",
		"__len__", "__mul__", "__reversed__", "__rmul__", "__setitem__"}
	tupleSlots = []string{"__add__", "__contains__", "__getitem__", "__iter__", "__len__", "__mul__", "__rmul__"}
	dictSlots  = []string{"__contains__", "__delitem__", "__getitem__", "__ior__", "__iter__", "__len__", "__or__",
		"__reversed__", "__ror__", "__setitem__"}
	frozensetSlots = []string{"__and__", "__contains__", "__iter__", "__len__", "__or__", "__rand__", "__ror__",
		"__rsub__", "__rxor__", "__sub__", "__xor__"}
	setSlots       = append([]string{"__iand__", "__ior__", "__isub__", "__ixor__"}, frozensetSlots...)
	rangeSlots     = []string{"__bool__", "__contains__", "__getitem__", "__iter__", "__len__", "__reversed__"}
	dictViewSlots  = []string{"__and__", "__contains__", "__iter__", "__len__", "__or__", "__rand__", "__reversed__", "__ror__", "__rsub__", "__rxor__", "__sub__", "__xor__"}
	dictValueSlots = []string{"__iter__", "__len__", "__reversed__"}
	iteratorSlots  = []string{"__iter__", "__next__"}
)

// slotTable maps a builtin type's name to the set of special methods its
// instances have.
var slotTable = map[string]map[string]bool{}

func init() {
	for name, own := range map[string][]string{
		"int": intSlots, "bool": intSlots, "float": floatSlots, "complex": complexSlots, "str": strSlots,
		"bytes": bytesSlots, "bytearray": bytearraySlots, "list": listSlots, "tuple": tupleSlots,
		"dict": dictSlots, "set": setSlots, "frozenset": frozensetSlots, "range": rangeSlots,
		"dict_keys": dictViewSlots, "dict_items": dictViewSlots, "dict_values": dictValueSlots,
		"NoneType": {"__bool__"}, "slice": nil, "ellipsis": nil, "NotImplementedType": nil,
	} {
		set := map[string]bool{}
		for _, n := range objectSlots {
			set[n] = true
		}
		for _, n := range own {
			set[n] = true
		}
		slotTable[name] = set
	}
}

// slotsOf returns the special methods of a builtin value's type.
func slotsOf(v Value) map[string]bool {
	if _, ok := v.(*iterObj); ok {
		set := map[string]bool{}
		for _, n := range append(objectSlots, iteratorSlots...) {
			set[n] = true
		}
		return set
	}
	return slotTable[v.pyType()]
}

// hasSlot reports whether the builtin value v's type defines the special
// method name.
func hasSlot(v Value, name string) bool {
	return slotsOf(v)[name]
}

// isSlotName reports whether name is a special method some builtin type
// implements through a slot, which its class shows as a slot wrapper.
func isSlotName(name string) bool {
	for _, set := range slotTable {
		if set[name] {
			return true
		}
	}
	return name == "__next__"
}

// unhashableTypes are the builtin types whose __hash__ is None.
var unhashableTypes = map[string]bool{"list": true, "dict": true, "set": true, "bytearray": true}

// slotWrapper is v.name for a special method of a builtin value: a
// method-wrapper that performs the operation.
func slotWrapper(v Value, name string) (Value, bool) {
	if name == "__hash__" && unhashableTypes[v.pyType()] {
		return none, true
	}
	if !hasSlot(v, name) {
		return nil, false
	}
	return &callable{name: name, typ: "method-wrapper", self: v,
		fn: func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if kw != nil && kw.Len() > 0 {
				return nil, typeErr("wrapper %s() takes no keyword arguments", name)
			}
			return slotCall(c, v, name, args)
		}}, true
}

// slotArity gives the number of arguments of the special methods that take
// other than one; min and max differ for those with optional arguments.
var slotArity = map[string][2]int{
	"__len__": {0, 0}, "__iter__": {0, 0}, "__next__": {0, 0}, "__repr__": {0, 0}, "__str__": {0, 0},
	"__hash__": {0, 0}, "__bool__": {0, 0}, "__neg__": {0, 0}, "__pos__": {0, 0}, "__abs__": {0, 0},
	"__invert__": {0, 0}, "__int__": {0, 0}, "__float__": {0, 0}, "__index__": {0, 0}, "__complex__": {0, 0},
	"__trunc__": {0, 0}, "__floor__": {0, 0}, "__ceil__": {0, 0}, "__reversed__": {0, 0}, "__bytes__": {0, 0},
	"__round__": {0, 1}, "__pow__": {1, 2}, "__rpow__": {1, 2}, "__setitem__": {2, 2},
}

// unaryOps maps the special methods of unary operators to their ast names.
var unaryOps = map[string]string{"__neg__": "USub", "__pos__": "UAdd", "__invert__": "Invert"}

// slotBinops maps the special methods of binary operators to the operator and
// whether the method is the reflected one.
var slotBinops = map[string]struct {
	op        string
	reflected bool
}{}

func init() {
	for op, names := range binopNames {
		slotBinops[names[0]] = struct {
			op        string
			reflected bool
		}{op, false}
		slotBinops[names[1]] = struct {
			op        string
			reflected bool
		}{op, true}
	}
}

var slotRichOps = map[string]string{"__eq__": "==", "__ne__": "!=", "__lt__": "<", "__le__": "<=", "__gt__": ">",
	"__ge__": ">="}

// slotCall calls the special method name of the builtin value v.
func slotCall(c *callCtx, v Value, name string, args []Value) (Value, error) {
	ar, ok := slotArity[name]
	if !ok {
		ar = [2]int{1, 1}
	}
	if len(args) < ar[0] || len(args) > ar[1] {
		if ar[0] == ar[1] {
			return nil, typeErr("expected %d argument%s, got %d", ar[0], plural(ar[0]), len(args))
		}
		return nil, typeErr("expected %d argument%s, got %d", ar[1], plural(ar[1]), len(args))
	}
	if op, ok := slotRichOps[name]; ok {
		return slotRichCompare(op, v, args[0])
	}
	if op, ok := unaryOps[name]; ok {
		return unop(op, v)
	}
	if b, ok := slotBinops[name]; ok {
		return builtinSlotBinary(b.op, b.reflected, v, args)
	}
	switch name {
	case "__len__":
		n, err := pyLen(v)
		return mkInt(int64(n)), err
	case "__iter__":
		return makeIterObject(v)
	case "__next__":
		return builtinNext(v, nil)
	case "__repr__":
		s, err := pyRepr(v)
		return strV(s), err
	case "__str__":
		s, err := pyStr(v)
		return strV(s), err
	case "__format__":
		spec, ok := args[0].(strV)
		if !ok {
			return nil, typeErr("argument to __format__ must be str, not %s", typeName(args[0]))
		}
		s, err := formatValue(v, string(spec))
		return strV(s), err
	case "__hash__":
		h, err := pyHash(v)
		return mkInt(h), err
	case "__bool__":
		t, err := truthy(v)
		return pyBool(t), err
	case "__contains__":
		in, err := pyContains(v, args[0])
		return pyBool(in), err
	case "__getitem__":
		return pyGetitem(v, args[0])
	case "__setitem__":
		return none, pySetitem(v, args[0], args[1])
	case "__delitem__":
		return none, pyDelitem(v, args[0])
	case "__reversed__":
		return builtinReversed(c, []Value{v}, nil)
	case "__abs__":
		return builtinAbs(v)
	case "__int__", "__index__", "__trunc__", "__floor__", "__ceil__":
		return slotToInt(v, name)
	case "__float__":
		if f, ok := v.(floatV); ok {
			return f, nil
		}
		f, err := toFloat(v)
		return floatV(f), err
	case "__complex__":
		return v, nil
	case "__bytes__":
		return v, nil
	case "__round__":
		return builtinRound(c, append([]Value{v}, args...), nil)
	case "__iadd__", "__imul__", "__ior__", "__iand__", "__isub__", "__ixor__":
		return slotInplace(v, name, args[0])
	}
	return nil, newPyErr("SystemError", "no special method "+name)
}

// slotToInt is __int__, __index__, __trunc__, __floor__ and __ceil__ of an
// int or a float.
func slotToInt(v Value, name string) (Value, error) {
	if i, ok := asInt(v); ok {
		return mkBig(new(big.Int).Set(i)), nil
	}
	f := float64(v.(floatV))
	switch name {
	case "__floor__":
		f = math.Floor(f)
	case "__ceil__":
		f = math.Ceil(f)
	}
	i, err := floatToBigInt(f)
	if err != nil {
		return nil, err
	}
	return mkBig(i), nil
}

// floatToBigInt is PyLong_FromDouble: the integral part of f.
func floatToBigInt(f float64) (*big.Int, error) {
	if math.IsInf(f, 0) {
		return nil, newPyErr("OverflowError", "cannot convert float infinity to integer")
	}
	if math.IsNaN(f) {
		return nil, valueErr("cannot convert float NaN to integer")
	}
	i, _ := big.NewFloat(math.Trunc(f)).Int(nil)
	return i, nil
}

// numericOperand reports whether other is an operand the number slots of
// self's type accept: an int for int, an int or a float for float, any of
// the three for complex.
func numericOperand(self, other Value) bool {
	other = unsub(other)
	switch self.(type) {
	case intV, boolV:
		_, ok := asInt(other)
		return ok
	case floatV:
		return isNumber(other)
	case complexV:
		_, isC := other.(complexV)
		return isC || isNumber(other)
	}
	return true
}

// builtinSlotBinary is a binary operator's special method of a builtin value.
func builtinSlotBinary(op string, reflected bool, v Value, args []Value) (Value, error) {
	other := args[0]
	switch v.(type) {
	case intV, boolV, floatV, complexV:
		if !numericOperand(v, other) {
			return notImplementedV{}, nil
		}
		if op == "Pow" && len(args) == 2 && !isNoneV(args[1]) {
			if reflected {
				return builtinPow(nil, []Value{other, v, args[1]}, nil)
			}
			return builtinPow(nil, []Value{v, other, args[1]}, nil)
		}
	case *setV, *dictViewV:
		if _, isSet := unsub(other).(*setV); !isSet {
			if _, isView := v.(*dictViewV); !isView {
				return notImplementedV{}, nil
			}
		}
	case *dictV:
		if _, isDict := unsub(other).(*dictV); !isDict {
			return notImplementedV{}, nil
		}
	case strV:
		if op == "Mod" && reflected {
			if _, isStr := unsub(other).(strV); !isStr {
				return notImplementedV{}, nil
			}
		}
	case bytesV, *bytearrayV:
		if op == "Mod" && reflected {
			if _, isBytes := bytesOf(unsub(other)); !isBytes {
				return notImplementedV{}, nil
			}
		}
	}
	a, b := v, other
	if reflected {
		a, b = b, a
		if op == "Mult" {
			// The sequence repeat slots take the count on either side.
			a, b = b, a
		}
	}
	if op != "Mod" {
		// An instance of a subclass of a builtin type takes part through
		// the value it holds, as the type's slot reads it (str.__add__(s, t)
		// for a str subclass instance t). str % args formats the instance
		// itself.
		a, b = unsub(a), unsub(b)
	}
	r, err := builtinBinop(op, a, b)
	if err != nil && isExc(err, "TypeError") {
		if _, isNum := v.(complexV); isNum || isNumber(v) {
			return notImplementedV{}, nil
		}
	}
	return r, err
}

// slotRichCompare is a comparison's special method of a builtin value:
// NotImplemented unless the other operand has a type the value's type
// compares with.
func slotRichCompare(op string, v, other Value) (Value, error) {
	o := unsub(other)
	comparable := false
	switch v.(type) {
	case intV, boolV:
		_, comparable = asInt(o)
	case floatV:
		comparable = isNumber(o)
	case complexV:
		_, isC := o.(complexV)
		comparable = (isC || isNumber(o)) && (op == "==" || op == "!=")
	case strV:
		_, comparable = o.(strV)
	case bytesV, *bytearrayV:
		_, comparable = bytesOf(o)
	case *listV:
		_, comparable = o.(*listV)
	case *tupleV:
		_, comparable = o.(*tupleV)
	case *dictV:
		_, comparable = o.(*dictV)
		comparable = comparable && (op == "==" || op == "!=")
	case *setV:
		_, comparable = o.(*setV)
	case *dictViewV:
		_, isSet := o.(*setV)
		_, isView := o.(*dictViewV)
		comparable = isSet || isView
	case rangeV:
		_, comparable = o.(rangeV)
		comparable = comparable && (op == "==" || op == "!=")
	case *sliceV:
		_, comparable = o.(*sliceV)
	default:
		// object's comparisons: identity for ==, its negation for !=.
		switch op {
		case "==":
			if sameObject(v, other) {
				return trueV, nil
			}
		case "!=":
			if sameObject(v, other) {
				return falseV, nil
			}
		}
		return notImplementedV{}, nil
	}
	if !comparable {
		return notImplementedV{}, nil
	}
	return builtinRich(op, v, o)
}

// slotInplace is an in-place operator's special method of a list, a
// bytearray, a dict or a set: the value updated and returned.
func slotInplace(v Value, name string, other Value) (Value, error) {
	switch x := v.(type) {
	case *listV:
		if name == "__iadd__" {
			items, err := toSlice(other)
			if err != nil {
				return nil, err
			}
			x.items = append(x.items, items...)
			return x, nil
		}
		r, err := pyMul(x, other)
		if err != nil {
			return nil, err
		}
		x.items = r.(*listV).items
		return x, nil
	case *bytearrayV:
		var r Value
		var err error
		if name == "__iadd__" {
			r, err = pyAdd(x, other)
		} else {
			r, err = pyMul(x, other)
		}
		if err != nil {
			return nil, err
		}
		x.b = r.(*bytearrayV).b
		return x, nil
	case *dictV:
		if _, err := methodsDictUpdate(x, []Value{other}, nil); err != nil {
			return nil, err
		}
		return x, nil
	case *setV:
		o, ok := unsub(other).(*setV)
		if !ok {
			return notImplementedV{}, nil
		}
		var r *setV
		var err error
		switch name {
		case "__ior__":
			return x, x.merge(o)
		case "__iand__":
			r, err = setIntersection(x, o, false)
		case "__isub__":
			r, err = setDifference(x, o, false)
		default:
			r, err = setSymmetricDifference(x, o, false)
		}
		if err != nil {
			return nil, err
		}
		x.table, x.fill, x.used, x.finger = r.table, r.fill, r.used, r.finger
		return x, nil
	}
	return notImplementedV{}, nil
}
