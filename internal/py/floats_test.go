// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestFloatResults calls the math functions and complex * and / on the
// arguments of testdata/floats/cases.txt and compares each result bit for
// bit with the one CPython gave (testdata/floats/record.py), or the
// exception class it raised.
func TestFloatResults(t *testing.T) {
	Init(Config{})
	g := execSource(t, "import math\n")
	mathMod := lookup(t, g, "math")
	f, err := os.Open("testdata/floats/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	parse := func(s string) float64 {
		x, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatalf("bad float %q: %v", s, err)
		}
		return x
	}
	same := func(a, b float64) bool {
		return math.Float64bits(a) == math.Float64bits(b) || math.IsNaN(a) && math.IsNaN(b)
	}
	sc := bufio.NewScanner(f)
	total, bad := 0, 0
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		name := fields[0]
		total++
		switch name {
		case "cmul", "cdiv":
			a := complex(parse(fields[1]), parse(fields[2]))
			b := complex(parse(fields[3]), parse(fields[4]))
			want := complex(parse(fields[5]), parse(fields[6]))
			var got complex128
			if name == "cmul" {
				got = complexMul(a, b)
			} else {
				got, _ = complexQuot(a, b)
			}
			if !same(real(got), real(want)) || !same(imag(got), imag(want)) {
				bad++
				t.Errorf("%s(%v, %v) = %v, want %v", name, a, b, got, want)
			}
			continue
		}
		// The outcome is the last field, or "error" and a class name.
		argText, outcome := fields[1:len(fields)-1], fields[len(fields)-1]
		wantErr := len(fields) > 2 && fields[len(fields)-2] == "error"
		if wantErr {
			argText = fields[1 : len(fields)-2]
		}
		var args []Value
		for _, a := range argText {
			args = append(args, Float(parse(a)))
		}
		fn, err := GetAttr(mathMod, name)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Call(fn, args, nil)
		switch {
		case wantErr:
			if err == nil || !ErrorIs(err, outcome) {
				bad++
				t.Errorf("%s%v gave %v, %v; want %s", name, args, r, err, outcome)
			}
		case err != nil:
			bad++
			t.Errorf("%s%v: %v", name, args, err)
		default:
			got, _ := AsFloat(r)
			if want := parse(outcome); !same(got, want) {
				bad++
				t.Errorf("%s%v = %x, want %x", name, args, got, want)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d of %d results identical", total-bad, total)
}
