// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"bytes"
	"strings"
	"testing"
)

// execSource compiles and runs source in a fresh globals dict named
// __main__ and returns the dict.
func execSource(t *testing.T, source string) Value {
	t.Helper()
	code, err := Compile(source, "<t>", "exec")
	if err != nil {
		t.Fatal(err)
	}
	g := StrDict([]string{"__name__"}, []Value{String("__main__")})
	if err := code.Exec(g); err != nil {
		t.Fatal(err)
	}
	return g
}

func lookup(t *testing.T, g Value, name string) Value {
	t.Helper()
	v, err := GetItem(g, String(name))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func reprText(t *testing.T, v Value) string {
	t.Helper()
	s, err := Repr(v)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func wantError(t *testing.T, err error, class, text string) {
	t.Helper()
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("error %v, want %s: %s", err, class, text)
	}
	if e.Class != class || e.Text != text {
		t.Errorf("error %s: %s, want %s: %s", e.Class, e.Text, class, text)
	}
}

func TestCodeObjects(t *testing.T) {
	Init(Config{})
	g := execSource(t, "def f(x):\n    y = x + 1\n    return y / 0\n")
	f := lookup(t, g, "f")
	c, ok := FunctionCode(f)
	if !ok {
		t.Fatal("no code object")
	}
	if c.Name() != "f" || c.Qualname() != "f" || c.Filename() != "<t>" || c.FirstLine() != 1 {
		t.Errorf("code %s %s %s %d", c.Name(), c.Qualname(), c.Filename(), c.FirstLine())
	}
	if again, _ := FunctionCode(f); again != c {
		t.Error("two *Code for one code object")
	}
	nc := c.WithLines("page.html", 10, map[int]int{1: 10, 3: 30})
	if nc.Filename() != "page.html" || nc.FirstLine() != 10 || nc.Name() != "f" {
		t.Errorf("new code %s %d %s", nc.Filename(), nc.FirstLine(), nc.Name())
	}
	for attr, want := range map[string]string{"co_filename": "'page.html'", "co_firstlineno": "10", "co_name": "'f'"} {
		v, err := GetAttr(nc.Value(), attr)
		if err != nil {
			t.Fatal(err)
		}
		if got := reprText(t, v); got != want {
			t.Errorf("%s is %s, want %s", attr, got, want)
		}
	}
	nf, err := FunctionType(nc, g, None(), None(), None())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Call(nf, []Value{Int(1)}, nil)
	wantError(t, err, "ZeroDivisionError", "division by zero")
	tb := Traceback(err)
	if len(tb) != 1 {
		t.Fatalf("traceback %v", tb)
	}
	if fr := tb[0]; fr.Code != nc || fr.Filename != "page.html" || fr.Name != "f" || fr.Lineno != 30 || !fr.HasLine {
		t.Errorf("frame %+v", fr)
	}
	_, err = Call(nf, []Value{String("s")}, nil)
	wantError(t, err, "TypeError", `can only concatenate str (not "int") to str`)
	if tb := Traceback(err); len(tb) != 1 || tb[0].HasLine || tb[0].Filename != "page.html" {
		t.Errorf("unmapped line: %+v", tb)
	}
	if err := SetAttr(f, "__code__", nc.Value()); err != nil {
		t.Fatal(err)
	}
	if got, _ := FunctionCode(f); got != nc {
		t.Error("__code__ assignment not seen")
	}
}

func TestFunctionType(t *testing.T) {
	Init(Config{})
	g := execSource(t, `
def outer():
    v = 'cell'
    def inner(a, b=2):
        return v, a, b
    return inner
fn = outer()
`)
	fn, outer := lookup(t, g, "fn"), lookup(t, g, "outer")
	code, _ := FunctionCode(fn)
	outerCode, _ := FunctionCode(outer)
	closure, err := GetAttr(fn, "__closure__")
	if err != nil {
		t.Fatal(err)
	}
	errs := []struct {
		code                           *Code
		globals, name, defaults, cells Value
		class, text                    string
	}{
		{code, EmptyDict(), String("zz"), None(), Tuple(), "ValueError", "inner requires closure of length 1, not 0"},
		{code, EmptyDict(), String("zz"), None(), None(), "TypeError", "arg 5 (closure) must be tuple"},
		{code, EmptyDict(), String("zz"), None(), List(Int(1)), "TypeError", "arg 5 (closure) must be None or tuple"},
		{code, EmptyDict(), String("zz"), None(), Tuple(Int(1)), "TypeError", "arg 5 (closure) expected cell, found int"},
		{code, List(), String("zz"), None(), None(), "TypeError", "function() argument 'globals' must be dict, not list"},
		{code, EmptyDict(), Int(3), None(), None(), "TypeError", "arg 3 (name) must be None or string"},
		{code, EmptyDict(), None(), List(), None(), "TypeError", "arg 4 (defaults) must be None or tuple"},
		{outerCode, EmptyDict(), None(), None(), closure, "ValueError", "outer requires closure of length 0, not 1"},
	}
	for _, c := range errs {
		_, err := FunctionType(c.code, c.globals, c.name, c.defaults, c.cells)
		wantError(t, err, c.class, c.text)
	}
	g2 := StrDict([]string{"__name__"}, []Value{String("m2")})
	nf, err := FunctionType(code, g2, String("renamed"), Tuple(Int(5)), closure)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Call(nf, []Value{Int(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := reprText(t, r); got != "('cell', 1, 5)" {
		t.Errorf("call gave %s", got)
	}
	for attr, want := range map[string]string{"__name__": "'renamed'", "__qualname__": "'outer.<locals>.inner'", "__module__": "'m2'"} {
		v, err := GetAttr(nf, attr)
		if err != nil {
			t.Fatal(err)
		}
		if got := reprText(t, v); got != want {
			t.Errorf("%s is %s, want %s", attr, got, want)
		}
	}
	if c, _ := FunctionCode(nf); c != code {
		t.Error("FunctionType made another code object")
	}
}

func TestRebindFunction(t *testing.T) {
	Init(Config{})
	g := execSource(t, "x = 'old'\ndef f(a, b=1, *, c=2):\n    return x, a, b, c\n")
	g2 := StrDict([]string{"x"}, []Value{String("new")})
	nf, err := RebindFunction(lookup(t, g, "f"), g2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Call(nf, []Value{Int(0)}, nil)
	wantError(t, err, "TypeError", "f() missing 1 required keyword-only argument: 'c'")
	r, err := Call(nf, []Value{Int(0)}, []Keyword{{Name: "c", Value: Int(3)}})
	if err != nil {
		t.Fatal(err)
	}
	if got := reprText(t, r); got != "('new', 0, 1, 3)" {
		t.Errorf("rebound call gave %s", got)
	}
	kw, err := GetAttr(nf, "__kwdefaults__")
	if err != nil {
		t.Fatal(err)
	}
	if !IsNone(kw) {
		t.Errorf("__kwdefaults__ is %s", reprText(t, kw))
	}
}

func TestGoClass(t *testing.T) {
	var out bytes.Buffer
	Init(Config{Stdout: &out})
	type state struct{ it *Iterator }
	ns := StrDict([]string{"__module__", "__qualname__"}, []Value{String("kajiki.util"), String("flattener")})
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(SetItem(ns, String("__init__"), Func("__init__", func(args []Value, kw []Keyword) (Value, error) {
		it, err := Iter(args[1])
		if err != nil {
			return nil, err
		}
		return nil, SetNative(args[0], &state{it: it})
	})))
	must(SetItem(ns, String("__iter__"), Func("__iter__", func(args []Value, kw []Keyword) (Value, error) {
		s := Native(args[0]).(*state)
		return NewIterator(s.it.Next), nil
	})))
	cls, err := NewClass("flattener", []Value{Builtin("object")}, ns)
	must(err)
	g := StrDict([]string{"__name__", "flattener"}, []Value{String("__main__"), cls})
	code, err := Compile(`
def f(n):
    for i in range(n):
        yield str(i)
r = flattener(f(3))
print(type(r).__name__, type(r).__module__, ''.join(r), isinstance(r, flattener), repr(flattener))
r.extra = 5
print(r.extra)
`, "<t>", "exec")
	must(err)
	must(code.Exec(g))
	want := "flattener kajiki.util 012 True <class 'kajiki.util.flattener'>\n5\n"
	if out.String() != want {
		t.Errorf("output %q, want %q", out.String(), want)
	}
	ok, err := IsInstance(lookup(t, g, "r"), cls)
	if err != nil || !ok {
		t.Errorf("isinstance %v %v", ok, err)
	}
}

func TestRecursionLimit(t *testing.T) {
	Init(Config{})
	code, err := Compile("def rec():\n    return rec()\nrec()\n", "<t>", "exec")
	if err != nil {
		t.Fatal(err)
	}
	err = code.Exec(EmptyDict())
	wantError(t, err, "RecursionError", "maximum recursion depth exceeded")
}

func TestCompileSyntaxInfo(t *testing.T) {
	Init(Config{})
	_, err := Compile("x = (1,\ny = 2\n", "tpl.py", "exec")
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("no SyntaxError: %v", err)
	}
	si, ok := e.Syntax()
	if !ok {
		t.Fatalf("%s is not a SyntaxError", e.Class)
	}
	want := SyntaxInfo{Msg: "'(' was never closed", Filename: "tpl.py", Lineno: 1, Offset: 5, EndLineno: 1, EndOffset: 0,
		Text: "x = (1,", HasText: true}
	if si != want {
		t.Errorf("syntax info %+v\n  want %+v", si, want)
	}
	if e.Text != "'(' was never closed (tpl.py, line 1)" {
		t.Errorf("str() is %q", e.Text)
	}
}

func TestDecode(t *testing.T) {
	Init(Config{})
	s, n, err := Decode([]byte("caf\xc3\xa9"), "utf-8", true)
	if err != nil || s != "café" || n != 5 {
		t.Errorf("Decode gave %q %d %v", s, n, err)
	}
	s, n, err = Decode([]byte("\xc3"), "utf-8", false)
	if err != nil || s != "" || n != 0 {
		t.Errorf("partial Decode gave %q %d %v", s, n, err)
	}
	_, _, err = Decode([]byte("\xff"), "utf-8", true)
	wantError(t, err, "UnicodeDecodeError", "'utf-8' codec can't decode byte 0xff in position 0: invalid start byte")
	for enc, want := range map[string]string{"latin-1": "latin_1", "UTF8": "utf_8"} {
		if got, err := EncodingModule(enc); err != nil || got != want {
			t.Errorf("EncodingModule(%q) = %q, %v", enc, got, err)
		}
	}
}

func TestGenerateTokensLazily(t *testing.T) {
	Init(Config{})
	s := GenerateTokens("x = 1\n)\n")
	var got []string
	for {
		tok, ok, err := s.Next()
		if err != nil {
			t.Fatalf("tokenize error after %v: %v", got, err)
		}
		if !ok {
			break
		}
		got = append(got, tok.String)
	}
	if want := []string{"x", "=", "1", "\n", ")", "\n", ""}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("tokens %q, want %q", got, want)
	}
}

// TestEnterRecursiveCall runs f and a Go function g calling each other, g
// counting as a frame: the RecursionError comes where it does when g is
// written in Python, after f has run with n up to 499.
func TestEnterRecursiveCall(t *testing.T) {
	Init(Config{})
	g := Func("g", func(args []Value, kw []Keyword) (Value, error) {
		leave, err := EnterRecursiveCall()
		if err != nil {
			return nil, err
		}
		defer leave()
		return Call(args[0], args[1:], nil)
	})
	globals := StrDict([]string{"__name__", "g"}, []Value{String("__main__"), g})
	code, err := Compile(`
seen = []
def f(n):
    seen.append(n)
    return g(f, n + 1)
try:
    f(0)
except RecursionError as e:
    result = (type(e).__name__, str(e), max(seen))
`, "<t>", "exec")
	if err != nil {
		t.Fatal(err)
	}
	depth := interp.depth
	if err := code.Exec(globals); err != nil {
		t.Fatal(err)
	}
	if got := reprText(t, lookup(t, globals, "result")); got != "('RecursionError', 'maximum recursion depth exceeded', 499)" {
		t.Errorf("result %s", got)
	}
	if interp.depth != depth {
		t.Errorf("depth %d after the run, %d before", interp.depth, depth)
	}
}

func TestNewFunction(t *testing.T) {
	var out bytes.Buffer
	Init(Config{Stdout: &out})
	fn := NewFunction(FuncSpec{Name: "cut", Qualname: "Cutter.cut", Module: "kajiki.util", PosOnly: []string{"coll"},
		Params: []string{"a", "b"}, Defaults: []Value{None()}, VarKw: "kw"}, func(bound []Value) (Value, error) {
		return Tuple(bound...), nil
	})
	g := StrDict([]string{"__name__", "cut"}, []Value{String("__main__"), fn})
	code, err := Compile(`
print(cut(1, 2), cut.__name__, cut.__qualname__, cut.__module__, cut.__defaults__, cut.__code__.co_varnames)
for call in (lambda: cut(), lambda: cut(1, 2, 3, 4), lambda: cut(coll=1, a=2)):
    try:
        call()
    except TypeError as e:
        print(e)
`, "t.py", "exec")
	if err != nil {
		t.Fatal(err)
	}
	if err := code.Exec(g); err != nil {
		t.Fatal(err)
	}
	want := "(1, 2, None, {}) cut Cutter.cut kajiki.util (None,) ('coll', 'a', 'b', 'kw')\n" +
		"Cutter.cut() missing 2 required positional arguments: 'coll' and 'a'\n" +
		"Cutter.cut() takes from 2 to 3 positional arguments but 4 were given\n" +
		"Cutter.cut() missing 1 required positional argument: 'coll'\n"
	if out.String() != want {
		t.Errorf("got\n%s\nwant\n%s", out.String(), want)
	}
	if _, ok := FunctionCode(fn); ok {
		t.Error("FunctionCode of a function written in Go")
	}
}
