// Ported from ARM optimized-routines (math/exp.c, exp2.c, log.c, log2.c, pow.c) to Go, 2026-11-25,
// as glibc 2.36 builds them for 64-bit ARM: with a fused multiply-add for r = z*invc - 1 and the
// other fma paths, and with each product that only feeds a sum fused into it, as the C
// compiler contracts them. These are the exp, exp2, log, log2 and pow CPython's math module
// and float ** call there.
// Copyright (c) 2018, Arm Limited.
// SPDX-License-Identifier: MIT

package py

import "math"

func asuint64(x float64) uint64 { return math.Float64bits(x) }

func asdouble(u uint64) float64 { return math.Float64frombits(u) }

// top12 is the sign and exponent bits of x.
func top12(x float64) uint32 { return uint32(asuint64(x) >> 52) }

// expScaled is scale·(1 + tmp) for the scale whose bits are sbits, when the
// k of the reduction (the low 32 bits of ki) may make it overflow or fall
// in the subnormal range: shift is 1009 for exp and pow, 1 for exp2.
func expScaled(tmp float64, sbits, ki uint64, shift uint) float64 {
	if ki&0x80000000 == 0 {
		sbits -= uint64(shift) << 52
		scale := asdouble(sbits)
		return math.Ldexp(math.FMA(scale, tmp, scale), int(shift))
	}
	sbits += 1022 << 52
	scale := asdouble(sbits)
	y := math.FMA(scale, tmp, scale)
	if math.Abs(y) < 1.0 {
		// Round y to double precision before scaling it into the subnormal
		// range, to avoid double rounding.
		one := 1.0
		if y < 0 {
			one = -1.0
		}
		lo := math.FMA(scale, tmp, scale-y)
		hi := one + y
		lo = one - hi + y + lo
		y = float64(hi+lo) - one
		if y == 0 {
			y = asdouble(sbits & 0x8000000000000000)
		}
	}
	return 0x1p-1022 * y
}

// expInline is sign·exp(x + xtail), |xtail| < 2^-8/N, the sign negative
// when signBias is set.
func expInline(x, xtail float64, signBias uint64) float64 {
	abstop := top12(x) & 0x7ff
	if abstop-top12(0x1p-54) >= top12(512.0)-top12(0x1p-54) {
		if abstop-top12(0x1p-54) >= 0x80000000 {
			// Tiny x: exp(x) is 1 + x, which avoids a spurious underflow.
			one := 1.0 + x
			if signBias != 0 {
				return -one
			}
			return one
		}
		if abstop >= top12(1024.0) {
			r := math.Inf(1)
			if asuint64(x)>>63 != 0 {
				r = 0
			}
			if signBias != 0 {
				return -r
			}
			return r
		}
		// Large |x| is handled by expScaled.
		abstop = 0
	}
	// exp(x) = 2^(k/N) · exp(r) with x = k·ln2/N + r, |r| ≤ ln2/2N.
	z := float64(expInvLn2N * x)
	kd := math.Round(z)
	ki := uint64(int64(kd))
	r := math.FMA(kd, expNegLn2loN, math.FMA(kd, expNegLn2hiN, x))
	r += xtail
	idx := 2 * (ki % 128)
	top := (ki + signBias) << (52 - 7)
	tail := asdouble(expTab[idx])
	sbits := expTab[idx+1] + top
	r2 := float64(r * r)
	tmp := math.FMA(float64(r2*r2), math.FMA(r, expPoly[3], expPoly[2]),
		math.FMA(r2, math.FMA(r, expPoly[1], expPoly[0]), tail+r))
	if abstop == 0 {
		return expScaled(tmp, sbits, ki, 1009)
	}
	scale := asdouble(sbits)
	return math.FMA(scale, tmp, scale)
}

// libmExp is exp(x).
func libmExp(x float64) float64 {
	if x == math.Inf(-1) {
		return 0
	}
	if math.IsNaN(x) || math.IsInf(x, 1) {
		return x
	}
	return expInline(x, 0, 0)
}

// libmExp2 is exp2(x).
func libmExp2(x float64) float64 {
	abstop := top12(x) & 0x7ff
	if abstop-top12(0x1p-54) >= top12(512.0)-top12(0x1p-54) {
		if abstop-top12(0x1p-54) >= 0x80000000 {
			return 1.0 + x
		}
		if abstop >= top12(1024.0) {
			switch {
			case x == math.Inf(-1):
				return 0
			case abstop >= top12(math.Inf(1)):
				return 1.0 + x
			case asuint64(x)>>63 == 0:
				return math.Inf(1)
			case asuint64(x) >= asuint64(-1075.0):
				return 0
			}
		}
		if 2*asuint64(x) > 2*asuint64(928.0) {
			abstop = 0
		}
	}
	// exp2(x) = 2^(k/N) · 2^r with x = k/N + r, |r| ≤ 1/2N.
	const shift = 0x1.8p52 / 128
	kd := float64(x + shift)
	ki := asuint64(kd)
	kd -= shift
	r := x - kd
	idx := 2 * (ki % 128)
	top := ki << (52 - 7)
	tail := asdouble(expTab[idx])
	sbits := expTab[idx+1] + top
	r2 := float64(r * r)
	tmp := math.FMA(float64(r2*r2), math.FMA(r, exp2Poly[4], exp2Poly[3]),
		math.FMA(r2, math.FMA(r, exp2Poly[2], exp2Poly[1]), math.FMA(r, exp2Poly[0], tail)))
	if abstop == 0 {
		return expScaled(tmp, sbits, ki, 1)
	}
	scale := asdouble(sbits)
	return math.FMA(scale, tmp, scale)
}

// logSpecial normalizes the bits ix of x for log and log2 (top is their
// top 16 bits), or gives the result for 0, negative numbers, inf and nan.
func logSpecial(x float64, ix uint64, top uint32) (uint64, float64, bool) {
	if top-0x0010 < 0x7ff0-0x0010 {
		return ix, 0, false
	}
	switch {
	case ix*2 == 0:
		return 0, math.Inf(-1), true
	case x == math.Inf(1):
		return 0, x, true
	case top&0x8000 != 0 || top&0x7ff0 == 0x7ff0:
		return 0, math.NaN(), true
	}
	// A subnormal x is normalized.
	return asuint64(x*0x1p52) - 52<<52, 0, false
}

// libmLog is log(x), the natural logarithm.
func libmLog(x float64) float64 {
	ix := asuint64(x)
	top := uint32(ix >> 48)
	const lo1, hi1 = 0x3fee000000000000, 0x3ff1090000000000
	if ix-lo1 < hi1-lo1 {
		// x close to 1: a polynomial in r = x - 1.
		if ix == asuint64(1.0) {
			return 0
		}
		B := &logPoly1
		r := x - 1.0
		r2 := float64(r * r)
		r3 := float64(r * r2)
		p3 := math.FMA(r3, B[10], math.FMA(r2, B[9], math.FMA(r, B[8], B[7])))
		p2 := math.FMA(r3, p3, math.FMA(r2, B[6], math.FMA(r, B[5], B[4])))
		p1 := math.FMA(r3, p2, math.FMA(r2, B[3], math.FMA(r, B[2], B[1])))
		w := float64(r * 0x1p27)
		rhi := r + w - w
		rlo := r - rhi
		w = float64(float64(rhi*rhi) * B[0])
		hi := r + w
		lo := r - hi + w
		lo = math.FMA(float64(B[0]*rlo), rhi+r, lo)
		return math.FMA(r3, p1, lo) + hi
	}
	ix, special, done := logSpecial(x, ix, top)
	if done {
		return special
	}
	// x = 2^k z with z in [OFF, 2·OFF), split into N subintervals, c near the
	// center of the one holding z: log(x) = log1p(z/c - 1) + log(c) + k·ln2.
	const off = 0x3fe6000000000000
	tmp := ix - off
	i := (tmp >> (52 - 7)) % 128
	k := int64(tmp) >> 52
	iz := ix - tmp&(0xfff<<52)
	invc, logc := logTab[i][0], logTab[i][1]
	r := math.FMA(asdouble(iz), invc, -1.0)
	kd := float64(k)
	w := math.FMA(kd, logLn2hi, logc)
	hi := w + r
	lo := math.FMA(kd, logLn2lo, w-hi+r)
	A := &logPoly
	r2 := float64(r * r)
	p := math.FMA(r2, math.FMA(r, A[4], A[3]), math.FMA(r, A[2], A[1]))
	return math.FMA(float64(r*r2), p, math.FMA(r2, A[0], lo)) + hi
}

// libmLog2 is log2(x).
func libmLog2(x float64) float64 {
	ix := asuint64(x)
	top := uint32(ix >> 48)
	lo1, hi1 := asuint64(1.0-0x1.5b51p-5), asuint64(1.0+0x1.6ab2p-5)
	if ix-lo1 < hi1-lo1 {
		if ix == asuint64(1.0) {
			return 0
		}
		B := &log2Poly1
		r := x - 1.0
		hi := float64(r * log2InvLn2hi)
		lo := math.FMA(r, log2InvLn2lo, math.FMA(r, log2InvLn2hi, -hi))
		r2 := float64(r * r)
		r4 := float64(r2 * r2)
		p := math.FMA(r, B[1], B[0])
		y := math.FMA(r2, p, hi)
		lo += math.FMA(r2, p, hi-y)
		q1 := math.FMA(r2, math.FMA(r, B[5], B[4]), math.FMA(r, B[3], B[2]))
		q2 := math.FMA(r2, math.FMA(r, B[9], B[8]), math.FMA(r, B[7], B[6]))
		lo = math.FMA(r4, math.FMA(r4, q2, q1), lo)
		return y + lo
	}
	ix, special, done := logSpecial(x, ix, top)
	if done {
		return special
	}
	const off = 0x3fe6000000000000
	tmp := ix - off
	i := (tmp >> (52 - 6)) % 64
	k := int64(tmp) >> 52
	iz := ix - tmp&(0xfff<<52)
	invc, logc := log2Tab[i][0], log2Tab[i][1]
	kd := float64(k)
	r := math.FMA(asdouble(iz), invc, -1.0)
	t1 := float64(r * log2InvLn2hi)
	t2 := math.FMA(r, log2InvLn2lo, math.FMA(r, log2InvLn2hi, -t1))
	t3 := kd + logc
	hi := t3 + t1
	lo := t3 - hi + t1 + t2
	A := &log2Poly
	r2 := float64(r * r)
	r4 := float64(r2 * r2)
	p := math.FMA(r4, math.FMA(r, A[5], A[4]), math.FMA(r2, math.FMA(r, A[3], A[2]), math.FMA(r, A[1], A[0])))
	return math.FMA(r2, p, lo) + hi
}

// powLog is log(x) as hi + lo, lo carrying about 15 more bits, for the bits
// ix of a positive normal x.
func powLog(ix uint64) (hi, lo float64) {
	const off = 0x3fe6955500000000
	tmp := ix - off
	i := (tmp >> (52 - 7)) % 128
	k := int64(tmp) >> 52
	iz := ix - tmp&(0xfff<<52)
	kd := float64(k)
	invc, logc, logctail := powLogTab[i][0], powLogTab[i][1], powLogTab[i][2]
	r := math.FMA(asdouble(iz), invc, -1.0)
	t1 := math.FMA(kd, powLn2hi, logc)
	t2 := t1 + r
	lo1 := math.FMA(kd, powLn2lo, logctail)
	lo2 := t1 - t2 + r
	A := &powPoly
	ar := float64(A[0] * r)
	ar2 := float64(r * ar)
	ar3 := float64(r * ar2)
	h := t2 + ar2
	lo3 := math.FMA(ar, r, -ar2)
	lo4 := t2 - h + ar2
	p := math.FMA(ar2, math.FMA(ar2, math.FMA(r, A[6], A[5]), math.FMA(r, A[4], A[3])), math.FMA(r, A[2], A[1]))
	l := math.FMA(ar3, p, lo1+lo2+lo3+lo4)
	y := h + l
	return y, h - y + l
}

// powCheckint is 0 when the finite non-zero double with bits iy is not an
// integer, 1 when it is an odd one and 2 when it is an even one.
func powCheckint(iy uint64) int {
	e := int(iy >> 52 & 0x7ff)
	switch {
	case e < 0x3ff:
		return 0
	case e > 0x3ff+52:
		return 2
	case iy&(1<<(0x3ff+52-e)-1) != 0:
		return 0
	case iy&(1<<(0x3ff+52-e)) != 0:
		return 1
	}
	return 2
}

// zeroInfNaN reports whether the double with bits i is ±0, ±inf or nan.
func zeroInfNaN(i uint64) bool { return 2*i-1 >= 2*asuint64(math.Inf(1))-1 }

// libmPow is pow(x, y), with C's special cases.
func libmPow(x, y float64) float64 {
	var signBias uint64
	ix, iy := asuint64(x), asuint64(y)
	topx, topy := top12(x), top12(y)
	if topx-0x001 >= 0x7ff-0x001 || topy&0x7ff-0x3be >= 0x43e-0x3be {
		// x is 0, subnormal, negative, inf or nan, or |y| is tiny or huge.
		if zeroInfNaN(iy) {
			switch {
			case 2*iy == 0, ix == asuint64(1.0):
				return 1.0
			case 2*ix > 2*asuint64(math.Inf(1)) || 2*iy > 2*asuint64(math.Inf(1)):
				return x + y
			case 2*ix == 2*asuint64(1.0):
				return 1.0
			case (2*ix < 2*asuint64(1.0)) == (iy>>63 == 0):
				return 0.0
			}
			return y * y
		}
		if zeroInfNaN(ix) {
			x2 := float64(x * x)
			if ix>>63 != 0 && powCheckint(iy) == 1 {
				x2 = -x2
			}
			if iy>>63 != 0 {
				return 1 / x2
			}
			return x2
		}
		if ix>>63 != 0 {
			// A negative x needs an integer y; an odd one makes the result
			// negative.
			yint := powCheckint(iy)
			if yint == 0 {
				return math.NaN()
			}
			if yint == 1 {
				signBias = 0x800 << 7
			}
			ix &= 0x7fffffffffffffff
			topx &= 0x7ff
		}
		if topy&0x7ff-0x3be >= 0x43e-0x3be {
			switch {
			case ix == asuint64(1.0):
				return 1.0
			case topy&0x7ff < 0x3be:
				if ix > asuint64(1.0) {
					return 1.0 + y
				}
				return 1.0 - y
			case (ix > asuint64(1.0)) == (topy < 0x800):
				return math.Inf(1)
			}
			return 0
		}
		if topx == 0 {
			// A subnormal x is normalized.
			ix = asuint64(x*0x1p52)&0x7fffffffffffffff - 52<<52
		}
	}
	hi, lo := powLog(ix)
	ehi := float64(y * hi)
	elo := math.FMA(y, lo, math.FMA(y, hi, -ehi))
	return expInline(ehi, elo, signBias)
}
