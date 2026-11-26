// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/template.py, kajiki/loader.py, kajiki/i18n.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"bufio"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// renderCase is one line of testdata/render.jsonl.gz (see
// testdata/record_render.py).
type renderCase struct {
	ID    string `json:"id"`
	Files []struct {
		Path string `json:"path"`
		Text string `json:"text"`
		Hex  string `json:"hex"`
	} `json:"files"`
	Steps []map[string]any `json:"steps"`
	Lines []string         `json:"lines"`
}

// pyJSON is json.dumps(s, ensure_ascii=True) of a str.
func pyJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r >= 0x20 && r < 0x7f:
			b.WriteRune(r)
		case r > 0xffff:
			r -= 0x10000
			fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// contextValue builds a value from its JSON description.
func contextValue(t *testing.T, v any) Value {
	switch x := v.(type) {
	case nil:
		return None()
	case bool:
		return Bool(x)
	case string:
		return String(x)
	case json.Number:
		n, ok := new(big.Int).SetString(string(x), 10)
		if !ok {
			t.Fatalf("not an int: %s", x)
		}
		return BigInt(n)
	case []any:
		var items []Value
		for _, i := range x {
			items = append(items, contextValue(t, i))
		}
		return List(items...)
	case map[string]any:
		for kind, a := range x {
			switch kind {
			case "f":
				f, err := strconv.ParseFloat(a.(string), 64)
				if err != nil {
					t.Fatal(err)
				}
				return Float(f)
			case "d":
				var keys, values []Value
				for _, kv := range a.([]any) {
					pair := kv.([]any)
					keys = append(keys, contextValue(t, pair[0]))
					values = append(values, contextValue(t, pair[1]))
				}
				d, err := NewDict(keys, values)
				if err != nil {
					t.Fatal(err)
				}
				return d
			case "lit":
				return Literal(a.(string))
			}
		}
	}
	t.Fatalf("no value for %v", v)
	return nil
}

func contextVars(t *testing.T, v any) []Var {
	vars := []Var{}
	for _, kv := range v.([]any) {
		pair := kv.([]any)
		vars = append(vars, Var{Name: pair[0].(string), Value: contextValue(t, pair[1])})
	}
	return vars
}

// player runs the steps of a case and prints the lines
// testdata/record_render.py prints for the original.
type player struct {
	t         *testing.T
	lines     []string
	templates map[string]*Template
	loaders   map[string]*Loader
}

func (p *player) print(format string, args ...any) {
	p.lines = append(p.lines, fmt.Sprintf(format, args...))
}

func (p *player) fail(err error) {
	var e *Error
	if !errors.As(err, &e) {
		p.t.Fatalf("not an *Error: %v", err)
	}
	p.print("error %s", e.Class)
	if e.Class == "KajikiSyntaxError" {
		p.print("msg %s", pyJSON(e.SyntaxMsg))
		return
	}
	p.print("msg %s", pyJSON(e.Message))
	p.print("at %s %d %d", pyJSON(e.Filename), e.Line, e.Column)
}

func strOpt(o map[string]any, key string) string {
	s, _ := o[key].(string)
	return s
}

func boolOpt(o map[string]any, key string) bool {
	b, _ := o[key].(bool)
	return b
}

func stringsOpt(o map[string]any, key string) []string {
	l, ok := o[key].([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, x := range l {
		out = append(out, x.(string))
	}
	return out
}

func (p *player) step(s map[string]any) {
	o, _ := s["opts"].(map[string]any)
	name, _ := s["name"].(string)
	switch s["op"] {
	case "xml":
		opts := XMLOptions{Filename: strOpt(o, "filename"), Mode: strOpt(o, "mode"), IsFragment: boolOpt(o, "is_fragment"),
			Encoding: strOpt(o, "encoding"), Autoblocks: stringsOpt(o, "autoblocks"), StripText: boolOpt(o, "strip_text")}
		if b, ok := o["cdata_scripts"].(bool); ok && !b {
			opts.NoCDATAScripts = true
		}
		if g, ok := o["base_globals"]; ok {
			opts.BaseGlobals = contextVars(p.t, g)
		}
		var t *Template
		var err error
		if src, ok := s["src"].(string); ok {
			t, err = XMLTemplate(src, opts)
		} else {
			t, err = XMLTemplateFile(opts)
		}
		if err != nil {
			p.fail(err)
		} else {
			p.templates[name] = t
		}
	case "text":
		opts := TextOptions{Filename: strOpt(o, "filename"), Autoescape: boolOpt(o, "autoescape"),
			Encoding: strOpt(o, "encoding")}
		var t *Template
		var err error
		if src, ok := s["src"].(string); ok {
			t, err = TextTemplate(src, opts)
		} else {
			t, err = TextTemplateFile(opts)
		}
		if err != nil {
			p.fail(err)
		} else {
			p.templates[name] = t
		}
	case "mock":
		modules := map[string]*Template{}
		for _, kv := range s["modules"].([]any) {
			pair := kv.([]any)
			t, ok := p.templates[pair[1].(string)]
			if !ok {
				p.print("missing %s", pair[1])
				return
			}
			modules[pair[0].(string)] = t
		}
		p.loaders[name] = NewMockLoader(modules)
	case "fileloader":
		var path []string
		for _, d := range s["path"].([]any) {
			path = append(path, d.(string))
		}
		opts := FileLoaderOptions{Reload: boolOpt(o, "reload"), ForceMode: strOpt(o, "force_mode"),
			AutoescapeText: boolOpt(o, "autoescape_text"), XMLAutoblocks: stringsOpt(o, "xml_autoblocks")}
		for key, dst := range map[string]**bool{"is_fragment": &opts.IsFragment, "strip_text": &opts.StripText,
			"cdata_scripts": &opts.CDATAScripts} {
			if b, ok := o[key].(bool); ok {
				*dst = &b
			}
		}
		if g, ok := o["base_globals"]; ok {
			opts.BaseGlobals = contextVars(p.t, g)
		}
		p.loaders[name] = NewFileLoader(path, opts)
	case "load":
		l, ok := p.loaders[s["loader"].(string)]
		if !ok {
			p.print("missing %s", s["loader"])
			return
		}
		if t, err := l.Import(s["path"].(string)); err != nil {
			p.fail(err)
		} else {
			p.templates[name] = t
		}
	case "render":
		t, ok := p.templates[s["tpl"].(string)]
		if !ok {
			p.print("missing %s", s["tpl"])
			return
		}
		if out, err := t.Render(contextVars(p.t, s["ctx"])...); err != nil {
			p.fail(err)
		} else {
			p.print("out %s", pyJSON(out))
		}
	case "extract":
		msgs, err := Extract([]byte(s["src"].(string)), ExtractOptions{StripText: boolOpt(o, "strip_text"),
			Mode: strOpt(o, "mode"), IsFragment: boolOpt(o, "is_fragment")})
		if err != nil {
			p.fail(err)
			return
		}
		p.print("extract %d", len(msgs))
		for _, m := range msgs {
			p.print("m %d %s %s []", m.Lineno, pyJSON(m.Funcname), pyJSON(m.Text))
		}
	case "write":
		path := filepath.FromSlash(s["path"].(string))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			p.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(s["text"].(string)), 0o644); err != nil {
			p.t.Fatal(err)
		}
	default:
		p.t.Fatalf("unknown step %v", s["op"])
	}
}

func TestRenderAgainstOriginal(t *testing.T) {
	f, err := os.Open("testdata/render.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := zr.Close(); err != nil {
			t.Error(err)
		}
	}()
	sc := bufio.NewScanner(zr)
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		dec := json.NewDecoder(strings.NewReader(sc.Text()))
		dec.UseNumber()
		var c renderCase
		if err := dec.Decode(&c); err != nil {
			t.Fatal(err)
		}
		t.Run(c.ID, func(t *testing.T) {
			dir := t.TempDir()
			for _, file := range c.Files {
				path := filepath.Join(dir, filepath.FromSlash(file.Path))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				data := []byte(file.Text)
				if file.Hex != "" {
					var err error
					if data, err = hex.DecodeString(file.Hex); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(dir)
			p := &player{t: t, templates: map[string]*Template{}, loaders: map[string]*Loader{}}
			for _, s := range c.Steps {
				p.step(s)
			}
			if !reflect.DeepEqual(p.lines, c.Lines) {
				t.Errorf("printed\n%s\nwant\n%s", strings.Join(p.lines, "\n"), strings.Join(c.Lines, "\n"))
			}
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}
