// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

// The functions of the C math library CPython calls that are evaluated here
// in extended precision with math/big and rounded once: sin, cos, tan, atan,
// atan2, asin, acos, cbrt, erf and erfc, with C's special cases, and
// glibc's hypot. The C library's results are these correctly rounded ones
// for nearly all arguments of the trigonometric functions; for cbrt, erf
// and erfc it is less often so.
package py

import (
	"math"
	"math/big"
	"sync"
)

// prec is the working precision in bits: far more than the 53 bits of the
// result, so the one rounding at the end is correct.
const prec = 320

func newF() *big.Float { return new(big.Float).SetPrec(prec) }

func bigOf(x float64) *big.Float { return newF().SetFloat64(x) }

func bigInt(i int64) *big.Float { return newF().SetInt64(i) }

func round(f *big.Float) float64 {
	r, _ := f.Float64()
	return r
}

var (
	constOnce sync.Once
	bigPi     *big.Float // π to piPrec bits
	bigLn2    *big.Float
)

// piPrec is the precision of π: enough to reduce the largest double modulo
// π/2 and keep prec bits.
const piPrec = 1200 + prec

func consts() {
	constOnce.Do(func() {
		// π = 16 atan(1/5) - 4 atan(1/239) (Machin).
		p := new(big.Float).SetPrec(piPrec)
		a := atanInv(5, piPrec)
		a.Mul(a, new(big.Float).SetPrec(piPrec).SetInt64(16))
		b := atanInv(239, piPrec)
		b.Mul(b, new(big.Float).SetPrec(piPrec).SetInt64(4))
		bigPi = p.Sub(a, b)
		bigLn2 = atanhSeries(newF().Quo(bigInt(1), bigInt(3)))
		bigLn2.Mul(bigLn2, bigInt(2))
	})
}

// atanInv is atan(1/n) to the given precision by its series.
func atanInv(n int64, p uint) *big.Float {
	x := new(big.Float).SetPrec(p).Quo(new(big.Float).SetPrec(p).SetInt64(1), new(big.Float).SetPrec(p).SetInt64(n))
	x2 := new(big.Float).SetPrec(p).Mul(x, x)
	sum := new(big.Float).SetPrec(p).Set(x)
	term := new(big.Float).SetPrec(p).Set(x)
	for k := int64(1); ; k++ {
		term.Mul(term, x2)
		t := new(big.Float).SetPrec(p).Quo(term, new(big.Float).SetPrec(p).SetInt64(2*k+1))
		if t.Sign() == 0 || t.MantExp(nil) < sum.MantExp(nil)-int(p)-8 {
			break
		}
		if k%2 == 1 {
			sum.Sub(sum, t)
		} else {
			sum.Add(sum, t)
		}
	}
	return sum
}

// atanhSeries is atanh(z) = z + z³/3 + z⁵/5 + ... for |z| < 1 (fast for
// small z).
func atanhSeries(z *big.Float) *big.Float {
	z2 := newF().Mul(z, z)
	sum := newF().Set(z)
	term := newF().Set(z)
	for k := int64(1); k < 10000; k++ {
		term.Mul(term, z2)
		t := newF().Quo(term, bigInt(2*k+1))
		if t.Sign() == 0 || t.MantExp(nil) < sum.MantExp(nil)-prec-8 {
			break
		}
		sum.Add(sum, t)
	}
	return sum
}

// expBig is e**t for a moderate t (|t| < 1e4): t = k·ln2 + r, exp(r) by
// its series on r/2^16 squared sixteen times.
func expBig(t *big.Float) *big.Float {
	consts()
	kf := newF().Quo(t, bigLn2)
	k, _ := kf.Int64()
	r := newF().Sub(t, newF().Mul(bigLn2, bigInt(k)))
	const halvings = 16
	r.SetMantExp(r, -halvings)
	sum := expm1Series(r)
	sum.Add(sum, bigInt(1))
	for i := 0; i < halvings; i++ {
		sum.Mul(sum, sum)
	}
	return sum.SetMantExp(sum, int(k))
}

// expm1Series is e**r - 1 for |r| < 1 by its series.
func expm1Series(r *big.Float) *big.Float {
	sum := newF()
	term := bigInt(1)
	for n := int64(1); n < 400; n++ {
		term.Mul(term, r)
		term.Quo(term, bigInt(n))
		if term.Sign() == 0 || (sum.Sign() != 0 && term.MantExp(nil) < sum.MantExp(nil)-prec-8) {
			break
		}
		sum.Add(sum, term)
	}
	return sum
}

// reduce returns r and k with x = k·π/2 + r and |r| ≤ π/4, r to prec bits.
func reduce(x float64) (*big.Float, int64) {
	consts()
	if math.Abs(x) <= math.Pi/4 {
		return bigOf(x), 0
	}
	p := uint(piPrec)
	halfPi := new(big.Float).SetPrec(p).Set(bigPi)
	halfPi.SetMantExp(halfPi, -1)
	bx := new(big.Float).SetPrec(p).SetFloat64(x)
	q := new(big.Float).SetPrec(p).Quo(bx, halfPi)
	// k = round(q)
	half := new(big.Float).SetPrec(p).SetFloat64(0.5)
	if q.Sign() < 0 {
		q.Sub(q, half)
	} else {
		q.Add(q, half)
	}
	kInt, _ := q.Int(nil)
	k := new(big.Float).SetPrec(p).SetInt(kInt)
	r := new(big.Float).SetPrec(p).Sub(bx, new(big.Float).SetPrec(p).Mul(k, halfPi))
	m := new(big.Int).And(kInt, big.NewInt(3))
	return newF().Set(r), m.Int64()
}

// sinCosSeries returns sin(r) and cos(r) for |r| ≤ π/4.
func sinCosSeries(r *big.Float) (*big.Float, *big.Float) {
	r2 := newF().Mul(r, r)
	sin := newF().Set(r)
	cos := bigInt(1)
	ts := newF().Set(r)
	tc := bigInt(1)
	for n := int64(1); n < 200; n++ {
		ts.Mul(ts, r2)
		ts.Quo(ts, bigInt((2*n)*(2*n+1)))
		tc.Mul(tc, r2)
		tc.Quo(tc, bigInt((2*n-1)*(2*n)))
		if n%2 == 1 {
			sin.Sub(sin, ts)
			cos.Sub(cos, tc)
		} else {
			sin.Add(sin, ts)
			cos.Add(cos, tc)
		}
		if tc.Sign() == 0 || tc.MantExp(nil) < -prec-16 {
			break
		}
	}
	return sin, cos
}

// libmSin is sin(x).
func libmSin(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	if x == 0 {
		return x
	}
	r, k := reduce(x)
	s, c := sinCosSeries(r)
	switch k {
	case 0:
		return round(s)
	case 1:
		return round(c)
	case 2:
		return round(s.Neg(s))
	}
	return round(c.Neg(c))
}

// libmCos is cos(x).
func libmCos(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	r, k := reduce(x)
	s, c := sinCosSeries(r)
	switch k {
	case 0:
		return round(c)
	case 1:
		return round(s.Neg(s))
	case 2:
		return round(c.Neg(c))
	}
	return round(s)
}

// libmTan is tan(x).
func libmTan(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	if x == 0 {
		return x
	}
	r, k := reduce(x)
	s, c := sinCosSeries(r)
	if k%2 == 0 {
		return round(newF().Quo(s, c))
	}
	return round(newF().Neg(newF().Quo(c, s)))
}

// atanBig is atan(z) for any z: reduced to |z| ≤ 1 by atan(z) = π/2 -
// atan(1/z), then halved with atan(z) = 2·atan(z / (1 + √(1+z²))) until
// small, then summed.
func atanBig(z *big.Float) *big.Float {
	consts()
	neg := z.Sign() < 0
	a := newF().Abs(z)
	invert := false
	if a.Cmp(bigInt(1)) > 0 {
		a.Quo(bigInt(1), a)
		invert = true
	}
	halvings := 0
	for a.Cmp(bigOf(1.0/64)) > 0 {
		s := newF().Mul(a, a)
		s.Add(s, bigInt(1))
		s.Sqrt(s)
		s.Add(s, bigInt(1))
		a.Quo(a, s)
		halvings++
	}
	a2 := newF().Mul(a, a)
	sum := newF().Set(a)
	term := newF().Set(a)
	for k := int64(1); k < 1000; k++ {
		term.Mul(term, a2)
		t := newF().Quo(term, bigInt(2*k+1))
		if t.Sign() == 0 || t.MantExp(nil) < sum.MantExp(nil)-prec-8 {
			break
		}
		if k%2 == 1 {
			sum.Sub(sum, t)
		} else {
			sum.Add(sum, t)
		}
	}
	sum.SetMantExp(sum, halvings)
	if invert {
		hp := newF().Set(bigPi)
		hp.SetMantExp(hp, -1)
		sum.Sub(hp, sum)
	}
	if neg {
		sum.Neg(sum)
	}
	return sum
}

// libmAtan is atan(x).
func libmAtan(x float64) float64 {
	switch {
	case math.IsNaN(x) || x == 0:
		return x
	case math.IsInf(x, 1):
		return math.Pi / 2
	case math.IsInf(x, -1):
		return -math.Pi / 2
	}
	return round(atanBig(bigOf(x)))
}

// libmAtan2 is atan2(y, x) with C's special cases.
func libmAtan2(y, x float64) float64 {
	consts()
	switch {
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case y == 0:
		if x > 0 || x == 0 && !math.Signbit(x) {
			return math.Copysign(0, y)
		}
		return math.Copysign(math.Pi, y)
	case x == 0:
		return math.Copysign(math.Pi/2, y)
	case math.IsInf(x, 0):
		if math.IsInf(y, 0) {
			if x > 0 {
				return math.Copysign(math.Pi/4, y)
			}
			return math.Copysign(3*math.Pi/4, y)
		}
		if x > 0 {
			return math.Copysign(0, y)
		}
		return math.Copysign(math.Pi, y)
	case math.IsInf(y, 0):
		return math.Copysign(math.Pi/2, y)
	}
	q := atanBig(newF().Quo(bigOf(y), bigOf(x)))
	if x < 0 {
		p := newF().Set(bigPi)
		if y < 0 {
			q.Sub(q, p)
		} else {
			q.Add(q, p)
		}
	}
	return round(q)
}

// libmAsin is asin(x).
func libmAsin(x float64) float64 {
	switch {
	case math.IsNaN(x) || x == 0:
		return x
	case math.Abs(x) > 1:
		return math.NaN()
	case math.Abs(x) == 1:
		return math.Copysign(math.Pi/2, x)
	}
	b := bigOf(x)
	d := newF().Mul(b, b)
	d.Sub(bigInt(1), d)
	d.Sqrt(d)
	return round(atanBig(newF().Quo(b, d)))
}

// libmAcos is acos(x).
func libmAcos(x float64) float64 {
	switch {
	case math.IsNaN(x):
		return x
	case math.Abs(x) > 1:
		return math.NaN()
	case x == 1:
		return 0
	case x == -1:
		return math.Pi
	}
	consts()
	b := bigOf(x)
	d := newF().Mul(b, b)
	d.Sub(bigInt(1), d)
	d.Sqrt(d)
	// acos(x) = π/2 - atan(x / √(1-x²))
	hp := newF().Set(bigPi)
	hp.SetMantExp(hp, -1)
	return round(hp.Sub(hp, atanBig(newF().Quo(b, d))))
}

// libmCbrt is cbrt(x).
func libmCbrt(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || x == 0 {
		return x
	}
	a := bigOf(math.Abs(x))
	// Newton's iteration from Go's estimate, which is within an ulp.
	y := bigOf(math.Cbrt(math.Abs(x)))
	three := bigInt(3)
	for i := 0; i < 8; i++ {
		y2 := newF().Mul(y, y)
		t := newF().Quo(a, y2)
		t.Add(t, newF().Mul(y, bigInt(2)))
		y = t.Quo(t, three)
	}
	return math.Copysign(round(y), x)
}

// libmHypot is the C library's hypot(x, y): Borges's corrected algorithm with
// a fused multiply-add (glibc's e_hypot.c on a machine with fma), which is
// not always correctly rounded.
func libmHypot(x, y float64) float64 {
	if math.IsInf(x, 0) || math.IsInf(y, 0) {
		return math.Inf(1)
	}
	if math.IsNaN(x) || math.IsNaN(y) {
		return x + y
	}
	const (
		scale    = 0x1p-600
		largeVal = 0x1p+511
		tinyVal  = 0x1p-511
		eps      = 0x1p-54
	)
	x, y = math.Abs(x), math.Abs(y)
	ax, ay := math.Max(x, y), math.Min(x, y)
	switch {
	case ax > largeVal:
		if ay <= float64(ax*eps) {
			return ax + ay
		}
		return hypotKernel(ax*scale, ay*scale) / scale
	case ay < tinyVal:
		if ax >= ay/eps {
			return ax + ay
		}
		return hypotKernel(ax/scale, ay/scale) * scale
	case ay <= float64(ax*eps):
		return ax + ay
	}
	return hypotKernel(ax, ay)
}

// hypotKernel is hypot for ax >= ay >= 0 whose squares neither overflow
// nor underflow.
func hypotKernel(ax, ay float64) float64 {
	t1 := ay + ay
	t2 := ax - ay
	if t1 >= ax {
		return math.Sqrt(math.FMA(t1, ax, float64(t2*t2)))
	}
	return math.Sqrt(math.FMA(ax, ax, float64(ay*ay)))
}

// erfBig is erf(x) for |x| < 6 by its Taylor series, which converges
// for all x; the terms grow before they shrink, so the precision is raised.
func erfBig(x float64) *big.Float {
	consts()
	const p = prec + 200
	b := new(big.Float).SetPrec(p).SetFloat64(x)
	b2 := new(big.Float).SetPrec(p).Mul(b, b)
	sum := new(big.Float).SetPrec(p).Set(b)
	term := new(big.Float).SetPrec(p).Set(b)
	for n := int64(1); n < 2000; n++ {
		term.Mul(term, b2)
		term.Quo(term, new(big.Float).SetPrec(p).SetInt64(n))
		term.Neg(term)
		t := new(big.Float).SetPrec(p).Quo(term, new(big.Float).SetPrec(p).SetInt64(2*n+1))
		sum.Add(sum, t)
		if t.Sign() == 0 || t.MantExp(nil) < sum.MantExp(nil)-p-8 {
			break
		}
	}
	// erf(x) = 2/√π · sum
	sp := new(big.Float).SetPrec(p).Sqrt(new(big.Float).SetPrec(p).Set(bigPi))
	sum.Quo(sum, sp)
	return sum.SetMantExp(sum, 1)
}

// libmErf is erf(x).
func libmErf(x float64) float64 {
	switch {
	case math.IsNaN(x) || x == 0:
		return x
	case math.Abs(x) >= 6:
		return math.Copysign(1, x)
	}
	return round(erfBig(x))
}

// libmErfc is erfc(x) = 1 - erf(x); for large x the continued fraction of the
// complementary function keeps the relative precision.
func libmErfc(x float64) float64 {
	switch {
	case math.IsNaN(x):
		return x
	case math.IsInf(x, 1):
		return 0
	case math.IsInf(x, -1):
		return 2
	case x < -6:
		return 2
	case x > 27.3:
		return 0
	}
	if x < 2 {
		e := erfBig(x)
		return round(newF().Sub(bigInt(1), e))
	}
	// erfc(x) = exp(-x²)/√π · 1/(x + 1/2/(x + 1/(x + 3/2/(x + ...)))).
	consts()
	b := bigOf(x)
	frac := newF()
	for n := int64(400); n >= 1; n-- {
		t := newF().Add(b, frac)
		frac.Quo(newF().Quo(bigInt(n), bigInt(2)), t)
	}
	denom := newF().Add(b, frac)
	e := expBig(newF().Neg(newF().Mul(b, b)))
	e.Quo(e, denom)
	e.Quo(e, newF().Sqrt(newF().Set(bigPi)))
	return round(e)
}
