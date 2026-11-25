// Ported from fdlibm 5.3 as glibc 2.36 has it (sysdeps/ieee754/dbl-64/e_log10.c, s_expm1.c,
// s_log1p.c, e_sinh.c, e_cosh.c, s_tanh.c, s_asinh.c, e_acosh.c; e_atanh.c of fdlibm) to Go,
// 2026-11-25, with the fused multiply-adds the C compiler makes of them for 64-bit ARM: a product
// feeding only a sum in the same block is fused into it, the first one of a sum of two.
// Copyright (C) 1993 by Sun Microsystems, Inc. All rights reserved.
// Developed at SunPro, a Sun Microsystems, Inc. business.
// Permission to use, copy, modify, and distribute this
// software is freely granted, provided that this notice
// is preserved.

package py

import "math"

// libmLog10 is log10(x): with n = ilogb(x) (n+1 when negative) and
// x = 2^n·m, n·log10_2hi + (n·log10_2lo + log(m)/ln 10).
func libmLog10(x float64) float64 {
	const (
		two54    = 0x1p54
		ivln10   = 4.34294481903251816668e-01
		log102hi = 3.01029995663611771306e-01
		log102lo = 3.69423907715893078616e-13
	)
	hx := int64(asuint64(x))
	k := int64(0)
	if hx < 0x0010000000000000 {
		switch {
		case hx&0x7fffffffffffffff == 0:
			return math.Inf(-1)
		case hx < 0:
			return math.NaN()
		}
		// A subnormal x is scaled up.
		k -= 54
		x *= two54
		hx = int64(asuint64(x))
	}
	if hx >= 0x7ff0000000000000 {
		return x + x
	}
	k += hx>>52 - 1023
	i := int64(uint64(k) >> 63)
	hx = hx&0x000fffffffffffff | (0x3ff-i)<<52
	y := float64(k + i)
	z := math.FMA(y, log102lo, float64(ivln10*libmLog(asdouble(uint64(hx)))))
	return math.FMA(y, log102hi, z)
}

// highWord is the high 32 bits of x.
func highWord(x float64) uint32 { return uint32(asuint64(x) >> 32) }

// setHighWord is x with its high 32 bits replaced by hi.
func setHighWord(x float64, hi uint32) float64 {
	return asdouble(uint64(hi)<<32 | asuint64(x)&0xffffffff)
}

// libmExpm1 is expm1(x), exp(x) - 1: with x = k·ln2 + r, from a rational
// approximation of r(e^r + 1)/(e^r - 1) on [0, 0.34658].
func libmExpm1(x float64) float64 {
	const (
		huge       = 1.0e+300
		tiny       = 1.0e-300
		oThreshold = 7.09782712893383973096e+02
		ln2Hi      = 6.93147180369123816490e-01
		ln2Lo      = 1.90821492927058770002e-10
		invln2     = 1.44269504088896338700e+00
		q1         = -3.33333333333331316428e-02
		q2         = 1.58730158725481460165e-03
		q3         = -7.93650757867487942473e-05
		q4         = 4.00821782732936239552e-06
		q5         = -2.01099218183624371326e-07
	)
	hx := highWord(x)
	xsb := hx & 0x80000000
	hx &= 0x7fffffff
	if hx >= 0x4043687A {
		if hx >= 0x40862E42 {
			if hx >= 0x7ff00000 {
				if math.IsNaN(x) {
					return x + x
				}
				if xsb == 0 {
					return x
				}
				return -1.0
			}
			if x > oThreshold {
				return math.Inf(1)
			}
		}
		if xsb != 0 {
			return tiny - 1
		}
	}
	var hi, lo, c float64
	var k int32
	if hx > 0x3fd62e42 {
		if hx < 0x3FF0A2B2 {
			if xsb == 0 {
				hi, lo, k = x-ln2Hi, ln2Lo, 1
			} else {
				hi, lo, k = x+ln2Hi, -ln2Lo, -1
			}
			x = hi - lo
			c = (hi - x) - lo
		} else {
			half := 0.5
			if xsb != 0 {
				half = -0.5
			}
			k = int32(math.FMA(invln2, x, half))
			t := float64(k)
			hi = math.FMA(-t, ln2Hi, x)
			x = math.FMA(-t, ln2Lo, hi)
			c = math.FMA(-t, ln2Lo, hi-x)
		}
	} else if hx < 0x3c900000 {
		return x
	} else {
		k = 0
	}
	hfx := float64(0.5 * x)
	hxs := float64(x * hfx)
	r1 := math.FMA(hxs, q1, 1)
	h2 := float64(hxs * hxs)
	r2 := math.FMA(hxs, q3, q2)
	h4 := float64(h2 * h2)
	r3 := math.FMA(hxs, q5, q4)
	rr := math.FMA(h4, r3, math.FMA(h2, r2, r1))
	t := math.FMA(-rr, hfx, 3.0)
	e := float64(hxs * ((rr - t) / math.FMA(-x, t, 6.0)))
	if k == 0 {
		return x - math.FMA(x, e, -hxs)
	}
	e = math.FMA(x, e-c, -c)
	e -= hxs
	switch {
	case k == -1:
		return math.FMA(0.5, x-e, -0.5)
	case k == 1:
		if x < -0.25 {
			return -2.0 * (e - (x + 0.5))
		}
		return math.FMA(2.0, x-e, 1)
	case k <= -2 || k > 56:
		y := 1 - (e - x)
		y = setHighWord(y, highWord(y)+uint32(k)<<20)
		return y - 1
	}
	var y float64
	if k < 20 {
		t := setHighWord(0, 0x3ff00000-(0x200000>>k))
		y = t - (e - x)
	} else {
		t := setHighWord(0, uint32(0x3ff-k)<<20)
		y = x - (e + t)
		y += 1
	}
	return setHighWord(y, highWord(y)+uint32(k)<<20)
}

// libmLog1p is log1p(x), log(1 + x): with 1 + x = 2^k(1 + f), k·ln2 plus
// log(1 + f) from a polynomial in s = f/(2 + f), and a correction for the
// rounding of 1 + x.
func libmLog1p(x float64) float64 {
	const (
		ln2Hi = 6.93147180369123816490e-01
		ln2Lo = 1.90821492927058770002e-10
		lp1   = 6.666666666666735130e-01
		lp2   = 3.999999999940941908e-01
		lp3   = 2.857142874366239149e-01
		lp4   = 2.222219843214978396e-01
		lp5   = 1.818357216161805012e-01
		lp6   = 1.531383769920937332e-01
		lp7   = 1.479819860511658591e-01
	)
	hx := int32(highWord(x))
	ax := hx & 0x7fffffff
	k := int32(1)
	var f, c float64
	var hu int32
	if hx < 0x3FDA827A {
		if ax >= 0x3ff00000 {
			if x == -1.0 {
				return math.Inf(-1)
			}
			return math.NaN()
		}
		if ax < 0x3e200000 {
			if ax < 0x3c900000 {
				return x
			}
			return math.FMA(-float64(x*x), 0.5, x)
		}
		if hx > 0 || hx <= int32(-0x402d413d) {
			k, f, hu = 0, x, 1
		}
	} else if hx >= 0x7ff00000 {
		return x + x
	}
	if k != 0 {
		var u float64
		if hx < 0x43400000 {
			u = 1.0 + x
			hu = int32(highWord(u))
			k = hu>>20 - 1023
			if k > 0 {
				c = 1.0 - (u - x)
			} else {
				c = x - (u - 1.0)
			}
			c /= u
		} else {
			u = x
			hu = int32(highWord(u))
			k = hu>>20 - 1023
			c = 0
		}
		hu &= 0x000fffff
		if hu < 0x6a09e {
			u = setHighWord(u, uint32(hu|0x3ff00000))
		} else {
			k++
			u = setHighWord(u, uint32(hu|0x3fe00000))
			hu = (0x00100000 - hu) >> 2
		}
		f = u - 1.0
	}
	kd := float64(k)
	hfsq := float64(float64(0.5*f) * f)
	if hu == 0 {
		if f == 0 {
			if k == 0 {
				return 0
			}
			c = math.FMA(kd, ln2Lo, c)
			return math.FMA(kd, ln2Hi, c)
		}
		r := float64(hfsq * math.FMA(-0.66666666666666666, f, 1.0))
		if k == 0 {
			return f - r
		}
		return math.FMA(kd, ln2Hi, -((r - math.FMA(kd, ln2Lo, c)) - f))
	}
	s := f / (2.0 + f)
	z := float64(s * s)
	z2 := float64(z * z)
	r2 := math.FMA(z, lp3, lp2)
	z4 := float64(z2 * z2)
	r3 := math.FMA(z, lp5, lp4)
	z6 := float64(z4 * z2)
	r4 := math.FMA(z, lp7, lp6)
	r := math.FMA(z6, r4, math.FMA(z4, r3, math.FMA(z, lp1, float64(z2*r2))))
	m := float64(s * (hfsq + r))
	if k == 0 {
		return f - (hfsq - m)
	}
	return math.FMA(kd, ln2Hi, -((hfsq - (m + math.FMA(kd, ln2Lo, c))) - f))
}

// libmSinh is sinh(x), from expm1 below 22 and exp above.
func libmSinh(x float64) float64 {
	jx := int32(highWord(x))
	ix := jx & 0x7fffffff
	if ix >= 0x7ff00000 {
		return x + x
	}
	h := 0.5
	if jx < 0 {
		h = -h
	}
	if ix < 0x40360000 {
		if ix < 0x3e300000 {
			return x
		}
		t := libmExpm1(math.Abs(x))
		if ix < 0x3ff00000 {
			return h * (2.0*t - float64(t*t)/(t+1))
		}
		return h * (t + t/(t+1))
	}
	if ix < 0x40862e42 {
		return h * libmExp(math.Abs(x))
	}
	lx := uint32(asuint64(x))
	if ix < 0x408633ce || ix == 0x408633ce && lx <= 0x8fb9f87d {
		w := libmExp(0.5 * math.Abs(x))
		t := float64(h * w)
		return t * w
	}
	return x * math.Inf(1)
}

// libmCosh is cosh(x), from expm1 near 0 and exp elsewhere.
func libmCosh(x float64) float64 {
	ix := int32(highWord(x)) & 0x7fffffff
	if ix < 0x40360000 {
		if ix < 0x3fd62e43 {
			if ix < 0x3c800000 {
				return 1
			}
			t := libmExpm1(math.Abs(x))
			w := 1 + t
			return 1 + float64(t*t)/(w+w)
		}
		t := libmExp(math.Abs(x))
		return 0.5*t + 0.5/t
	}
	if ix < 0x40862e42 {
		return 0.5 * libmExp(math.Abs(x))
	}
	if asuint64(x)&0x7fffffffffffffff <= 0x408633ce8fb9f87d {
		w := libmExp(0.5 * math.Abs(x))
		t := float64(0.5 * w)
		return t * w
	}
	if ix >= 0x7ff00000 {
		return x * x
	}
	return math.Inf(1)
}

// libmTanh is tanh(x), from expm1.
func libmTanh(x float64) float64 {
	jx := int32(highWord(x))
	ix := jx & 0x7fffffff
	if ix >= 0x7ff00000 {
		if jx >= 0 {
			return 1/x + 1
		}
		return 1/x - 1
	}
	var z float64
	if ix < 0x40360000 {
		if x == 0 {
			return x
		}
		if ix < 0x3c800000 {
			return x * (1 + x)
		}
		if ix >= 0x3ff00000 {
			t := libmExpm1(2 * math.Abs(x))
			z = 1 - 2/(t+2)
		} else {
			t := libmExpm1(-2 * math.Abs(x))
			z = -t / (t + 2)
		}
	} else {
		z = 1
	}
	if jx >= 0 {
		return z
	}
	return -z
}

// libmAsinh is asinh(x), from log and log1p.
func libmAsinh(x float64) float64 {
	const ln2 = 6.93147180559945286227e-01
	ix := int32(highWord(x)) & 0x7fffffff
	if ix < 0x3e300000 {
		return x
	}
	var w float64
	xa := math.Abs(x)
	switch {
	case ix > 0x41b00000:
		if ix >= 0x7ff00000 {
			return x + x
		}
		w = libmLog(xa) + ln2
	case ix > 0x40000000:
		w = libmLog(math.FMA(xa, 2.0, 1/(math.Sqrt(float64(x*x)+1)+xa)))
	default:
		t := float64(x * x)
		w = libmLog1p(xa + t/(1+math.Sqrt(1+t)))
	}
	return math.Copysign(w, x)
}

// libmAcosh is acosh(x), from log and log1p.
func libmAcosh(x float64) float64 {
	const ln2 = 6.93147180559945286227e-01
	hx := int64(asuint64(x))
	switch {
	case hx > 0x4000000000000000:
		if hx >= 0x41b0000000000000 {
			if hx >= 0x7ff0000000000000 {
				return x + x
			}
			return libmLog(x) + ln2
		}
		return libmLog(math.FMA(x, 2.0, -(1 / (x + math.Sqrt(math.FMA(x, x, -1))))))
	case hx > 0x3ff0000000000000:
		t := x - 1
		return libmLog1p(t + math.Sqrt(math.FMA(t, 2.0, float64(t*t))))
	case hx == 0x3ff0000000000000:
		return 0
	}
	return math.NaN()
}

// libmAtanh is atanh(x), 0.5·log1p(2x + 2x²/(1 - x)) for |x| < 0.5 and
// 0.5·log1p(2x/(1 - x)) above.
func libmAtanh(x float64) float64 {
	xa := math.Abs(x)
	var t float64
	switch {
	case xa < 0.5:
		if xa < 0x1p-28 {
			return x
		}
		t = xa + xa
		t = 0.5 * libmLog1p(t+float64(t*xa)/(1-xa))
	case xa < 1:
		t = 0.5 * libmLog1p((xa+xa)/(1-xa))
	case xa > 1 || math.IsNaN(xa):
		return math.NaN()
	default:
		return x / 0
	}
	return math.Copysign(t, x)
}
