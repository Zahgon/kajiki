// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/xml_template.py, kajiki/text.py, kajiki/ir.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// compileCase is one line of testdata/compile.jsonl.gz (see
// testdata/record_compile.py).
type compileCase struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Src    string         `json:"src"`
	Opts   map[string]any `json:"opts"`
	Result struct {
		OK *struct {
			Py     string   `json:"py"`
			Lines  [][2]int `json:"lines"`
			Syntax []string `json:"syntax"`
		} `json:"ok"`
		Error map[string]any `json:"error"`
	} `json:"result"`
}

// generatedName matches the names gen_name draws. The original's names
// after the first have random suffixes, so modules are compared with each
// name replaced by its rank of first appearance.
var generatedName = regexp.MustCompile(`_kj__?\d*`)

func canonicalNames(s string) string {
	seen := map[string]string{}
	return generatedName.ReplaceAllStringFunc(s, func(m string) string {
		if _, ok := seen[m]; !ok {
			seen[m] = fmt.Sprintf("_kj%d", len(seen))
		}
		return seen[m]
	})
}

// compileExpected compiles a case as testdata/record_compile.py compiles it
// with the original: to the generated module, its line map, and the
// SyntaxError compiling it gives.
func compileExpected(c compileCase) (string, [][2]int, []string, error) {
	filename, _ := c.Opts["filename"].(string)
	if filename == "" {
		filename = "<string>"
	}
	var tree *templateNode
	var err error
	if c.Kind == "text" {
		autoescape, _ := c.Opts["autoescape"].(bool)
		tree, err = compileTextTemplate(c.Src, filename, autoescape)
	} else {
		opts := xmlCompileOptions{cdataScripts: true}
		opts.mode, _ = c.Opts["mode"].(string)
		opts.isFragment, _ = c.Opts["is_fragment"].(bool)
		opts.stripText, _ = c.Opts["strip_text"].(bool)
		if v, ok := c.Opts["cdata_scripts"].(bool); ok {
			opts.cdataScripts = v
		}
		if blocks, ok := c.Opts["autoblocks"].([]any); ok {
			for _, b := range blocks {
				opts.autoblocks = append(opts.autoblocks, b.(string))
			}
		}
		tree, err = compileXMLTemplate(c.Src, filename, opts)
	}
	if err != nil {
		return "", nil, nil, err
	}
	text, linenos := moduleSource(tree)
	lines := [][2]int{}
	for _, l := range linenos {
		lines = append(lines, [2]int{l.pyLine, l.tplLine})
	}
	var syntax []string
	if _, err := py.Compile(text, "<string>", "exec"); err != nil {
		pe := err.(*py.Error)
		si, _ := pe.Syntax()
		syntax = []string{pe.Class, si.Msg}
	}
	return text, lines, syntax, nil
}

// errorFields is what the expected results keep of an exception.
func errorFields(err error, want map[string]any) map[string]any {
	pe, ok := err.(*py.Error)
	if !ok {
		return map[string]any{"go": err.Error()}
	}
	got := map[string]any{"class": pe.Class, "str": pe.Text}
	for _, k := range []string{"filename", "linenum", "colnum"} {
		if _, kept := want[k]; !kept {
			continue
		}
		v, err := py.GetAttr(pe.Value(), k)
		if err != nil {
			continue
		}
		if s, ok := py.AsString(v); ok {
			got[k] = s
		} else if i, ok := py.AsInt(v); ok {
			got[k] = float64(i.Int64())
		}
	}
	return got
}

func TestCompileAgainstOriginal(t *testing.T) {
	f, err := os.Open("testdata/compile.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(zr)
	sc.Buffer(nil, 1<<24)
	n, bad := 0, 0
	for sc.Scan() {
		var c compileCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		n++
		text, lines, syntax, err := compileExpected(c)
		var diff string
		switch want := c.Result.OK; {
		case want != nil && err != nil:
			diff = fmt.Sprintf("want a module, got %v", err)
		case want != nil && canonicalNames(text) != canonicalNames(want.Py):
			diff = fmt.Sprintf("module:\n%s\nwant:\n%s", text, want.Py)
		case want != nil && !reflect.DeepEqual(lines, want.Lines):
			diff = fmt.Sprintf("line map %v, want %v", lines, want.Lines)
		case want != nil && !reflect.DeepEqual(syntax, want.Syntax):
			diff = fmt.Sprintf("syntax error %q, want %q", syntax, want.Syntax)
		case want == nil && err == nil:
			diff = fmt.Sprintf("want %v, got a module", c.Result.Error)
		case want == nil && !reflect.DeepEqual(errorFields(err, c.Result.Error), c.Result.Error):
			diff = fmt.Sprintf("error %q, want %q", errorFields(err, c.Result.Error), c.Result.Error)
		}
		if diff != "" {
			bad++
			if bad <= 5 {
				t.Errorf("case %s %q %v: %s", c.ID, c.Src, c.Opts, diff)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if bad > 0 {
		t.Errorf("%d of %d cases differ", bad, n)
	}
}
