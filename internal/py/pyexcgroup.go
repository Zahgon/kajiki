// Ported from CPython 3.12 (Objects/exceptions.c BaseExceptionGroup, Python/ceval.c
// _PyExc_PrepReraiseStar and the except* opcodes) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Exception groups (BaseExceptionGroup and ExceptionGroup: construction, str, split, subgroup,
// derive) and the try statement with except* clauses.

package py

func init() {
	// ExceptionGroup derives from both BaseExceptionGroup and Exception.
	eg, beg := classes["ExceptionGroup"], classes["BaseExceptionGroup"]
	exc, base := classes["Exception"], classes["BaseException"]
	eg.bases = []*classV{beg, exc}
	eg.mro = []*classV{eg, beg, exc, base, objectClass}
}

// isExcGroup reports whether e is an exception group.
func isExcGroup(e *excV) bool {
	return excClass(e).subclassOf(classes["BaseExceptionGroup"])
}

// groupExceptions is e.exceptions as a slice.
func groupExceptions(e *excV) []*excV {
	t, _ := e.field("exceptions").(*tupleV)
	if t == nil {
		return nil
	}
	out := make([]*excV, len(t.items))
	for i, it := range t.items {
		out[i] = it.(*excV)
	}
	return out
}

// newExcGroup is BaseExceptionGroup.__new__(cls, message, exceptions): the
// checks of the arguments, and ExceptionGroup in place of
// BaseExceptionGroup when every exception is an Exception.
func newExcGroup(cls *classV, args []Value, kw *dictV) (*excV, error) {
	if kw != nil && kw.Len() > 0 {
		return nil, typeErr("BaseExceptionGroup.__new__() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, typeErr("BaseExceptionGroup.__new__() takes exactly 2 arguments (%d given)", len(args))
	}
	if _, ok := unsub(args[0]).(strV); !ok {
		return nil, typeErr("BaseExceptionGroup.__new__() argument 1 must be str, not %s", typeName(args[0]))
	}
	if !isSequence(args[1]) {
		return nil, typeErr("second argument (exceptions) must be a sequence")
	}
	items, err := toSlice(args[1])
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, valueErr("second argument (exceptions) must be a non-empty sequence")
	}
	allExc := true
	for i, it := range items {
		x, ok := it.(*excV)
		if !ok {
			return nil, valueErr("Item %d of second argument (exceptions) is not an exception", i)
		}
		if !excIsInstance(x, "Exception") {
			allExc = false
		}
	}
	beg, eg := classes["BaseExceptionGroup"], classes["ExceptionGroup"]
	switch {
	case cls == beg && allExc:
		cls = eg
	case cls == eg && !allExc:
		return nil, typeErr("Cannot nest BaseExceptions in an ExceptionGroup")
	case cls != beg && cls != eg && !allExc && cls.subclassOf(classes["Exception"]):
		return nil, typeErr("Cannot nest BaseExceptions in '%s'", cls.name)
	}
	e := &excV{Class: cls.name, args: []Value{args[0], args[1]}}
	if cls.isUser() {
		e.cls = cls
	}
	e.setField("message", args[0])
	e.setField("exceptions", newTuple(items))
	e.Message = groupStr(e)
	return e, nil
}

// groupStr is BaseExceptionGroup.__str__: "message (n sub-exceptions)".
func groupStr(e *excV) string {
	msg, _ := unsub(e.field("message")).(strV)
	n := len(groupExceptions(e))
	suffix := "s"
	if n == 1 {
		suffix = ""
	}
	return string(msg) + " (" + itoa(n) + " sub-exception" + suffix + ")"
}

// deriveGroup is e.derive(excs) with __traceback__, __cause__, __context__
// and __notes__ copied, as split and subgroup make their parts.
func deriveGroup(e *excV, excs []*excV) (*excV, error) {
	vals := make([]Value, len(excs))
	for i, x := range excs {
		vals[i] = x
	}
	var d Value
	var err error
	if m, ok := specialOf(e, "derive"); ok {
		d, err = callValue(nil, m, []Value{newList(vals)}, nil)
	} else {
		d, err = newExcGroup(classes["BaseExceptionGroup"], []Value{e.field("message"), newList(vals)}, nil)
	}
	if err != nil {
		return nil, err
	}
	g, ok := d.(*excV)
	if !ok || !isExcGroup(g) {
		return nil, typeErr("derive must return an instance of BaseExceptionGroup")
	}
	g.tb = append([]tbEntry(nil), e.tb...)
	g.cause, g.context, g.suppressContext = e.cause, e.context, e.suppressContext
	if notes, ok := e.attrDict().getStr("__notes__"); ok {
		g.attrDict().setStr("__notes__", notes)
	}
	return g, nil
}

// splitCondition is the condition split and subgroup take: an exception
// class, a tuple of them, or a predicate.
func splitCondition(v Value) (func(*excV) (bool, error), error) {
	if k, ok := v.(*classV); ok && isExcClass(k) {
		return func(e *excV) (bool, error) { return excClass(e).subclassOf(k), nil }, nil
	}
	if t, ok := v.(*tupleV); ok {
		for _, it := range t.items {
			if k, ok := it.(*classV); !ok || !isExcClass(k) {
				return nil, typeErr("expected an exception type, a tuple of exception types, or a callable (other than a class)")
			}
		}
		return func(e *excV) (bool, error) { return excMatches(e, t) }, nil
	}
	if _, isClass := v.(*classV); !isClass && isCallable(v) {
		return func(e *excV) (bool, error) {
			r, err := callValue(nil, v, []Value{e}, nil)
			if err != nil {
				return false, err
			}
			return truthy(r)
		}, nil
	}
	return nil, typeErr("expected an exception type, a tuple of exception types, or a callable (other than a class)")
}

// splitGroup is e.split(cond): the part of the group (and its nested
// groups) whose leaves match, and the rest; each nil when empty.
func splitGroup(e *excV, cond func(*excV) (bool, error)) (match, rest *excV, err error) {
	whole, err := cond(e)
	if err != nil {
		return nil, nil, err
	}
	if whole {
		return e, nil, nil
	}
	var ms, rs []*excV
	for _, x := range groupExceptions(e) {
		if isExcGroup(x) {
			m, r, err := splitGroup(x, cond)
			if err != nil {
				return nil, nil, err
			}
			if m != nil {
				ms = append(ms, m)
			}
			if r != nil {
				rs = append(rs, r)
			}
			continue
		}
		ok, err := cond(x)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			ms = append(ms, x)
		} else {
			rs = append(rs, x)
		}
	}
	if len(ms) > 0 {
		if match, err = deriveGroup(e, ms); err != nil {
			return nil, nil, err
		}
	}
	if len(rs) > 0 {
		if rest, err = deriveGroup(e, rs); err != nil {
			return nil, nil, err
		}
	}
	return match, rest, nil
}

// excGroupAttr is an attribute exception groups add: split, subgroup,
// derive.
func excGroupAttr(e *excV, name string) (Value, bool) {
	if !isExcGroup(e) {
		return nil, false
	}
	optional := func(x *excV) Value {
		if x == nil {
			return none
		}
		return x
	}
	switch name {
	case "split", "subgroup":
		return methodsBound(e, name, func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 1, 1, args, kw); err != nil {
				return nil, err
			}
			cond, err := splitCondition(args[0])
			if err != nil {
				return nil, err
			}
			m, r, err := splitGroup(e, cond)
			if err != nil {
				return nil, err
			}
			if name == "subgroup" {
				return optional(m), nil
			}
			return newTuple([]Value{optional(m), optional(r)}), nil
		}), true
	case "derive":
		return methodsBound(e, name, func(args []Value, kw *dictV) (Value, error) {
			if err := posOnly(name, 1, 1, args, kw); err != nil {
				return nil, err
			}
			return newExcGroup(classes["BaseExceptionGroup"], []Value{e.field("message"), args[0]}, nil)
		}), true
	}
	return nil, false
}

// leaves are the exceptions of a group that are not groups themselves,
// nested groups included.
func leaves(e *excV, out map[*excV]bool) {
	if !isExcGroup(e) {
		out[e] = true
		return
	}
	for _, x := range groupExceptions(e) {
		leaves(x, out)
	}
}

// projectGroup is the part of the group whose leaves are in keep, nil when
// none is.
func projectGroup(e *excV, keep map[*excV]bool) (*excV, error) {
	m, _, err := splitGroup(e, func(x *excV) (bool, error) {
		if isExcGroup(x) {
			return false, nil
		}
		return keep[x], nil
	})
	return m, err
}

// execTryStar runs a try statement with except* clauses.
func (f *frame) execTryStar(n *Node) (ctlFlow, error) {
	run := func() (ctlFlow, error) {
		c, err := f.execBlock(n.Body)
		if err == nil {
			if c != ctlNone {
				return c, nil
			}
			return f.execBlock(n.Orelse)
		}
		if err == errGenStopped {
			return ctlNone, err
		}
		orig := asExc(err)
		group := orig
		naked := !isExcGroup(orig)
		if naked {
			group = &excV{Class: "BaseExceptionGroup", args: []Value{strV(""), newTuple([]Value{orig})}}
			if excIsInstance(orig, "Exception") {
				group.Class = "ExceptionGroup"
			}
			group.setField("message", strV(""))
			group.setField("exceptions", newTuple([]Value{orig}))
			group.Message = groupStr(group)
		}
		rest := group
		var raised []*excV
		keep := map[*excV]bool{}
		for _, h := range n.L1 {
			if rest == nil {
				break
			}
			t, err := f.eval(h.A)
			if err != nil {
				return ctlNone, err
			}
			if k, ok := t.(*classV); ok && k.subclassOf(classes["BaseExceptionGroup"]) {
				return ctlNone, typeErr("catching ExceptionGroup with except* is not allowed. Use except instead.")
			}
			cond, err := splitCondition(t)
			if err != nil {
				return ctlNone, typeErr("catching classes that do not inherit from BaseException is not allowed")
			}
			var match, r *excV
			if naked {
				// A naked exception matches as the group wrapping it,
				// whose traceback starts at the matching clause.
				ok, err := cond(orig)
				if err != nil {
					return ctlNone, err
				}
				if ok {
					match = group
					g := f.codeFrame()
					group.tb, group.tbFrame = []tbEntry{{fr: g, co: g.co, line: h.Lineno}}, g
				} else {
					r = group
				}
			} else if match, r, err = splitGroup(rest, cond); err != nil {
				return ctlNone, err
			}
			if match == nil {
				continue
			}
			rest = r
			_, herr := f.runHandler(h, match)
			if herr == nil {
				continue
			}
			if herr == errGenStopped {
				return ctlNone, herr
			}
			hx := asExc(herr)
			if hx == match && !naked {
				// A bare raise re-raises the matched part.
				leaves(match, keep)
				continue
			}
			raised = append(raised, hx)
		}
		if rest != nil {
			leaves(rest, keep)
		}
		if naked {
			if len(raised) > 0 {
				return ctlNone, raised[0]
			}
			if keep[orig] {
				return ctlNone, orig
			}
			return ctlNone, nil
		}
		reraised, err := projectGroup(orig, keep)
		if err != nil {
			return ctlNone, err
		}
		if len(raised) == 0 {
			if reraised != nil {
				return ctlNone, reraised
			}
			return ctlNone, nil
		}
		if reraised != nil {
			raised = append(raised, reraised)
		}
		if len(raised) == 1 {
			return ctlNone, raised[0]
		}
		vals := make([]Value, len(raised))
		for i, x := range raised {
			vals[i] = x
		}
		g, err := newExcGroup(classes["BaseExceptionGroup"], []Value{strV(""), newList(vals)}, nil)
		if err != nil {
			return ctlNone, err
		}
		return ctlNone, g
	}
	c, err := run()
	return f.runFinally(n, c, err)
}

// isSequence is PySequence_Check: the builtin sequences and the classes
// that define __getitem__, dicts excepted.
func isSequence(v Value) bool {
	switch unsub(v).(type) {
	case *listV, *tupleV, rangeV, strV, bytesV, *bytearrayV:
		return true
	case *dictV:
		return false
	}
	if k := userClassOf(v); k != nil {
		_, ok := k.lookupUser("__getitem__")
		return ok
	}
	return false
}
