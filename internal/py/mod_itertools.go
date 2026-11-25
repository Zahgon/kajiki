// Ported from CPython 3.12 (Modules/itertoolsmodule.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"strconv"
)

func init() {
	register("itertools", func(b *builder) {
		b.iterClass("count", newCount, true, nil)
		b.iterClass("cycle", newCycle, false, nil)
		b.iterClass("repeat", newRepeat, true, nil)
		b.iterClass("accumulate", newAccumulate, false, nil)
		chainMs := &methods{}
		chainMs.classmethod("from_iterable", func(args []Value, kwargs []Keyword) (Value, error) {
			if err := ArgsOne("chain.from_iterable", args[1:], kwargs); err != nil {
				return nil, err
			}
			src, err := Iter(args[1])
			if err != nil {
				return nil, err
			}
			return instance(args[0], chainOf(src.Next))
		})
		b.iterClass("chain", newChain, false, chainMs)
		b.iterClass("compress", newCompress, false, nil)
		b.iterClass("dropwhile", predicateIter("dropwhile"), false, nil)
		b.iterClass("takewhile", predicateIter("takewhile"), false, nil)
		b.iterClass("filterfalse", predicateIter("filterfalse"), false, nil)
		grouper := b.iterClass("_grouper", noInstances("itertools._grouper"), false, nil)
		b.iterClass("groupby", func(args []Value, kwargs []Keyword) (*goIter, error) {
			return newGroupby(grouper, args, kwargs)
		}, false, nil)
		b.iterClass("islice", newIslice, false, nil)
		b.iterClass("starmap", newStarmap, false, nil)
		tee := b.iterClass("_tee", noInstances("itertools._tee"), false, nil)
		b.fn("tee", func(args []Value, kwargs []Keyword) (Value, error) { return teeFunc(tee, args, kwargs) })
		b.iterClass("zip_longest", newZipLongest, false, nil)
		b.iterClass("product", newProduct, false, nil)
		b.iterClass("permutations", newPermutations, false, nil)
		b.iterClass("combinations", combinationsIter("combinations", false), false, nil)
		b.iterClass("combinations_with_replacement", combinationsIter("combinations_with_replacement", true), false, nil)
		b.iterClass("pairwise", newPairwise, false, nil)
		b.iterClass("batched", newBatched, false, nil)
	})
}

// noInstances is the constructor of a helper class Python code cannot
// instantiate.
func noInstances(name string) func([]Value, []Keyword) (*goIter, error) {
	return func([]Value, []Keyword) (*goIter, error) {
		return nil, raise("TypeError", "cannot create '%s' instances", name)
	}
}

// fixedArgs checks a constructor that takes exactly n positional arguments
// and no keywords.
func fixedArgs(name string, n int, args []Value, kwargs []Keyword) error {
	if err := NoKeywords(name, kwargs); err != nil {
		return err
	}
	return CheckPositional(name, len(args), n, n)
}

// isCountNumber is PyNumber_Check.
func isCountNumber(v Value) bool {
	if IsNumber(v) {
		return true
	}
	if _, ok := AsComplex(v); ok {
		return true
	}
	for _, name := range []string{"__index__", "__int__", "__float__"} {
		if has, err := HasAttr(TypeOf(v), name); err == nil && has {
			return true
		}
	}
	return false
}

func newCount(args []Value, kwargs []Keyword) (*goIter, error) {
	buf, err := ParseArgs("count", []string{"start", "step"}, 0, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	cnt, step := Value(Int(0)), Value(Int(1))
	if buf[0] != nil {
		cnt = buf[0]
	}
	if buf[1] != nil {
		step = buf[1]
	}
	if !isCountNumber(cnt) || !isCountNumber(step) {
		return nil, raise("TypeError", "a number is required")
	}
	return &goIter{
		next: func() (Value, bool, error) {
			v := cnt
			n, err := BinaryOp("Add", cnt, step)
			if err != nil {
				return nil, false, err
			}
			cnt = n
			return v, true, nil
		},
		repr: func(cls string) (string, error) {
			c, err := repr(cnt)
			if err != nil {
				return "", err
			}
			if n, ok := AsInt(step); ok && n.IsInt64() && n.Int64() == 1 {
				return cls + "(" + c + ")", nil
			}
			s, err := repr(step)
			if err != nil {
				return "", err
			}
			return cls + "(" + c + ", " + s + ")", nil
		},
	}, nil
}

func newCycle(args []Value, kwargs []Keyword) (*goIter, error) {
	if err := fixedArgs("cycle", 1, args, kwargs); err != nil {
		return nil, err
	}
	src, err := Iter(args[0])
	if err != nil {
		return nil, err
	}
	var saved []Value
	firstPass := true
	i := 0
	return &goIter{next: func() (Value, bool, error) {
		if firstPass {
			v, ok, err := src.Next()
			if err != nil {
				return nil, false, err
			}
			if ok {
				saved = append(saved, v)
				return v, true, nil
			}
			firstPass = false
		}
		if len(saved) == 0 {
			return nil, false, nil
		}
		v := saved[i]
		i = (i + 1) % len(saved)
		return v, true, nil
	}}, nil
}

func newRepeat(args []Value, kwargs []Keyword) (*goIter, error) {
	buf, err := ParseArgs("repeat", []string{"object", "times"}, 1, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	obj := buf[0]
	cnt := -1
	if buf[1] != nil {
		if cnt, err = Ssize(buf[1]); err != nil {
			return nil, err
		}
		cnt = max(cnt, 0)
	}
	return &goIter{
		next: func() (Value, bool, error) {
			if cnt == 0 {
				return nil, false, nil
			}
			if cnt > 0 {
				cnt--
			}
			return obj, true, nil
		},
		repr: func(cls string) (string, error) {
			o, err := repr(obj)
			if err != nil {
				return "", err
			}
			if cnt == -1 {
				return cls + "(" + o + ")", nil
			}
			return cls + "(" + o + ", " + strconv.Itoa(cnt) + ")", nil
		},
	}, nil
}

func newAccumulate(args []Value, kwargs []Keyword) (*goIter, error) {
	buf, err := ParseArgs("accumulate", []string{"iterable", "func", "initial"}, 1, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	src, err := Iter(buf[0])
	if err != nil {
		return nil, err
	}
	f := buf[1]
	if f != nil && IsNone(f) {
		f = nil
	}
	var total Value
	initial := buf[2]
	if initial != nil && IsNone(initial) {
		initial = nil
	}
	return &goIter{next: func() (Value, bool, error) {
		if initial != nil {
			total, initial = initial, nil
			return total, true, nil
		}
		v, ok, err := src.Next()
		if err != nil || !ok {
			return nil, false, err
		}
		switch {
		case total == nil:
			total = v
		case f == nil:
			if total, err = BinaryOp("Add", total, v); err != nil {
				return nil, false, err
			}
		default:
			if total, err = call(f, total, v); err != nil {
				return nil, false, err
			}
		}
		return total, true, nil
	}}, nil
}

// chainOf is a chain over the iterables nextSource yields.
func chainOf(nextSource func() (Value, bool, error)) *goIter {
	var active *Iterator
	return &goIter{next: func() (Value, bool, error) {
		for {
			if active == nil {
				src, ok, err := nextSource()
				if err != nil || !ok {
					return nil, false, err
				}
				if active, err = Iter(src); err != nil {
					return nil, false, err
				}
			}
			v, ok, err := active.Next()
			if err != nil {
				return nil, false, err
			}
			if ok {
				return v, true, nil
			}
			active = nil
		}
	}}
}

func newChain(args []Value, kwargs []Keyword) (*goIter, error) {
	if err := NoKeywords("chain", kwargs); err != nil {
		return nil, err
	}
	i := 0
	return chainOf(func() (Value, bool, error) {
		if i >= len(args) {
			return nil, false, nil
		}
		i++
		return args[i-1], true, nil
	}), nil
}

func newCompress(args []Value, kwargs []Keyword) (*goIter, error) {
	buf, err := ParseArgs("compress", []string{"data", "selectors"}, 2, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	data, err := Iter(buf[0])
	if err != nil {
		return nil, err
	}
	sel, err := Iter(buf[1])
	if err != nil {
		return nil, err
	}
	return &goIter{next: func() (Value, bool, error) {
		for {
			d, ok, err := data.Next()
			if err != nil || !ok {
				return nil, false, err
			}
			s, ok, err := sel.Next()
			if err != nil || !ok {
				return nil, false, err
			}
			t, err := truth(s)
			if err != nil {
				return nil, false, err
			}
			if t {
				return d, true, nil
			}
		}
	}}, nil
}

// predicateIter is dropwhile, takewhile or filterfalse(predicate, iterable).
func predicateIter(name string) func(args []Value, kwargs []Keyword) (*goIter, error) {
	return func(args []Value, kwargs []Keyword) (*goIter, error) {
		if err := fixedArgs(name, 2, args, kwargs); err != nil {
			return nil, err
		}
		pred := args[0]
		src, err := Iter(args[1])
		if err != nil {
			return nil, err
		}
		test := func(v Value) (bool, error) {
			if name == "filterfalse" && IsNone(pred) {
				return truth(v)
			}
			r, err := call(pred, v)
			if err != nil {
				return false, err
			}
			return truth(r)
		}
		dropping, stopped := true, false
		return &goIter{next: func() (Value, bool, error) {
			for !stopped {
				v, ok, err := src.Next()
				if err != nil || !ok {
					return nil, false, err
				}
				switch name {
				case "dropwhile":
					if !dropping {
						return v, true, nil
					}
					t, err := test(v)
					if err != nil {
						return nil, false, err
					}
					if !t {
						dropping = false
						return v, true, nil
					}
				case "takewhile":
					t, err := test(v)
					if err != nil {
						return nil, false, err
					}
					if t {
						return v, true, nil
					}
					stopped = true
				default:
					t, err := test(v)
					if err != nil {
						return nil, false, err
					}
					if !t {
						return v, true, nil
					}
				}
			}
			return nil, false, nil
		}}, nil
	}
}

// groupbyState is shared by a groupby object and its groupers.
type groupbyState struct {
	src              *Iterator
	keyfunc          Value
	tgtkey, currkey  Value
	currvalue        Value
	currgrouper      *goIter
	hasTgt, hasCurrK bool
}

// step is groupby_step: the next value and its key; false at the end.
func (g *groupbyState) step() (bool, error) {
	v, ok, err := g.src.Next()
	if err != nil || !ok {
		return false, err
	}
	key := v
	if g.keyfunc != nil {
		if key, err = call(g.keyfunc, v); err != nil {
			return false, err
		}
	}
	g.currvalue, g.currkey, g.hasCurrK = v, key, true
	return true, nil
}

func newGroupby(grouperCls Value, args []Value, kwargs []Keyword) (*goIter, error) {
	buf, err := ParseArgs("groupby", []string{"iterable", "key"}, 1, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	src, err := Iter(buf[0])
	if err != nil {
		return nil, err
	}
	g := &groupbyState{src: src}
	if buf[1] != nil && !IsNone(buf[1]) {
		g.keyfunc = buf[1]
	}
	return &goIter{next: func() (Value, bool, error) {
		g.currgrouper = nil
		for {
			if g.hasCurrK {
				if !g.hasTgt {
					break
				}
				eq, err := Compare("==", g.tgtkey, g.currkey)
				if err != nil {
					return nil, false, err
				}
				if !eq {
					break
				}
			}
			ok, err := g.step()
			if err != nil || !ok {
				return nil, false, err
			}
		}
		g.tgtkey, g.hasTgt = g.currkey, true
		tgt := g.tgtkey
		gr := &goIter{}
		gr.next = func() (Value, bool, error) {
			if g.currgrouper != gr {
				return nil, false, nil
			}
			if g.currvalue == nil {
				ok, err := g.step()
				if err != nil || !ok {
					return nil, false, err
				}
			}
			eq, err := Compare("==", tgt, g.currkey)
			if err != nil || !eq {
				return nil, false, err
			}
			v := g.currvalue
			g.currvalue = nil
			return v, true, nil
		}
		g.currgrouper = gr
		inst, err := instance(grouperCls, gr)
		if err != nil {
			return nil, false, err
		}
		return Tuple(g.currkey, inst), true, nil
	}}, nil
}

func newIslice(args []Value, kwargs []Keyword) (*goIter, error) {
	if err := NoKeywords("islice", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("islice", len(args), 2, 4); err != nil {
		return nil, err
	}
	stopErr := raise("ValueError", "Stop argument for islice() must be None or an integer: 0 <= x <= sys.maxsize.")
	asSize := func(v Value) (int, bool) {
		n, err := Ssize(v)
		return n, err == nil
	}
	start, stop, step := 0, -1, 1
	if len(args) == 2 {
		if !IsNone(args[1]) {
			n, ok := asSize(args[1])
			if !ok || n == -1 {
				return nil, stopErr
			}
			stop = n
		}
	} else {
		if !IsNone(args[1]) {
			n, ok := asSize(args[1])
			if !ok {
				n = -1
			}
			start = n
		}
		if !IsNone(args[2]) {
			n, ok := asSize(args[2])
			if !ok || n == -1 {
				return nil, stopErr
			}
			stop = n
		}
	}
	if start < 0 || stop < -1 {
		return nil, raise("ValueError", "Indices for islice() must be None or an integer: 0 <= x <= sys.maxsize.")
	}
	if len(args) == 4 && !IsNone(args[3]) {
		n, ok := asSize(args[3])
		if !ok {
			n = -1
		}
		step = n
	}
	if step < 1 {
		return nil, raise("ValueError", "Step for islice() must be a positive integer or None.")
	}
	src, err := Iter(args[0])
	if err != nil {
		return nil, err
	}
	cnt, next := 0, start
	return &goIter{next: func() (Value, bool, error) {
		for cnt < next {
			_, ok, err := src.Next()
			if err != nil || !ok {
				return nil, false, err
			}
			cnt++
		}
		if stop != -1 && cnt >= stop {
			return nil, false, nil
		}
		v, ok, err := src.Next()
		if err != nil || !ok {
			return nil, false, err
		}
		cnt++
		old := next
		next += step
		if next < old || stop != -1 && next > stop {
			next = stop
		}
		return v, true, nil
	}}, nil
}

func newStarmap(args []Value, kwargs []Keyword) (*goIter, error) {
	if err := fixedArgs("starmap", 2, args, kwargs); err != nil {
		return nil, err
	}
	f := args[0]
	src, err := Iter(args[1])
	if err != nil {
		return nil, err
	}
	return &goIter{next: func() (Value, bool, error) {
		v, ok, err := src.Next()
		if err != nil || !ok {
			return nil, false, err
		}
		items, isTuple := SeqItems(v)
		if !isTuple || !IsTuple(v) {
			if items, err = listOf(v); err != nil {
				return nil, false, err
			}
		}
		r, err := Call(f, items, nil)
		return r, err == nil, err
	}}, nil
}

// teeBuffer holds the items a group of tee iterators has read and not all
// of them have consumed.
type teeBuffer struct {
	src   *Iterator
	items []Value
}

// teeLink is one tee iterator's position in the buffer.
type teeLink struct {
	buf *teeBuffer
	pos int
}

func (t *teeLink) iter() *goIter {
	return &goIter{next: func() (Value, bool, error) {
		if t.pos == len(t.buf.items) {
			v, ok, err := t.buf.src.Next()
			if err != nil || !ok {
				return nil, false, err
			}
			t.buf.items = append(t.buf.items, v)
		}
		v := t.buf.items[t.pos]
		t.pos++
		return v, true, nil
	}}
}

func teeFunc(teeCls Value, args []Value, kwargs []Keyword) (Value, error) {
	if err := NoKeywords("tee", kwargs); err != nil {
		return nil, err
	}
	if err := CheckPositional("tee", len(args), 1, 2); err != nil {
		return nil, err
	}
	n := 2
	if len(args) == 2 {
		var err error
		if n, err = Ssize(args[1]); err != nil {
			return nil, err
		}
	}
	if n < 0 {
		return nil, raise("ValueError", "n must be >= 0")
	}
	if n == 0 {
		return Tuple(), nil
	}
	var first *teeLink
	if st, ok := Native(args[0]).(*goIter); ok && Is(TypeOf(args[0]), teeCls) {
		link := st.data.(*teeLink)
		first = &teeLink{buf: link.buf, pos: link.pos}
	} else {
		src, err := Iter(args[0])
		if err != nil {
			return nil, err
		}
		first = &teeLink{buf: &teeBuffer{src: src}}
	}
	out := make([]Value, n)
	for i := range out {
		link := &teeLink{buf: first.buf, pos: first.pos}
		st := link.iter()
		st.data = link
		v, err := instance(teeCls, st)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return Tuple(out...), nil
}

func newZipLongest(args []Value, kwargs []Keyword) (*goIter, error) {
	var fill Value = None()
	for _, k := range kwargs {
		if k.Name != "fillvalue" {
			return nil, raise("TypeError", "zip_longest() got an unexpected keyword argument")
		}
		fill = k.Value
	}
	srcs := make([]*Iterator, len(args))
	for i, a := range args {
		it, err := Iter(a)
		if err != nil {
			return nil, err
		}
		srcs[i] = it
	}
	active := len(srcs)
	done := make([]bool, len(srcs))
	return &goIter{next: func() (Value, bool, error) {
		if len(srcs) == 0 || active == 0 {
			return nil, false, nil
		}
		items := make([]Value, len(srcs))
		for i, it := range srcs {
			if done[i] {
				items[i] = fill
				continue
			}
			v, ok, err := it.Next()
			if err != nil {
				return nil, false, err
			}
			if !ok {
				done[i] = true
				active--
				if active == 0 {
					return nil, false, nil
				}
				v = fill
			}
			items[i] = v
		}
		return Tuple(items...), true, nil
	}}, nil
}

// indexIter yields tuples of pool items picked by indices, which advance
// returns the next combination of, false at the end.
func indexIter(pool []Value, indices []int, empty bool, advance func() bool) *goIter {
	first := true
	return &goIter{next: func() (Value, bool, error) {
		if empty {
			return nil, false, nil
		}
		if !first && !advance() {
			empty = true
			return nil, false, nil
		}
		first = false
		out := make([]Value, len(indices))
		for i, j := range indices {
			out[i] = pool[j]
		}
		return Tuple(out...), true, nil
	}}
}

func newProduct(args []Value, kwargs []Keyword) (*goIter, error) {
	repeat := 1
	for _, k := range kwargs {
		if k.Name != "repeat" {
			return nil, raise("TypeError", "'%s' is an invalid keyword argument for product()", k.Name)
		}
		var err error
		if repeat, err = Ssize(k.Value); err != nil {
			return nil, err
		}
	}
	if repeat < 0 {
		return nil, raise("ValueError", "repeat argument cannot be negative")
	}
	var pools [][]Value
	for _, a := range args {
		items, err := listOf(a)
		if err != nil {
			return nil, err
		}
		pools = append(pools, items)
	}
	var all [][]Value
	for r := 0; r < repeat; r++ {
		all = append(all, pools...)
	}
	indices := make([]int, len(all))
	empty := false
	for _, p := range all {
		if len(p) == 0 {
			empty = true
		}
	}
	first := true
	return &goIter{next: func() (Value, bool, error) {
		if empty {
			return nil, false, nil
		}
		if !first {
			i := len(indices) - 1
			for ; i >= 0; i-- {
				indices[i]++
				if indices[i] < len(all[i]) {
					break
				}
				indices[i] = 0
			}
			if i < 0 {
				empty = true
				return nil, false, nil
			}
		}
		first = false
		out := make([]Value, len(indices))
		for i, j := range indices {
			out[i] = all[i][j]
		}
		return Tuple(out...), true, nil
	}}, nil
}

func newPermutations(args []Value, kwargs []Keyword) (*goIter, error) {
	buf, err := ParseArgs("permutations", []string{"iterable", "r"}, 1, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	pool, err := listOf(buf[0])
	if err != nil {
		return nil, err
	}
	n := len(pool)
	r := n
	if buf[1] != nil && !IsNone(buf[1]) {
		rv, ok := AsInt(buf[1])
		if !ok {
			return nil, raise("TypeError", "Expected int as r")
		}
		if rv.Sign() < 0 {
			return nil, raise("ValueError", "r must be non-negative")
		}
		if !rv.IsInt64() || rv.Int64() > int64(n) {
			r = n + 1
		} else {
			r = int(rv.Int64())
		}
	}
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}
	cycles := make([]int, r)
	for i := range cycles {
		cycles[i] = n - i
	}
	picked := make([]int, max(r, 0))
	copy(picked, indices)
	empty := r > n
	return indexIter(pool, picked, empty, func() bool {
		for i := r - 1; i >= 0; i-- {
			cycles[i]--
			if cycles[i] == 0 {
				idx := indices[i]
				copy(indices[i:], indices[i+1:])
				indices[n-1] = idx
				cycles[i] = n - i
			} else {
				j := cycles[i]
				indices[i], indices[n-j] = indices[n-j], indices[i]
				copy(picked, indices[:r])
				return true
			}
		}
		return false
	}), nil
}

// combinationsIter is combinations or combinations_with_replacement
// (iterable, r).
func combinationsIter(name string, replace bool) func(args []Value, kwargs []Keyword) (*goIter, error) {
	return func(args []Value, kwargs []Keyword) (*goIter, error) {
		buf, err := ParseArgs(name, []string{"iterable", "r"}, 2, 2, args, kwargs)
		if err != nil {
			return nil, err
		}
		pool, err := listOf(buf[0])
		if err != nil {
			return nil, err
		}
		r, err := Ssize(buf[1])
		if err != nil {
			return nil, err
		}
		if r < 0 {
			return nil, raise("ValueError", "r must be non-negative")
		}
		n := len(pool)
		indices := make([]int, r)
		if !replace {
			for i := range indices {
				indices[i] = i
			}
		}
		empty := !replace && r > n || replace && n == 0 && r > 0
		return indexIter(pool, indices, empty, func() bool {
			i := r - 1
			if replace {
				for ; i >= 0 && indices[i] == n-1; i-- {
				}
				if i < 0 {
					return false
				}
				v := indices[i] + 1
				for j := i; j < r; j++ {
					indices[j] = v
				}
				return true
			}
			for ; i >= 0 && indices[i] == i+n-r; i-- {
			}
			if i < 0 {
				return false
			}
			indices[i]++
			for j := i + 1; j < r; j++ {
				indices[j] = indices[j-1] + 1
			}
			return true
		}), nil
	}
}

func newPairwise(args []Value, kwargs []Keyword) (*goIter, error) {
	if err := fixedArgs("pairwise", 1, args, kwargs); err != nil {
		return nil, err
	}
	src, err := Iter(args[0])
	if err != nil {
		return nil, err
	}
	var old Value
	return &goIter{next: func() (Value, bool, error) {
		if old == nil {
			v, ok, err := src.Next()
			if err != nil || !ok {
				return nil, false, err
			}
			old = v
		}
		v, ok, err := src.Next()
		if err != nil || !ok {
			return nil, false, err
		}
		pair := Tuple(old, v)
		old = v
		return pair, true, nil
	}}, nil
}

func newBatched(args []Value, kwargs []Keyword) (*goIter, error) {
	buf, err := ParseArgs("batched", []string{"iterable", "n"}, 2, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	n, err := Ssize(buf[1])
	if err != nil {
		return nil, err
	}
	if n < 1 {
		return nil, raise("ValueError", "n must be at least one")
	}
	src, err := Iter(buf[0])
	if err != nil {
		return nil, err
	}
	return &goIter{next: func() (Value, bool, error) {
		var items []Value
		for len(items) < n {
			v, ok, err := src.Next()
			if err != nil {
				return nil, false, err
			}
			if !ok {
				break
			}
			items = append(items, v)
		}
		if len(items) == 0 {
			return nil, false, nil
		}
		return Tuple(items...), true, nil
	}}, nil
}
