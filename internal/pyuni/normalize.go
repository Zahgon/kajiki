// Port of CPython 3.12's unicodedata (Modules/unicodedata.c; PSF License Version 2;
// Copyright (c) 2001-2024 Python Software Foundation).

package pyuni

import "sort"

// combiningRun gives the code points lo..hi the canonical combining class ccc.
type combiningRun struct {
	lo, hi rune
	ccc    uint8
}

// The Hangul syllables are named after, decompose into and compose from their jamo by arithmetic
// (Unicode 15.0, section 3.12).
const (
	hangulSBase  = 0xAC00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11A7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulSCount = hangulLCount * hangulNCount
)

// combiningClass is the canonical combining class of r.
func combiningClass(r rune) uint8 {
	i := sort.Search(len(combiningClasses), func(i int) bool { return combiningClasses[i].hi >= r })
	if i < len(combiningClasses) && combiningClasses[i].lo <= r {
		return combiningClasses[i].ccc
	}
	return 0
}

// NFKC is unicodedata.normalize('NFKC', s): the compatibility decomposition of s, its combining
// marks in canonical order, then the canonical composition. The parser normalises identifiers
// with it.
func NFKC(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var rs []rune
	for _, r := range s {
		switch {
		case r >= hangulSBase && r < hangulSBase+hangulSCount:
			i := r - hangulSBase
			rs = append(rs, hangulLBase+i/hangulNCount, hangulVBase+i%hangulNCount/hangulTCount)
			if t := i % hangulTCount; t != 0 {
				rs = append(rs, hangulTBase+t)
			}
		case compatDecompositions[r] != "":
			rs = append(rs, []rune(compatDecompositions[r])...)
		default:
			rs = append(rs, r)
		}
	}
	canonicalOrder(rs)
	return string(compose(rs))
}

// canonicalOrder sorts each run of non-starters by combining class, keeping the order of the
// marks of one class.
func canonicalOrder(rs []rune) {
	for i := 0; i < len(rs); {
		if combiningClass(rs[i]) == 0 {
			i++
			continue
		}
		j := i
		for j < len(rs) && combiningClass(rs[j]) != 0 {
			j++
		}
		run := rs[i:j]
		sort.SliceStable(run, func(a, b int) bool { return combiningClass(run[a]) < combiningClass(run[b]) })
		i = j
	}
}

// compose is the canonical composition of a canonically ordered sequence: a character joins the
// last starter when the pair has a primary composite and no character between them has a class
// as high as its own.
func compose(rs []rune) []rune {
	if len(rs) == 0 {
		return rs
	}
	starter := 0
	lastClass := int(combiningClass(rs[0]))
	if lastClass != 0 {
		// A sequence that starts with a mark has no starter to compose with.
		lastClass = 256
	}
	n := 1
	for _, r := range rs[1:] {
		class := int(combiningClass(r))
		if c, ok := composite(rs[starter], r); ok && (lastClass < class || lastClass == 0) {
			rs[starter] = c
			continue
		}
		if class == 0 {
			starter = n
		}
		lastClass = class
		rs[n] = r
		n++
	}
	return rs[:n]
}

// composite is the primary composite of the pair a, b.
func composite(a, b rune) (rune, bool) {
	switch {
	case a >= hangulLBase && a < hangulLBase+hangulLCount && b >= hangulVBase && b < hangulVBase+hangulVCount:
		return hangulSBase + ((a-hangulLBase)*hangulVCount+b-hangulVBase)*hangulTCount, true
	case a >= hangulSBase && a < hangulSBase+hangulSCount && (a-hangulSBase)%hangulTCount == 0 &&
		b > hangulTBase && b < hangulTBase+hangulTCount:
		return a + b - hangulTBase, true
	}
	c, ok := compositions[[2]rune{a, b}]
	return c, ok
}
