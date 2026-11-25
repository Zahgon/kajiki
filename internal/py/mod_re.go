// Ported from CPython 3.12 (Lib/re/__init__.py, Lib/re/_constants.py, Modules/_sre/sre.c) to Go,
// 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"github.com/jackrosenthal/kajiki-go/internal/pyre"
	"strconv"
	"strings"
)

// regexFlags are the members of re.RegexFlag in definition order, which
// their reprs follow.
var regexFlags = []struct {
	name, short string
	value       int
}{
	{"ASCII", "A", 256}, {"IGNORECASE", "I", 2}, {"LOCALE", "L", 4}, {"UNICODE", "U", 32},
	{"MULTILINE", "M", 8}, {"DOTALL", "S", 16}, {"VERBOSE", "X", 64}, {"TEMPLATE", "T", 1}, {"DEBUG", "", 128},
}

// patternFlagNames is the order pattern_repr names flags in.
var patternFlagNames = []struct {
	name  string
	value int
}{
	{"re.IGNORECASE", 2}, {"re.LOCALE", 4}, {"re.MULTILINE", 8}, {"re.DOTALL", 16}, {"re.UNICODE", 32},
	{"re.VERBOSE", 64}, {"re.DEBUG", 128}, {"re.ASCII", 256},
}

// reModule holds the classes of the re module the functions need.
type reModule struct {
	flagCls, patternCls, matchCls, errCls, proxyCls, iterCls Value
}

func init() {
	register("re", func(b *builder) {
		m := &reModule{}
		object := []Value{Builtin("object")}
		m.flagCls = b.class("RegexFlag", []Value{Builtin("int")}, regexFlagMethods(m))
		m.errCls = b.class("error", []Value{Builtin("Exception")}, reErrorMethods())
		b.set("PatternError", m.errCls)
		m.proxyCls = b.newClass("mappingproxy", object, mappingProxyMethods())
		m.iterCls = b.newClass("callable_iterator", object, iterMethods("callable_iterator"))
		m.patternCls = b.cClass("Pattern", object, patternMethods(m))
		m.matchCls = b.cClass("Match", object, matchMethods(m))
		if b.err != nil {
			return
		}
		noflag, err := m.flag(0)
		if err != nil {
			b.err = err
			return
		}
		b.set("NOFLAG", noflag)
		for _, f := range regexFlags {
			v, err := m.flag(f.value)
			if err != nil {
				b.err = err
				return
			}
			b.set(f.name, v)
			if f.short != "" {
				b.set(f.short, v)
			}
		}
		b.pyfn("compile", func(args []Value, kwargs []Keyword) (Value, error) {
			buf, err := ParseArgs("compile", []string{"pattern", "flags"}, 1, 2, args, kwargs)
			if err != nil {
				return nil, err
			}
			return m.compile(buf[0], buf[1])
		})
		for _, name := range []string{"match", "fullmatch", "search", "findall", "finditer"} {
			name := name
			b.pyfn(name, func(args []Value, kwargs []Keyword) (Value, error) {
				buf, err := ParseArgs(name, []string{"pattern", "string", "flags"}, 2, 3, args, kwargs)
				if err != nil {
					return nil, err
				}
				p, err := m.compile(buf[0], buf[2])
				if err != nil {
					return nil, err
				}
				return CallMethod(p, name, buf[1])
			})
		}
		for _, name := range []string{"sub", "subn"} {
			name := name
			b.pyfn(name, func(args []Value, kwargs []Keyword) (Value, error) {
				buf, err := ParseArgs(name, []string{"pattern", "repl", "string", "count", "flags"}, 3, 5, args, kwargs)
				if err != nil {
					return nil, err
				}
				p, err := m.compile(buf[0], buf[4])
				if err != nil {
					return nil, err
				}
				return CallMethod(p, name, buf[1], buf[2], orDefault(buf[3], Int(0)))
			})
		}
		b.pyfn("split", func(args []Value, kwargs []Keyword) (Value, error) {
			buf, err := ParseArgs("split", []string{"pattern", "string", "maxsplit", "flags"}, 2, 4, args, kwargs)
			if err != nil {
				return nil, err
			}
			p, err := m.compile(buf[0], buf[3])
			if err != nil {
				return nil, err
			}
			return CallMethod(p, "split", buf[1], orDefault(buf[2], Int(0)))
		})
		b.pyfn("escape", func(args []Value, kwargs []Keyword) (Value, error) {
			buf, err := ParseArgs("escape", []string{"pattern"}, 1, 1, args, kwargs)
			if err != nil {
				return nil, err
			}
			if s, ok := AsString(buf[0]); ok {
				return String(pyre.Escape(s)), nil
			}
			if bs, ok := AsBytes(buf[0]); ok {
				return Bytes([]byte(latin1Encode(pyre.Escape(latin1Decode(bs))))), nil
			}
			return nil, raise("TypeError", "expected string or bytes-like object, got '%s'", TypeName(buf[0]))
		})
		b.pyfn("purge", func(args []Value, kwargs []Keyword) (Value, error) { return nil, nil })
	})
}

// ---------------------------------------------------------------- RegexFlag

// flag is the RegexFlag of value v.
func (m *reModule) flag(v int) (Value, error) {
	return NewInstance(m.flagCls, Int(int64(v)))
}

// flagValue is int(v) of a flags argument.
func flagValue(v Value) (int, error) {
	if v == nil {
		return 0, nil
	}
	n, err := Index(v)
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

// flagRepr is the repr of a RegexFlag of value v.
func flagRepr(v int) string {
	if v == 0 {
		return "re.NOFLAG"
	}
	var parts []string
	rest := v
	for _, f := range regexFlags {
		if v&f.value != 0 {
			parts = append(parts, "re."+f.name)
			rest &^= f.value
		}
	}
	if rest != 0 {
		parts = append(parts, "0x"+strconv.FormatInt(int64(rest), 16))
	}
	return strings.Join(parts, "|")
}

func regexFlagMethods(m *reModule) *methods {
	ms := &methods{}
	str := func(args []Value, _ []Keyword) (Value, error) {
		v, err := flagValue(args[0])
		if err != nil {
			return nil, err
		}
		return String(flagRepr(v)), nil
	}
	ms.method("__repr__", str)
	ms.method("__str__", str)
	ms.method("__format__", func(args []Value, kwargs []Keyword) (Value, error) {
		s, err := str(args, kwargs)
		if err != nil || len(args) < 2 {
			return s, err
		}
		return CallMethod(s, "__format__", args[1])
	})
	binary := func(name string, f func(a, b int) int) {
		ms.method(name, func(args []Value, _ []Keyword) (Value, error) {
			if _, ok := AsInt(args[1]); !ok {
				return NotImplemented(), nil
			}
			a, err := flagValue(args[0])
			if err != nil {
				return nil, err
			}
			b, err := flagValue(args[1])
			if err != nil {
				return nil, err
			}
			return m.flag(f(a, b))
		})
	}
	for _, op := range []struct {
		names [2]string
		f     func(a, b int) int
	}{
		{[2]string{"__or__", "__ror__"}, func(a, b int) int { return a | b }},
		{[2]string{"__and__", "__rand__"}, func(a, b int) int { return a & b }},
		{[2]string{"__xor__", "__rxor__"}, func(a, b int) int { return a ^ b }},
	} {
		binary(op.names[0], op.f)
		binary(op.names[1], op.f)
	}
	ms.method("__invert__", func(args []Value, _ []Keyword) (Value, error) {
		v, err := flagValue(args[0])
		if err != nil {
			return nil, err
		}
		return m.flag(511 &^ v)
	})
	ms.property("value", func(self Value) (Value, error) {
		v, err := flagValue(self)
		return Int(int64(v)), err
	})
	ms.property("name", func(self Value) (Value, error) {
		v, err := flagValue(self)
		if err != nil {
			return nil, err
		}
		if v == 0 {
			return String("NOFLAG"), nil
		}
		var names []string
		for _, f := range regexFlags {
			if v&f.value != 0 {
				names = append(names, f.name)
			}
		}
		return String(strings.Join(names, "|")), nil
	})
	return ms
}

// ---------------------------------------------------------------- re.error

func reErrorMethods() *methods {
	ms := &methods{}
	ms.method("__init__", func(args []Value, kwargs []Keyword) (Value, error) {
		buf, err := ParseArgs("error", []string{"msg", "pattern", "pos"}, 1, 3, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		self, msg := args[0], buf[0]
		pattern, pos := orDefault(buf[1], None()), orDefault(buf[2], None())
		text, err := Str(msg)
		if err != nil {
			return nil, err
		}
		lineno, colno := Value(None()), Value(None())
		if !IsNone(pattern) && !IsNone(pos) {
			p, err := Ssize(pos)
			if err != nil {
				return nil, err
			}
			pat, _ := AsString(pattern)
			rs := []rune(pat)
			before := string(rs[:min(max(p, 0), len(rs))])
			l := strings.Count(before, "\n") + 1
			c := p + 1
			if i := strings.LastIndexByte(before, '\n'); i >= 0 {
				c = p - len([]rune(before[:i]))
			}
			lineno, colno = Int(int64(l)), Int(int64(c))
			text += " at position " + strconv.Itoa(p)
			if strings.Contains(pat, "\n") {
				text += " (line " + strconv.Itoa(l) + ", column " + strconv.Itoa(c) + ")"
			}
		}
		if _, err := CallSuper(TypeOf(self), self, "__init__", []Value{String(text)}, nil); err != nil {
			return nil, err
		}
		for _, a := range []struct {
			name string
			v    Value
		}{{"msg", msg}, {"pattern", pattern}, {"pos", pos}, {"lineno", lineno}, {"colno", colno}} {
			if err := SetAttr(self, a.name, a.v); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return ms
}

// reError turns an error of package pyre into the exception the re module
// raises: re.error(msg, pattern, pos) for an invalid pattern or template,
// the builtin exception for the others.
func (m *reModule) reError(err error) error {
	switch e := err.(type) {
	case *pyre.PyError:
		return raise(e.Class, "%s", e.Msg)
	case *pyre.Error:
		args := []Value{String(e.Msg)}
		if e.HasPos {
			args = append(args, String(e.Pattern), Int(int64(e.Pos)))
		}
		v, cerr := Call(m.errCls, args, nil)
		if cerr != nil {
			return cerr
		}
		return RaiseValue(v)
	}
	return err
}

// regex is a pattern compiled by package pyre, applied to the Go strings
// the re module holds.
type regex struct {
	p *pyre.Pattern
}

// compileRegex compiles a str pattern (or the latin-1 reading of a bytes
// pattern) with the re flags.
func compileRegex(pattern string, flags int) (*regex, error) {
	p, err := pyre.Compile(pattern, flags)
	if err != nil {
		return nil, err
	}
	return &regex{p: p}, nil
}

// Flags is Pattern.flags as the engine reports it.
func (r *regex) Flags() int { return r.p.Flags() }

// Groups is Pattern.groups.
func (r *regex) Groups() int { return r.p.Groups() }

// GroupIndex is Pattern.groupindex as a map.
func (r *regex) GroupIndex() map[string]int {
	out := map[string]int{}
	for _, g := range r.p.GroupIndex() {
		out[g.Name] = g.Index
	}
	return out
}

// Match, FullMatch and Search apply the pattern to s from code point pos.
func (r *regex) Match(s string, pos int) *pyre.Match { return r.p.Match(strRunes(s), pos) }

// FullMatch is Pattern.fullmatch.
func (r *regex) FullMatch(s string, pos int) *pyre.Match {
	rs := strRunes(s)
	return r.p.FullMatch(rs, pos, len(rs))
}

// Search is Pattern.search.
func (r *regex) Search(s string, pos int) *pyre.Match { return r.p.Search(strRunes(s), pos) }

// FindAllFrom is the matches of finditer from code point pos.
func (r *regex) FindAllFrom(s string, pos int) []*pyre.Match {
	rs := strRunes(s)
	return r.p.FindIter(rs, pos, len(rs))
}

// Sub is Pattern.subn with a template: the new text and the count.
func (r *regex) Sub(tmpl, s string, count int) (string, int, error) {
	return r.p.Subn(tmpl, strRunes(s), count)
}

// SubFunc is Pattern.subn with a callable.
func (r *regex) SubFunc(s string, fn func(*pyre.Match) (string, error), count int) (string, int, error) {
	return r.p.SubnFunc(fn, strRunes(s), count)
}

// Split is Pattern.split: nil for a group that did not take part.
func (r *regex) Split(rs []rune, maxsplit int) []*string { return r.p.Split(rs, maxsplit) }

// ---------------------------------------------------------------- Pattern

// patternState is a compiled pattern: the engine's Regexp (over the
// latin-1 reading of a bytes pattern), the pattern object and the flags it
// reports.
type patternState struct {
	re      *regex
	pattern Value
	isBytes bool
	flags   int
}

// compile is re.compile(pattern, flags).
func (m *reModule) compile(pattern, flagsV Value) (Value, error) {
	flags, err := flagValue(flagsV)
	if err != nil {
		return nil, err
	}
	if _, ok := Native(pattern).(*patternState); ok {
		if flags != 0 {
			return nil, raise("ValueError", "cannot process flags argument with a compiled pattern")
		}
		return pattern, nil
	}
	st := &patternState{pattern: pattern}
	if s, ok := AsString(pattern); ok {
		r, err := compileRegex(s, flags)
		if err != nil {
			return nil, m.reError(err)
		}
		st.re, st.flags = r, r.Flags()
	} else if b, ok := AsBytes(pattern); ok {
		if flags&pyre.UNICODE != 0 {
			return nil, raise("ValueError", "cannot use UNICODE flag with a bytes pattern")
		}
		if flags&pyre.LOCALE != 0 && flags&pyre.ASCII != 0 {
			return nil, raise("ValueError", "ASCII and LOCALE flags are incompatible")
		}
		r, err := compileRegex(latin1Decode(b), flags&^pyre.LOCALE|pyre.ASCII)
		if err != nil {
			return nil, m.reError(err)
		}
		st.re, st.isBytes = r, true
		st.flags = r.Flags()&^pyre.ASCII | flags&pyre.ASCII
	} else {
		return nil, raise("TypeError", "first argument must be string or compiled pattern")
	}
	return instance(m.patternCls, st)
}

// subject reads the string a pattern is applied to, checking it is of the
// pattern's kind, as a Go string of code points (bytes read as latin-1).
func (st *patternState) subject(v Value) (string, error) {
	if s, ok := AsString(v); ok {
		if st.isBytes {
			return "", raise("TypeError", "cannot use a bytes pattern on a string-like object")
		}
		return s, nil
	}
	b, ok := bytesOf(unsub(v))
	if !ok {
		return "", raise("TypeError", "expected string or bytes-like object, got '%s'", TypeName(v))
	}
	if !st.isBytes {
		return "", raise("TypeError", "cannot use a string pattern on a bytes-like object")
	}
	return latin1Decode([]byte(b)), nil
}

// text makes a result string of the pattern's kind.
func (st *patternState) text(s string) Value {
	if st.isBytes {
		return Bytes(latin1Encode(s))
	}
	return String(s)
}

// window applies pos and endpos: the subject cut at endpos, and pos.
func window(s string, posV, endV Value) (string, int, error) {
	n := strLen(s)
	pos, end := 0, n
	if posV != nil {
		p, err := Ssize(posV)
		if err != nil {
			return "", 0, err
		}
		pos = p
	}
	if endV != nil {
		e, err := Ssize(endV)
		if err != nil {
			return "", 0, err
		}
		end = min(max(e, 0), n)
	}
	if end < n {
		s = runesStr([]rune(s)[:end])
	}
	return s, pos, nil
}

// matchState is a re.Match: the engine's match, its pattern object, the
// subject value and the pos and endpos it was searched with.
type matchState struct {
	m           *pyre.Match
	pat         *patternState
	patternObj  Value
	subject     Value
	pos, endpos int
}

func (m *reModule) newMatch(pm *pyre.Match, st *patternState, patObj, subject Value, pos, endpos int) (Value, error) {
	if pm == nil {
		return None(), nil
	}
	return instance(m.matchCls, &matchState{m: pm, pat: st, patternObj: patObj, subject: subject, pos: max(pos, 0), endpos: endpos})
}

func patternOf(self Value) (*patternState, error) {
	return stateOf[*patternState](self, "re.Pattern", "match")
}

// groupValue is a group's text, or dflt when it did not take part.
func (st *patternState) groupValue(pm *pyre.Match, g int, dflt Value) Value {
	s, ok := pm.Group(g)
	if !ok {
		return dflt
	}
	return st.text(s)
}

func patternMethods(m *reModule) *methods {
	ms := &methods{}
	search := func(name string) {
		ms.method(name, func(args []Value, kwargs []Keyword) (Value, error) {
			st, err := patternOf(args[0])
			if err != nil {
				return nil, err
			}
			buf, err := ParseArgs(name, []string{"string", "pos", "endpos"}, 1, 3, args[1:], kwargs)
			if err != nil {
				return nil, err
			}
			s, err := st.subject(buf[0])
			if err != nil {
				return nil, err
			}
			cut, pos, err := window(s, buf[1], buf[2])
			if err != nil {
				return nil, err
			}
			var pm *pyre.Match
			switch name {
			case "match":
				pm = st.re.Match(cut, pos)
			case "fullmatch":
				pm = st.re.FullMatch(cut, pos)
			default:
				pm = st.re.Search(cut, pos)
			}
			return m.newMatch(pm, st, args[0], buf[0], pos, strLen(cut))
		})
	}
	search("match")
	search("fullmatch")
	search("search")
	ms.method("findall", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := patternOf(args[0])
		if err != nil {
			return nil, err
		}
		buf, err := ParseArgs("findall", []string{"string", "pos", "endpos"}, 1, 3, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		s, err := st.subject(buf[0])
		if err != nil {
			return nil, err
		}
		cut, pos, err := window(s, buf[1], buf[2])
		if err != nil {
			return nil, err
		}
		var out []Value
		empty := st.text("")
		for _, pm := range st.re.FindAllFrom(cut, pos) {
			switch n := pm.Re().Groups(); n {
			case 0:
				out = append(out, st.groupValue(pm, 0, empty))
			case 1:
				out = append(out, st.groupValue(pm, 1, empty))
			default:
				items := make([]Value, n)
				for g := 1; g <= n; g++ {
					items[g-1] = st.groupValue(pm, g, empty)
				}
				out = append(out, Tuple(items...))
			}
		}
		return List(out...), nil
	})
	ms.method("finditer", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := patternOf(args[0])
		if err != nil {
			return nil, err
		}
		buf, err := ParseArgs("finditer", []string{"string", "pos", "endpos"}, 1, 3, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		s, err := st.subject(buf[0])
		if err != nil {
			return nil, err
		}
		cut, pos, err := window(s, buf[1], buf[2])
		if err != nil {
			return nil, err
		}
		matches := st.re.FindAllFrom(cut, pos)
		i := 0
		return instance(m.iterCls, &goIter{next: func() (Value, bool, error) {
			if i >= len(matches) {
				return nil, false, nil
			}
			i++
			v, err := m.newMatch(matches[i-1], st, args[0], buf[0], pos, strLen(cut))
			return v, err == nil, err
		}})
	})
	sub := func(name string) {
		ms.method(name, func(args []Value, kwargs []Keyword) (Value, error) {
			st, err := patternOf(args[0])
			if err != nil {
				return nil, err
			}
			buf, err := ParseArgs(name, []string{"repl", "string", "count"}, 2, 3, args[1:], kwargs)
			if err != nil {
				return nil, err
			}
			s, err := st.subject(buf[1])
			if err != nil {
				return nil, err
			}
			count := 0
			if buf[2] != nil {
				if count, err = Ssize(buf[2]); err != nil {
					return nil, err
				}
				if count < 0 {
					count = -1
				}
			}
			var out string
			var n int
			repl := buf[0]
			if IsCallable(repl) {
				out, n, err = st.re.SubFunc(s, func(pm *pyre.Match) (string, error) {
					mv, err := m.newMatch(pm, st, args[0], buf[1], 0, strLen(s))
					if err != nil {
						return "", err
					}
					r, err := call(repl, mv)
					if err != nil {
						return "", err
					}
					if st.isBytes {
						b, ok := AsBytes(r)
						if !ok {
							return "", raise("TypeError", "expected a bytes-like object, %s found", TypeName(r))
						}
						return latin1Decode(b), nil
					}
					t, ok := AsString(r)
					if !ok {
						return "", raise("TypeError", "expected str instance, %s found", TypeName(r))
					}
					return t, nil
				}, count)
			} else {
				tmpl, terr := st.subject(repl)
				if terr != nil {
					return nil, terr
				}
				out, n, err = st.re.Sub(tmpl, s, count)
				if err != nil {
					err = m.reError(err)
				}
			}
			if err != nil {
				return nil, err
			}
			if name == "subn" {
				return Tuple(st.text(out), Int(int64(n))), nil
			}
			return st.text(out), nil
		})
	}
	sub("sub")
	sub("subn")
	ms.method("split", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := patternOf(args[0])
		if err != nil {
			return nil, err
		}
		buf, err := ParseArgs("split", []string{"string", "maxsplit"}, 1, 2, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		s, err := st.subject(buf[0])
		if err != nil {
			return nil, err
		}
		maxsplit := 0
		if buf[1] != nil {
			if maxsplit, err = Ssize(buf[1]); err != nil {
				return nil, err
			}
		}
		if maxsplit < 0 {
			return List(buf[0]), nil
		}
		var out []Value
		for _, p := range st.re.Split(strRunes(s), maxsplit) {
			if p != nil {
				out = append(out, st.text(*p))
			} else {
				out = append(out, None())
			}
		}
		return List(out...), nil
	})
	ms.property("pattern", func(self Value) (Value, error) {
		st, err := patternOf(self)
		if err != nil {
			return nil, err
		}
		return st.pattern, nil
	})
	ms.property("flags", func(self Value) (Value, error) {
		st, err := patternOf(self)
		if err != nil {
			return nil, err
		}
		return Int(int64(st.flags)), nil
	})
	ms.property("groups", func(self Value) (Value, error) {
		st, err := patternOf(self)
		if err != nil {
			return nil, err
		}
		return Int(int64(st.re.Groups())), nil
	})
	ms.property("groupindex", func(self Value) (Value, error) {
		st, err := patternOf(self)
		if err != nil {
			return nil, err
		}
		gi := st.re.GroupIndex()
		if len(gi) == 0 {
			return EmptyDict(), nil
		}
		names := make([]string, 0, len(gi))
		for name := range gi {
			names = append(names, name)
		}
		sortByGroup(names, gi)
		vals := make([]Value, len(names))
		for i, n := range names {
			vals[i] = Int(int64(gi[n]))
		}
		return call(m.proxyCls, StrDict(names, vals))
	})
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		st, err := patternOf(args[0])
		if err != nil {
			return nil, err
		}
		r, err := Repr(st.pattern)
		if err != nil {
			return nil, err
		}
		r = strPrefix(r, 200)
		flags := st.flags
		if !st.isBytes && flags&(4|32|256) == 32 {
			flags &^= 32
		}
		if flags == 0 {
			return String("re.compile(" + r + ")"), nil
		}
		var parts []string
		for _, f := range patternFlagNames {
			if flags&f.value != 0 {
				parts = append(parts, f.name)
				flags &^= f.value
			}
		}
		if flags != 0 {
			parts = append(parts, "0x"+strconv.FormatInt(int64(flags), 16))
		}
		return String("re.compile(" + r + ", " + strings.Join(parts, "|") + ")"), nil
	})
	ms.method("__eq__", func(args []Value, _ []Keyword) (Value, error) {
		a, err := patternOf(args[0])
		if err != nil {
			return nil, err
		}
		b, ok := Native(args[1]).(*patternState)
		if !ok {
			return NotImplemented(), nil
		}
		if a == b {
			return Bool(true), nil
		}
		same, err := Compare("==", a.pattern, b.pattern)
		return Bool(err == nil && same && a.flags == b.flags && a.isBytes == b.isBytes), err
	})
	ms.method("__hash__", func(args []Value, _ []Keyword) (Value, error) {
		st, err := patternOf(args[0])
		if err != nil {
			return nil, err
		}
		h, err := Hash(st.pattern)
		if err != nil {
			return nil, err
		}
		return Int(h ^ int64(st.flags)), nil
	})
	return ms
}

// sortByGroup orders group names by their group numbers.
func sortByGroup(names []string, gi map[string]int) {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && gi[names[j]] < gi[names[j-1]]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
}

// ---------------------------------------------------------------- Match

func matchOf(self Value) (*matchState, error) {
	return stateOf[*matchState](self, "re.Match", "group")
}

// groupIndex resolves a group argument (number or name).
func (ms *matchState) groupIndex(v Value) (int, error) {
	if n, ok := AsInt(v); ok {
		if n.IsInt64() && n.Int64() >= 0 && int(n.Int64()) <= ms.m.Re().Groups() {
			return int(n.Int64()), nil
		}
		return 0, raise("IndexError", "no such group")
	}
	if name, ok := AsString(v); ok {
		if g, found := ms.m.Re().GroupIndexOf(name); found {
			return g, nil
		}
	}
	return 0, raise("IndexError", "no such group")
}

func matchMethods(m *reModule) *methods {
	ms := &methods{}
	ms.method("group", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := matchOf(args[0])
		if err != nil {
			return nil, err
		}
		if err := NoKeywords("group", kwargs); err != nil {
			return nil, err
		}
		if len(args) == 1 {
			return st.pat.groupValue(st.m, 0, None()), nil
		}
		out := make([]Value, len(args)-1)
		for i, a := range args[1:] {
			g, err := st.groupIndex(a)
			if err != nil {
				return nil, err
			}
			out[i] = st.pat.groupValue(st.m, g, None())
		}
		if len(out) == 1 {
			return out[0], nil
		}
		return Tuple(out...), nil
	})
	ms.method("__getitem__", func(args []Value, _ []Keyword) (Value, error) {
		st, err := matchOf(args[0])
		if err != nil {
			return nil, err
		}
		g, err := st.groupIndex(args[1])
		if err != nil {
			return nil, err
		}
		return st.pat.groupValue(st.m, g, None()), nil
	})
	ms.method("groups", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := matchOf(args[0])
		if err != nil {
			return nil, err
		}
		buf, err := ParseArgs("groups", []string{"default"}, 0, 1, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		dflt := orDefault(buf[0], None())
		out := make([]Value, st.m.Re().Groups())
		for g := range out {
			out[g] = st.pat.groupValue(st.m, g+1, dflt)
		}
		return Tuple(out...), nil
	})
	ms.method("groupdict", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := matchOf(args[0])
		if err != nil {
			return nil, err
		}
		buf, err := ParseArgs("groupdict", []string{"default"}, 0, 1, args[1:], kwargs)
		if err != nil {
			return nil, err
		}
		dflt := orDefault(buf[0], None())
		gi := st.pat.re.GroupIndex()
		names := make([]string, 0, len(gi))
		for n := range gi {
			names = append(names, n)
		}
		sortByGroup(names, gi)
		vals := make([]Value, len(names))
		for i, n := range names {
			vals[i] = st.pat.groupValue(st.m, gi[n], dflt)
		}
		return StrDict(names, vals), nil
	})
	position := func(name string, f func(st *matchState, g int) Value) {
		ms.method(name, func(args []Value, kwargs []Keyword) (Value, error) {
			st, err := matchOf(args[0])
			if err != nil {
				return nil, err
			}
			if err := NoKeywords(name, kwargs); err != nil {
				return nil, err
			}
			if err := CheckPositional(name, len(args)-1, 0, 1); err != nil {
				return nil, err
			}
			g := 0
			if len(args) == 2 {
				if g, err = st.groupIndex(args[1]); err != nil {
					return nil, err
				}
			}
			return f(st, g), nil
		})
	}
	position("start", func(st *matchState, g int) Value { return Int(int64(st.m.Start(g))) })
	position("end", func(st *matchState, g int) Value { return Int(int64(st.m.End(g))) })
	position("span", func(st *matchState, g int) Value {
		return Tuple(Int(int64(st.m.Start(g))), Int(int64(st.m.End(g))))
	})
	ms.method("expand", func(args []Value, kwargs []Keyword) (Value, error) {
		st, err := matchOf(args[0])
		if err != nil {
			return nil, err
		}
		if err := CheckPositional("expand", len(args)-1, 1, 1); err != nil {
			return nil, err
		}
		tmpl, err := st.pat.subject(args[1])
		if err != nil {
			return nil, err
		}
		expanded, err := st.m.Expand(tmpl)
		if err != nil {
			return nil, m.reError(err)
		}
		return st.pat.text(expanded), nil
	})
	property := func(name string, f func(st *matchState) Value) {
		ms.property(name, func(self Value) (Value, error) {
			st, err := matchOf(self)
			if err != nil {
				return nil, err
			}
			return f(st), nil
		})
	}
	property("string", func(st *matchState) Value { return st.subject })
	property("re", func(st *matchState) Value { return st.patternObj })
	property("pos", func(st *matchState) Value { return Int(int64(st.pos)) })
	property("endpos", func(st *matchState) Value { return Int(int64(st.endpos)) })
	property("lastindex", func(st *matchState) Value {
		if last := st.m.LastIndex(); last > 0 {
			return Int(int64(last))
		}
		return None()
	})
	property("lastgroup", func(st *matchState) Value {
		last := st.m.LastIndex()
		for name, g := range st.pat.re.GroupIndex() {
			if g == last && last != 0 {
				return String(name)
			}
		}
		return None()
	})
	property("regs", func(st *matchState) Value {
		out := make([]Value, st.m.Re().Groups()+1)
		for g := range out {
			out[g] = Tuple(Int(int64(st.m.Start(g))), Int(int64(st.m.End(g))))
		}
		return Tuple(out...)
	})
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		st, err := matchOf(args[0])
		if err != nil {
			return nil, err
		}
		r, err := Repr(st.pat.groupValue(st.m, 0, None()))
		if err != nil {
			return nil, err
		}
		return String("<re.Match object; span=(" + strconv.Itoa(st.m.Start(0)) + ", " +
			strconv.Itoa(st.m.End(0)) + "), match=" + strPrefix(r, 50) + ">"), nil
	})
	ms.method("__bool__", func(args []Value, _ []Keyword) (Value, error) { return Bool(true), nil })
	return ms
}

// mappingProxyMethods is the namespace of the read-only view of a dict
// that Pattern.groupindex is.
func mappingProxyMethods() *methods {
	ms := &methods{}
	ms.method("__init__", func(args []Value, _ []Keyword) (Value, error) {
		return nil, SetNative(args[0], args[1])
	})
	mapping := func(self Value) Value {
		if d, ok := Native(self).(Value); ok {
			return d
		}
		return EmptyDict()
	}
	for _, name := range []string{"__getitem__", "__len__", "__iter__", "__contains__", "keys", "values", "items",
		"get", "copy"} {
		name := name
		ms.method(name, func(args []Value, kwargs []Keyword) (Value, error) {
			return CallMethodKw(mapping(args[0]), name, args[1:], kwargs)
		})
	}
	ms.method("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		r, err := Repr(mapping(args[0]))
		if err != nil {
			return nil, err
		}
		return String("mappingproxy(" + r + ")"), nil
	})
	ms.method("__str__", func(args []Value, _ []Keyword) (Value, error) {
		s, err := Str(mapping(args[0]))
		return String(s), err
	})
	ms.method("__eq__", func(args []Value, _ []Keyword) (Value, error) {
		return RichCompare("==", mapping(args[0]), args[1])
	})
	return ms
}
