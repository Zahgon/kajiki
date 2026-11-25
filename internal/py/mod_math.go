// Ported from CPython 3.12 (Modules/mathmodule.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"math"
	"math/big"
)

// The errno values the C functions report through.
const (
	errNone = iota
	errEDOM
	errERANGE
)

func domainError() error { return raise("ValueError", "math domain error") }
func rangeError() error  { return raise("OverflowError", "math range error") }

// isError is is_error: EDOM is a ValueError, ERANGE an OverflowError unless
// the result underflowed (|r| < 1.5).
func isError(errno int, r float64) error {
	switch errno {
	case errEDOM:
		return domainError()
	case errERANGE:
		if math.Abs(r) < 1.5 {
			return nil
		}
		return rangeError()
	}
	return nil
}

// floatArg is PyFloat_AsDouble of a math function's argument.
func floatArg(v Value) (float64, error) {
	return ToFloat(v)
}

// math1 is math_1: a function of one float whose NaN result for a non-NaN
// argument is a domain error and whose infinite result for a finite
// argument is a range error (canOverflow) or a domain error.
func math1(name string, f func(float64) float64, canOverflow bool) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := ArgsOne(name, args, kwargs); err != nil {
			return nil, err
		}
		x, err := floatArg(args[0])
		if err != nil {
			return nil, err
		}
		r := f(x)
		if math.IsNaN(r) && !math.IsNaN(x) {
			return nil, domainError()
		}
		if math.IsInf(r, 0) && !math.IsInf(x, 0) && !math.IsNaN(x) {
			if canOverflow {
				return nil, rangeError()
			}
			return nil, domainError()
		}
		return Float(r), nil
	}
}

// scaleFunc is math.degrees or math.radians: x times 180/π or π/180 (as
// doubles), with no check of the result.
func scaleFunc(name string, factor float64) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := ArgsOne(name, args, kwargs); err != nil {
			return nil, err
		}
		x, err := floatArg(args[0])
		if err != nil {
			return nil, err
		}
		return Float(x * factor), nil
	}
}

// math1a is math_1a, for functions that report errors through errno.
func math1a(name string, f func(float64) (float64, int)) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := ArgsOne(name, args, kwargs); err != nil {
			return nil, err
		}
		x, err := floatArg(args[0])
		if err != nil {
			return nil, err
		}
		r, errno := f(x)
		if err := isError(errno, r); err != nil {
			return nil, err
		}
		return Float(r), nil
	}
}

// math2 is math_2: a function of two floats.
func math2(name string, f func(x, y float64) float64) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := NoKeywords(name, kwargs); err != nil {
			return nil, err
		}
		if err := CheckPositional(name, len(args), 2, 2); err != nil {
			return nil, err
		}
		x, err := floatArg(args[0])
		if err != nil {
			return nil, err
		}
		y, err := floatArg(args[1])
		if err != nil {
			return nil, err
		}
		r := f(x, y)
		errno := errNone
		if math.IsNaN(r) {
			if !math.IsNaN(x) && !math.IsNaN(y) {
				errno = errEDOM
			}
		} else if math.IsInf(r, 0) && !math.IsInf(x, 0) && !math.IsInf(y, 0) && !math.IsNaN(x) && !math.IsNaN(y) {
			errno = errERANGE
		}
		if err := isError(errno, r); err != nil {
			return nil, err
		}
		return Float(r), nil
	}
}

func init() {
	register("math", func(b *builder) {
		b.set("pi", Float(math.Pi))
		b.set("e", Float(math.E))
		b.set("tau", Float(2*math.Pi))
		b.set("inf", Float(math.Inf(1)))
		b.set("nan", Float(math.NaN()))
		for _, f := range []struct {
			name        string
			f           func(float64) float64
			canOverflow bool
		}{
			{"acos", libmAcos, false}, {"acosh", libmAcosh, false}, {"asin", libmAsin, false},
			{"asinh", libmAsinh, false}, {"atan", libmAtan, false}, {"atanh", libmAtanh, false},
			{"cbrt", libmCbrt, false}, {"cos", libmCos, false}, {"cosh", libmCosh, true},
			{"erf", libmErf, false}, {"erfc", libmErfc, false}, {"exp", libmExp, true},
			{"exp2", libmExp2, true}, {"expm1", libmExpm1, true}, {"fabs", math.Abs, false},
			{"sin", libmSin, false}, {"sinh", libmSinh, true}, {"sqrt", math.Sqrt, false},
			{"tan", libmTan, false}, {"tanh", libmTanh, false},
		} {
			b.fn(f.name, math1(f.name, f.f, f.canOverflow))
		}
		b.fn("gamma", math1a("gamma", tgamma))
		b.fn("lgamma", math1a("lgamma", lgamma))
		b.fn("atan2", math2("atan2", libmAtan2))
		b.fn("copysign", math2("copysign", math.Copysign))
		b.fn("remainder", math2("remainder", math.Remainder))
		b.fn("fmod", mathFmod)
		b.fn("pow", mathPow)
		b.fn("log", mathLog)
		b.fn("log2", logFunc("log2", libmLog2))
		b.fn("log10", logFunc("log10", libmLog10))
		b.fn("log1p", math1("log1p", libmLog1p, false))
		b.fn("degrees", scaleFunc("degrees", 0x1.ca5dc1a63c1f8p+5))
		b.fn("radians", scaleFunc("radians", 0x1.1df46a2529d39p-6))
		b.fn("floor", roundingFunc("floor", "__floor__", math.Floor))
		b.fn("ceil", roundingFunc("ceil", "__ceil__", math.Ceil))
		b.fn("trunc", mathTrunc)
		b.fn("isnan", floatPred("isnan", math.IsNaN))
		b.fn("isinf", floatPred("isinf", func(x float64) bool { return math.IsInf(x, 0) }))
		b.fn("isfinite", floatPred("isfinite", func(x float64) bool { return !math.IsInf(x, 0) && !math.IsNaN(x) }))
		b.fn("isclose", mathIsclose)
		b.fn("frexp", mathFrexp)
		b.fn("ldexp", mathLdexp)
		b.fn("modf", mathModf)
		b.fn("fsum", mathFsum)
		b.fn("prod", mathProd)
		b.fn("sumprod", mathSumprod)
		b.fn("hypot", mathHypot)
		b.fn("dist", mathDist)
		b.fn("factorial", mathFactorial)
		b.fn("gcd", gcdLcm("gcd", false))
		b.fn("lcm", gcdLcm("lcm", true))
		b.fn("isqrt", mathIsqrt)
		b.fn("comb", combPerm("comb"))
		b.fn("perm", combPerm("perm"))
		b.fn("nextafter", mathNextafter)
		b.fn("ulp", mathUlp)
	})
}

func floatPred(name string, f func(float64) bool) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := ArgsOne(name, args, kwargs); err != nil {
			return nil, err
		}
		x, err := floatArg(args[0])
		if err != nil {
			return nil, err
		}
		return Bool(f(x)), nil
	}
}

func mathFmod(args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("fmod", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("fmod", len(args), 2, 2); err != nil {
		return nil, err
	}
	x, err := floatArg(args[0])
	if err != nil {
		return nil, err
	}
	y, err := floatArg(args[1])
	if err != nil {
		return nil, err
	}
	if math.IsInf(y, 0) && !math.IsInf(x, 0) && !math.IsNaN(x) {
		return Float(x), nil
	}
	r := math.Mod(x, y)
	if math.IsNaN(r) && !math.IsNaN(x) && !math.IsNaN(y) {
		return nil, domainError()
	}
	return Float(r), nil
}

// mathPow is math.pow, which handles the IEEE special values itself.
func mathPow(args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("pow", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("pow", len(args), 2, 2); err != nil {
		return nil, err
	}
	x, err := floatArg(args[0])
	if err != nil {
		return nil, err
	}
	y, err := floatArg(args[1])
	if err != nil {
		return nil, err
	}
	finite := func(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) }
	var r float64
	errno := errNone
	switch {
	case !finite(x) || !finite(y):
		switch {
		case math.IsNaN(x):
			r = x
			if y == 0 {
				r = 1
			}
		case math.IsNaN(y):
			r = y
			if x == 1 {
				r = 1
			}
		case math.IsInf(x, 0):
			oddY := finite(y) && math.Mod(math.Abs(y), 2) == 1
			switch {
			case y > 0:
				r = math.Abs(x)
				if oddY {
					r = x
				}
			case y == 0:
				r = 1
			default:
				r = 0
				if oddY {
					r = math.Copysign(0, x)
				}
			}
		default:
			switch {
			case math.Abs(x) == 1:
				r = 1
			case y > 0 && math.Abs(x) > 1:
				r = y
			case y < 0 && math.Abs(x) < 1:
				r = -y
			default:
				r = 0
			}
		}
	default:
		r = libmPow(x, y)
		if math.IsNaN(r) {
			errno = errEDOM
		} else if math.IsInf(r, 0) {
			if x == 0 {
				errno = errEDOM
			} else {
				errno = errERANGE
			}
		}
	}
	if err := isError(errno, r); err != nil {
		return nil, err
	}
	return Float(r), nil
}

// mLog is m_log: log with C's domain errors made NaN or -inf.
func mLog(f func(float64) float64) func(float64) float64 {
	return func(x float64) float64 {
		switch {
		case math.IsNaN(x):
			return x
		case math.IsInf(x, 1):
			return x
		case math.IsInf(x, -1) || x < 0:
			return math.NaN()
		case x == 0:
			return math.Inf(-1)
		}
		return f(x)
	}
}

// logHelper is loghelper: the logarithm of an int of any size (by its
// mantissa and exponent when it does not fit a float), or math_1 of any
// other number.
func logHelper(arg Value, f func(float64) float64) (float64, error) {
	if n, ok := AsInt(arg); ok {
		if n.Sign() <= 0 {
			return 0, domainError()
		}
		if x, _ := new(big.Float).SetInt(n).Float64(); !math.IsInf(x, 0) {
			return f(x), nil
		}
		// loghelper's func(x) + func(2.0) * e for x = m·2^e.
		m, e := intFrexp(n)
		return f(m) + float64(f(2.0)*float64(e)), nil
	}
	x, err := floatArg(arg)
	if err != nil {
		return 0, err
	}
	r := mLog(f)(x)
	if math.IsNaN(r) && !math.IsNaN(x) || math.IsInf(r, 0) && !math.IsInf(x, 0) && !math.IsNaN(x) {
		return 0, domainError()
	}
	return r, nil
}

// intFrexp is _PyLong_Frexp: n = m·2**e with m in [0.5, 1) rounded to a
// double (half to even).
func intFrexp(n *big.Int) (float64, int) {
	f := new(big.Float).SetPrec(53).SetMode(big.ToNearestEven).SetInt(n)
	mant := new(big.Float)
	e := f.MantExp(mant)
	m, _ := mant.Float64()
	return m, e
}

func logFunc(name string, f func(float64) float64) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := ArgsOne(name, args, kwargs); err != nil {
			return nil, err
		}
		r, err := logHelper(args[0], f)
		if err != nil {
			return nil, err
		}
		return Float(r), nil
	}
}

// mathLog is math.log(x[, base]).
func mathLog(args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("log", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("log", len(args), 1, 2); err != nil {
		return nil, err
	}
	num, err := logHelper(args[0], libmLog)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return Float(num), nil
	}
	den, err := logHelper(args[1], libmLog)
	if err != nil {
		return nil, err
	}
	return BinaryOp("Div", Float(num), Float(den))
}

// roundingFunc is math.floor or math.ceil: the special method for objects
// that define it, else the float rounded to an int.
func roundingFunc(name, special string, f func(float64) float64) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := ArgsOne(name, args, kwargs); err != nil {
			return nil, err
		}
		x := args[0]
		if fl, ok := AsFloat(x); !ok || TypeName(x) != "float" {
			if has, err := HasAttr(TypeOf(x), special); err == nil && has {
				return CallMethod(x, special)
			}
		} else {
			return floatToInt(f(fl))
		}
		fl, err := floatArg(x)
		if err != nil {
			return nil, err
		}
		return floatToInt(f(fl))
	}
}

// floatToInt is PyLong_FromDouble.
func floatToInt(f float64) (Value, error) {
	if math.IsInf(f, 0) {
		return nil, raise("OverflowError", "cannot convert float infinity to integer")
	}
	if math.IsNaN(f) {
		return nil, raise("ValueError", "cannot convert float NaN to integer")
	}
	i, _ := big.NewFloat(f).Int(nil)
	return BigInt(i), nil
}

func mathTrunc(args []Value, kwargs []Keyword) (Value, error) {
	if err := ArgsOne("trunc", args, kwargs); err != nil {
		return nil, err
	}
	x := args[0]
	if f, ok := AsFloat(x); ok && TypeName(x) == "float" {
		return floatToInt(math.Trunc(f))
	}
	if has, err := HasAttr(TypeOf(x), "__trunc__"); err != nil || !has {
		if err != nil {
			return nil, err
		}
		return nil, raise("TypeError", "type %s doesn't define __trunc__ method", TypeName(x))
	}
	return CallMethod(x, "__trunc__")
}

func mathIsclose(args []Value, kwargs []Keyword) (Value, error) {
	buf, err := ParseArgs("isclose", []string{"a", "b", "rel_tol", "abs_tol"}, 2, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	a, err := floatArg(buf[0])
	if err != nil {
		return nil, err
	}
	b, err := floatArg(buf[1])
	if err != nil {
		return nil, err
	}
	relTol, absTol := 1e-09, 0.0
	if buf[2] != nil {
		if relTol, err = floatArg(buf[2]); err != nil {
			return nil, err
		}
	}
	if buf[3] != nil {
		if absTol, err = floatArg(buf[3]); err != nil {
			return nil, err
		}
	}
	if relTol < 0 || absTol < 0 {
		return nil, raise("ValueError", "tolerances must be non-negative")
	}
	if a == b {
		return Bool(true), nil
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return Bool(false), nil
	}
	diff := math.Abs(b - a)
	return Bool(diff <= math.Abs(float64(relTol*b)) || diff <= math.Abs(float64(relTol*a)) || diff <= absTol), nil
}

func mathFrexp(args []Value, kwargs []Keyword) (Value, error) {
	if err := ArgsOne("frexp", args, kwargs); err != nil {
		return nil, err
	}
	x, err := floatArg(args[0])
	if err != nil {
		return nil, err
	}
	if math.IsNaN(x) || math.IsInf(x, 0) || x == 0 {
		return Tuple(Float(x), Int(0)), nil
	}
	m, e := math.Frexp(x)
	return Tuple(Float(m), Int(int64(e))), nil
}

func mathLdexp(args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("ldexp", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("ldexp", len(args), 2, 2); err != nil {
		return nil, err
	}
	x, err := floatArg(args[0])
	if err != nil {
		return nil, err
	}
	n, ok := AsInt(args[1])
	if !ok {
		return nil, raise("TypeError", "Expected an int as second argument to ldexp.")
	}
	if x == 0 || math.IsInf(x, 0) || math.IsNaN(x) {
		return Float(x), nil
	}
	exp := 0
	switch {
	case n.Cmp(big.NewInt(1<<20)) > 0:
		exp = 1 << 20
	case n.Cmp(big.NewInt(-(1 << 20))) < 0:
		exp = -(1 << 20)
	default:
		exp = int(n.Int64())
	}
	r := math.Ldexp(x, exp)
	if math.IsInf(r, 0) {
		return nil, rangeError()
	}
	return Float(r), nil
}

func mathModf(args []Value, kwargs []Keyword) (Value, error) {
	if err := ArgsOne("modf", args, kwargs); err != nil {
		return nil, err
	}
	x, err := floatArg(args[0])
	if err != nil {
		return nil, err
	}
	if math.IsInf(x, 0) {
		return Tuple(Float(math.Copysign(0, x)), Float(x)), nil
	}
	if math.IsNaN(x) {
		return Tuple(Float(x), Float(x)), nil
	}
	i, f := math.Modf(x)
	return Tuple(Float(f), Float(i)), nil
}

// sumArg is the conversion of a summand of fsum and of a coordinate of
// hypot and dist (ASSIGN_DOUBLE).
func sumArg(v Value) (float64, error) {
	if f, ok := AsFloat(v); ok && TypeName(v) == "float" {
		return f, nil
	}
	if n, ok := AsInt(v); ok {
		f, _ := new(big.Float).SetInt(n).Float64()
		if math.IsInf(f, 0) {
			return 0, raise("OverflowError", "int too large to convert to float")
		}
		return f, nil
	}
	return floatArg(v)
}

// mathFsum is math.fsum: Shewchuk's exact summation with partials (msum).
func mathFsum(args []Value, kwargs []Keyword) (Value, error) {
	if err := ArgsOne("fsum", args, kwargs); err != nil {
		return nil, err
	}
	var p []float64
	specialSum, infSum := 0.0, 0.0
	var overflow error
	err := iterate(args[0], func(item Value) error {
		x, err := sumArg(item)
		if err != nil {
			return err
		}
		xsave := x
		i := 0
		for j := range p {
			y := p[j]
			if math.Abs(x) < math.Abs(y) {
				x, y = y, x
			}
			hi := x + y
			lo := y - (hi - x)
			if lo != 0 {
				p[i] = lo
				i++
			}
			x = hi
		}
		p = p[:i]
		if x != 0 {
			if math.IsInf(x, 0) || math.IsNaN(x) {
				if !math.IsInf(xsave, 0) && !math.IsNaN(xsave) {
					overflow = raise("OverflowError", "intermediate overflow in fsum")
					return overflow
				}
				if math.IsInf(xsave, 0) {
					infSum += xsave
				}
				specialSum += xsave
				p = p[:0]
			} else {
				p = append(p, x)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if specialSum != 0 {
		if math.IsNaN(infSum) {
			return nil, raise("ValueError", "-inf + inf in fsum")
		}
		return Float(specialSum), nil
	}
	hi := 0.0
	n := len(p)
	if n > 0 {
		var lo float64
		n--
		hi = p[n]
		for n > 0 {
			x := hi
			n--
			y := p[n]
			hi = x + y
			yr := hi - x
			lo = y - yr
			if lo != 0 {
				break
			}
		}
		if n > 0 && (lo < 0 && p[n-1] < 0 || lo > 0 && p[n-1] > 0) {
			y := lo * 2
			x := hi + y
			yr := x - hi
			if y == yr {
				hi = x
			}
		}
	}
	return Float(hi), nil
}

// mathProd is math.prod(iterable, *, start=1).
func mathProd(args []Value, kwargs []Keyword) (Value, error) {
	buf, err := ParseArgs("prod", []string{"", "start"}, 1, 1, args, kwargs)
	if err != nil {
		return nil, err
	}
	result := buf[1]
	if result == nil {
		result = Int(1)
	}
	err = iterate(buf[0], func(item Value) error {
		r, err := BinaryOp("Mult", result, item)
		result = r
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// mathSumprod is math.sumprod(p, q): the sum of the products, exact for
// ints and rounded once for floats.
func mathSumprod(args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("sumprod", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("sumprod", len(args), 2, 2); err != nil {
		return nil, err
	}
	p, err := listOf(args[0])
	if err != nil {
		return nil, err
	}
	q, err := listOf(args[1])
	if err != nil {
		return nil, err
	}
	if len(p) != len(q) {
		return nil, raise("ValueError", "Inputs are not the same length")
	}
	exact, anyFloat := true, false
	for _, v := range append(append([]Value(nil), p...), q...) {
		switch TypeName(v) {
		case "int", "bool":
		case "float":
			anyFloat = true
		default:
			exact = false
		}
	}
	if exact {
		sum := new(big.Rat)
		for i := range p {
			a, b := ratOf(p[i]), ratOf(q[i])
			if a == nil || b == nil {
				exact = false
				break
			}
			sum.Add(sum, new(big.Rat).Mul(a, b))
		}
		if exact {
			if !anyFloat {
				return BigInt(sum.Num()), nil
			}
			f, _ := sum.Float64()
			return Float(f), nil
		}
	}
	var total Value = Int(0)
	for i := range p {
		prod, err := BinaryOp("Mult", p[i], q[i])
		if err != nil {
			return nil, err
		}
		if total, err = BinaryOp("Add", total, prod); err != nil {
			return nil, err
		}
	}
	return total, nil
}

// ratOf is the exact value of an int or a finite float, nil otherwise.
func ratOf(v Value) *big.Rat {
	if n, ok := AsInt(v); ok {
		return new(big.Rat).SetInt(n)
	}
	f, ok := AsFloat(v)
	if !ok || math.IsInf(f, 0) || math.IsNaN(f) {
		return nil
	}
	return new(big.Rat).SetFloat64(f)
}

// vectorNorm is vector_norm: the Euclidean norm of vec, whose largest
// absolute value is max, with an error-free accumulation of the squares.
func vectorNorm(vec []float64, max float64, foundNaN bool) float64 {
	if math.IsInf(max, 0) {
		return max
	}
	if foundNaN {
		return math.NaN()
	}
	if max == 0 || len(vec) <= 1 {
		return max
	}
	_, maxE := math.Frexp(max)
	if maxE < -1023 {
		const dblMin = 0x1p-1022
		for i := range vec {
			vec[i] /= dblMin
		}
		return dblMin * vectorNorm(vec, max/dblMin, foundNaN)
	}
	scale := math.Ldexp(1, -maxE)
	csum, frac1, frac2 := 1.0, 0.0, 0.0
	for _, x := range vec {
		x *= scale
		hi, lo := dlMul(x, x)
		shi, slo := dlFastSum(csum, hi)
		csum = shi
		frac1 += lo
		frac2 += slo
	}
	h := math.Sqrt(csum - 1 + (frac1 + frac2))
	hi, lo := dlMul(-h, h)
	shi, slo := dlFastSum(csum, hi)
	csum = shi
	frac1 += lo
	frac2 += slo
	x := csum - 1 + (frac1 + frac2)
	h += x / (2 * h)
	return h / scale
}

// dlMul is the exact product x·y as hi + lo. The product is rounded on its
// own, so that no sum it enters is fused with it.
func dlMul(x, y float64) (float64, float64) {
	z := float64(x * y)
	return z, math.FMA(x, y, -z)
}

// dlFastSum is the exact sum a + b (|a| ≥ |b|) as hi + lo.
func dlFastSum(a, b float64) (float64, float64) {
	x := a + b
	return x, (a - x) + b
}

func mathHypot(args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("hypot", kwargs); err != nil {
		return nil, err
	}
	vec := make([]float64, len(args))
	max, foundNaN := 0.0, false
	for i, a := range args {
		x, err := sumArg(a)
		if err != nil {
			return nil, err
		}
		x = math.Abs(x)
		vec[i] = x
		foundNaN = foundNaN || math.IsNaN(x)
		if x > max {
			max = x
		}
	}
	return Float(vectorNorm(vec, max, foundNaN)), nil
}

func mathDist(args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("dist", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("dist", len(args), 2, 2); err != nil {
		return nil, err
	}
	var pts [2][]Value
	for i, a := range args {
		items, ok := SeqItems(a)
		if !ok || !IsTuple(a) {
			t, err := Call(Builtin("tuple"), []Value{a}, nil)
			if err != nil {
				return nil, err
			}
			items, _ = SeqItems(t)
		}
		pts[i] = items
	}
	if len(pts[0]) != len(pts[1]) {
		return nil, raise("ValueError", "both points must have the same number of dimensions")
	}
	vec := make([]float64, len(pts[0]))
	max, foundNaN := 0.0, false
	for i := range vec {
		px, err := sumArg(pts[0][i])
		if err != nil {
			return nil, err
		}
		qx, err := sumArg(pts[1][i])
		if err != nil {
			return nil, err
		}
		x := math.Abs(px - qx)
		vec[i] = x
		foundNaN = foundNaN || math.IsNaN(x)
		if x > max {
			max = x
		}
	}
	return Float(vectorNorm(vec, max, foundNaN)), nil
}

func mathFactorial(args []Value, kwargs []Keyword) (Value, error) {
	if err := ArgsOne("factorial", args, kwargs); err != nil {
		return nil, err
	}
	n, err := Index(args[0])
	if err != nil {
		return nil, err
	}
	if n.Sign() < 0 {
		return nil, raise("ValueError", "factorial() not defined for negative values")
	}
	if !n.IsInt64() || n.Int64() > 1<<20 {
		return nil, raise("OverflowError", "factorial() argument should not exceed %d", int64(math.MaxInt64))
	}
	return BigInt(new(big.Int).MulRange(1, n.Int64())), nil
}

// gcdLcm is math.gcd(*integers) or math.lcm(*integers).
func gcdLcm(name string, lcm bool) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := NoKeywords(name, kwargs); err != nil {
			return nil, err
		}
		acc := big.NewInt(0)
		if lcm {
			acc = big.NewInt(1)
		}
		for i, a := range args {
			n, err := Index(a)
			if err != nil {
				return nil, err
			}
			n = new(big.Int).Abs(n)
			switch {
			case i == 0:
				acc = n
			case lcm:
				if acc.Sign() == 0 || n.Sign() == 0 {
					acc = big.NewInt(0)
				} else {
					g := new(big.Int).GCD(nil, nil, acc, n)
					acc = new(big.Int).Mul(new(big.Int).Quo(acc, g), n)
				}
			default:
				acc = new(big.Int).GCD(nil, nil, acc, n)
			}
		}
		return BigInt(acc), nil
	}
}

func mathIsqrt(args []Value, kwargs []Keyword) (Value, error) {
	if err := ArgsOne("isqrt", args, kwargs); err != nil {
		return nil, err
	}
	n, err := Index(args[0])
	if err != nil {
		return nil, err
	}
	if n.Sign() < 0 {
		return nil, raise("ValueError", "isqrt() argument must be nonnegative")
	}
	return BigInt(new(big.Int).Sqrt(n)), nil
}

// combPerm is math.comb(n, k) or math.perm(n, k=None).
func combPerm(name string) fn {
	return func(args []Value, kwargs []Keyword) (Value, error) {
		if err := NoKeywords(name, kwargs); err != nil {
			return nil, err
		}
		min := 2
		if name == "perm" {
			min = 1
		}
		if err := CheckPositional(name, len(args), min, 2); err != nil {
			return nil, err
		}
		if name == "perm" && (len(args) == 1 || IsNone(args[1])) {
			return mathFactorial(args[:1], nil)
		}
		n, err := Index(args[0])
		if err != nil {
			return nil, err
		}
		k, err := Index(args[1])
		if err != nil {
			return nil, err
		}
		if n.Sign() < 0 {
			return nil, raise("ValueError", "n must be a non-negative integer")
		}
		if k.Sign() < 0 {
			return nil, raise("ValueError", "k must be a non-negative integer")
		}
		if k.Cmp(n) > 0 {
			return Int(0), nil
		}
		if name == "perm" {
			if !k.IsInt64() {
				return nil, raise("OverflowError", "k must not exceed %d", int64(math.MaxInt64))
			}
			lo := new(big.Int).Sub(n, k)
			lo.Add(lo, big.NewInt(1))
			r := big.NewInt(1)
			for i := int64(0); i < k.Int64(); i++ {
				r.Mul(r, new(big.Int).Add(lo, big.NewInt(i)))
			}
			return BigInt(r), nil
		}
		if nk := new(big.Int).Sub(n, k); nk.Cmp(k) < 0 {
			k = nk
		}
		if !k.IsInt64() {
			return nil, raise("OverflowError", "min(n - k, k) must not exceed %d", int64(math.MaxInt64))
		}
		r := big.NewInt(1)
		for i := int64(0); i < k.Int64(); i++ {
			r.Mul(r, new(big.Int).Sub(n, big.NewInt(i)))
			r.Quo(r, big.NewInt(i+1))
		}
		return BigInt(r), nil
	}
}

func mathNextafter(args []Value, kwargs []Keyword) (Value, error) {
	buf, err := ParseArgs("nextafter", []string{"", "", "steps"}, 2, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	x, err := floatArg(buf[0])
	if err != nil {
		return nil, err
	}
	y, err := floatArg(buf[1])
	if err != nil {
		return nil, err
	}
	if buf[2] == nil || IsNone(buf[2]) {
		return Float(math.Nextafter(x, y)), nil
	}
	steps, err := Index(buf[2])
	if err != nil {
		return nil, err
	}
	if steps.Sign() < 0 {
		return nil, raise("ValueError", "steps must be a non-negative integer")
	}
	n := int64(math.MaxInt64)
	if steps.IsInt64() {
		n = steps.Int64()
	}
	for i := int64(0); i < n && x != y && !math.IsNaN(x); i++ {
		x = math.Nextafter(x, y)
	}
	if math.IsNaN(y) {
		return Float(y), nil
	}
	return Float(x), nil
}

func mathUlp(args []Value, kwargs []Keyword) (Value, error) {
	if err := ArgsOne("ulp", args, kwargs); err != nil {
		return nil, err
	}
	x, err := floatArg(args[0])
	if err != nil {
		return nil, err
	}
	if math.IsNaN(x) {
		return Float(x), nil
	}
	x = math.Abs(x)
	if math.IsInf(x, 0) {
		return Float(x), nil
	}
	x2 := math.Nextafter(x, math.Inf(1))
	if math.IsInf(x2, 0) {
		x2 = math.Nextafter(x, math.Inf(-1))
		return Float(x - x2), nil
	}
	return Float(x2 - x), nil
}

// ---------------------------------------------------------------- gamma

const (
	lanczosG          = 6.024680040776729583740234375
	lanczosGMinusHalf = 5.524680040776729583740234375
	logPi             = 1.144729885849400174143427351353058711647
)

var lanczosNumCoeffs = [13]float64{
	23531376880.410759688572007674451636754734846804940,
	42919803642.649098768957899047001988850926355848959,
	35711959237.355668049440185451547166705960488635843,
	17921034426.037209699919755754458931112671403265390,
	6039542586.3520280050642916443072979210699388420708,
	1439720407.3117216736632230727949123939715485786772,
	248874557.86205415651146038641322942321632125127801,
	31426415.585400194380614231628318205362874684987640,
	2876370.6289353724412254090516208496135991145378768,
	186056.26539522349504029498971604569928220784236328,
	8071.6720023658162106380029022722506138218516325024,
	210.82427775157934587250973392071336271166969580291,
	2.5066282746310002701649081771338373386264310793408,
}

var lanczosDenCoeffs = [13]float64{
	0.0, 39916800.0, 120543840.0, 150917976.0, 105258076.0, 45995730.0,
	13339535.0, 2637558.0, 357423.0, 32670.0, 1925.0, 66.0, 1.0,
}

var gammaIntegral = [23]float64{
	1.0, 1.0, 2.0, 6.0, 24.0, 120.0, 720.0, 5040.0, 40320.0, 362880.0,
	3628800.0, 39916800.0, 479001600.0, 6227020800.0, 87178291200.0,
	1307674368000.0, 20922789888000.0, 355687428096000.0,
	6402373705728000.0, 121645100408832000.0, 2432902008176640000.0,
	51090942171709440000.0, 1124000727777607680000.0,
}

// lanczosSum is the Lanczos rational function, by Horner's rule in x or
// in 1/x, each product rounded on its own.
func lanczosSum(x float64) float64 {
	num, den := 0.0, 0.0
	if x < 5 {
		for i := len(lanczosNumCoeffs) - 1; i >= 0; i-- {
			num = float64(num*x) + lanczosNumCoeffs[i]
			den = float64(den*x) + lanczosDenCoeffs[i]
		}
	} else {
		for i := range lanczosNumCoeffs {
			num = num/x + lanczosNumCoeffs[i]
			den = den/x + lanczosDenCoeffs[i]
		}
	}
	return num / den
}

// sinpi is m_sinpi: sin(πx), exact at the integers and half-integers.
func sinpi(x float64) float64 {
	y := math.Mod(math.Abs(x), 2)
	var r float64
	switch int(math.Round(2 * y)) {
	case 0:
		r = libmSin(math.Pi * y)
	case 1:
		r = libmCos(math.Pi * (y - 0.5))
	case 2:
		r = libmSin(math.Pi * (1 - y))
	case 3:
		r = -libmCos(math.Pi * (y - 1.5))
	default:
		r = libmSin(math.Pi * (y - 2))
	}
	return math.Copysign(1, x) * r
}

// tgamma is m_tgamma, with its errno.
func tgamma(x float64) (float64, int) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		if math.IsNaN(x) || x > 0 {
			return x, errNone
		}
		return math.NaN(), errEDOM
	}
	if x == 0 {
		return math.Copysign(math.Inf(1), x), errEDOM
	}
	if x == math.Floor(x) {
		if x < 0 {
			return math.NaN(), errEDOM
		}
		if x <= float64(len(gammaIntegral)) {
			return gammaIntegral[int(x)-1], errNone
		}
	}
	absx := math.Abs(x)
	if absx < 1e-20 {
		r := 1 / x
		if math.IsInf(r, 0) {
			return r, errERANGE
		}
		return r, errNone
	}
	if absx > 200 {
		if x < 0 {
			return 0.0 / sinpi(x), errNone
		}
		return math.Inf(1), errERANGE
	}
	y := absx + lanczosGMinusHalf
	var z float64
	if absx > lanczosGMinusHalf {
		q := y - absx
		z = q - lanczosGMinusHalf
	} else {
		q := y - lanczosGMinusHalf
		z = q - absx
	}
	z = z * lanczosG / y
	var r float64
	if x < 0 {
		r = -math.Pi / sinpi(absx) / absx * libmExp(y) / lanczosSum(absx)
		r -= float64(z * r)
		if absx < 140 {
			r /= libmPow(y, absx-0.5)
		} else {
			sqrtpow := libmPow(y, absx/2-0.25)
			r /= sqrtpow
			r /= sqrtpow
		}
	} else {
		r = lanczosSum(absx) / libmExp(y)
		r += float64(z * r)
		if absx < 140 {
			r *= libmPow(y, absx-0.5)
		} else {
			sqrtpow := libmPow(y, absx/2-0.25)
			r *= sqrtpow
			r *= sqrtpow
		}
	}
	if math.IsInf(r, 0) {
		return r, errERANGE
	}
	return r, errNone
}

// lgamma is m_lgamma, with its errno.
func lgamma(x float64) (float64, int) {
	if math.IsNaN(x) {
		return x, errNone
	}
	if math.IsInf(x, 0) {
		return math.Inf(1), errNone
	}
	if x == math.Floor(x) && x <= 2 {
		if x <= 0 {
			return math.Inf(1), errEDOM
		}
		return 0, errNone
	}
	absx := math.Abs(x)
	if absx < 1e-20 {
		return -libmLog(absx), errNone
	}
	r := libmLog(lanczosSum(absx)) - lanczosG
	r += float64((absx - 0.5) * (libmLog(absx+lanczosG-0.5) - 1))
	if x < 0 {
		r = logPi - libmLog(math.Abs(sinpi(absx))) - libmLog(absx) - r
	}
	if math.IsInf(r, 0) {
		return r, errERANGE
	}
	return r, errNone
}
