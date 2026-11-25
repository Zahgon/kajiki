// Ported from CPython 3.12 (Lib/string.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"strings"
)

const (
	asciiLowercase = "abcdefghijklmnopqrstuvwxyz"
	asciiUppercase = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits         = "0123456789"
	punctuation    = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	whitespace     = " \t\n\r\x0b\x0c"
)

func init() {
	register("string", func(b *builder) {
		b.set("ascii_lowercase", String(asciiLowercase))
		b.set("ascii_uppercase", String(asciiUppercase))
		b.set("ascii_letters", String(asciiLowercase+asciiUppercase))
		b.set("digits", String(digits))
		b.set("hexdigits", String(digits+"abcdef"+"ABCDEF"))
		b.set("octdigits", String("01234567"))
		b.set("punctuation", String(punctuation))
		b.set("whitespace", String(whitespace))
		b.set("printable", String(digits+asciiLowercase+asciiUppercase+punctuation+whitespace))
		b.pyfn("capwords", capwords)
		b.class("Template", []Value{Builtin("object")}, templateMethods())
		b.source("string.py", formatterSource)
	})
	register("_string", func(b *builder) {
		b.fn("formatter_parser", formatterParser)
		b.fn("formatter_field_name_split", formatterFieldNameSplit)
	})
}

// formatterSource is string.Formatter, which CPython writes in Python.
const formatterSource = `import _string

class Formatter:
    def format(self, format_string, /, *args, **kwargs):
        return self.vformat(format_string, args, kwargs)

    def vformat(self, format_string, args, kwargs):
        used_args = set()
        result, _ = self._vformat(format_string, args, kwargs, used_args, 2)
        self.check_unused_args(used_args, args, kwargs)
        return result

    def _vformat(self, format_string, args, kwargs, used_args, recursion_depth,
                 auto_arg_index=0):
        if recursion_depth < 0:
            raise ValueError('Max string recursion exceeded')
        result = []
        for literal_text, field_name, format_spec, conversion in \
                self.parse(format_string):
            if literal_text:
                result.append(literal_text)
            if field_name is not None:
                if field_name == '':
                    if auto_arg_index is False:
                        raise ValueError('cannot switch from manual field '
                                         'specification to automatic field '
                                         'numbering')
                    field_name = str(auto_arg_index)
                    auto_arg_index += 1
                elif field_name.isdigit():
                    if auto_arg_index:
                        raise ValueError('cannot switch from manual field '
                                         'specification to automatic field '
                                         'numbering')
                    auto_arg_index = False
                obj, arg_used = self.get_field(field_name, args, kwargs)
                used_args.add(arg_used)
                obj = self.convert_field(obj, conversion)
                format_spec, auto_arg_index = self._vformat(
                    format_spec, args, kwargs,
                    used_args, recursion_depth-1,
                    auto_arg_index=auto_arg_index)
                result.append(self.format_field(obj, format_spec))
        return ''.join(result), auto_arg_index

    def get_value(self, key, args, kwargs):
        if isinstance(key, int):
            return args[key]
        else:
            return kwargs[key]

    def check_unused_args(self, used_args, args, kwargs):
        pass

    def format_field(self, value, format_spec):
        return format(value, format_spec)

    def convert_field(self, value, conversion):
        if conversion is None:
            return value
        elif conversion == 's':
            return str(value)
        elif conversion == 'r':
            return repr(value)
        elif conversion == 'a':
            return ascii(value)
        raise ValueError("Unknown conversion specifier {0!s}".format(conversion))

    def parse(self, format_string):
        return _string.formatter_parser(format_string)

    def get_field(self, field_name, args, kwargs):
        first, rest = _string.formatter_field_name_split(field_name)
        obj = self.get_value(first, args, kwargs)
        for is_attr, i in rest:
            if is_attr:
                obj = getattr(obj, i)
            else:
                obj = obj[i]
        return obj, first
`

// capwords is string.capwords(s, sep=None): the words of s.split(sep)
// capitalized and joined by sep (a space by default).
func capwords(args []Value, kwargs []Keyword) (Value, error) {
	buf, err := ParseArgs("capwords", []string{"s", "sep"}, 1, 2, args, kwargs)
	if err != nil {
		return nil, err
	}
	sep := buf[1]
	if sep == nil {
		sep = None()
	}
	words, err := CallMethod(buf[0], "split", sep)
	if err != nil {
		return nil, err
	}
	items, _ := SeqItems(words)
	out := make([]Value, len(items))
	for i, w := range items {
		if out[i], err = CallMethod(w, "capitalize"); err != nil {
			return nil, err
		}
	}
	joiner := Value(String(" "))
	if !IsNone(sep) {
		joiner = sep
	}
	return CallMethod(joiner, "join", List(out...))
}

// templateIdentStart and templateIdentPart are Template's idpattern,
// (?a:[_a-z][_a-z0-9]*) matched ignoring case.
func templateIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func templateIdentPart(c byte) bool { return templateIdentStart(c) || c >= '0' && c <= '9' }

// templateSubstitute is Template.substitute (safe=false) or
// safe_substitute: the template's placeholders replaced through lookup.
func templateSubstitute(tmpl string, lookup func(name string) (Value, error), safe bool) (string, error) {
	var out strings.Builder
	for i := 0; i < len(tmpl); {
		c := tmpl[i]
		if c != '$' {
			out.WriteByte(c)
			i++
			continue
		}
		start := i
		i++
		var name string
		switch {
		case i < len(tmpl) && tmpl[i] == '$':
			out.WriteByte('$')
			i++
			continue
		case i < len(tmpl) && templateIdentStart(tmpl[i]):
			j := i
			for j < len(tmpl) && templateIdentPart(tmpl[j]) {
				j++
			}
			name, i = tmpl[i:j], j
		case i+1 < len(tmpl) && tmpl[i] == '{' && templateIdentStart(tmpl[i+1]):
			j := i + 1
			for j < len(tmpl) && templateIdentPart(tmpl[j]) {
				j++
			}
			if j < len(tmpl) && tmpl[j] == '}' {
				name, i = tmpl[i+1:j], j+1
			}
		}
		if name == "" {
			if safe {
				out.WriteByte('$')
				continue
			}
			lines := strings.SplitAfter(tmpl[:start+1], "\n")
			lineno := len(lines)
			colno := len([]rune(lines[len(lines)-1]))
			return "", raise("ValueError", "Invalid placeholder in string: line %d, col %d", lineno, colno)
		}
		v, err := lookup(name)
		if err != nil {
			if safe && ErrorIs(err, "KeyError") {
				out.WriteString(tmpl[start:i])
				continue
			}
			return "", err
		}
		s, err := Str(v)
		if err != nil {
			return "", err
		}
		out.WriteString(s)
	}
	return out.String(), nil
}

func templateMethods() *methods {
	ms := &methods{}
	ms.add("delimiter", String("$"))
	ms.add("idpattern", String("(?a:[_a-z][_a-z0-9]*)"))
	ms.add("braceidpattern", None())
	ms.method("__init__", func(args []Value, kwargs []Keyword) (Value, error) {
		switch {
		case len(args) == 2 && len(kwargs) == 0:
			return nil, SetAttr(args[0], "template", args[1])
		case len(args) == 1 && len(kwargs) == 1 && kwargs[0].Name == "template":
			return nil, SetAttr(args[0], "template", kwargs[0].Value)
		}
		return nil, raise("TypeError", "Template.__init__() missing 1 required positional argument: 'template'")
	})
	subst := func(name string, safe bool) {
		ms.method(name, func(args []Value, kwargs []Keyword) (Value, error) {
			if len(args) > 2 {
				return nil, raise("TypeError", "Template.%s() takes from 1 to 2 positional arguments but %d were given", name, len(args))
			}
			tv, err := GetAttr(args[0], "template")
			if err != nil {
				return nil, err
			}
			tmpl, ok := AsString(tv)
			if !ok {
				return nil, raise("TypeError", "expected string or bytes-like object, got '%s'", TypeName(tv))
			}
			kws := make([]string, len(kwargs))
			kwVals := make([]Value, len(kwargs))
			for i, k := range kwargs {
				kws[i], kwVals[i] = k.Name, k.Value
			}
			kwDict := StrDict(kws, kwVals)
			lookup := func(n string) (Value, error) {
				if len(args) == 2 {
					// ChainMap(kws, mapping)[n]: a KeyError from kws goes on
					// to mapping.
					if len(kwargs) > 0 {
						v, err := GetItem(kwDict, String(n))
						if err == nil || !ErrorIs(err, "KeyError") {
							return v, err
						}
					}
					return GetItem(args[1], String(n))
				}
				return GetItem(kwDict, String(n))
			}
			s, err := templateSubstitute(tmpl, lookup, safe)
			if err != nil {
				return nil, err
			}
			return String(s), nil
		})
	}
	subst("substitute", false)
	subst("safe_substitute", true)
	return ms
}

// formatterParser is _string.formatter_parser(str): an iterator over the
// (literal_text, field_name, format_spec, conversion) parts of a format
// string; the last three are None after literal text with no field, and
// conversion is None for a field without one.
func formatterParser(args []Value, kwargs []Keyword) (Value, error) {
	if err := ArgsOne("formatter_parser", args, kwargs); err != nil {
		return nil, err
	}
	str, ok := unsub(args[0]).(strV)
	if !ok {
		return nil, raise("TypeError", "expected str, got %s", TypeName(args[0]))
	}
	s, p := strRunes(string(str)), 0
	return &iterObj{typ: "formatteriterator", it: funcIter(func() (Value, bool, error) {
		if p >= len(s) {
			return nil, false, nil
		}
		lit, f, err := fmtMarkupNext(s, &p)
		if err != nil {
			return nil, false, err
		}
		if f == nil {
			return newTuple([]Value{strV(lit), none, none, none}), true, nil
		}
		var conv Value = none
		if f.conv != 0 {
			conv = strV(string(f.conv))
		}
		return newTuple([]Value{strV(lit), strV(runesStr(f.name)), strV(runesStr(f.spec)), conv}), true, nil
	})}, nil
}

// formatterFieldNameSplit is _string.formatter_field_name_split(str): the
// first part of a field name (an int when it is all decimal digits) and an
// iterator over the (is_attr, key) pairs of its .attr and [key] parts.
func formatterFieldNameSplit(args []Value, kwargs []Keyword) (Value, error) {
	if err := ArgsOne("formatter_field_name_split", args, kwargs); err != nil {
		return nil, err
	}
	str, ok := unsub(args[0]).(strV)
	if !ok {
		return nil, raise("TypeError", "expected str, got %s", TypeName(args[0]))
	}
	first, rest := fmtFieldNameSplit(strRunes(string(str)))
	idx, err := fmtFieldIndex(first)
	if err != nil {
		return nil, err
	}
	var head Value = strV(runesStr(first))
	if idx != -1 {
		head = mkInt(int64(idx))
	}
	j := 0
	it := &iterObj{typ: "fieldnameiterator", it: funcIter(func() (Value, bool, error) {
		if j >= len(rest) {
			return nil, false, nil
		}
		isAttr, name, idx, err := fmtFieldNameNext(rest, &j)
		if err != nil {
			return nil, false, err
		}
		var key Value = strV(runesStr(name))
		if idx != -1 {
			key = mkInt(int64(idx))
		}
		return newTuple([]Value{pyBool(isAttr), key}), true, nil
	})}
	return newTuple([]Value{head, it}), nil
}
