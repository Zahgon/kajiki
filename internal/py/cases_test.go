// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// recordedCase is one line of a cases file: the input fields, and under
// "expect" what CPython did with them (see the record.py beside the file).
type recordedCase struct {
	ID       string         `json:"id"`
	Src      string         `json:"src"`
	Mode     string         `json:"mode"`
	Filename string         `json:"filename"`
	Expect   map[string]any `json:"expect"`
}

// readCases reads a cases file and calls check with each case, reporting the
// number that matched.
func readCases(t *testing.T, path string, check func(c recordedCase) map[string]any, fields ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	total, bad := 0, 0
	for sc.Scan() {
		var c recordedCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		total++
		got := jsonRoundTrip(t, check(c))
		for _, f := range fields {
			if !reflect.DeepEqual(got[f], c.Expect[f]) {
				bad++
				if bad <= 20 {
					t.Errorf("case %d %q: %s is %s, want %s", total, c.Src, f, jsonText(got[f]), jsonText(c.Expect[f]))
				}
				break
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatalf("%s has no cases", path)
	}
	t.Logf("%d of %d cases identical", total-bad, total)
}

// jsonRoundTrip gives a result the types encoding/json gives the expected
// values in, so that the two compare with reflect.DeepEqual.
func jsonRoundTrip(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// TestCompileErrors compiles each case of testdata/compile and compares the
// outcome, down to the SyntaxError's class, message, location and text.
func TestCompileErrors(t *testing.T) {
	Init(Config{})
	readCases(t, "testdata/compile/cases.jsonl", func(c recordedCase) map[string]any {
		filename := c.Filename
		if filename == "" {
			filename = "<string>"
		}
		_, err := Compile(c.Src, filename, c.Mode)
		if err == nil {
			return map[string]any{"ok": true}
		}
		e := err.(*Error)
		r := map[string]any{"cls": e.Class, "str": e.Text}
		if si, ok := e.Syntax(); ok {
			r["msg"] = si.Msg
			r["lineno"], r["offset"] = si.Lineno, si.Offset
			r["end_lineno"], r["end_offset"] = si.EndLineno, si.EndOffset
			if si.HasText {
				r["text"] = si.Text
			}
		}
		// A location CPython leaves unset reads as None, which the Go
		// API reports as 0.
		if _, ok := c.Expect["msg"]; ok {
			for _, f := range []string{"lineno", "offset", "end_lineno", "end_offset"} {
				if c.Expect[f] == nil && r[f] == 0 {
					r[f] = nil
				}
			}
		}
		return r
	}, "ok", "cls", "msg", "lineno", "offset", "end_lineno", "end_offset", "text", "str")
}

// TestGenerateTokens tokenizes each case of testdata/tokenize and compares
// the tokens, and the exception that ends the stream, if any.
func TestGenerateTokens(t *testing.T) {
	Init(Config{})
	readCases(t, "testdata/tokenize/cases.jsonl", func(c recordedCase) map[string]any {
		toks := [][]any{}
		r := map[string]any{}
		s := GenerateTokens(c.Src)
		for {
			tok, ok, err := s.Next()
			if err != nil {
				e := err.(*Error)
				r["err"], r["str"] = e.Class, e.Text
				break
			}
			if !ok {
				break
			}
			toks = append(toks, []any{tok.Type, tok.String, tok.Start, tok.End, tok.Line})
		}
		r["toks"] = toks
		return r
	}, "toks", "err", "str")
}

// TestCorpus runs each program of testdata/corpus as __main__ and compares
// what it prints and the exception that escapes it: class, str(), the
// traceback entries in the program, __cause__, __context__, and for a
// SyntaxError its position.
func TestCorpus(t *testing.T) {
	readCases(t, "testdata/corpus/programs.jsonl", func(c recordedCase) map[string]any {
		var out bytes.Buffer
		Init(Config{Stdout: &out})
		r := map[string]any{}
		code, err := Compile(c.Src, "<prog>", "exec")
		if err == nil {
			err = code.Exec(StrDict([]string{"__name__"}, []Value{String("__main__")}))
		}
		if err != nil {
			e := err.(*Error)
			r["exc"] = []any{e.Class, e.Text}
			tb := [][]any{}
			for _, f := range Traceback(err) {
				if f.Filename == "<prog>" {
					tb = append(tb, []any{f.Filename, f.Name, f.Lineno})
				}
			}
			r["tb"] = tb
			for _, link := range []string{"__cause__", "__context__"} {
				if v, err := GetAttr(e.Value(), link); err == nil && !IsNone(v) {
					r[link[2:len(link)-2]] = TypeName(v)
				}
			}
			if si, ok := e.Syntax(); ok {
				r["syn"] = []any{si.Msg, si.Lineno, si.Offset}
			}
		}
		r["out"] = out.String()
		return r
	}, "out", "exc", "tb", "cause", "context", "syn")
}
