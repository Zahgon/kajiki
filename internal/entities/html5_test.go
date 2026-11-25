// Ported from CPython 3.12.15 (Lib/html/entities.py) to Go, 2026-11-22.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package entities

import "testing"

func TestHTML5(t *testing.T) {
	if got := len(HTML5); got != 2231 {
		t.Fatalf("len(HTML5) = %d, want 2231", got)
	}
	cases := map[string]string{
		"amp;":                             "&",
		"amp":                              "&",
		"AMP":                              "&",
		"lt;":                              "<",
		"nbsp":                             " ",
		"Tab;":                             "\t",
		"NewLine;":                         "\n",
		"acE;":                             "∾̳",
		"fjlig;":                           "fj",
		"Afr;":                             "\U0001d504",
		"CounterClockwiseContourIntegral;": "∳",
		"notin;":                           "∉",
		"not":                              "¬",
	}
	for name, want := range cases {
		got, ok := HTML5[name]
		if !ok || got != want {
			t.Errorf("HTML5[%q] = %q, %v; want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"notin", "apos", "", "&amp;"} {
		if v, ok := HTML5[name]; ok {
			t.Errorf("HTML5[%q] = %q, want no entry", name, v)
		}
	}
}
