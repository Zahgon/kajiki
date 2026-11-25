// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/doctype.py, kajiki/util.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// helpers is testdata/helpers.json (see testdata/record_helpers.py).
type helpers struct {
	Shlex    [][3]any    `json:"shlex"`
	Unescape [][2]string `json:"unescape"`
	Path     [][5]string `json:"path"`
	Alias    [][2]string `json:"alias"`
	DTD      [][6]any    `json:"dtd"`
}

func loadHelpers(t *testing.T) helpers {
	t.Helper()
	data, err := os.ReadFile("testdata/helpers.json")
	if err != nil {
		t.Fatal(err)
	}
	var h helpers
	if err := json.Unmarshal(data, &h); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestShlexSplit(t *testing.T) {
	for _, c := range loadHelpers(t).Shlex {
		s := c[0].(string)
		parts, err := shlexSplit(s)
		if c[2] != nil {
			if err == nil || err.(*py.Error).Class != "ValueError" || err.Error() != c[2] {
				t.Errorf("shlex.split(%q): got %q, %v; want ValueError %v", s, parts, err, c[2])
			}
			continue
		}
		want := []string{}
		for _, p := range c[1].([]any) {
			want = append(want, p.(string))
		}
		if err != nil || !reflect.DeepEqual(append([]string{}, parts...), want) {
			t.Errorf("shlex.split(%q) = %q, %v; want %q", s, parts, err, want)
		}
	}
}

func TestHTMLUnescape(t *testing.T) {
	for _, c := range loadHelpers(t).Unescape {
		if got := htmlUnescape(c[0]); got != c[1] {
			t.Errorf("html.unescape(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestPaths(t *testing.T) {
	h := loadHelpers(t)
	for _, c := range h.Path {
		p := joinPath(c[0], c[1])
		if p != c[2] || pathName(p) != c[3] || pathSuffix(pathName(p)) != c[4] {
			t.Errorf("Path(%q) / %q: %q %q %q, want %q", c[0], c[1], p, pathName(p), pathSuffix(pathName(p)), c[2:])
		}
	}
	for _, c := range h.Alias {
		if got := defaultAliasFor(c[0]); got != c[1] {
			t.Errorf("default_alias_for(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestDoctypes(t *testing.T) {
	for _, c := range loadHelpers(t).DTD {
		markup := c[0].(string)
		dtd, pos, rest := extractDTD(markup)
		chars := utf8.RuneCountInString(markup[:pos])
		name, mode := any(nil), any(nil)
		if d := matchingDoctype(dtd); d != nil {
			name, mode = d.name, d.renderingMode
		}
		got := []any{markup, dtd, float64(chars), rest, name, mode}
		if !reflect.DeepEqual(got, c[:]) {
			t.Errorf("extract_dtd(%q): got %q, want %q", markup, got, c)
		}
	}
}
