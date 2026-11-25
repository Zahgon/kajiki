// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Hashing: Python's hash() for the values that have one, and the canonical keys dicts use to find
// equal keys.

package py

import (
	"math"
	"math/big"
	"math/bits"
)

// hashModulus is CPython's _PyHASH_MODULUS, 2**61 - 1.
var hashModulus = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 61), big.NewInt(1))

const (
	hashBits = 61
	hashInf  = 314159
	hashImag = 1000003
)

// pyHash is Python's hash(): the documented numeric hash for numbers, the
// tuple hash of CPython 3.12 for tuples, and an arbitrary but stable hash for
// str and bytes (CPython's depends on PYTHONHASHSEED). It fails with
// TypeError for an unhashable value.
func pyHash(v Value) (int64, error) {
	if h, ok, err := userHash(v); ok {
		return h, err
	}
	v = unsub(v)
	switch x := v.(type) {
	case noneV:
		return 0xFCA86420, nil
	case boolV:
		if x {
			return 1, nil
		}
		return 0, nil
	case intV:
		return intHash(x.v), nil
	case floatV:
		return floatHash(float64(x), v), nil
	case complexV:
		h := uint64(floatHash(real(x), v)) + hashImag*uint64(floatHash(imag(x), v))
		if int64(h) == -1 {
			return -2, nil
		}
		return int64(h), nil
	case strV:
		return strHash(string(x)), nil
	case bytesV:
		return hashString(string(x)), nil
	case *tupleV:
		return tupleHash(x.items)
	case *setV:
		if !x.frozen {
			return 0, unhashable(v)
		}
		return frozensetHash(x), nil
	case *listV, *dictV, *bytearrayV:
		return 0, unhashable(v)
	case *sliceV:
		return sliceHash(x)
	case rangeV:
		return tupleHash([]Value{mkInt(x.length()), mkInt(x.start), mkInt(x.step)})
	case *methodV:
		h, err := pyHash(x.fn)
		if err != nil {
			return 0, err
		}
		return hashNotMinusOne(int64(objID(x.self)>>4) ^ h), nil
	case *callable:
		if x.self != nil && x.typ != "method" {
			return hashNotMinusOne(int64(objID(x.self)>>4) ^ strHash(x.qual+" "+x.name+" "+x.typ)), nil
		}
	}
	return int64(objID(v) >> 4), nil
}

func unhashable(v Value) error {
	return newPyErr("TypeError", "unhashable type: '"+v.pyType()+"'")
}

// hashString is _Py_HashBytes of the bytes s holds: SipHash-1-3 with
// the all-zero key CPython uses when PYTHONHASHSEED is 0 (the hash of
// nothing is 0).
func hashString(s string) int64 {
	if len(s) == 0 {
		return 0
	}
	r := int64(siphash13(s))
	if r == -1 {
		r = -2
	}
	return r
}

// strHash is the hash of a str: hashString of its code points stored as
// CPython stores them, one, two or four bytes each by the widest one.
func strHash(s string) int64 {
	if isASCII(s) {
		// One byte per character: the UTF-8 bytes are the stored ones.
		return hashString(s)
	}
	rs := strRunes(s)
	width := 1
	for _, r := range rs {
		if r > 0xFFFF {
			width = 4
			break
		}
		if r > 0xFF {
			width = 2
		}
	}
	b := make([]byte, 0, len(rs)*width)
	for _, r := range rs {
		switch width {
		case 1:
			b = append(b, byte(r))
		case 2:
			b = append(b, byte(r), byte(r>>8))
		default:
			b = append(b, byte(r), byte(r>>8), byte(r>>16), byte(r>>24))
		}
	}
	return hashString(string(b))
}

// siphash13 is CPython's siphash13 with k0 = k1 = 0.
func siphash13(src string) uint64 {
	v0 := uint64(0x736f6d6570736575)
	v1 := uint64(0x646f72616e646f6d)
	v2 := uint64(0x6c7967656e657261)
	v3 := uint64(0x7465646279746573)
	round := func() {
		v0 += v1
		v2 += v3
		v1 = bits.RotateLeft64(v1, 13) ^ v0
		v3 = bits.RotateLeft64(v3, 16) ^ v2
		v0 = bits.RotateLeft64(v0, 32)
		v2 += v1
		v0 += v3
		v1 = bits.RotateLeft64(v1, 17) ^ v2
		v3 = bits.RotateLeft64(v3, 21) ^ v0
		v2 = bits.RotateLeft64(v2, 32)
	}
	b := uint64(len(src)) << 56
	for len(src) >= 8 {
		mi := uint64(src[0]) | uint64(src[1])<<8 | uint64(src[2])<<16 | uint64(src[3])<<24 |
			uint64(src[4])<<32 | uint64(src[5])<<40 | uint64(src[6])<<48 | uint64(src[7])<<56
		src = src[8:]
		v3 ^= mi
		round()
		v0 ^= mi
	}
	var t uint64
	for i := len(src) - 1; i >= 0; i-- {
		t = t<<8 | uint64(src[i])
	}
	b |= t
	v3 ^= b
	round()
	v0 ^= b
	v2 ^= 0xff
	round()
	round()
	round()
	return (v0 ^ v1) ^ (v2 ^ v3)
}

// intHash is the numeric hash of an int: n mod 2**61-1 with n's sign.
func intHash(n *big.Int) int64 {
	var r int64
	if n.IsInt64() {
		a := n.Int64()
		neg := a < 0
		u := uint64(a)
		if neg {
			u = uint64(-a)
		}
		r = int64(u % (1<<61 - 1))
		if neg {
			r = -r
		}
	} else {
		m := new(big.Int).Rem(new(big.Int).Abs(n), hashModulus)
		r = m.Int64()
		if n.Sign() < 0 {
			r = -r
		}
	}
	if r == -1 {
		r = -2
	}
	return r
}

// floatHash is the numeric hash of a float, as documented for Python's
// numeric types. A NaN hashes by identity.
func floatHash(f float64, obj Value) int64 {
	if math.IsInf(f, 0) {
		if f > 0 {
			return hashInf
		}
		return -hashInf
	}
	if math.IsNaN(f) {
		return int64(objID(obj) >> 4)
	}
	m, e := math.Frexp(f)
	sign := int64(1)
	if m < 0 {
		sign = -1
		m = -m
	}
	var x uint64
	for m != 0 {
		x = ((x << 28) & (1<<61 - 1)) | x>>(hashBits-28)
		m *= 268435456.0
		e -= 28
		y := uint64(m)
		m -= float64(y)
		x += y
		if x >= 1<<61-1 {
			x -= 1<<61 - 1
		}
	}
	if e >= 0 {
		e = e % hashBits
	} else {
		e = hashBits - 1 - ((-1 - e) % hashBits)
	}
	x = ((x << uint(e)) & (1<<61 - 1)) | x>>uint(hashBits-e)
	r := int64(x) * sign
	if r == -1 {
		r = -2
	}
	return r
}

// tupleHash is CPython 3.12's tuple hash (an xxHash-based mix).
func tupleHash(items []Value) (int64, error) {
	return xxHashItems(items, true)
}

// sliceHash is slice_hash: the tuple hash of (start, stop, step) without
// the length folded in.
func sliceHash(s *sliceV) (int64, error) {
	return xxHashItems([]Value{s.start, s.stop, s.step}, false)
}

// xxHashItems mixes the hashes of items as tuplehash does; withLen adds the
// item count at the end, which tuples do and slices do not.
func xxHashItems(items []Value, withLen bool) (int64, error) {
	const (
		prime1 uint64 = 11400714785074694791
		prime2 uint64 = 14029467366897019727
		prime5 uint64 = 2870177450012600261
	)
	acc := prime5
	for _, it := range items {
		lane, err := pyHash(it)
		if err != nil {
			return 0, err
		}
		acc += uint64(lane) * prime2
		acc = bits.RotateLeft64(acc, 31)
		acc *= prime1
	}
	if withLen {
		acc += uint64(len(items)) ^ (prime5 ^ 3527539)
	}
	if int64(acc) == -1 {
		return 1546275796, nil
	}
	return int64(acc), nil
}

// frozensetHash is CPython 3.12's frozenset hash.
func frozensetHash(s *setV) int64 {
	var hash uint64
	shuffle := func(h uint64) uint64 { return ((h ^ 89869747) ^ (h << 16)) * 3644798167 }
	for _, e := range s.table {
		if e.live() {
			hash ^= shuffle(uint64(e.hash))
		}
	}
	if s.used&1 != 0 {
		hash ^= shuffle(^uint64(0))
	}
	hash ^= (uint64(s.used) + 1) * 1927868237
	hash ^= (hash >> 11) ^ (hash >> 25)
	hash = hash*69069 + 907133923
	if int64(hash) == -1 {
		hash = 590923713
	}
	return int64(hash)
}

// hashNotMinusOne is h, with -1 (which CPython reserves for errors) made -2.
func hashNotMinusOne(h int64) int64 {
	if h == -1 {
		return -2
	}
	return h
}
