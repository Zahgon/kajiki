// Ported from CPython 3.12 (Python/compile.c pattern matching, Python/ceval.c MATCH_* opcodes) to
// Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The match statement: each case's pattern is tried against the subject; the names a pattern
// captures are bound once the whole pattern has matched, before its guard runs.

package py

// capture is a name a pattern binds and its value.
type capture struct {
	name  string
	value Value
}

// execMatch runs a match statement.
func (f *frame) execMatch(n *Node) (ctlFlow, error) {
	subject, err := f.eval(n.A)
	if err != nil {
		return ctlNone, err
	}
	for _, c := range n.L1 {
		var caps []capture
		ok, err := f.matchPattern(c.A, subject, &caps)
		if err != nil {
			return ctlNone, err
		}
		if !ok {
			continue
		}
		for _, cp := range caps {
			if err := f.storeName(cp.name, cp.value); err != nil {
				return ctlNone, err
			}
		}
		if c.B != nil {
			t, err := f.evalTruth(c.B)
			if err != nil {
				return ctlNone, err
			}
			if !t {
				continue
			}
		}
		return f.execBlock(c.Body)
	}
	return ctlNone, nil
}

// matchPattern tries pattern p on v, appending the captures to caps. An
// exception raised while matching is reported at the pattern.
func (f *frame) matchPattern(p *Node, v Value, caps *[]capture) (bool, error) {
	ok, err := f.matchPatternAt(p, v, caps)
	if err != nil {
		noteTraceback(f, p, err)
	}
	return ok, err
}

func (f *frame) matchPatternAt(p *Node, v Value, caps *[]capture) (bool, error) {
	switch p.Kind {
	case "MatchAs":
		if p.A != nil {
			ok, err := f.matchPattern(p.A, v, caps)
			if err != nil || !ok {
				return false, err
			}
		}
		if !p.NoID && p.ID != "" {
			*caps = append(*caps, capture{p.ID, v})
		}
		return true, nil
	case "MatchValue":
		want, err := f.eval(p.A)
		if err != nil {
			return false, err
		}
		return pyEq(v, want)
	case "MatchSingleton":
		return sameObject(v, p.Value), nil
	case "MatchOr":
		for _, alt := range p.L1 {
			var altCaps []capture
			ok, err := f.matchPattern(alt, v, &altCaps)
			if err != nil {
				return false, err
			}
			if ok {
				*caps = append(*caps, altCaps...)
				return true, nil
			}
		}
		return false, nil
	case "MatchSequence":
		return f.matchSequence(p, v, caps)
	case "MatchMapping":
		return f.matchMapping(p, v, caps)
	case "MatchClass":
		return f.matchClass(p, v, caps)
	}
	return false, newPyErr("SystemError", "unknown pattern kind "+p.Kind)
}

// isMatchSequence is the Py_TPFLAGS_SEQUENCE test of sequence patterns:
// list, tuple, range, collections.deque and their subclasses, not str,
// bytes or bytearray.
func isMatchSequence(v Value) bool {
	switch unsub(v).(type) {
	case *listV, *tupleV, rangeV:
		return true
	}
	// collections.deque is the one sequence type of the standard library
	// modules written in Go.
	for _, b := range classOf(v).mroList() {
		if b.name == "deque" && b.moduleName() == "collections" {
			return true
		}
	}
	return false
}

func (f *frame) matchSequence(p *Node, v Value, caps *[]capture) (bool, error) {
	if !isMatchSequence(v) {
		return false, nil
	}
	items, err := toSlice(v)
	if err != nil {
		return false, err
	}
	star := -1
	for i, sp := range p.L1 {
		if sp.Kind == "MatchStar" {
			star = i
		}
	}
	if star < 0 {
		if len(items) != len(p.L1) {
			return false, nil
		}
		for i, sp := range p.L1 {
			ok, err := f.matchPattern(sp, items[i], caps)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	}
	before, after := star, len(p.L1)-star-1
	if len(items) < before+after {
		return false, nil
	}
	for i := 0; i < before; i++ {
		ok, err := f.matchPattern(p.L1[i], items[i], caps)
		if err != nil || !ok {
			return false, err
		}
	}
	sp := p.L1[star]
	if !sp.NoID && sp.ID != "" {
		rest := append([]Value(nil), items[before:len(items)-after]...)
		*caps = append(*caps, capture{sp.ID, newList(rest)})
	}
	for i := 0; i < after; i++ {
		ok, err := f.matchPattern(p.L1[star+1+i], items[len(items)-after+i], caps)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// isMatchMapping is the Py_TPFLAGS_MAPPING test of mapping patterns: dict
// and its subclasses.
func isMatchMapping(v Value) bool {
	_, ok := unsub(v).(*dictV)
	return ok
}

func (f *frame) matchMapping(p *Node, v Value, caps *[]capture) (bool, error) {
	if !isMatchMapping(v) {
		return false, nil
	}
	n, err := pyLen(v)
	if err != nil {
		return false, err
	}
	if n < len(p.L1) {
		return false, nil
	}
	keys := make([]Value, len(p.L1))
	seen := newDict()
	for i, k := range p.L1 {
		kv, err := f.eval(k)
		if err != nil {
			return false, err
		}
		if _, dup, err := seen.get(kv); err != nil {
			return false, err
		} else if dup {
			r, err := pyRepr(kv)
			if err != nil {
				return false, err
			}
			return false, valueErr("mapping pattern checks duplicate key (%s)", r)
		}
		if err := seen.set(kv, none); err != nil {
			return false, err
		}
		keys[i] = kv
	}
	get, err := pyGetattr(v, "get")
	if err != nil {
		return false, err
	}
	missing := &objectV{}
	vals := make([]Value, len(keys))
	for i, k := range keys {
		r, err := callValue(nil, get, []Value{k, missing}, nil)
		if err != nil {
			return false, err
		}
		if r == Value(missing) {
			return false, nil
		}
		vals[i] = r
	}
	for i, sp := range p.L2 {
		ok, err := f.matchPattern(sp, vals[i], caps)
		if err != nil || !ok {
			return false, err
		}
	}
	if !p.NoID && p.ID != "" {
		rest := newDict()
		if _, err := methodsDictUpdate(rest, []Value{v}, nil); err != nil {
			return false, err
		}
		for _, k := range keys {
			if _, err := rest.del(k); err != nil {
				return false, err
			}
		}
		*caps = append(*caps, capture{p.ID, rest})
	}
	return true, nil
}

// matchSelfTypes are the builtin types whose single positional
// sub-pattern matches the subject itself.
var matchSelfTypes = map[string]bool{"bool": true, "bytearray": true, "bytes": true, "dict": true,
	"float": true, "frozenset": true, "int": true, "list": true, "set": true, "str": true, "tuple": true}

func (f *frame) matchClass(p *Node, v Value, caps *[]capture) (bool, error) {
	cv, err := f.eval(p.A)
	if err != nil {
		return false, err
	}
	k, ok := cv.(*classV)
	if !ok {
		return false, typeErr("called match pattern must be a class")
	}
	if !classOf(v).subclassOf(k) {
		return false, nil
	}
	var attrs []string
	var subs []*Node
	if len(p.L1) > 0 {
		names, self, err := matchArgs(k, len(p.L1))
		if err != nil {
			return false, err
		}
		if self {
			ok, err := f.matchPattern(p.L1[0], v, caps)
			if err != nil || !ok {
				return false, err
			}
		} else {
			attrs = append(attrs, names...)
			subs = append(subs, p.L1...)
		}
	}
	attrs = append(attrs, p.Names...)
	subs = append(subs, p.L2...)
	for i, a := range attrs {
		for _, b := range attrs[:i] {
			if a == b {
				return false, typeErr("%s() got multiple sub-patterns for attribute '%s'", k.name, a)
			}
		}
	}
	for i, a := range attrs {
		av, err := getattrOpt(v, a)
		if err != nil {
			return false, err
		}
		if av == nil {
			return false, nil
		}
		ok, err := f.matchPattern(subs[i], av, caps)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// matchArgs gives the attributes n positional sub-patterns of class k
// match, from __match_args__; self is set for the builtin types whose one
// positional sub-pattern matches the subject itself.
func matchArgs(k *classV, n int) (names []string, self bool, err error) {
	ma, err := getattrOpt(k, "__match_args__")
	if err != nil {
		return nil, false, err
	}
	if ma == nil {
		if matchSelfTypes[k.builtinBase().name] && k.builtinBase() != objectClass {
			if n > 1 {
				return nil, false, typeErr("%s() accepts 1 positional sub-pattern (%d given)", k.name, n)
			}
			return nil, true, nil
		}
		return nil, false, typeErr("%s() accepts 0 positional sub-patterns (%d given)", k.name, n)
	}
	t, ok := ma.(*tupleV)
	if !ok {
		return nil, false, typeErr("%s.__match_args__ must be a tuple (got %s)", k.name, typeName(ma))
	}
	if n > len(t.items) {
		plural := "s"
		if len(t.items) == 1 {
			plural = ""
		}
		return nil, false, typeErr("%s() accepts %d positional sub-pattern%s (%d given)", k.name, len(t.items), plural, n)
	}
	for _, it := range t.items[:n] {
		s, ok := it.(strV)
		if !ok {
			return nil, false, typeErr("__match_args__ elements must be strings (got %s)", typeName(it))
		}
		names = append(names, string(s))
	}
	return names, false, nil
}
