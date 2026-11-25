// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// dict, set and frozenset. A dict keeps insertion order; a set keeps CPython 3.12's open-addressing
// table (setobject.c), so that it iterates, prints and pops its elements in CPython's order.

package py

// ---------------------------------------------------------------- dict

// dictV is an insertion-ordered dict. Keys are found as CPython finds
// them: by hash (hashes holds each key's), then by identity, then by ==
// with the stored key on the left, so keys of classes defining __eq__ and
// __hash__ and equal numbers of different types (1, 1.0, True) share an
// entry. index maps a hash to the positions of the keys that have it.
type dictV struct {
	keys   []Value
	vals   []Value
	hashes []int64
	index  map[int64][]int
}

func newDict() *dictV { return &dictV{index: map[int64][]int{}} }

func (*dictV) pyType() string { return "dict" }

// lookup returns the position of key, whose hash is h, or -1.
func (d *dictV) lookup(key Value, h int64) (int, error) {
	for _, i := range d.index[h] {
		k := d.keys[i]
		if sameObject(k, key) {
			return i, nil
		}
		eq, err := pyEq(k, key)
		if err != nil {
			return -1, err
		}
		if eq {
			return i, nil
		}
	}
	return -1, nil
}

func (d *dictV) set(key, value Value) error {
	h, err := pyHash(key)
	if err != nil {
		return err
	}
	return d.setHashed(key, h, value)
}

// setStr is set for a str key; it compares with the str keys only, so it
// cannot fail.
func (d *dictV) setStr(key string, value Value) {
	h := strHash(key)
	for _, i := range d.index[h] {
		if k, ok := unsub(d.keys[i]).(strV); ok && string(k) == key {
			d.vals[i] = value
			return
		}
	}
	d.index[h] = append(d.index[h], len(d.keys))
	d.keys = append(d.keys, strV(key))
	d.vals = append(d.vals, value)
	d.hashes = append(d.hashes, h)
}

func (d *dictV) setHashed(key Value, h int64, value Value) error {
	i, err := d.lookup(key, h)
	if err != nil {
		return err
	}
	if i >= 0 {
		d.vals[i] = value
		return nil
	}
	d.index[h] = append(d.index[h], len(d.keys))
	d.keys = append(d.keys, key)
	d.vals = append(d.vals, value)
	d.hashes = append(d.hashes, h)
	return nil
}

func (d *dictV) get(key Value) (Value, bool, error) {
	h, err := pyHash(key)
	if err != nil {
		return nil, false, err
	}
	i, err := d.lookup(key, h)
	if err != nil || i < 0 {
		return nil, false, err
	}
	return d.vals[i], true, nil
}

func (d *dictV) getStr(key string) (Value, bool) {
	h := strHash(key)
	for _, i := range d.index[h] {
		if k, ok := unsub(d.keys[i]).(strV); ok && string(k) == key {
			return d.vals[i], true
		}
	}
	return nil, false
}

// pop removes key and returns its value, if the dict has it.
func (d *dictV) pop(key Value) (Value, bool, error) {
	h, err := pyHash(key)
	if err != nil {
		return nil, false, err
	}
	i, err := d.lookup(key, h)
	if err != nil || i < 0 {
		return nil, false, err
	}
	v := d.vals[i]
	d.removeAt(i)
	return v, true, nil
}

func (d *dictV) del(key Value) (bool, error) {
	_, found, err := d.pop(key)
	return found, err
}

// delStr is del for a str key.
func (d *dictV) delStr(key string) {
	h := strHash(key)
	for _, i := range d.index[h] {
		if k, ok := unsub(d.keys[i]).(strV); ok && string(k) == key {
			d.removeAt(i)
			return
		}
	}
}

// removeAt removes the item at position i, keeping the order of the others.
func (d *dictV) removeAt(i int) {
	d.keys = append(d.keys[:i:i], d.keys[i+1:]...)
	d.vals = append(d.vals[:i:i], d.vals[i+1:]...)
	d.hashes = append(d.hashes[:i:i], d.hashes[i+1:]...)
	d.reindex()
}

// reindex rebuilds index from hashes.
func (d *dictV) reindex() {
	d.index = make(map[int64][]int, len(d.hashes))
	for j, h := range d.hashes {
		d.index[h] = append(d.index[h], j)
	}
}

func (d *dictV) clear() { d.keys, d.vals, d.hashes, d.index = nil, nil, nil, map[int64][]int{} }

func (d *dictV) copy() *dictV {
	n := &dictV{keys: append([]Value(nil), d.keys...), vals: append([]Value(nil), d.vals...),
		hashes: append([]int64(nil), d.hashes...)}
	n.reindex()
	return n
}

// ---------------------------------------------------------------- set

const (
	setMinSize      = 8
	setLinearProbes = 9
	setPerturbShift = 5
)

type setEntry struct {
	key   Value // nil for an unused slot
	hash  int64
	dummy bool // a deleted slot
}

func (e setEntry) live() bool { return e.key != nil && !e.dummy }

// setV is a set, or a frozenset when frozen is true.
type setV struct {
	table  []setEntry
	fill   int // live and dummy slots
	used   int // live slots
	finger int
	frozen bool
}

func newSet(frozen bool) *setV {
	return &setV{table: make([]setEntry, setMinSize), frozen: frozen}
}

func (s *setV) pyType() string {
	if s.frozen {
		return "frozenset"
	}
	return "set"
}

func (s *setV) mask() int { return len(s.table) - 1 }

// items returns the elements in table order.
func (s *setV) items() []Value {
	out := make([]Value, 0, s.used)
	for _, e := range s.table {
		if e.live() {
			out = append(out, e.key)
		}
	}
	return out
}

func (s *setV) length() (int, error) { return s.used, nil }

func (s *setV) iter() (iterator, error) {
	size, i := s.used, 0
	return funcIter(func() (Value, bool, error) {
		if s.used != size {
			size = -1
			return nil, false, newPyErr("RuntimeError", "Set changed size during iteration")
		}
		for i < len(s.table) {
			e := s.table[i]
			i++
			if e.live() {
				return e.key, true, nil
			}
		}
		return nil, false, nil
	}), nil
}

// find returns the slot of key, or -1.
func (s *setV) find(key Value, hash int64) (int, error) {
	mask := s.mask()
	perturb := uint64(hash)
	i := int(uint64(hash) & uint64(mask))
	for {
		probes := 0
		if i+setLinearProbes <= mask {
			probes = setLinearProbes
		}
		j := i
		for {
			e := s.table[j]
			if e.key == nil && !e.dummy {
				return -1, nil
			}
			if e.live() && e.hash == hash {
				if sameObject(e.key, key) {
					return j, nil
				}
				eq, err := pyEq(e.key, key)
				if err != nil {
					return -1, err
				}
				if eq {
					return j, nil
				}
			}
			if probes == 0 {
				break
			}
			probes--
			j++
		}
		perturb >>= setPerturbShift
		i = int((uint64(i)*5 + 1 + perturb) & uint64(mask))
	}
}

func (s *setV) addHashed(key Value, hash int64) error {
	mask := s.mask()
	perturb := uint64(hash)
	i := int(uint64(hash) & uint64(mask))
	free := -1 // the first dummy slot passed, which an insertion reuses
	for {
		probes := 0
		if i+setLinearProbes <= mask {
			probes = setLinearProbes
		}
		j := i
		for {
			e := &s.table[j]
			if e.key == nil && !e.dummy {
				if free >= 0 {
					s.used++
					s.table[free] = setEntry{key: key, hash: hash}
					return nil
				}
				s.fill++
				s.used++
				e.key, e.hash = key, hash
				if s.fill*5 < mask*3 {
					return nil
				}
				if s.used > 50000 {
					s.resize(s.used * 2)
				} else {
					s.resize(s.used * 4)
				}
				return nil
			}
			if e.live() && e.hash == hash {
				if sameObject(e.key, key) {
					return nil
				}
				eq, err := pyEq(e.key, key)
				if err != nil {
					return err
				}
				if eq {
					return nil
				}
			} else if e.dummy && free < 0 {
				free = j
			}
			if probes == 0 {
				break
			}
			probes--
			j++
		}
		perturb >>= setPerturbShift
		i = int((uint64(i)*5 + 1 + perturb) & uint64(mask))
	}
}

func (s *setV) add(key Value) error {
	h, err := pyHash(key)
	if err != nil {
		return err
	}
	return s.addHashed(key, h)
}

func insertClean(table []setEntry, key Value, hash int64) {
	mask := len(table) - 1
	perturb := uint64(hash)
	i := int(uint64(hash) & uint64(mask))
	for {
		if table[i].key == nil {
			table[i] = setEntry{key: key, hash: hash}
			return
		}
		if i+setLinearProbes <= mask {
			for j := 1; j <= setLinearProbes; j++ {
				if table[i+j].key == nil {
					table[i+j] = setEntry{key: key, hash: hash}
					return
				}
			}
		}
		perturb >>= setPerturbShift
		i = int((uint64(i)*5 + 1 + perturb) & uint64(mask))
	}
}

func (s *setV) resize(minused int) {
	size := setMinSize
	for size <= minused {
		size <<= 1
	}
	old := s.table
	s.table = make([]setEntry, size)
	for _, e := range old {
		if e.live() {
			insertClean(s.table, e.key, e.hash)
		}
	}
	s.fill = s.used
}

// discard removes key and reports whether it was present.
func (s *setV) discard(key Value) (bool, error) {
	h, err := pyHash(key)
	if err != nil {
		return false, err
	}
	j, err := s.find(key, h)
	if err != nil || j < 0 {
		return false, err
	}
	s.table[j] = setEntry{hash: -1, dummy: true}
	s.used--
	return true, nil
}

func (s *setV) contains(key Value) (bool, error) {
	h, err := pyHash(key)
	if err != nil {
		if sk, ok := key.(*setV); ok && !sk.frozen {
			// set.__contains__ retries an unhashable set as a frozenset.
			fk := sk.copyAs(true)
			j, err := s.find(fk, frozensetHash(fk))
			return j >= 0, err
		}
		return false, err
	}
	j, err := s.find(key, h)
	return j >= 0, err
}

func (s *setV) clear() {
	s.table = make([]setEntry, setMinSize)
	s.fill, s.used, s.finger = 0, 0, 0
}

// fillFrom is set_merge into the empty set s, which never compares
// elements and so cannot fail.
func (s *setV) fillFrom(other *setV) {
	if other.used == 0 {
		return
	}
	if other.used*5 >= s.mask()*3 {
		s.resize(other.used * 2)
	}
	if s.mask() == other.mask() && other.fill == other.used {
		copy(s.table, other.table)
		s.fill, s.used = other.fill, other.used
		return
	}
	for _, e := range other.table {
		if e.live() {
			insertClean(s.table, e.key, e.hash)
		}
	}
	s.fill, s.used = other.used, other.used
}

// merge is set_merge: other's elements added to s.
func (s *setV) merge(other *setV) error {
	if other == s || other.used == 0 {
		return nil
	}
	if s.fill == 0 {
		s.fillFrom(other)
		return nil
	}
	if (s.fill+other.used)*5 >= s.mask()*3 {
		s.resize((s.used + other.used) * 2)
	}
	for _, e := range other.table {
		if e.live() {
			if err := s.addHashed(e.key, e.hash); err != nil {
				return err
			}
		}
	}
	return nil
}

// update is set_update_internal: the elements of any iterable added to s.
func (s *setV) update(v Value) error {
	switch o := v.(type) {
	case *setV:
		return s.merge(o)
	case *dictV:
		if (s.fill+len(o.keys))*5 >= s.mask()*3 {
			s.resize((s.used + len(o.keys)) * 2)
		}
		for _, k := range o.keys {
			if err := s.add(k); err != nil {
				return err
			}
		}
		return nil
	}
	it, err := pyIter(v)
	if err != nil {
		return err
	}
	for {
		item, ok, err := it.next()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := s.add(item); err != nil {
			return err
		}
	}
}

// ConstSetDisplay is the set a display of more than two constants, such as
// {'a', 'b', 'c'}, evaluates to. The compiler folds the elements into a
// frozenset constant, which merging the code object's constants rebuilds from
// the frozenset's own iteration order; BUILD_SET and SET_UPDATE then merge
// that frozenset into an empty set. Each step can reorder the table, so the
// result iterates differently from adding the elements one by one.
func ConstSetDisplay(items []Value) (Value, error) {
	folded, err := ConstFrozenset(items)
	if err != nil {
		return nil, err
	}
	s := newSet(false)
	s.fillFrom(folded.(*setV))
	return s, nil
}

// ConstFrozenset is the frozenset constant the compiler makes of a set
// display of constants (the iterable of a for loop, the right operand of in):
// built from the elements, then rebuilt from its own iteration order.
func ConstFrozenset(items []Value) (Value, error) {
	first := newSet(true)
	for _, it := range items {
		if err := first.add(it); err != nil {
			return nil, err
		}
	}
	if first.used == 0 {
		return first, nil
	}
	second := newSet(true)
	for _, it := range first.items() {
		if err := second.add(it); err != nil {
			return nil, err
		}
	}
	return second, nil
}

// copyAs is set_copy (make_new_set over s).
func (s *setV) copyAs(frozen bool) *setV {
	n := newSet(frozen)
	n.fillFrom(s)
	return n
}

// pop is set.pop(): the first live slot from the finger on.
func (s *setV) pop() (Value, error) {
	if s.used == 0 {
		return nil, newExc("KeyError", []Value{strV("pop from an empty set")})
	}
	i := s.finger & s.mask()
	for !s.table[i].live() {
		i++
		if i > s.mask() {
			i = 0
		}
	}
	key := s.table[i].key
	s.table[i] = setEntry{hash: -1, dummy: true}
	s.used--
	s.finger = i + 1
	return key, nil
}

func toSet(v Value) (*setV, error) {
	if s, ok := v.(*setV); ok {
		return s, nil
	}
	s := newSet(true)
	if err := s.update(v); err != nil {
		return nil, err
	}
	return s, nil
}

// setUnion is set_or / set.union.
func setUnion(a *setV, others []Value, frozen bool) (*setV, error) {
	r := a.copyAs(frozen)
	for _, o := range others {
		if err := r.update(o); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// setIntersection is set_intersection for one other operand.
func setIntersection(a *setV, other Value, frozen bool) (*setV, error) {
	r := newSet(frozen)
	if o, ok := other.(*setV); ok {
		big, small := a, o
		if o.used > a.used {
			big, small = o, a
		}
		for _, e := range small.table {
			if !e.live() {
				continue
			}
			j, err := big.find(e.key, e.hash)
			if err != nil {
				return nil, err
			}
			if j >= 0 {
				if err := r.addHashed(e.key, e.hash); err != nil {
					return nil, err
				}
			}
		}
		return r, nil
	}
	it, err := pyIter(other)
	if err != nil {
		return nil, err
	}
	for {
		item, ok, err := it.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return r, nil
		}
		h, err := pyHash(item)
		if err != nil {
			return nil, err
		}
		j, err := a.find(item, h)
		if err != nil {
			return nil, err
		}
		if j >= 0 {
			if err := r.addHashed(item, h); err != nil {
				return nil, err
			}
		}
	}
}

// setDifference is set_difference for one other operand.
func setDifference(a *setV, other Value, frozen bool) (*setV, error) {
	otherSize := 0
	switch o := other.(type) {
	case *setV:
		otherSize = o.used
	case *dictV:
		otherSize = len(o.keys)
	default:
		r := a.copyAs(frozen)
		it, err := pyIter(other)
		if err != nil {
			return nil, err
		}
		for {
			item, ok, err := it.next()
			if err != nil {
				return nil, err
			}
			if !ok {
				return r, nil
			}
			if _, err := r.discard(item); err != nil {
				return nil, err
			}
		}
	}
	if a.used>>2 > otherSize {
		r := a.copyAs(frozen)
		os, err := toSet(other)
		if err != nil {
			return nil, err
		}
		for _, e := range os.table {
			if e.live() {
				if _, err := r.discard(e.key); err != nil {
					return nil, err
				}
			}
		}
		return r, nil
	}
	r := newSet(frozen)
	for _, e := range a.table {
		if !e.live() {
			continue
		}
		in, err := pyContains(other, e.key)
		if err != nil {
			return nil, err
		}
		if !in {
			if err := r.addHashed(e.key, e.hash); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

// setSymmetricDifference is set_symmetric_difference: a copy of other with
// each element of a toggled in it.
func setSymmetricDifference(a *setV, other Value, frozen bool) (*setV, error) {
	r := newSet(frozen)
	if err := r.update(other); err != nil {
		return nil, err
	}
	for _, e := range a.table {
		if !e.live() {
			continue
		}
		found, err := r.discard(e.key)
		if err != nil {
			return nil, err
		}
		if !found {
			if err := r.addHashed(e.key, e.hash); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

func (s *setV) subsetOf(o *setV) (bool, error) {
	if s.used > o.used {
		return false, nil
	}
	for _, e := range s.table {
		if e.live() {
			j, err := o.find(e.key, e.hash)
			if err != nil || j < 0 {
				return false, err
			}
		}
	}
	return true, nil
}

func (s *setV) eq(other Value) (bool, error) {
	o, ok := other.(*setV)
	if !ok {
		return false, nil
	}
	if s.used != o.used {
		return false, nil
	}
	return s.subsetOf(o)
}

// setCompare is set_richcompare for the ordering operators: subset and
// superset tests.
func setCompare(op string, a, b *setV) (bool, error) {
	switch op {
	case "<":
		if a.used >= b.used {
			return false, nil
		}
		return a.subsetOf(b)
	case "<=":
		return a.subsetOf(b)
	case ">":
		if b.used >= a.used {
			return false, nil
		}
		return b.subsetOf(a)
	}
	return b.subsetOf(a)
}

func (s *setV) repr(st *reprState) (string, error) { return s.reprAs(s.pyType(), !s.frozen, st) }

// reprAs is the repr of the set as an instance of the class named name: the
// braces alone for a non-empty set when bare (an instance of set itself),
// name(...) otherwise.
func (s *setV) reprAs(name string, bare bool, st *reprState) (string, error) {
	items := s.items()
	if len(items) == 0 {
		return name + "()", nil
	}
	if !st.enter(s) {
		return name + "(...)", nil
	}
	defer st.leave()
	body, err := seqRepr("{", "}", items, false, st)
	if err != nil || bare {
		return body, err
	}
	return name + "(" + body + ")", nil
}

// ---------------------------------------------------------------- dict views as sets

// isSetView reports whether v is a dict_keys or dict_items view, which
// support the set operators.
func isSetView(v Value) bool {
	dv, ok := v.(*dictViewV)
	return ok && dv.kind != "values"
}

// viewToSet is dictviews_to_set: a new set of the elements of a view (the
// keys of its dict for a keys view) or of any iterable.
func viewToSet(v Value) (*setV, error) {
	s := newSet(false)
	if dv, ok := v.(*dictViewV); ok && dv.kind == "keys" {
		v = dv.d
	}
	if err := s.update(v); err != nil {
		return nil, err
	}
	return s, nil
}

// dictViewSetOp is a set operator with a dict view operand (dictviews_and,
// dictviews_or, dictviews_sub and dictviews_xor): the result is a set.
func dictViewSetOp(op string, a, b Value) (Value, error) {
	if op == "&" {
		return dictViewIntersect(a, b)
	}
	r, err := viewToSet(a)
	if err != nil {
		return nil, err
	}
	switch op {
	case "|":
		err = r.update(b)
	case "-":
		var d *setV
		if d, err = setDifference(r, b, false); err == nil {
			r = d
		}
	default:
		var x *setV
		if x, err = setSymmetricDifference(r, b, false); err == nil {
			r = x
		}
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// dictViewIntersect is _PyDictView_Intersect: the elements of the smaller
// operand that the view contains.
func dictViewIntersect(a, b Value) (Value, error) {
	self, other := a, b
	if !isSetView(self) {
		self, other = other, self
	}
	view := self.(*dictViewV)
	if os, ok := other.(*setV); ok && !os.frozen && len(view.d.keys) <= os.used {
		return setIntersection(os, view, false)
	}
	if ov, ok := other.(*dictViewV); ok && isSetView(ov) && len(ov.d.keys) > len(view.d.keys) {
		view, other = ov, view
	}
	r := newSet(false)
	it, err := pyIter(other)
	if err != nil {
		return nil, err
	}
	for {
		item, ok, err := it.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return r, nil
		}
		in, err := view.contains(item)
		if err != nil {
			return nil, err
		}
		if in {
			if err := r.add(item); err != nil {
				return nil, err
			}
		}
	}
}

// eq is dictview_richcompare for ==: a keys or items view equals a set or
// another such view with the same elements; a values view only itself.
func (v *dictViewV) eq(other Value) (bool, error) {
	if v.kind == "values" {
		return v == other, nil
	}
	var olen int
	switch o := unsub(other).(type) {
	case *setV:
		olen = o.used
	case *dictViewV:
		if o.kind == "values" {
			return false, nil
		}
		olen = len(o.d.keys)
	default:
		return false, nil
	}
	if olen != len(v.d.keys) {
		return false, nil
	}
	it, err := pyIter(v)
	if err != nil {
		return false, err
	}
	for {
		item, ok, err := it.next()
		if err != nil || !ok {
			return err == nil, err
		}
		in, err := pyContains(unsub(other), item)
		if err != nil || !in {
			return false, err
		}
	}
}
