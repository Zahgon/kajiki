// Ported from CPython 3.12 (Objects/longobject.c, Objects/floatobject.c, Objects/complexobject.c)
// to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The methods of int, float and complex that are not special methods, and the class methods of the
// builtin classes (int.from_bytes, float.fromhex, bytes.fromhex, str.maketrans, dict.fromkeys).

package py

import (
	"math"
	"math/big"
	"math/bits"
	"strconv"
	"strings"
)

// intMethod is an attribute of an int or a bool that is a method.
func intMethod(v Value, n *big.Int, name string) (Value, bool) {
	typ := v.pyType()
	noArgs := func(f func() Value) (Value, bool) {
		return methodsBound(v, name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone(typ+"."+name, args, kw); err != nil {
				return nil, err
			}
			return f(), nil
		}), true
	}
	switch name {
	case "bit_count":
		return noArgs(func() Value {
			c := 0
			for _, w := range new(big.Int).Abs(n).Bits() {
				c += bits.OnesCount(uint(w))
			}
			return mkInt(int64(c))
		})
	case "as_integer_ratio":
		return noArgs(func() Value { return newTuple([]Value{mkBig(new(big.Int).Set(n)), mkInt(1)}) })
	case "conjugate", "__trunc__", "__floor__", "__ceil__":
		return noArgs(func() Value { return mkBig(new(big.Int).Set(n)) })
	case "to_bytes":
		return methodsBound(v, name, func(args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKwRequired("to_bytes", []string{"length", "byteorder", "signed"}, 0, 2, args, kw)
			if err != nil {
				return nil, err
			}
			return intToBytes(n, buf[0], buf[1], buf[2])
		}), true
	}
	return nil, false
}

// byteorderArg is the byteorder argument of int.to_bytes and
// int.from_bytes: true for little-endian.
func byteorderArg(v Value, fn string) (bool, error) {
	if v == nil {
		return false, nil
	}
	s, ok := unsub(v).(strV)
	if !ok {
		return false, typeErr("%s() argument 'byteorder' must be str, not %s", fn, typeName(v))
	}
	switch s {
	case "little":
		return true, nil
	case "big":
		return false, nil
	}
	return false, valueErr("byteorder must be either 'little' or 'big'")
}

// intToBytes is int.to_bytes(length=1, byteorder='big', *, signed=False).
func intToBytes(n *big.Int, lengthV, orderV, signedV Value) (Value, error) {
	length := 1
	if lengthV != nil {
		l, err := methodsSsize(lengthV)
		if err != nil {
			return nil, err
		}
		if l < 0 {
			return nil, valueErr("length argument must be non-negative")
		}
		length = l
	}
	little, err := byteorderArg(orderV, "to_bytes")
	if err != nil {
		return nil, err
	}
	signed := false
	if signedV != nil {
		if signed, err = truthy(signedV); err != nil {
			return nil, err
		}
	}
	if n.Sign() < 0 && !signed {
		return nil, newPyErr("OverflowError", "can't convert negative int to unsigned")
	}
	// The value as length bytes of two's complement.
	mod := new(big.Int).Lsh(bigOne, uint(8*length))
	v := new(big.Int).Set(n)
	tooBig := false
	if signed {
		half := new(big.Int).Rsh(mod, 1)
		tooBig = v.Cmp(half) >= 0 || v.Cmp(new(big.Int).Neg(half)) < 0
	} else {
		tooBig = v.Cmp(mod) >= 0
	}
	if tooBig {
		return nil, newPyErr("OverflowError", "int too big to convert")
	}
	if v.Sign() < 0 {
		v.Add(v, mod)
	}
	out := make([]byte, length)
	v.FillBytes(out)
	if little {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return bytesV(out), nil
}

// intFromBytes is the class method int.from_bytes(bytes, byteorder='big',
// *, signed=False).
func intFromBytes(c *callCtx, cls *classV, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKwRequired("from_bytes", []string{"bytes", "byteorder", "signed"}, 1, 2, args, kw)
	if err != nil {
		return nil, err
	}
	little, err := byteorderArg(buf[1], "from_bytes")
	if err != nil {
		return nil, err
	}
	signed := false
	if buf[2] != nil {
		if signed, err = truthy(buf[2]); err != nil {
			return nil, err
		}
	}
	b, ok := bytesOf(unsub(buf[0]))
	if !ok {
		conv, err := builtinBytes(false)(c, []Value{buf[0]}, nil)
		if err != nil {
			return nil, err
		}
		b = string(conv.(bytesV))
	}
	raw := []byte(b)
	if little {
		raw = append([]byte(nil), raw...)
		for i, j := 0, len(raw)-1; i < j; i, j = i+1, j-1 {
			raw[i], raw[j] = raw[j], raw[i]
		}
	}
	n := new(big.Int).SetBytes(raw)
	if signed && len(raw) > 0 && raw[0]&0x80 != 0 {
		n.Sub(n, new(big.Int).Lsh(bigOne, uint(8*len(raw))))
	}
	return fromClassMethodResult(c, cls, "int", mkBig(n))
}

// fromClassMethodResult is the result of an alternate constructor called on
// cls: the value itself for the builtin class, cls(value) for a subclass.
func fromClassMethodResult(c *callCtx, cls *classV, builtin string, v Value) (Value, error) {
	if cls == nil || cls.name == builtin && !cls.isUser() {
		return v, nil
	}
	return callValue(c, cls, []Value{v}, nil)
}

// floatMethod is an attribute of a float that is a method.
func floatMethod(f float64, name string) (Value, bool) {
	noArgs := func(fn func() (Value, error)) (Value, bool) {
		return methodsBound(floatV(f), name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("float."+name, args, kw); err != nil {
				return nil, err
			}
			return fn()
		}), true
	}
	switch name {
	case "conjugate":
		return noArgs(func() (Value, error) { return floatV(f), nil })
	case "hex":
		return noArgs(func() (Value, error) { return strV(floatHex(f)), nil })
	case "as_integer_ratio":
		return noArgs(func() (Value, error) { return floatAsIntegerRatio(f) })
	}
	return nil, false
}

// floatAsIntegerRatio is float.as_integer_ratio(): the exact value as a
// fraction in lowest terms.
func floatAsIntegerRatio(f float64) (Value, error) {
	if math.IsInf(f, 0) {
		return nil, newPyErr("OverflowError", "cannot convert Infinity to integer ratio")
	}
	if math.IsNaN(f) {
		return nil, valueErr("cannot convert NaN to integer ratio")
	}
	r := new(big.Rat).SetFloat64(f)
	return newTuple([]Value{mkBig(new(big.Int).Set(r.Num())), mkBig(new(big.Int).Set(r.Denom()))}), nil
}

// floatHex is float.hex(): the mantissa in hexadecimal with 13 digits after
// the point and a decimal power-of-two exponent.
func floatHex(x float64) string {
	switch {
	case math.IsNaN(x) || math.IsInf(x, 0):
		return floatRepr(x)
	case x == 0:
		if math.Signbit(x) {
			return "-0x0.0p+0"
		}
		return "0x0.0p+0"
	}
	const hexDigits = "0123456789abcdef"
	m, e := math.Frexp(math.Abs(x))
	shift := 1 - max(-1021-e, 0)
	m = math.Ldexp(m, shift)
	e -= shift
	var b strings.Builder
	if x < 0 {
		b.WriteByte('-')
	}
	b.WriteString("0x")
	b.WriteByte(hexDigits[int(m)])
	m -= float64(int(m))
	b.WriteByte('.')
	for i := 0; i < 13; i++ {
		m *= 16
		b.WriteByte(hexDigits[int(m)])
		m -= float64(int(m))
	}
	b.WriteByte('p')
	if e < 0 {
		b.WriteByte('-')
		e = -e
	} else {
		b.WriteByte('+')
	}
	b.WriteString(strconv.Itoa(e))
	return b.String()
}

// floatFromhex is the class method float.fromhex(string).
func floatFromhex(c *callCtx, cls *classV, args []Value, kw *dictV) (Value, error) {
	if err := methodsArgsOne("fromhex", args, kw); err != nil {
		return nil, err
	}
	s, ok := unsub(args[0]).(strV)
	if !ok {
		return nil, typeErr("must be str, not %s", typeName(args[0]))
	}
	f, err := parseHexFloat(string(s))
	if err != nil {
		return nil, err
	}
	return fromClassMethodResult(c, cls, "float", floatV(f))
}

// parseHexFloat is float_fromhex: optional whitespace and sign, inf,
// infinity or nan, or an optional 0x prefix, hexadecimal digits with an
// optional point and an optional binary exponent p±ddd; correctly rounded.
func parseHexFloat(s string) (float64, error) {
	bad := valueErr("invalid hexadecimal floating-point string")
	i := 0
	for i < len(s) && isCSpace(rune(s[i])) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	end := len(s)
	for end > i && isCSpace(rune(s[end-1])) {
		end--
	}
	body := s[i:end]
	switch strings.ToLower(body) {
	case "inf", "infinity":
		return math.Copysign(math.Inf(1), signOf(neg)), nil
	case "nan":
		return math.Copysign(math.NaN(), signOf(neg)), nil
	}
	if len(body) >= 2 && body[0] == '0' && (body[1] == 'x' || body[1] == 'X') {
		body = body[2:]
	}
	mant := new(big.Int)
	fracDigits, digits := 0, 0
	j := 0
	seenPoint := false
	for ; j < len(body); j++ {
		ch := body[j]
		if ch == '.' && !seenPoint {
			seenPoint = true
			continue
		}
		d := strings.IndexByte("0123456789abcdef", ch|0x20)
		if !isHexDigit(rune(ch)) || d < 0 {
			break
		}
		mant.Lsh(mant, 4)
		mant.Or(mant, big.NewInt(int64(d)))
		digits++
		if seenPoint {
			fracDigits++
		}
	}
	if digits == 0 {
		return 0, bad
	}
	exp := new(big.Int)
	if j < len(body) && (body[j] == 'p' || body[j] == 'P') {
		j++
		k := j
		if k < len(body) && (body[k] == '+' || body[k] == '-') {
			k++
		}
		start := k
		for k < len(body) && body[k] >= '0' && body[k] <= '9' {
			k++
		}
		if k == start {
			return 0, bad
		}
		exp.SetString(body[j:k], 10)
		j = k
	}
	if j != len(body) {
		return 0, bad
	}
	exp.Sub(exp, big.NewInt(int64(4*fracDigits)))
	if mant.Sign() == 0 {
		return math.Copysign(0, signOf(neg)), nil
	}
	// The value is mant * 2**exp; beyond these bounds it certainly
	// overflows or underflows.
	top := int64(mant.BitLen()) + exp.Int64()
	if !exp.IsInt64() || top > 1100 {
		return 0, newPyErr("OverflowError", "hexadecimal value too large to represent as a float")
	}
	if top < -1100 {
		return math.Copysign(0, signOf(neg)), nil
	}
	v := new(big.Float).SetPrec(uint(mant.BitLen()) + 8).SetInt(mant)
	v.SetMantExp(v, int(exp.Int64()))
	f, _ := v.Float64()
	if math.IsInf(f, 0) {
		return 0, newPyErr("OverflowError", "hexadecimal value too large to represent as a float")
	}
	return math.Copysign(f, signOf(neg)), nil
}

func signOf(neg bool) float64 {
	if neg {
		return -1
	}
	return 1
}

// complexMethod is an attribute of a complex other than its special
// methods.
func complexMethod(x complex128, name string) (Value, bool) {
	switch name {
	case "real":
		return floatV(real(x)), true
	case "imag":
		return floatV(imag(x)), true
	case "conjugate":
		return methodsBound(complexV(x), name, func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("complex.conjugate", args, kw); err != nil {
				return nil, err
			}
			return complexV(complex(real(x), -imag(x))), nil
		}), true
	}
	return slotWrapper(complexV(x), name)
}

// ---------------------------------------------------------------- complex()

// builtinComplex is complex(real=0, imag=0), as complex_new_impl.
func builtinComplex(c *callCtx, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKw("complex", []string{"real", "imag"}, 2, args, kw)
	if err != nil {
		return nil, err
	}
	r, i := buf[0], buf[1]
	if r == nil {
		r = mkInt(0)
	}
	if x, ok := r.(complexV); ok && i == nil {
		return x, nil
	}
	if s, ok := unsub(r).(strV); ok {
		if i != nil {
			return nil, typeErr("complex() can't take second arg if first is a string")
		}
		return parseComplexStr(string(s), r)
	}
	if i != nil {
		if _, ok := unsub(i).(strV); ok {
			return nil, typeErr("complex() second arg can't be a string")
		}
	}
	if conv, ok, err := callSpecial(r, "__complex__"); ok {
		if err != nil {
			return nil, err
		}
		if _, isC := unsub(conv).(complexV); !isC {
			return nil, typeErr("__complex__ returned non-complex (type %s)", typeName(conv))
		}
		r = conv
	}
	if !complexOperand(r) {
		return nil, typeErr("complex() first argument must be a string or a number, not '%s'", typeName(r))
	}
	if i != nil && !complexOperand(i) {
		return nil, typeErr("complex() second argument must be a number, not '%s'", typeName(i))
	}
	var crReal, crImag, ciReal, ciImag float64
	crComplex, ciComplex := false, false
	if x, ok := unsub(r).(complexV); ok {
		crReal, crImag, crComplex = real(x), imag(x), true
	} else {
		f, err := builtinFloat(c, []Value{r}, nil)
		if err != nil {
			return nil, err
		}
		crReal = float64(unsub(f).(floatV))
	}
	switch {
	case i == nil:
		ciReal = crImag
	default:
		if x, ok := unsub(i).(complexV); ok {
			ciReal, ciImag, ciComplex = real(x), imag(x), true
		} else {
			f, err := builtinFloat(c, []Value{i}, nil)
			if err != nil {
				return nil, err
			}
			ciReal = float64(unsub(f).(floatV))
		}
	}
	if ciComplex {
		crReal -= ciImag
	}
	if crComplex && i != nil {
		ciReal += crImag
	}
	return complexV(complex(crReal, ciReal)), nil
}

// complexOperand reports whether complex() accepts v as a number: a value
// with __float__ or __index__, or a complex.
func complexOperand(v Value) bool {
	switch unsub(v).(type) {
	case intV, boolV, floatV, complexV:
		return true
	}
	if userClassOf(v) != nil {
		for _, name := range []string{"__float__", "__index__"} {
			if _, ok := userClassOf(v).lookupUser(name); ok {
				return true
			}
		}
	}
	return false
}

// parseComplexStr is complex(s) for the str object obj with text s
// (complex_subtype_from_string and complex_from_string_inner).
func parseComplexStr(s string, obj Value) (Value, error) {
	b := numberText(s)
	if strings.IndexByte(string(b), '_') >= 0 {
		clean, ok := stripNumberUnderscores(b)
		if !ok {
			r, err := pyRepr(obj)
			if err != nil {
				return nil, err
			}
			return nil, valueErr("could not convert string to complex: %s", r)
		}
		b = clean
	}
	bad := valueErr("complex() arg is a malformed string")
	t := string(b)
	p := 0
	skipSpace := func() {
		for p < len(t) && isCSpace(rune(t[p])) {
			p++
		}
	}
	skipSpace()
	bracket := false
	if p < len(t) && t[p] == '(' {
		bracket = true
		p++
		skipSpace()
	}
	var x, y float64
	z, n := strtodPrefix(t[p:])
	if n > 0 {
		p += n
		switch {
		case p < len(t) && (t[p] == '+' || t[p] == '-'):
			x = z
			w, m := strtodPrefix(t[p:])
			if m > 0 {
				y = w
				p += m
			} else {
				y = 1
				if t[p] == '-' {
					y = -1
				}
				p++
			}
			if p >= len(t) || t[p] != 'j' && t[p] != 'J' {
				return nil, bad
			}
			p++
		case p < len(t) && (t[p] == 'j' || t[p] == 'J'):
			p++
			y = z
		default:
			x = z
		}
	} else {
		y = 1
		if p < len(t) && (t[p] == '+' || t[p] == '-') {
			if t[p] == '-' {
				y = -1
			}
			p++
		}
		if p >= len(t) || t[p] != 'j' && t[p] != 'J' {
			return nil, bad
		}
		p++
	}
	skipSpace()
	if bracket {
		if p >= len(t) || t[p] != ')' {
			return nil, bad
		}
		p++
		skipSpace()
	}
	if p != len(t) {
		return nil, bad
	}
	return complexV(complex(x, y)), nil
}

// ---------------------------------------------------------------- class methods

// classMethodFn is a class method of a builtin class, called with the class
// it is bound to.
type classMethodFn func(c *callCtx, cls *classV, args []Value, kw *dictV) (Value, error)

// builtinClassMethods are the class methods of the builtin classes;
// builtinStaticMethods their static methods.
var (
	builtinClassMethods  map[string]map[string]classMethodFn
	builtinStaticMethods map[string]map[string]func(args []Value, kw *dictV) (Value, error)
)

func init() {
	builtinClassMethods = map[string]map[string]classMethodFn{
		"int":       {"from_bytes": intFromBytes},
		"bool":      {"from_bytes": intFromBytes},
		"float":     {"fromhex": floatFromhex},
		"bytes":     {"fromhex": bytesFromhex(false)},
		"bytearray": {"fromhex": bytesFromhex(true)},
		"dict":      {"fromkeys": dictFromkeysOf},
	}
	builtinStaticMethods = map[string]map[string]func(args []Value, kw *dictV) (Value, error){
		"str":       {"maketrans": strMaketrans},
		"bytes":     {"maketrans": bytesMaketrans},
		"bytearray": {"maketrans": bytesMaketrans},
	}
}

// builtinClassAttr is a class or static method of the builtin class base
// reached from cls (base itself or a subclass of it), bound to cls.
func builtinClassAttr(base string, cls *classV, name string) (Value, bool) {
	if fn, ok := builtinClassMethods[base][name]; ok {
		return &callable{name: name, typ: "builtin_function_or_method", self: cls,
			fn: func(c *callCtx, args []Value, kw *dictV) (Value, error) { return fn(c, cls, args, kw) }}, true
	}
	if fn, ok := builtinStaticMethods[base][name]; ok {
		return &callable{name: name, typ: "builtin_function_or_method",
			fn: func(_ *callCtx, args []Value, kw *dictV) (Value, error) { return fn(args, kw) }}, true
	}
	return nil, false
}
