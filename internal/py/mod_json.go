// Ported from CPython 3.12 (Lib/json/__init__.py, Lib/json/encoder.py, Lib/json/decoder.py,
// Lib/json/scanner.py, Modules/_json.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

func init() {
	register("json", func(b *builder) {
		ns := &methods{}
		ns.method("__init__", jsonDecodeErrorInit)
		names := append([]string{"__module__", "__qualname__"}, ns.names...)
		values := append([]Value{String("json.decoder"), String("JSONDecodeError")}, ns.values...)
		decodeErr, err := NewClass("JSONDecodeError", []Value{Builtin("ValueError")}, StrDict(names, values))
		if err != nil {
			b.err = err
			return
		}
		b.set("JSONDecodeError", decodeErr)
		encoderCls := b.class("JSONEncoder", []Value{Builtin("object")}, jsonEncoderMethods())
		b.pyfn("dumps", func(args []Value, kwargs []Keyword) (Value, error) {
			if err := CheckPositional("dumps", len(args), 1, 1); err != nil {
				return nil, err
			}
			return jsonDumps(encoderCls, args[0], kwargs)
		})
		b.pyfn("dump", func(args []Value, kwargs []Keyword) (Value, error) {
			if len(args) != 2 {
				return nil, raise("TypeError", "dump() missing required positional arguments: 'obj' and 'fp'")
			}
			s, err := jsonDumps(encoderCls, args[0], kwargs)
			if err != nil {
				return nil, err
			}
			_, err = CallMethod(args[1], "write", s)
			return nil, err
		})
		b.pyfn("loads", func(args []Value, kwargs []Keyword) (Value, error) {
			if err := CheckPositional("loads", len(args), 1, 1); err != nil {
				return nil, err
			}
			return jsonLoads(decodeErr, args[0], kwargs)
		})
		b.pyfn("load", func(args []Value, kwargs []Keyword) (Value, error) {
			if err := CheckPositional("load", len(args), 1, 1); err != nil {
				return nil, err
			}
			s, err := CallMethod(args[0], "read")
			if err != nil {
				return nil, err
			}
			return jsonLoads(decodeErr, s, kwargs)
		})
	})
}

// jsonDecodeErrorInit is JSONDecodeError.__init__(msg, doc, pos).
func jsonDecodeErrorInit(args []Value, kwargs []Keyword) (Value, error) {
	if len(args) != 4 {
		return nil, raise("TypeError", "JSONDecodeError.__init__() missing required arguments: 'msg', 'doc' and 'pos'")
	}
	self, msg, doc, pos := args[0], args[1], args[2], args[3]
	docText, _ := AsString(doc)
	p, err := Ssize(pos)
	if err != nil {
		return nil, err
	}
	rs := []rune(docText)
	p = min(max(p, 0), len(rs))
	before := string(rs[:p])
	lineno := strings.Count(before, "\n") + 1
	colno := p + 1
	if i := strings.LastIndexByte(before, '\n'); i >= 0 {
		colno = p - len([]rune(before[:i]))
	}
	m, err := Str(msg)
	if err != nil {
		return nil, err
	}
	text := m + ": line " + strconv.Itoa(lineno) + " column " + strconv.Itoa(colno) + " (char " + strconv.Itoa(p) + ")"
	if _, err := CallSuper(TypeOf(self), self, "__init__", []Value{String(text)}, nil); err != nil {
		return nil, err
	}
	for _, a := range []struct {
		name string
		v    Value
	}{{"msg", msg}, {"doc", doc}, {"pos", pos}, {"lineno", Int(int64(lineno))}, {"colno", Int(int64(colno))}} {
		if err := SetAttr(self, a.name, a.v); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// ---------------------------------------------------------------- encoding

// jsonEncoder holds the options of one encoding.
type jsonEncoder struct {
	skipkeys, ensureASCII, checkCircular, allowNaN, sortKeys bool
	indent                                                   *string
	itemSep, keySep                                          string
	defaultFn                                                Value
	markers                                                  map[Value]bool
}

var jsonEncoderOptions = []string{"skipkeys", "ensure_ascii", "check_circular", "allow_nan", "sort_keys",
	"indent", "separators", "default"}

func jsonEncoderMethods() *methods {
	ms := &methods{}
	ms.add("item_separator", String(", "))
	ms.add("key_separator", String(": "))
	ms.method("__init__", func(args []Value, kwargs []Keyword) (Value, error) {
		if len(args) > 1 {
			return nil, raise("TypeError", "JSONEncoder.__init__() takes 1 positional argument but %d were given", len(args))
		}
		self := args[0]
		vals := map[string]Value{"skipkeys": Bool(false), "ensure_ascii": Bool(true),
			"check_circular": Bool(true), "allow_nan": Bool(true), "sort_keys": Bool(false),
			"indent": None()}
		for _, k := range kwargs {
			if !slices.Contains(jsonEncoderOptions, k.Name) {
				return nil, raise("TypeError", "JSONEncoder.__init__() got an unexpected keyword argument '%s'", k.Name)
			}
			vals[k.Name] = k.Value
		}
		for _, name := range []string{"skipkeys", "ensure_ascii", "check_circular", "allow_nan", "sort_keys", "indent"} {
			if err := SetAttr(self, name, vals[name]); err != nil {
				return nil, err
			}
		}
		if sep, ok := vals["separators"]; ok && !IsNone(sep) {
			parts, err := listOf(sep)
			if err != nil {
				return nil, err
			}
			if len(parts) != 2 {
				return nil, raise("ValueError", "too many values to unpack (expected 2)")
			}
			if err := SetAttr(self, "item_separator", parts[0]); err != nil {
				return nil, err
			}
			if err := SetAttr(self, "key_separator", parts[1]); err != nil {
				return nil, err
			}
		} else if !IsNone(vals["indent"]) {
			if err := SetAttr(self, "item_separator", String(",")); err != nil {
				return nil, err
			}
		}
		if d, ok := vals["default"]; ok && !IsNone(d) {
			return nil, SetAttr(self, "default", d)
		}
		return nil, nil
	})
	ms.method("default", func(args []Value, _ []Keyword) (Value, error) {
		if len(args) != 2 {
			return nil, raise("TypeError", "default() takes 2 positional arguments but %d were given", len(args))
		}
		return nil, raise("TypeError", "Object of type %s is not JSON serializable", TypeName(args[1]))
	})
	ms.method("encode", func(args []Value, _ []Keyword) (Value, error) {
		if len(args) != 2 {
			return nil, raise("TypeError", "encode() takes 2 positional arguments but %d were given", len(args))
		}
		enc, err := encoderOf(args[0])
		if err != nil {
			return nil, err
		}
		var out strings.Builder
		if err := enc.encode(&out, args[1], 0); err != nil {
			return nil, err
		}
		return String(out.String()), nil
	})
	return ms
}

// encoderOf reads the options of a JSONEncoder instance.
func encoderOf(self Value) (*jsonEncoder, error) {
	enc := &jsonEncoder{markers: map[Value]bool{}}
	flags := []struct {
		name string
		dst  *bool
	}{{"skipkeys", &enc.skipkeys}, {"ensure_ascii", &enc.ensureASCII}, {"check_circular", &enc.checkCircular},
		{"allow_nan", &enc.allowNaN}, {"sort_keys", &enc.sortKeys}}
	for _, f := range flags {
		v, err := GetAttr(self, f.name)
		if err != nil {
			return nil, err
		}
		if *f.dst, err = truth(v); err != nil {
			return nil, err
		}
	}
	indent, err := GetAttr(self, "indent")
	if err != nil {
		return nil, err
	}
	if !IsNone(indent) {
		var s string
		if n, ok := AsInt(indent); ok {
			s = strings.Repeat(" ", max(int(n.Int64()), 0))
		} else if str, ok := AsString(indent); ok {
			s = str
		}
		enc.indent = &s
	}
	for _, sep := range []struct {
		name string
		dst  *string
	}{{"item_separator", &enc.itemSep}, {"key_separator", &enc.keySep}} {
		v, err := GetAttr(self, sep.name)
		if err != nil {
			return nil, err
		}
		if *sep.dst, err = Str(v); err != nil {
			return nil, err
		}
	}
	if enc.defaultFn, err = GetAttr(self, "default"); err != nil {
		return nil, err
	}
	return enc, nil
}

// jsonDumps is json.dumps(obj, **kw): the encoder is cls(**kw), JSONEncoder
// by default.
func jsonDumps(encoderCls Value, obj Value, kwargs []Keyword) (Value, error) {
	cls := encoderCls
	var rest []Keyword
	for _, k := range kwargs {
		if k.Name == "cls" {
			if !IsNone(k.Value) {
				cls = k.Value
			}
			continue
		}
		rest = append(rest, k)
	}
	enc, err := Call(cls, nil, rest)
	if err != nil {
		return nil, err
	}
	return CallMethod(enc, "encode", obj)
}

// floatRepr is the encoder's floatstr: repr(), or NaN and the infinities
// when allowed.
func (e *jsonEncoder) floatRepr(f float64, v Value) (string, error) {
	var text string
	switch {
	case math.IsNaN(f):
		text = "NaN"
	case math.IsInf(f, 1):
		text = "Infinity"
	case math.IsInf(f, -1):
		text = "-Infinity"
	default:
		return repr(Float(f))
	}
	if !e.allowNaN {
		r, err := repr(v)
		if err != nil {
			return "", err
		}
		return "", raise("ValueError", "Out of range float values are not JSON compliant: %s", r)
	}
	return text, nil
}

// quote is encode_basestring_ascii (ensureASCII) or encode_basestring.
func (e *jsonEncoder) quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r < 0x20 || e.ensureASCII && r > 0x7e:
				if r > 0xffff {
					r1, r2 := utf16.EncodeRune(r)
					b.WriteString(`\u` + hex4(r1) + `\u` + hex4(r2))
				} else {
					b.WriteString(`\u` + hex4(r))
				}
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func hex4(r rune) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[r>>12&15], digits[r>>8&15], digits[r>>4&15], digits[r&15]})
}

// typeIs reports whether v is an instance of the builtin class named name
// (a subclass included).
func typeIs(v Value, name string) bool {
	ok, err := IsInstance(v, Builtin(name))
	return err == nil && ok
}

// newline writes the line break and indentation of nesting level.
func (e *jsonEncoder) newline(out *strings.Builder, level int) {
	out.WriteByte('\n')
	out.WriteString(strings.Repeat(*e.indent, level))
}

// encode is _iterencode: o written to out at nesting level.
func (e *jsonEncoder) encode(out *strings.Builder, o Value, level int) error {
	switch {
	case IsNone(o):
		out.WriteString("null")
		return nil
	case TypeName(o) == "bool":
		t, _ := AsBool(o)
		if t {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
		return nil
	case typeIs(o, "str"):
		s, _ := AsString(o)
		out.WriteString(e.quote(s))
		return nil
	case typeIs(o, "int"):
		n, _ := AsInt(o)
		out.WriteString(n.String())
		return nil
	case typeIs(o, "float"):
		f, _ := AsFloat(o)
		s, err := e.floatRepr(f, o)
		if err != nil {
			return err
		}
		out.WriteString(s)
		return nil
	case typeIs(o, "list") || typeIs(o, "tuple"):
		return e.encodeList(out, o, level)
	case typeIs(o, "dict"):
		return e.encodeDict(out, o, level)
	}
	if err := e.enter(o); err != nil {
		return err
	}
	conv, err := call(e.defaultFn, o)
	if err != nil {
		return err
	}
	if err := e.encode(out, conv, level); err != nil {
		return err
	}
	e.leave(o)
	return nil
}

// enter records a container being encoded, refusing one already open.
func (e *jsonEncoder) enter(o Value) error {
	if !e.checkCircular {
		return nil
	}
	if e.markers[o] {
		return raise("ValueError", "Circular reference detected")
	}
	e.markers[o] = true
	return nil
}

func (e *jsonEncoder) leave(o Value) {
	if e.checkCircular {
		delete(e.markers, o)
	}
}

func (e *jsonEncoder) encodeList(out *strings.Builder, o Value, level int) error {
	items, err := listOf(o)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		out.WriteString("[]")
		return nil
	}
	if err := e.enter(o); err != nil {
		return err
	}
	out.WriteByte('[')
	if e.indent != nil {
		level++
		e.newline(out, level)
	}
	for i, it := range items {
		if i > 0 {
			out.WriteString(e.itemSep)
			if e.indent != nil {
				e.newline(out, level)
			}
		}
		if err := e.encode(out, it, level); err != nil {
			return err
		}
	}
	if e.indent != nil {
		level--
		e.newline(out, level)
	}
	out.WriteByte(']')
	e.leave(o)
	return nil
}

// dictKey is the str a dict key is written as, ok false to skip it.
func (e *jsonEncoder) dictKey(k Value) (string, bool, error) {
	switch {
	case typeIs(k, "str"):
		s, _ := AsString(k)
		return s, true, nil
	case typeIs(k, "float"):
		f, _ := AsFloat(k)
		s, err := e.floatRepr(f, k)
		return s, true, err
	case TypeName(k) == "bool":
		t, _ := AsBool(k)
		if t {
			return "true", true, nil
		}
		return "false", true, nil
	case IsNone(k):
		return "null", true, nil
	case typeIs(k, "int"):
		n, _ := AsInt(k)
		return n.String(), true, nil
	case e.skipkeys:
		return "", false, nil
	}
	return "", false, raise("TypeError", "keys must be str, int, float, bool or None, not %s", TypeName(k))
}

func (e *jsonEncoder) encodeDict(out *strings.Builder, o Value, level int) error {
	pairs, err := CallMethod(o, "items")
	if err != nil {
		return err
	}
	if e.sortKeys {
		if pairs, err = call(Builtin("sorted"), pairs); err != nil {
			return err
		}
	}
	items, err := listOf(pairs)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		out.WriteString("{}")
		return nil
	}
	if err := e.enter(o); err != nil {
		return err
	}
	out.WriteByte('{')
	if e.indent != nil {
		level++
		e.newline(out, level)
	}
	first := true
	for _, p := range items {
		kv, _ := SeqItems(p)
		key, ok, err := e.dictKey(kv[0])
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if !first {
			out.WriteString(e.itemSep)
			if e.indent != nil {
				e.newline(out, level)
			}
		}
		first = false
		out.WriteString(e.quote(key))
		out.WriteString(e.keySep)
		if err := e.encode(out, kv[1], level); err != nil {
			return err
		}
	}
	if e.indent != nil {
		level--
		e.newline(out, level)
	}
	out.WriteByte('}')
	e.leave(o)
	return nil
}

// ---------------------------------------------------------------- decoding

// jsonDecoder is one json.loads: the document as code points and the
// hooks.
type jsonDecoder struct {
	doc                                 []rune
	docValue                            Value
	errCls                              Value
	objectHook, pairsHook               Value
	parseFloat, parseInt, parseConstant Value
}

func (d *jsonDecoder) fail(msg string, pos int) error {
	v, err := call(d.errCls, String(msg), d.docValue, Int(int64(pos)))
	if err != nil {
		return err
	}
	return RaiseValue(v)
}

// jsonLoads is json.loads(s, **kw).
func jsonLoads(errCls, s Value, kwargs []Keyword) (Value, error) {
	text, ok := AsString(s)
	if !ok {
		raw, isBytes := AsBytes(s)
		if !isBytes {
			if b, err := Call(Builtin("bytes"), []Value{s}, nil); err == nil && TypeName(s) == "bytearray" {
				raw, isBytes = AsBytes(b)
			}
		}
		if !isBytes {
			return nil, raise("TypeError", "the JSON object must be str, bytes or bytearray, not %s", TypeName(s))
		}
		decoded, err := CallMethod(Bytes(raw), "decode", String(detectEncoding(raw)), String("surrogatepass"))
		if err != nil {
			return nil, err
		}
		text, _ = AsString(decoded)
		s = decoded
	} else if strings.HasPrefix(text, "\ufeff") {
		return nil, (&jsonDecoder{doc: []rune(text), docValue: s, errCls: errCls}).fail(
			"Unexpected UTF-8 BOM (decode using utf-8-sig)", 0)
	}
	d := &jsonDecoder{doc: []rune(text), docValue: s, errCls: errCls}
	for _, k := range kwargs {
		v := k.Value
		if IsNone(v) {
			continue
		}
		switch k.Name {
		case "object_hook":
			d.objectHook = v
		case "object_pairs_hook":
			d.pairsHook = v
		case "parse_float":
			d.parseFloat = v
		case "parse_int":
			d.parseInt = v
		case "parse_constant":
			d.parseConstant = v
		case "cls", "strict":
		default:
			return nil, raise("TypeError", "JSONDecoder.__init__() got an unexpected keyword argument '%s'", k.Name)
		}
	}
	i := d.skipSpace(0)
	v, end, err := d.value(i)
	if err != nil {
		return nil, err
	}
	end = d.skipSpace(end)
	if end != len(d.doc) {
		return nil, d.fail("Extra data", end)
	}
	return v, nil
}

// detectEncoding is json.detect_encoding.
func detectEncoding(b []byte) string {
	switch {
	case len(b) >= 4 && (string(b[:4]) == "\x00\x00\xfe\xff" || string(b[:4]) == "\xff\xfe\x00\x00"):
		return "utf-32"
	case len(b) >= 2 && (string(b[:2]) == "\xff\xfe" || string(b[:2]) == "\xfe\xff"):
		return "utf-16"
	case len(b) >= 3 && string(b[:3]) == "\xef\xbb\xbf":
		return "utf-8-sig"
	}
	if len(b) >= 4 {
		switch {
		case b[0] == 0 && b[1] == 0:
			return "utf-32-be"
		case b[0] == 0:
			return "utf-16-be"
		case b[1] == 0 && b[2] == 0 && b[3] == 0:
			return "utf-32-le"
		case b[1] == 0:
			return "utf-16-le"
		}
	} else if len(b) == 2 {
		switch {
		case b[0] == 0:
			return "utf-16-be"
		case b[1] == 0:
			return "utf-16-le"
		}
	}
	return "utf-8"
}

func (d *jsonDecoder) skipSpace(i int) int {
	for i < len(d.doc) && strings.ContainsRune(" \t\n\r", d.doc[i]) {
		i++
	}
	return i
}

func (d *jsonDecoder) hasPrefix(i int, s string) bool {
	rs := []rune(s)
	if i+len(rs) > len(d.doc) {
		return false
	}
	for j, r := range rs {
		if d.doc[i+j] != r {
			return false
		}
	}
	return true
}

// value is scan_once: the value starting at i and the position after it.
func (d *jsonDecoder) value(i int) (Value, int, error) {
	if i >= len(d.doc) {
		return nil, 0, d.fail("Expecting value", i)
	}
	switch c := d.doc[i]; {
	case c == '"':
		return d.str(i + 1)
	case c == '{':
		return d.object(i + 1)
	case c == '[':
		return d.array(i + 1)
	case c == 'n' && d.hasPrefix(i, "null"):
		return None(), i + 4, nil
	case c == 't' && d.hasPrefix(i, "true"):
		return Bool(true), i + 4, nil
	case c == 'f' && d.hasPrefix(i, "false"):
		return Bool(false), i + 5, nil
	case c == 'N' && d.hasPrefix(i, "NaN"):
		v, err := d.constant("NaN")
		return v, i + 3, err
	case c == 'I' && d.hasPrefix(i, "Infinity"):
		v, err := d.constant("Infinity")
		return v, i + 8, err
	case c == '-' && d.hasPrefix(i, "-Infinity"):
		v, err := d.constant("-Infinity")
		return v, i + 9, err
	}
	return d.number(i)
}

func (d *jsonDecoder) constant(name string) (Value, error) {
	if d.parseConstant != nil {
		return call(d.parseConstant, String(name))
	}
	switch name {
	case "NaN":
		return Float(math.NaN()), nil
	case "Infinity":
		return Float(math.Inf(1)), nil
	}
	return Float(math.Inf(-1)), nil
}

// number is the NUMBER_RE match at i: -?(0|[1-9]\d*)(\.\d+)?([eE][-+]?\d+)?
func (d *jsonDecoder) number(i int) (Value, int, error) {
	j := i
	digit := func(k int) bool { return k < len(d.doc) && d.doc[k] >= '0' && d.doc[k] <= '9' }
	if j < len(d.doc) && d.doc[j] == '-' {
		j++
	}
	switch {
	case j < len(d.doc) && d.doc[j] == '0':
		j++
	case digit(j) && d.doc[j] != '0':
		for digit(j) {
			j++
		}
	default:
		return nil, 0, d.fail("Expecting value", i)
	}
	isFloat := false
	if j < len(d.doc) && d.doc[j] == '.' && digit(j+1) {
		j++
		for digit(j) {
			j++
		}
		isFloat = true
	}
	if j < len(d.doc) && (d.doc[j] == 'e' || d.doc[j] == 'E') {
		k := j + 1
		if k < len(d.doc) && (d.doc[k] == '+' || d.doc[k] == '-') {
			k++
		}
		if digit(k) {
			for digit(k) {
				k++
			}
			j = k
			isFloat = true
		}
	}
	text := String(string(d.doc[i:j]))
	var v Value
	var err error
	switch {
	case isFloat && d.parseFloat != nil:
		v, err = call(d.parseFloat, text)
	case isFloat:
		v, err = call(Builtin("float"), text)
	case d.parseInt != nil:
		v, err = call(d.parseInt, text)
	default:
		v, err = call(Builtin("int"), text)
	}
	return v, j, err
}

// str is scanstring: the string whose opening quote is just before i.
func (d *jsonDecoder) str(i int) (Value, int, error) {
	start := i - 1
	var b strings.Builder
	for {
		if i >= len(d.doc) {
			return nil, 0, d.fail("Unterminated string starting at", start)
		}
		c := d.doc[i]
		switch {
		case c == '"':
			return String(b.String()), i + 1, nil
		case c < 0x20:
			return nil, 0, d.fail("Invalid control character at", i)
		case c != '\\':
			b.WriteRune(c)
			i++
			continue
		}
		i++
		if i >= len(d.doc) {
			return nil, 0, d.fail("Unterminated string starting at", start)
		}
		esc := d.doc[i]
		if esc != 'u' {
			r, ok := map[rune]rune{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}[esc]
			if !ok {
				return nil, 0, d.fail("Invalid \\escape", i-1)
			}
			b.WriteRune(r)
			i++
			continue
		}
		u, ok := d.hex4At(i + 1)
		if !ok || i+5 >= len(d.doc) {
			return nil, 0, d.fail("Invalid \\uXXXX escape", i)
		}
		i += 5
		if u >= 0xd800 && u <= 0xdbff && d.hasPrefix(i, "\\u") {
			if u2, ok := d.hex4At(i + 2); ok && u2 >= 0xdc00 && u2 <= 0xdfff {
				u = utf16.DecodeRune(u, u2)
				i += 6
			}
		}
		b.WriteString(string(appendCodePoint(nil, u)))
	}
}

// appendCodePoint appends r as the str runtime stores it, lone surrogates
// included.
func appendCodePoint(b []byte, r rune) []byte {
	if r >= 0xd800 && r <= 0xdfff {
		return append(b, byte(0xe0|r>>12), byte(0x80|r>>6&0x3f), byte(0x80|r&0x3f))
	}
	return append(b, string(r)...)
}

func (d *jsonDecoder) hex4At(i int) (rune, bool) {
	if i+4 > len(d.doc) {
		return 0, false
	}
	n, err := strconv.ParseUint(string(d.doc[i:i+4]), 16, 32)
	if err != nil || strings.ContainsAny(string(d.doc[i:i+4]), "+-xX_") {
		return 0, false
	}
	return rune(n), true
}

// object is JSONObject: the members after the '{' at i-1.
func (d *jsonDecoder) object(i int) (Value, int, error) {
	var keys, vals []Value
	i = d.skipSpace(i)
	if i < len(d.doc) && d.doc[i] == '}' {
		return d.finishObject(keys, vals, i+1)
	}
	for {
		if i >= len(d.doc) || d.doc[i] != '"' {
			return nil, 0, d.fail("Expecting property name enclosed in double quotes", i)
		}
		k, end, err := d.str(i + 1)
		if err != nil {
			return nil, 0, err
		}
		i = d.skipSpace(end)
		if i >= len(d.doc) || d.doc[i] != ':' {
			return nil, 0, d.fail("Expecting ':' delimiter", i)
		}
		i = d.skipSpace(i + 1)
		v, end, err := d.value(i)
		if err != nil {
			return nil, 0, err
		}
		keys, vals = append(keys, k), append(vals, v)
		i = d.skipSpace(end)
		if i < len(d.doc) && d.doc[i] == '}' {
			return d.finishObject(keys, vals, i+1)
		}
		if i >= len(d.doc) || d.doc[i] != ',' {
			return nil, 0, d.fail("Expecting ',' delimiter", i)
		}
		i = d.skipSpace(i + 1)
	}
}

func (d *jsonDecoder) finishObject(keys, vals []Value, end int) (Value, int, error) {
	if d.pairsHook != nil {
		pairs := make([]Value, len(keys))
		for i := range keys {
			pairs[i] = Tuple(keys[i], vals[i])
		}
		v, err := call(d.pairsHook, List(pairs...))
		return v, end, err
	}
	obj := EmptyDict()
	for i := range keys {
		if err := SetItem(obj, keys[i], vals[i]); err != nil {
			return nil, 0, err
		}
	}
	if d.objectHook != nil {
		v, err := call(d.objectHook, obj)
		return v, end, err
	}
	return obj, end, nil
}

// array is JSONArray: the items after the '[' at i-1.
func (d *jsonDecoder) array(i int) (Value, int, error) {
	var items []Value
	i = d.skipSpace(i)
	if i < len(d.doc) && d.doc[i] == ']' {
		return List(), i + 1, nil
	}
	for {
		v, end, err := d.value(i)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, v)
		i = d.skipSpace(end)
		if i < len(d.doc) && d.doc[i] == ']' {
			return List(items...), i + 1, nil
		}
		if i >= len(d.doc) || d.doc[i] != ',' {
			return nil, 0, d.fail("Expecting ',' delimiter", i)
		}
		i = d.skipSpace(i + 1)
	}
}
