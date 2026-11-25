// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The Python builtins, with CPython 3.12's argument checks, results and
// exception messages.

package py

import (
	"math"
	"math/big"
	"strconv"
	"sync"
)

var (
	builtinsTable = map[string]Value{}
	classes       = map[string]*classV{}
	objectClass   = &classV{name: "object"}
)

func cls(name string, base *classV, construct func(c *callCtx, args []Value, kw *dictV) (Value, error)) *classV {
	k := &classV{name: name, base: base, construct: construct}
	classes[name] = k
	return k
}

// noKw is the check of builtins that take no keyword arguments.
func noKw(name string, kw *dictV) error {
	if kw != nil && kw.Len() > 0 {
		return typeErr("%s() takes no keyword arguments", name)
	}
	return nil
}

// posOnly is _PyArg_NoKeywords followed by _PyArg_CheckPositional.
func posOnly(name string, min, max int, args []Value, kw *dictV) error {
	if err := noKw(name, kw); err != nil {
		return err
	}
	return methodsCheckPositional(name, len(args), min, max)
}

// acArgs is _PyArg_UnpackKeywords for an Argument Clinic function whose
// first `required` parameters are required, the first maxpos positional.
func acArgs(name string, names []string, required, maxpos int, args []Value, kw *dictV) ([]Value, error) {
	return methodsArgsKwRequired(name, names, required, maxpos, args, kw)
}

func init() {
	for name := range excParents {
		name := name
		construct := func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			return constructExc(name, args, kw)
		}
		cls(name, nil, construct)
	}
	for name, parent := range excParents {
		if parent != "" {
			classes[name].base = classes[parent]
		} else {
			classes[name].base = objectClass
		}
	}
	intC := cls("int", objectClass, builtinInt)
	cls("bool", intC, builtinBool)
	cls("float", objectClass, builtinFloat)
	cls("complex", objectClass, builtinComplex)
	cls("str", objectClass, builtinStr)
	cls("bytes", objectClass, builtinBytes(false))
	cls("bytearray", objectClass, builtinBytes(true))
	cls("list", objectClass, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("list", 0, 1, args, kw); err != nil {
			return nil, err
		}
		if len(args) == 0 {
			return newList(nil), nil
		}
		items, err := toSlice(args[0])
		return newList(items), err
	})
	cls("tuple", objectClass, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("tuple", 0, 1, args, kw); err != nil {
			return nil, err
		}
		if len(args) == 0 {
			return newTuple(nil), nil
		}
		if t, ok := args[0].(*tupleV); ok {
			return t, nil
		}
		items, err := toSlice(args[0])
		return newTuple(items), err
	})
	cls("dict", objectClass, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if len(args) > 1 {
			return nil, typeErr("dict expected at most 1 argument, got %d", len(args))
		}
		d := newDict()
		if _, err := methodsDictUpdate(d, args, kw); err != nil {
			return nil, err
		}
		return d, nil
	})
	for _, frozen := range []bool{false, true} {
		frozen := frozen
		name := "set"
		if frozen {
			name = "frozenset"
		}
		cls(name, objectClass, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 0, 1, args, kw); err != nil {
				return nil, err
			}
			s := newSet(frozen)
			if len(args) == 1 {
				if err := s.update(args[0]); err != nil {
					return nil, err
				}
			}
			return s, nil
		})
	}
	cls("range", objectClass, builtinRange)
	cls("slice", objectClass, func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("slice", 1, 3, args, kw); err != nil {
			return nil, err
		}
		if len(args) == 1 {
			return &sliceV{start: none, stop: args[0], step: none}, nil
		}
		s := &sliceV{start: args[0], stop: args[1], step: none}
		if len(args) == 3 {
			s.step = args[2]
		}
		return s, nil
	})
	cls("enumerate", objectClass, builtinEnumerate)
	cls("filter", objectClass, builtinFilter)
	cls("map", objectClass, builtinMap)
	cls("reversed", objectClass, builtinReversed)
	cls("zip", objectClass, builtinZip)
	cls("type", objectClass, nil)

	fns := map[string]func(c *callCtx, args []Value, kw *dictV) (Value, error){
		"abs": oneArg("abs", builtinAbs),
		"all": oneArg("all", func(v Value) (Value, error) { return anyAll(v, true) }),
		"any": oneArg("any", func(v Value) (Value, error) { return anyAll(v, false) }),
		"bin": oneArg("bin", func(v Value) (Value, error) { return radix(v, 2, "0b") }),
		"oct": oneArg("oct", func(v Value) (Value, error) { return radix(v, 8, "0o") }),
		"hex": oneArg("hex", func(v Value) (Value, error) { return radix(v, 16, "0x") }),
		"chr": oneArg("chr", builtinChr),
		"ord": oneArg("ord", builtinOrd),
		"len": oneArg("len", func(v Value) (Value, error) {
			n, err := pyLen(v)
			return mkInt(int64(n)), err
		}),
		"repr": oneArg("repr", func(v Value) (Value, error) {
			s, err := pyRepr(v)
			return strV(s), err
		}),
		"hash": oneArg("hash", func(v Value) (Value, error) {
			h, err := pyHash(v)
			return mkInt(h), err
		}),
		"id": oneArg("id", func(v Value) (Value, error) { return mkInt(int64(objID(v))), nil }),
		"divmod": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("divmod", 2, 2, args, kw); err != nil {
				return nil, err
			}
			return binop("DivMod", args[0], args[1])
		},
		"format": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("format", 1, 2, args, kw); err != nil {
				return nil, err
			}
			spec := ""
			if len(args) == 2 {
				s, ok := args[1].(strV)
				if !ok {
					return nil, typeErr("format() argument 2 must be str, not %s", args[1].pyType())
				}
				spec = string(s)
			}
			r, err := formatValue(args[0], spec)
			return strV(r), err
		},
		"isinstance": func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			if err := posOnly("isinstance", 2, 2, args, kw); err != nil {
				return nil, err
			}
			ok, err := isInstance(args[0], args[1])
			return pyBool(ok), err
		},
		"max":    minMax("max", ">"),
		"min":    minMax("min", "<"),
		"pow":    builtinPow,
		"round":  builtinRound,
		"sorted": builtinSorted,
		"sum":    builtinSum,
	}
	for name, fn := range fns {
		builtinsTable[name] = newBuiltin(name, fn)
	}
	initExtraBuiltins()
	for _, name := range allPythonBuiltins {
		if _, ok := builtinsTable[name]; ok {
			continue
		}
		switch name {
		case "True":
			builtinsTable[name] = trueV
		case "False":
			builtinsTable[name] = falseV
		case "None":
			builtinsTable[name] = none
		case "EnvironmentError", "IOError":
			builtinsTable[name] = classes["OSError"]
		default:
			if k, ok := classes[name]; ok {
				builtinsTable[name] = k
			}
		}
	}
}

// pyBuiltinName is a name looked up in Python's builtins module; nil when it is
// not there.
func pyBuiltinName(name string) Value {
	if v, ok := builtinsTable[name]; ok {
		return v
	}
	return nil
}

func excClassOf(name string) *classV {
	if k, ok := classes[name]; ok {
		return k
	}
	return objectClass
}

// classOf is type(v).
func classOf(v Value) *classV {
	switch x := v.(type) {
	case *excV:
		return excClass(x)
	case *instV:
		return x.cls
	case *genericAliasV:
		return genericAliasClass
	case *unionV:
		return unionClass
	case *classV:
		return metaclassOf(x)
	}
	if k, ok := classes[v.pyType()]; ok {
		return k
	}
	return otherClass(v.pyType())
}

var (
	otherMu      sync.Mutex
	otherClasses = map[string]*classV{}
)

// otherClass is the class object of a builtin type the class table does not
// list (function, generator, dict views and the like).
func otherClass(name string) *classV {
	otherMu.Lock()
	defer otherMu.Unlock()
	if k, ok := otherClasses[name]; ok {
		return k
	}
	k := &classV{name: name, base: objectClass}
	otherClasses[name] = k
	return k
}

// typeName is type(v).__name__.
func typeName(v Value) string {
	if k, ok := v.(*classV); ok {
		return metaclassOf(k).name
	}
	return v.pyType()
}

// metaHook calls the metaclass method name (__instancecheck__ or
// __subclasscheck__) of the class k on arg, when a Python-defined
// metaclass of k defines it; ok is false otherwise.
func metaHook(k *classV, name string, arg Value) (bool, bool, error) {
	if k.meta == nil {
		return false, false, nil
	}
	m, ok := k.meta.lookupUser(name)
	if !ok {
		return false, false, nil
	}
	r, err := callValue(nil, bindDescriptor(m, k, k.meta), []Value{arg}, nil)
	if err != nil {
		return false, true, err
	}
	t, err := truthy(r)
	return t, true, err
}

func isInstance(obj, info Value) (bool, error) {
	if k, ok := info.(*classV); ok {
		if r, hooked, err := metaHook(k, "__instancecheck__", obj); hooked {
			return r, err
		}
	}
	switch k := info.(type) {
	case *unionV:
		return isInstance(obj, newTuple(k.args))
	case *classV:
		if k == objectClass {
			return true, nil
		}
		return classOf(obj).subclassOf(k), nil
	case *tupleV:
		for _, it := range k.items {
			ok, err := isInstance(obj, it)
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	}
	if _, err := getattrOpt(info, "__bases__"); err != nil {
		return false, err
	}
	return false, typeErr("isinstance() arg 2 must be a type, a tuple of types, or a union")
}

func oneArg(name string, fn func(Value) (Value, error)) func(c *callCtx, args []Value, kw *dictV) (Value, error) {
	return func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne(name, args, kw); err != nil {
			return nil, err
		}
		return fn(args[0])
	}
}

// ---------------------------------------------------------------- numbers

func builtinAbs(v Value) (Value, error) {
	if r, ok, err := callSpecialOf(v, "__abs__"); ok {
		return r, err
	}
	switch x := builtinValue(v, "__abs__").(type) {
	case intV, boolV:
		i, _ := asInt(x)
		return mkBig(new(big.Int).Abs(i)), nil
	case floatV:
		return floatV(math.Abs(float64(x))), nil
	case complexV:
		r := libmHypot(real(x), imag(x))
		if math.IsInf(r, 0) && !math.IsInf(real(x), 0) && !math.IsInf(imag(x), 0) {
			return nil, newPyErr("OverflowError", "absolute value too large")
		}
		return floatV(r), nil
	}
	return nil, typeErr("bad operand type for abs(): '%s'", v.pyType())
}

func anyAll(v Value, all bool) (Value, error) {
	it, err := pyIter(v)
	if err != nil {
		return nil, err
	}
	for {
		item, ok, err := it.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return pyBool(all), nil
		}
		t, err := truthy(item)
		if err != nil {
			return nil, err
		}
		if t != all {
			return pyBool(!all), nil
		}
	}
}

func radix(v Value, base int, prefix string) (Value, error) {
	i, err := methodsIndex(v)
	if err != nil {
		return nil, err
	}
	s := new(big.Int).Abs(i).Text(base)
	if i.Sign() < 0 {
		return strV("-" + prefix + s), nil
	}
	return strV(prefix + s), nil
}

func builtinChr(v Value) (Value, error) {
	i, err := methodsIndex(v)
	if err != nil {
		return nil, err
	}
	// A surrogate code point gives a str holding that lone surrogate.
	s, err := chrOf(i)
	if err != nil {
		return nil, fromLeaf(err)
	}
	return strV(s), nil
}

func builtinOrd(v Value) (Value, error) {
	switch x := unsub(v).(type) {
	case strV:
		rs := strRunes(string(x))
		if len(rs) != 1 {
			return nil, typeErr("ord() expected a character, but string of length %d found", len(rs))
		}
		return mkInt(int64(rs[0])), nil
	case bytesV, *bytearrayV:
		b, _ := bytesOf(x)
		if len(b) != 1 {
			return nil, typeErr("ord() expected a character, but string of length %d found", len(b))
		}
		return mkInt(int64(b[0])), nil
	}
	return nil, typeErr("ord() expected string of length 1, but %s found", v.pyType())
}

func builtinInt(c *callCtx, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKw("int", []string{"", "base"}, 2, args, kw)
	if err != nil {
		if kw != nil && kw.Len() > 0 {
			if _, ok := kw.getStr("x"); ok {
				return nil, typeErr("'x' is an invalid keyword argument for int()")
			}
		}
		return nil, err
	}
	x, base := buf[0], buf[1]
	if x == nil {
		if base != nil {
			return nil, typeErr("int() missing string argument")
		}
		return mkInt(0), nil
	}
	if base == nil {
		switch v := builtinValue(x, "__int__").(type) {
		case intV:
			return v, nil
		case boolV:
			i, _ := asInt(v)
			return mkBig(i), nil
		case floatV:
			i, err := floatToBigInt(float64(v))
			if err != nil {
				return nil, fromLeaf(err)
			}
			return mkBig(i), nil
		case strV:
			return pyParseIntOf(string(v), 10, v)
		case bytesV, *bytearrayV:
			b, _ := bytesOf(v)
			return pyParseIntOf(b, 10, v)
		}
		if s, ok := unsub(x).(strV); ok {
			return pyParseIntOf(string(s), 10, x)
		}
		for _, name := range []string{"__int__", "__index__"} {
			if r, ok, err := callSpecial(x, name); ok {
				if err != nil {
					return nil, err
				}
				if _, isInt := asInt(r); !isInt {
					return nil, typeErr("%s returned non-int (type %s)", name, typeName(r))
				}
				return r, nil
			}
		}
		if r, ok, err := callSpecial(x, "__trunc__"); ok {
			if err != nil {
				return nil, err
			}
			if _, isInt := asInt(r); isInt {
				return r, nil
			}
			// PyNumber_Long: a result without __index__ is not Integral;
			// what __index__ itself raises is passed on.
			if !hasIndex(r) {
				return nil, typeErr("__trunc__ returned non-Integral (type %s)", typeName(r))
			}
			n, err := methodsIndex(r)
			if err != nil {
				return nil, err
			}
			return mkBig(n), nil
		}
		return nil, typeErr("int() argument must be a string, a bytes-like object or a real number, not '%s'", typeName(x))
	}
	bi, err := methodsIndex(base)
	if err != nil {
		return nil, err
	}
	b := bi.Int64()
	if !bi.IsInt64() || b != 0 && (b < 2 || b > 36) {
		return nil, valueErr("int() base must be >= 2 and <= 36, or 0")
	}
	switch v := x.(type) {
	case bytesV, *bytearrayV:
		s, _ := bytesOf(v)
		return pyParseIntOf(s, int(b), v)
	}
	if s, ok := unsub(x).(strV); ok {
		return pyParseIntOf(string(s), int(b), x)
	}
	return nil, typeErr("int() can't convert non-string with explicit base")
}

func builtinBool(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("bool", 0, 1, args, kw); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return falseV, nil
	}
	t, err := truthy(args[0])
	return pyBool(t), err
}

func builtinFloat(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("float", 0, 1, args, kw); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return floatV(0), nil
	}
	switch x := builtinValue(args[0], "__float__").(type) {
	case floatV:
		return x, nil
	case intV, boolV:
		f, err := toFloat(x)
		return floatV(f), err
	case strV:
		f, err := pyParseFloatOf(string(x), x)
		return floatV(f), err
	case bytesV, *bytearrayV:
		s, _ := bytesOf(x)
		f, err := pyParseFloatOf(s, x)
		return floatV(f), err
	}
	if s, ok := unsub(args[0]).(strV); ok {
		f, err := pyParseFloatOf(string(s), args[0])
		return floatV(f), err
	}
	if r, ok, err := callSpecial(args[0], "__float__"); ok {
		if err != nil {
			return nil, err
		}
		if _, isFloat := r.(floatV); !isFloat {
			return nil, typeErr("%s.__float__ returned non-float (type %s)", typeName(args[0]), typeName(r))
		}
		return r, nil
	}
	return nil, typeErr("float() argument must be a string or a real number, not '%s'", typeName(args[0]))
}

func builtinStr(c *callCtx, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKw("str", []string{"object", "encoding", "errors"}, 3, args, kw)
	if err != nil {
		return nil, err
	}
	for i, name := range []string{"", "encoding", "errors"} {
		if i > 0 && buf[i] != nil {
			if _, ok := unsub(buf[i]).(strV); !ok {
				return nil, typeErr("str() argument '%s' must be str, not %s", name, methodsBadArgType(buf[i]))
			}
		}
	}
	if buf[0] == nil {
		if buf[1] != nil || buf[2] != nil {
			return strV(""), nil
		}
		return strV(""), nil
	}
	if buf[1] == nil && buf[2] == nil {
		s, err := pyStr(buf[0])
		return strV(s), err
	}
	b, ok := bytesOf(buf[0])
	if !ok {
		if _, isStr := buf[0].(strV); isStr {
			return nil, typeErr("decoding str is not supported")
		}
		return nil, typeErr("decoding to str: need a bytes-like object, %s found", buf[0].pyType())
	}
	return decodeBytes(b, buf[1], buf[2])
}

func builtinRange(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("range", 1, 3, args, kw); err != nil {
		return nil, err
	}
	vals := make([]int64, len(args))
	for i, a := range args {
		n, err := methodsIndex(a)
		if err != nil {
			return nil, err
		}
		if !n.IsInt64() || n.Int64() > 1<<62 || n.Int64() < -(1<<62) {
			return nil, newPyErr("OverflowError", "Python int too large to convert to C ssize_t")
		}
		vals[i] = n.Int64()
	}
	switch len(vals) {
	case 1:
		return rangeV{start: 0, stop: vals[0], step: 1}, nil
	case 2:
		return rangeV{start: vals[0], stop: vals[1], step: 1}, nil
	}
	if vals[2] == 0 {
		return nil, valueErr("range() arg 3 must not be zero")
	}
	return rangeV{start: vals[0], stop: vals[1], step: vals[2]}, nil
}

// builtinPow is pow(base, exp, mod=None).
func builtinPow(c *callCtx, args []Value, kw *dictV) (Value, error) {
	buf, err := acArgs("pow", []string{"base", "exp", "mod"}, 2, 3, args, kw)
	if err != nil {
		return nil, err
	}
	if buf[2] == nil || isNoneV(buf[2]) {
		return binop("Pow", buf[0], buf[1])
	}
	if k := userClassOf(buf[0]); k != nil {
		// Three-argument pow calls only the first operand's __pow__.
		if _, ok := k.lookupUser("__pow__"); ok {
			r, _, err := callSpecial(buf[0], "__pow__", buf[1], buf[2])
			if err != nil || !isNotImplemented(r) {
				return r, err
			}
		}
		return nil, typeErr("unsupported operand type(s) for ** or pow(): '%s', '%s', '%s'",
			typeName(buf[0]), typeName(buf[1]), typeName(buf[2]))
	}
	if isComplexOperand(buf[0], buf[1]) || isComplexOperand(buf[0], buf[2]) && isNumber(buf[1]) {
		return nil, valueErr("complex modulo")
	}
	b, ok1 := asInt(buf[0])
	e, ok2 := asInt(buf[1])
	m, ok3 := asInt(buf[2])
	if !ok1 || !ok2 || !ok3 {
		if isNumber(buf[0]) && isNumber(buf[1]) && isNumber(buf[2]) {
			return nil, typeErr("pow() 3rd argument not allowed unless all arguments are integers")
		}
		return nil, typeErr("unsupported operand type(s) for ** or pow(): '%s', '%s', '%s'", buf[0].pyType(), buf[1].pyType(), buf[2].pyType())
	}
	if m.Sign() == 0 {
		return nil, valueErr("pow() 3rd argument cannot be 0")
	}
	am := new(big.Int).Abs(m)
	base := new(big.Int).Mod(b, am)
	var r *big.Int
	if e.Sign() < 0 {
		inv := new(big.Int).ModInverse(base, am)
		if inv == nil {
			return nil, valueErr("base is not invertible for the given modulus")
		}
		r = new(big.Int).Exp(inv, new(big.Int).Neg(e), am)
	} else {
		r = new(big.Int).Exp(base, e, am)
	}
	if m.Sign() < 0 && r.Sign() != 0 {
		r.Add(r, m)
	}
	return mkBig(r), nil
}

// builtinRound is round(number, ndigits=None).
func builtinRound(c *callCtx, args []Value, kw *dictV) (Value, error) {
	buf, err := acArgs("round", []string{"number", "ndigits"}, 1, 2, args, kw)
	if err != nil {
		return nil, err
	}
	num, nd := buf[0], buf[1]
	if k := userClassOf(num); k != nil {
		if _, ok := k.lookupUser("__round__"); ok {
			var r Value
			if nd == nil || isNoneV(nd) {
				r, _, err = callSpecial(num, "__round__")
			} else {
				r, _, err = callSpecial(num, "__round__", nd)
			}
			return r, err
		}
	}
	if nd != nil && isNoneV(nd) {
		nd = nil
	}
	switch x := builtinValue(num, "__round__").(type) {
	case intV, boolV:
		i, _ := asInt(x)
		if nd == nil {
			return mkBig(new(big.Int).Set(i)), nil
		}
		n, err := methodsIndex(nd)
		if err != nil {
			return nil, err
		}
		if n.Sign() >= 0 {
			return mkBig(new(big.Int).Set(i)), nil
		}
		return mkBig(roundIntHalfEven(i, new(big.Int).Neg(n))), nil
	case floatV:
		f := float64(x)
		if nd == nil {
			if math.IsInf(f, 0) {
				return nil, newPyErr("OverflowError", "cannot convert float infinity to integer")
			}
			if math.IsNaN(f) {
				return nil, valueErr("cannot convert float NaN to integer")
			}
			r, _ := big.NewFloat(math.RoundToEven(f)).Int(nil)
			return mkBig(r), nil
		}
		n, err := methodsIndex(nd)
		if err != nil {
			return nil, err
		}
		r, err := roundFloat(f, n)
		if err != nil {
			return nil, err
		}
		return floatV(r), nil
	}
	return nil, typeErr("type %s doesn't define __round__ method", num.pyType())
}

func roundIntHalfEven(i, digits *big.Int) *big.Int {
	if digits.BitLen() > 20 {
		return big.NewInt(0)
	}
	pow := new(big.Int).Exp(big.NewInt(10), digits, nil)
	q, r := intFloorDivMod(i, pow)
	twice := new(big.Int).Lsh(r, 1)
	switch c := twice.Cmp(pow); {
	case c > 0 || c == 0 && q.Bit(0) == 1:
		q.Add(q, big.NewInt(1))
	}
	return q.Mul(q, pow)
}

// roundFloat is CPython's double_round: x rounded to ndigits decimal places,
// half to even on the exact binary value, with its OverflowError for a
// result too large for a float.
func roundFloat(x float64, nd *big.Int) (float64, error) {
	if math.IsInf(x, 0) || math.IsNaN(x) || x == 0 {
		return x, nil
	}
	if nd.Cmp(big.NewInt(323)) > 0 {
		return x, nil
	}
	if nd.Cmp(big.NewInt(-308)) < 0 {
		return 0.0 * x, nil
	}
	var res float64
	if n := int(nd.Int64()); n >= 0 {
		var err error
		res, err = strconv.ParseFloat(strconv.FormatFloat(x, 'f', n, 64), 64)
		if err != nil {
			return 0, newPyErr("OverflowError", "rounded value too large to represent")
		}
	} else {
		exact, _ := new(big.Float).SetPrec(2000).SetFloat64(x).Int(nil)
		rem := new(big.Float).SetPrec(2000).Sub(new(big.Float).SetPrec(2000).SetFloat64(x), new(big.Float).SetPrec(2000).SetInt(exact))
		pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-n)), nil)
		q, r := intFloorDivMod(exact, pow)
		// compare 2*(r + rem) with pow
		twice := new(big.Float).SetPrec(2000).SetInt(r)
		twice.Add(twice, rem)
		twice.Mul(twice, big.NewFloat(2))
		c := twice.Cmp(new(big.Float).SetPrec(2000).SetInt(pow))
		if c > 0 || c == 0 && q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
		res, _ = new(big.Float).SetInt(q.Mul(q, pow)).Float64()
		if math.IsInf(res, 0) {
			return 0, newPyErr("OverflowError", "rounded value too large to represent")
		}
	}
	if res == 0 {
		return math.Copysign(0, x), nil
	}
	return res, nil
}

func minMax(name, op string) func(c *callCtx, args []Value, kw *dictV) (Value, error) {
	return func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if len(args) == 0 {
			return nil, typeErr("%s expected at least 1 argument, got 0", name)
		}
		var key, def Value
		if kw != nil {
			for i, k := range kw.keys {
				ks, _ := k.(strV)
				switch ks {
				case "key":
					key = kw.vals[i]
				case "default":
					def = kw.vals[i]
				default:
					return nil, typeErr("'%s' is an invalid keyword argument for %s()", ks, name)
				}
			}
		}
		if len(args) == 0 {
			return nil, typeErr("%s expected at least 1 argument, got 0", name)
		}
		var items []Value
		if len(args) == 1 {
			var err error
			if items, err = toSlice(args[0]); err != nil {
				return nil, err
			}
		} else {
			if def != nil {
				return nil, typeErr("Cannot specify a default for %s() with multiple positional arguments", name)
			}
			items = args
		}
		if key != nil && isNoneV(key) {
			key = nil
		}
		var best, bestKey Value
		for _, it := range items {
			k := it
			if key != nil {
				var err error
				if k, err = callValue(c, key, []Value{it}, nil); err != nil {
					return nil, err
				}
			}
			if best == nil {
				best, bestKey = it, k
				continue
			}
			better, err := pyCompare(op, k, bestKey)
			if err != nil {
				return nil, err
			}
			if better {
				best, bestKey = it, k
			}
		}
		if best == nil {
			if def != nil {
				return def, nil
			}
			return nil, valueErr("%s() iterable argument is empty", name)
		}
		return best, nil
	}
}

func builtinSorted(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if len(args) != 1 {
		return nil, typeErr("sorted expected 1 argument, got %d", len(args))
	}
	items, err := toSlice(args[0])
	if err != nil {
		return nil, err
	}
	l := newList(items)
	if _, err := methodsListSort(l, nil, kw); err != nil {
		return nil, err
	}
	return l, nil
}

// builtinSum is sum(iterable, /, start=0), with CPython 3.12's fast paths:
// exact int accumulation, then compensated (Neumaier) float summation.
func builtinSum(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if len(args) == 0 {
		return nil, typeErr("sum() takes at least 1 positional argument (0 given)")
	}
	buf, err := acArgs("sum", []string{"", "start"}, 1, 2, args, kw)
	if err != nil {
		return nil, err
	}
	it, err := pyIter(buf[0])
	if err != nil {
		return nil, err
	}
	var result Value = mkInt(0)
	if buf[1] != nil {
		result = buf[1]
		switch unsub(result).(type) {
		case strV:
			return nil, typeErr("sum() can't sum strings [use ''.join(seq) instead]")
		case bytesV:
			return nil, typeErr("sum() can't sum bytes [use b''.join(seq) instead]")
		case *bytearrayV:
			return nil, typeErr("sum() can't sum bytearray [use b''.join(seq) instead]")
		}
	}
	next := func() (Value, bool, error) { return it.next() }
	if _, ok := result.(intV); ok {
		for {
			item, ok, err := next()
			if err != nil {
				return nil, err
			}
			if !ok {
				return result, nil
			}
			if result, err = binop("Add", result, item); err != nil {
				return nil, err
			}
			if _, isInt := asInt(item); !isInt {
				break
			}
		}
	}
	if f, ok := result.(floatV); ok {
		fr, comp := float64(f), 0.0
		for {
			item, ok, err := next()
			if err != nil {
				return nil, err
			}
			if !ok {
				if comp != 0 && !math.IsInf(comp, 0) && !math.IsNaN(comp) {
					fr += comp
				}
				return floatV(fr), nil
			}
			if x, isF := item.(floatV); isF {
				t := fr + float64(x)
				if math.Abs(fr) >= math.Abs(float64(x)) {
					comp += (fr - t) + float64(x)
				} else {
					comp += (float64(x) - t) + fr
				}
				fr = t
				continue
			}
			if i, isInt := item.(intV); isInt && i.v.IsInt64() {
				fr += float64(i.v.Int64())
				continue
			}
			if comp != 0 && !math.IsInf(comp, 0) && !math.IsNaN(comp) {
				fr += comp
			}
			if result, err = binop("Add", floatV(fr), item); err != nil {
				return nil, err
			}
			break
		}
	}
	for {
		item, ok, err := next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return result, nil
		}
		if result, err = binop("Add", result, item); err != nil {
			return nil, err
		}
	}
}

// ---------------------------------------------------------------- iterators

func builtinEnumerate(c *callCtx, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKw("enumerate", []string{"iterable", "start"}, 2, args, kw)
	if err != nil {
		return nil, err
	}
	if buf[0] == nil {
		return nil, typeErr("enumerate() missing required argument 'iterable'")
	}
	n := big.NewInt(0)
	if buf[1] != nil {
		s, ok := asInt(buf[1])
		if !ok {
			return nil, typeErr("'%s' object cannot be interpreted as an integer", typeName(buf[1]))
		}
		n = new(big.Int).Set(s)
	}
	it, err := pyIter(buf[0])
	if err != nil {
		return nil, err
	}
	return &iterObj{typ: "enumerate", it: funcIter(func() (Value, bool, error) {
		v, ok, err := it.next()
		if err != nil || !ok {
			return nil, ok, err
		}
		t := newTuple([]Value{mkBig(new(big.Int).Set(n)), v})
		n.Add(n, bigOne)
		return t, true, nil
	})}, nil
}

func builtinFilter(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("filter", 2, 2, args, kw); err != nil {
		return nil, err
	}
	fn := args[0]
	it, err := pyIter(args[1])
	if err != nil {
		return nil, err
	}
	return &iterObj{typ: "filter", it: funcIter(func() (Value, bool, error) {
		for {
			v, ok, err := it.next()
			if err != nil || !ok {
				return nil, ok, err
			}
			test := v
			if !isNoneV(fn) {
				if test, err = callValue(c, fn, []Value{v}, nil); err != nil {
					return nil, false, err
				}
			}
			t, err := truthy(test)
			if err != nil {
				return nil, false, err
			}
			if t {
				return v, true, nil
			}
		}
	})}, nil
}

func builtinMap(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := noKw("map", kw); err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, typeErr("map() must have at least two arguments.")
	}
	fn := args[0]
	its := make([]iterator, len(args)-1)
	for i, a := range args[1:] {
		it, err := pyIter(a)
		if err != nil {
			return nil, err
		}
		its[i] = it
	}
	return &iterObj{typ: "map", it: funcIter(func() (Value, bool, error) {
		vals := make([]Value, len(its))
		for i, it := range its {
			v, ok, err := it.next()
			if err != nil || !ok {
				return nil, ok, err
			}
			vals[i] = v
		}
		r, err := callValue(c, fn, vals, nil)
		return r, err == nil, err
	})}, nil
}

func builtinZip(c *callCtx, args []Value, kw *dictV) (Value, error) {
	strict := false
	if kw != nil {
		for i, k := range kw.keys {
			ks, _ := k.(strV)
			if ks != "strict" {
				return nil, typeErr("'%s' is an invalid keyword argument for zip()", ks)
			}
			t, err := truthy(kw.vals[i])
			if err != nil {
				return nil, err
			}
			strict = t
		}
	}
	its := make([]iterator, len(args))
	for i, a := range args {
		it, err := pyIter(a)
		if err != nil {
			return nil, err
		}
		its[i] = it
	}
	done := false
	return &iterObj{typ: "zip", it: funcIter(func() (Value, bool, error) {
		if done || len(its) == 0 {
			return nil, false, nil
		}
		vals := make([]Value, len(its))
		for i, it := range its {
			v, ok, err := it.next()
			if err != nil {
				return nil, false, err
			}
			if !ok {
				done = true
				if strict {
					if i > 0 {
						return nil, false, valueErr("zip() argument %d is shorter than argument%s %s", i+1, plural(i), argList(i))
					}
					for j := 1; j < len(its); j++ {
						if _, more, err := its[j].next(); err != nil {
							return nil, false, err
						} else if more {
							return nil, false, valueErr("zip() argument %d is longer than argument%s %s", j+1, plural(j), argList(j))
						}
					}
				}
				return nil, false, nil
			}
			vals[i] = v
		}
		return newTuple(vals), true, nil
	})}, nil
}

func argList(n int) string {
	if n == 1 {
		return "1"
	}
	return "1-" + strconv.Itoa(n)
}

func builtinReversed(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("reversed", 1, 1, args, kw); err != nil {
		return nil, err
	}
	var items []Value
	var err error
	typ := "reversed"
	switch x := args[0].(type) {
	case *listV:
		typ = "list_reverseiterator"
		l := x
		i := len(l.items) - 1
		return &iterObj{typ: typ, it: funcIter(func() (Value, bool, error) {
			if i < 0 || i >= len(l.items) {
				return nil, false, nil
			}
			i--
			return l.items[i+1], true, nil
		})}, nil
	case rangeV:
		typ = "range_iterator"
		items, err = toSlice(x)
	case *dictV:
		typ = "dict_reversekeyiterator"
		items = append([]Value(nil), x.keys...)
	case *dictViewV:
		typ = map[string]string{"keys": "dict_reversekeyiterator", "values": "dict_reversevalueiterator",
			"items": "dict_reverseitemiterator"}[x.kind]
		items = x.items()
	case *tupleV, strV, bytesV, *bytearrayV:
		items, err = toSlice(x)
	default:
		if k := userClassOf(x); k != nil {
			return userReversed(x, k)
		}
		if isStrSubclass(x) {
			items, err = toSlice(unsub(x))
			break
		}
		return nil, typeErr("'%s' object is not reversible", typeName(args[0]))
	}
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return &iterObj{typ: typ, it: &sliceIter{items: items}}, nil
}

// ---------------------------------------------------------------- bytes

func builtinBytes(mutable bool) func(c *callCtx, args []Value, kw *dictV) (Value, error) {
	name := "bytes"
	if mutable {
		name = "bytearray"
	}
	mk := func(b []byte) Value {
		if mutable {
			return &bytearrayV{b: b}
		}
		return bytesV(b)
	}
	return func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		buf, err := methodsArgsKw(name, []string{"source", "encoding", "errors"}, 3, args, kw)
		if err != nil {
			return nil, err
		}
		src := buf[0]
		if src == nil {
			if buf[1] != nil {
				return nil, typeErr("encoding without a string argument")
			}
			return mk(nil), nil
		}
		if s, ok := src.(strV); ok {
			if buf[1] == nil {
				return nil, typeErr("string argument without an encoding")
			}
			enc, ok := buf[1].(strV)
			if !ok {
				return nil, typeErr("%s() argument 'encoding' must be str, not %s", name, buf[1].pyType())
			}
			errs, err := optStrArg(buf[2], "strict", name, "errors")
			if err != nil {
				return nil, err
			}
			b, err := encodeStr(string(s), string(enc), errs)
			if err != nil {
				return nil, err
			}
			return mk(b), nil
		}
		if buf[1] != nil {
			return nil, typeErr("encoding without a string argument")
		}
		if k := userClassOf(src); k != nil && !mutable {
			if _, ok := k.lookupUser("__bytes__"); ok {
				r, _, err := callSpecial(src, "__bytes__")
				if err != nil {
					return nil, err
				}
				if _, isBytes := unsub(r).(bytesV); !isBytes {
					return nil, typeErr("__bytes__ returned non-bytes (type %s)", typeName(r))
				}
				return r, nil
			}
		}
		if n, ok := asInt(src); ok {
			if n.Sign() < 0 {
				return nil, valueErr("negative count")
			}
			if !n.IsInt64() || n.Int64() > 1<<28 {
				return nil, newPyErr("MemoryError", "")
			}
			return mk(make([]byte, n.Int64())), nil
		}
		if b, ok := bytesOf(src); ok {
			return mk([]byte(b)), nil
		}
		items, err := toSliceAs(src, typeErr("cannot convert '%s' object to %s", src.pyType(), name))
		if err != nil {
			return nil, err
		}
		out := make([]byte, len(items))
		for i, it := range items {
			n, err := methodsIndex(it)
			if err != nil {
				return nil, err
			}
			if !n.IsInt64() || n.Int64() < 0 || n.Int64() > 255 {
				return nil, valueErr("bytes must be in range(0, 256)")
			}
			out[i] = byte(n.Int64())
		}
		return mk(out), nil
	}
}
