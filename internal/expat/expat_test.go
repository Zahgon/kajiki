// Ported from CPython 3.12.15 (Modules/pyexpat.c) to Go, 2026-11-22.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package expat

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// expectedDoc is one line of testdata/expat.jsonl.gz: a document and what
// pyexpat reported for it.
type expectedDoc struct {
	Src    string `json:"src"`
	Chunks []int  `json:"chunks"`
	Log    []any  `json:"log"`
	Result []any  `json:"result"`
}

// keyError stands for the KeyError the skipped entity handler of
// testdata/record.py raises.
type keyError struct{ key string }

func (e *keyError) Error() string { return "'" + e.key + "'" }

// replay feeds doc to a parser set up as testdata/record.py sets up
// pyexpat's, and returns the handler log and the outcome in its form.
func replay(doc expectedDoc) ([]any, []any) {
	log := []any{}
	p := NewParser()
	at := func(entry ...any) {
		log = append(log, append(entry, p.CurrentLineNumber(), p.CurrentColumnNumber()))
	}
	optional := func(s *string) any {
		if s == nil {
			return nil
		}
		return *s
	}
	p.StartElementHandler = func(name string, attrs []string) error {
		pairs := []any{}
		for i := 0; i < len(attrs); i += 2 {
			pairs = append(pairs, []any{attrs[i], attrs[i+1]})
		}
		at("start", name, pairs)
		return nil
	}
	p.EndElementHandler = func(name string) error { at("end", name); return nil }
	p.CharacterDataHandler = func(data string) error { at("data", data); return nil }
	p.ProcessingInstructionHandler = func(target, data string) error { at("pi", target, data); return nil }
	p.CommentHandler = func(data string) error { at("comment", data); return nil }
	p.StartCdataSectionHandler = func() error { at("startcdata"); return nil }
	p.EndCdataSectionHandler = func() error { at("endcdata"); return nil }
	p.StartDoctypeDeclHandler = func(name string, sysid, pubid *string, internal bool) error {
		at("doctype", name, optional(sysid), optional(pubid), flag(internal))
		return nil
	}
	p.ExternalEntityRefHandler = func(context, base, sysid, pubid *string) (int, error) {
		at("extref", optional(context), optional(sysid), optional(pubid))
		return 1, nil
	}
	p.SkippedEntityHandler = func(name string, isPE bool) error {
		at("skipped", name, flag(isPE))
		if strings.HasPrefix(name, "bogus") {
			return &keyError{name + ";"}
		}
		return nil
	}
	p.SetParamEntityParsing(ParamEntityParsingUnlessStandalone)
	runes := []rune(doc.Src)
	pos := 0
	var err error
	for _, n := range doc.Chunks {
		if err = p.Parse(string(runes[pos:pos+n]), false); err != nil {
			break
		}
		pos += n
	}
	if err == nil {
		err = p.Parse("", true)
	}
	var xe *Error
	var ke *keyError
	switch {
	case err == nil:
		return log, []any{"ok"}
	case errors.As(err, &xe):
		return log, []any{"error", xe.Code, xe.Lineno, xe.Offset, xe.Msg}
	case errors.As(err, &ke):
		return log, []any{"exception", "KeyError", ke.Error()}
	}
	return log, []any{"exception", "?", err.Error()}
}

// flag is a C int flag pyexpat passes to a handler.
func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}

// normalize gives the log the types encoding/json decodes the expected log
// to.
func normalize(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

func TestDocumentsAgainstPyexpat(t *testing.T) {
	f, err := os.Open("testdata/expat.jsonl.gz")
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
	n, bad := 0, 0
	for sc.Scan() {
		var doc expectedDoc
		if err := json.Unmarshal(sc.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		n++
		log, result := replay(doc)
		gotLog, gotResult := normalize(log), normalize(result)
		if !reflect.DeepEqual(gotLog, normalize(doc.Log)) || !reflect.DeepEqual(gotResult, normalize(doc.Result)) {
			bad++
			if bad <= 5 {
				t.Errorf("doc %d %q:\n got  %v %v\n want %v %v", n, doc.Src, gotLog, gotResult, doc.Log, doc.Result)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if bad > 0 {
		t.Errorf("%d of %d documents differ", bad, n)
	}
}

func TestNamespaceProcessing(t *testing.T) {
	p := NewParserNS('}')
	var got []string
	p.StartElementHandler = func(name string, attrs []string) error {
		got = append(got, name+"["+strings.Join(attrs, ",")+"]")
		return nil
	}
	if err := p.Parse("<a xmlns='u' xmlns:p='v' p:x='1'><p:b/></a>", true); err != nil {
		t.Fatal(err)
	}
	want := []string{"u}a[v}x,1]", "v}b[]"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	p = NewParserNS('}')
	var xe *Error
	if err := p.Parse("<a:b:c/>", true); !errors.As(err, &xe) || xe.Msg != "not well-formed (invalid token): line 1, column 4" {
		t.Errorf("got %v", err)
	}
	p = NewParser()
	got = nil
	p.StartElementHandler = func(name string, attrs []string) error {
		got = append(got, name+"["+strings.Join(attrs, ",")+"]")
		return nil
	}
	if err := p.Parse("<a:b:c xmlns:a='u'/>", true); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a:b:c[xmlns:a,u]"}; !reflect.DeepEqual(got, want) {
		t.Errorf("without namespaces: got %q, want %q", got, want)
	}
}

func TestPositionsCountCharacters(t *testing.T) {
	p := NewParser()
	var cols []int
	p.CharacterDataHandler = func(string) error {
		cols = append(cols, p.CurrentColumnNumber())
		return nil
	}
	doc := "<a>\u00e9\U0001F600<b/>x</a>"
	if err := p.Parse(doc, true); err != nil {
		t.Fatal(err)
	}
	if want := []int{3, 9}; !reflect.DeepEqual(cols, want) {
		t.Errorf("columns %v, want %v", cols, want)
	}
}
