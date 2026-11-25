// Ported from CPython 3.12 (Python/traceback.c, Objects/frameobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Traceback objects (an exception's __traceback__) and the frame objects they
// and generators show.

package py

// tracebackV is a traceback object: one entry of an exception's traceback,
// tb_next leading to the entry of the frame it called, towards the one that
// raised.
type tracebackV struct {
	entry tbEntry
	next  *tracebackV
}

func (*tracebackV) pyType() string { return "traceback" }

func (t *tracebackV) repr(*reprState) (string, error) {
	return "<traceback object at " + objAddr(t) + ">", nil
}

func (t *tracebackV) getAttr(name string) (Value, error) {
	switch name {
	case "tb_frame":
		return t.entry.fr.object(), nil
	case "tb_lineno":
		if l, ok := t.entry.co.line(t.entry.line); ok {
			return mkInt(int64(l)), nil
		}
		return none, nil
	case "tb_next":
		if t.next == nil {
			return none, nil
		}
		return t.next, nil
	}
	return nil, nil
}

func (t *tracebackV) setAttr(name string, v Value) error {
	if name != "tb_next" {
		return newPyErr("AttributeError", "'traceback' object has no attribute '"+name+"'")
	}
	if isNoneV(v) {
		t.next = nil
		return nil
	}
	n, ok := v.(*tracebackV)
	if !ok {
		return typeErr("expected traceback object or None, got '%s'", typeName(v))
	}
	for c := n; c != nil; c = c.next {
		if c == t {
			return valueErr("traceback loop detected")
		}
	}
	t.next = n
	return nil
}

// traceback is the exception's __traceback__: the chain of its traceback
// entries from the outermost frame inwards, None when it has none. The same
// objects come back until the exception leaves another frame.
func (e *excV) traceback() Value {
	if len(e.tb) == 0 {
		return none
	}
	if e.tbObj == nil || e.tbObjLen != len(e.tb) {
		var next *tracebackV
		for i := range e.tb {
			next = &tracebackV{entry: e.tb[i], next: next}
		}
		e.tbObj, e.tbObjLen = next, len(e.tb)
	}
	return e.tbObj
}

// setTraceback is setting __traceback__ (or with_traceback): a traceback
// object, whose chain becomes the exception's traceback, or None.
func (e *excV) setTraceback(v Value) error {
	if isNoneV(v) {
		e.tb, e.tbFrame, e.tbObj = nil, nil, nil
		return nil
	}
	t, ok := v.(*tracebackV)
	if !ok {
		return typeErr("__traceback__ must be a traceback or None")
	}
	var entries []tbEntry
	for c := t; c != nil; c = c.next {
		entries = append(entries, c.entry)
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	e.tb, e.tbFrame, e.tbObj, e.tbObjLen = entries, nil, t, len(entries)
	return nil
}

// frameObjV is a frame object: a running or finished frame of Python code.
type frameObjV struct {
	f *frame
}

func (*frameObjV) pyType() string { return "frame" }

// object is the frame object of the frame f, the same one each time.
func (f *frame) object() *frameObjV {
	if f.obj == nil {
		f.obj = &frameObjV{f: f}
	}
	return f.obj
}

// lineno is f_lineno: the line of the statement running in the frame, the
// first line of its code before the frame has started, None when the
// statement has no line.
func (o *frameObjV) lineno() Value {
	if o.f.line == 0 {
		return mkInt(int64(o.f.co.firstLine))
	}
	if l, ok := o.f.co.line(o.f.line); ok {
		return mkInt(int64(l))
	}
	return none
}

func (o *frameObjV) repr(*reprState) (string, error) {
	co := o.f.co
	line := "None"
	if l, ok := o.lineno().(intV); ok {
		line = l.v.String()
	}
	file, err := pyRepr(strV(co.filename))
	if err != nil {
		return "", err
	}
	return "<frame at " + objAddr(o) + ", file " + file + ", line " + line + ", code " + co.name + ">", nil
}

func (o *frameObjV) getAttr(name string) (Value, error) {
	f := o.f
	switch name {
	case "f_code":
		return f.co, nil
	case "f_globals":
		return f.vm.globals, nil
	case "f_locals":
		return localsOf(f), nil
	case "f_builtins":
		return interp.builtins.dict, nil
	case "f_lineno":
		return o.lineno(), nil
	case "f_back":
		for b := f.back; b != nil; b = b.back {
			if b.co != nil {
				return b.object(), nil
			}
		}
		return none, nil
	case "f_trace":
		return none, nil
	}
	return nil, nil
}
