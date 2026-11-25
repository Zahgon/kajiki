// Ported from CPython 3.12 (Objects/bytesobject.c, Objects/bytearrayobject.c,
// Objects/bytes_methods.c, Objects/stringlib) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The methods of bytes and bytearray, and the methods only bytearray has (append, extend, pop, ...).

package py

import (
	"strings"
)

// asciiSpace is the whitespace of bytes methods.
const asciiSpace = " \t\n\r\x0b\x0c"

func isASCIISpace(c byte) bool { return strings.IndexByte(asciiSpace, c) >= 0 }
func isASCIILower(c byte) bool { return c >= 'a' && c <= 'z' }
func isASCIIUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func asciiToUpper(c byte) byte {
	if isASCIILower(c) {
		return c - 32
	}
	return c
}
func asciiToLower(c byte) byte {
	if isASCIIUpper(c) {
		return c + 32
	}
	return c
}

// bytesLikeArg is the bytes of a bytes-like argument, with the TypeError of
// a function that requires one.
func bytesLikeArg(v Value) (string, error) {
	if b, ok := bytesOf(unsub(v)); ok {
		return b, nil
	}
	return "", typeErr("a bytes-like object is required, not '%s'", typeName(v))
}

// bytesSubArg is the needle of find, count, index and their right-hand
// variants: bytes-like, or an int that stands for one byte.
func bytesSubArg(v Value) (string, error) {
	if b, ok := bytesOf(unsub(v)); ok {
		return b, nil
	}
	if i, ok := asInt(v); ok {
		if !i.IsInt64() || i.Int64() < 0 || i.Int64() > 255 {
			return "", valueErr("byte must be in range(0, 256)")
		}
		return string([]byte{byte(i.Int64())}), nil
	}
	return "", typeErr("argument should be integer or bytes-like object, not '%s'", typeName(v))
}

// bytesFillArg is the fill character of center, ljust and rjust.
func bytesFillArg(fn string, v Value) (byte, error) {
	if v == nil {
		return ' ', nil
	}
	b, ok := bytesOf(unsub(v))
	if !ok || len(b) != 1 {
		return 0, typeErr("%s() argument 2 must be a byte string of length 1, not %s", fn, typeName(v))
	}
	return b[0], nil
}

// bytesMethod is an attribute of a bytes or a bytearray.
func bytesMethod(obj Value, name string) (Value, bool) {
	b, _ := bytesOf(obj)
	typ := obj.pyType()
	ba, mutable := obj.(*bytearrayV)
	wrap := func(s string) Value {
		if mutable {
			return &bytearrayV{b: []byte(s)}
		}
		return bytesV(s)
	}
	bound := func(fn func(args []Value, kw *dictV) (Value, error)) (Value, bool) {
		return methodsBound(obj, name, fn), true
	}
	noArgs := func(fn func() Value) (Value, bool) {
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone(typ+"."+name, args, kw); err != nil {
				return nil, err
			}
			return fn(), nil
		})
	}
	pred := func(f func(string) bool) (Value, bool) {
		return noArgs(func() Value { return pyBool(f(b)) })
	}
	mapBytes := func(f func(string) string) (Value, bool) {
		return noArgs(func() Value { return wrap(f(b)) })
	}
	if v, ok := builtinClassAttr(typ, classOf(obj), name); ok {
		return v, true
	}
	switch name {
	case "decode":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKw("decode", []string{"encoding", "errors"}, 2, args, kw)
			if err != nil {
				return nil, err
			}
			return decodeBytes(b, buf[0], buf[1])
		})
	case "hex":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKw("hex", []string{"sep", "bytes_per_sep"}, 2, args, kw)
			if err != nil {
				return nil, err
			}
			return bytesHex(b, buf[0], buf[1])
		})
	case "upper":
		return mapBytes(func(s string) string { return mapEachByte(s, asciiToUpper) })
	case "lower":
		return mapBytes(func(s string) string { return mapEachByte(s, asciiToLower) })
	case "swapcase":
		return mapBytes(func(s string) string {
			return mapEachByte(s, func(c byte) byte {
				if isASCIILower(c) {
					return c - 32
				}
				return asciiToLower(c)
			})
		})
	case "capitalize":
		return mapBytes(func(s string) string {
			out := []byte(s)
			for i := range out {
				if i == 0 {
					out[i] = asciiToUpper(out[i])
				} else {
					out[i] = asciiToLower(out[i])
				}
			}
			return string(out)
		})
	case "title":
		return mapBytes(bytesTitle)
	case "isalpha":
		return pred(func(s string) bool {
			return s != "" && allBytes(s, func(c byte) bool { return isASCIILetter(rune(c)) })
		})
	case "isalnum":
		return pred(func(s string) bool {
			return s != "" && allBytes(s, func(c byte) bool { return isASCIIAlnum(rune(c)) })
		})
	case "isdigit":
		return pred(func(s string) bool { return s != "" && allBytes(s, func(c byte) bool { return isASCIIDigit(rune(c)) }) })
	case "isspace":
		return pred(func(s string) bool { return s != "" && allBytes(s, isASCIISpace) })
	case "isascii":
		return pred(func(s string) bool { return allBytes(s, func(c byte) bool { return c < 0x80 }) })
	case "islower":
		return pred(func(s string) bool {
			return strings.IndexFunc(s, func(r rune) bool { return r < 0x80 && isASCIIUpper(byte(r)) }) < 0 &&
				strings.IndexFunc(s, func(r rune) bool { return r < 0x80 && isASCIILower(byte(r)) }) >= 0
		})
	case "isupper":
		return pred(func(s string) bool {
			return strings.IndexFunc(s, func(r rune) bool { return r < 0x80 && isASCIILower(byte(r)) }) < 0 &&
				strings.IndexFunc(s, func(r rune) bool { return r < 0x80 && isASCIIUpper(byte(r)) }) >= 0
		})
	case "istitle":
		return pred(bytesIsTitle)
	case "find", "rfind", "index", "rindex", "count":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsPos(name, 1, 3, args, kw); err != nil {
				return nil, err
			}
			sub, err := bytesSubArg(args[0])
			if err != nil {
				return nil, err
			}
			st, en, err := methodsSearchBounds(args[1:])
			if err != nil {
				return nil, err
			}
			start, end := pyAdjustIndices(st, en, len(b))
			if name == "count" {
				if start > len(b) || end < start {
					return mkInt(0), nil
				}
				if sub == "" {
					return mkInt(int64(end - start + 1)), nil
				}
				return mkInt(int64(strings.Count(b[start:end], sub))), nil
			}
			i := -1
			if start <= len(b) && end >= start && end-start >= len(sub) {
				if name == "find" || name == "index" {
					i = strings.Index(b[start:end], sub)
				} else {
					i = strings.LastIndex(b[start:end], sub)
				}
				if i >= 0 {
					i += start
				}
			}
			if i < 0 && (name == "index" || name == "rindex") {
				return nil, valueErr("subsection not found")
			}
			return mkInt(int64(i)), nil
		})
	case "startswith", "endswith":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsPos(name, 1, 3, args, kw); err != nil {
				return nil, err
			}
			st, en, err := methodsSearchBounds(args[1:])
			if err != nil {
				return nil, err
			}
			match := func(v Value) (bool, error) {
				sub, ok := bytesOf(unsub(v))
				if !ok {
					return false, typeErr("%s first arg must be bytes or a tuple of bytes, not %s", name, typeName(v))
				}
				start, end := pyAdjustIndices(st, en, len(b))
				if start > len(b) || end-start < len(sub) {
					return false, nil
				}
				if name == "startswith" {
					return strings.HasPrefix(b[start:end], sub), nil
				}
				return strings.HasSuffix(b[start:end], sub), nil
			}
			if t, ok := args[0].(*tupleV); ok {
				for _, it := range t.items {
					m, err := match(it)
					if err != nil || m {
						return pyBool(m), err
					}
				}
				return falseV, nil
			}
			m, err := match(args[0])
			return pyBool(m), err
		})
	case "strip", "lstrip", "rstrip":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsPos(name, 0, 1, args, kw); err != nil {
				return nil, err
			}
			cut := asciiSpace
			if len(args) == 1 && !isNoneV(args[0]) {
				var err error
				if cut, err = bytesLikeArg(args[0]); err != nil {
					return nil, err
				}
			}
			switch name {
			case "lstrip":
				return wrap(strings.TrimLeft(b, cut)), nil
			case "rstrip":
				return wrap(strings.TrimRight(b, cut)), nil
			}
			return wrap(strings.Trim(b, cut)), nil
		})
	case "split", "rsplit":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKw(name, []string{"sep", "maxsplit"}, 2, args, kw)
			if err != nil {
				return nil, err
			}
			maxsplit := -1
			if buf[1] != nil {
				if maxsplit, err = methodsSsize(buf[1]); err != nil {
					return nil, err
				}
			}
			var parts []string
			if buf[0] == nil || isNoneV(buf[0]) {
				parts = bytesSplitWhitespace(b, maxsplit, name == "rsplit")
			} else {
				sep, err := bytesLikeArg(buf[0])
				if err != nil {
					return nil, err
				}
				if sep == "" {
					return nil, valueErr("empty separator")
				}
				parts = bytesSplitSep(b, sep, maxsplit, name == "rsplit")
			}
			items := make([]Value, len(parts))
			for i, p := range parts {
				items[i] = wrap(p)
			}
			return newList(items), nil
		})
	case "splitlines":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKw("splitlines", []string{"keepends"}, 1, args, kw)
			if err != nil {
				return nil, err
			}
			keep := false
			if buf[0] != nil {
				if keep, err = truthy(buf[0]); err != nil {
					return nil, err
				}
			}
			var items []Value
			for i := 0; i < len(b); {
				j := i
				for j < len(b) && b[j] != '\n' && b[j] != '\r' {
					j++
				}
				eol := j
				if j < len(b) {
					if b[j] == '\r' && j+1 < len(b) && b[j+1] == '\n' {
						j += 2
					} else {
						j++
					}
					if keep {
						eol = j
					}
				}
				items = append(items, wrap(b[i:eol]))
				i = j
			}
			return newList(items), nil
		})
	case "partition", "rpartition":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne(name, args, kw); err != nil {
				return nil, err
			}
			sep, err := bytesLikeArg(args[0])
			if err != nil {
				return nil, err
			}
			if sep == "" {
				return nil, valueErr("empty separator")
			}
			var i int
			if name == "partition" {
				i = strings.Index(b, sep)
				if i < 0 {
					return newTuple([]Value{wrap(b), wrap(""), wrap("")}), nil
				}
			} else {
				i = strings.LastIndex(b, sep)
				if i < 0 {
					return newTuple([]Value{wrap(""), wrap(""), wrap(b)}), nil
				}
			}
			return newTuple([]Value{wrap(b[:i]), wrap(sep), wrap(b[i+len(sep):])}), nil
		})
	case "join":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne(typ+".join", args, kw); err != nil {
				return nil, err
			}
			items, err := toSliceAs(args[0], typeErr("can only join an iterable"))
			if err != nil {
				return nil, err
			}
			parts := make([]string, len(items))
			for i, it := range items {
				p, ok := bytesOf(unsub(it))
				if !ok {
					return nil, typeErr("sequence item %d: expected a bytes-like object, %s found", i, typeName(it))
				}
				parts[i] = p
			}
			return wrap(strings.Join(parts, b)), nil
		})
	case "replace":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsPos("replace", 2, 3, args, kw); err != nil {
				return nil, err
			}
			old, err := bytesLikeArg(args[0])
			if err != nil {
				return nil, err
			}
			repl, err := bytesLikeArg(args[1])
			if err != nil {
				return nil, err
			}
			count := -1
			if len(args) == 3 {
				if count, err = methodsSsize(args[2]); err != nil {
					return nil, err
				}
			}
			return wrap(pyReplaceBytes(b, old, repl, count)), nil
		})
	case "center", "ljust", "rjust":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsPos(name, 1, 2, args, kw); err != nil {
				return nil, err
			}
			width, err := methodsSsize(args[0])
			if err != nil {
				return nil, err
			}
			var fillV Value
			if len(args) == 2 {
				fillV = args[1]
			}
			fill, err := bytesFillArg(name, fillV)
			if err != nil {
				return nil, err
			}
			if width <= len(b) {
				return wrap(b), nil
			}
			pad := width - len(b)
			left := 0
			switch name {
			case "center":
				left = pad/2 + (pad & width & 1)
			case "rjust":
				left = pad
			}
			f := string([]byte{fill})
			return wrap(strings.Repeat(f, left) + b + strings.Repeat(f, pad-left)), nil
		})
	case "zfill":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne(name, args, kw); err != nil {
				return nil, err
			}
			width, err := methodsSsize(args[0])
			if err != nil {
				return nil, err
			}
			return wrap(pyZfill(b, width)), nil
		})
	case "expandtabs":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKw("expandtabs", []string{"tabsize"}, 1, args, kw)
			if err != nil {
				return nil, err
			}
			tab := 8
			if buf[0] != nil {
				if tab, err = methodsCInt(buf[0]); err != nil {
					return nil, err
				}
			}
			return wrap(bytesExpandtabs(b, tab)), nil
		})
	case "removeprefix", "removesuffix":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne(name, args, kw); err != nil {
				return nil, err
			}
			affix, err := bytesLikeArg(args[0])
			if err != nil {
				return nil, err
			}
			if name == "removeprefix" {
				return wrap(strings.TrimPrefix(b, affix)), nil
			}
			return wrap(strings.TrimSuffix(b, affix)), nil
		})
	case "translate":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKw("translate", []string{"table", "delete"}, 2, args, kw)
			if err != nil {
				return nil, err
			}
			if buf[0] == nil {
				return nil, typeErr("translate() missing required argument 'table' (pos 1)")
			}
			var table string
			if !isNoneV(buf[0]) {
				if table, err = bytesLikeArg(buf[0]); err != nil {
					return nil, err
				}
				if len(table) != 256 {
					return nil, valueErr("translation table must be 256 characters long")
				}
			}
			del := ""
			if buf[1] != nil {
				if del, err = bytesLikeArg(buf[1]); err != nil {
					return nil, err
				}
			}
			out := make([]byte, 0, len(b))
			for i := 0; i < len(b); i++ {
				if strings.IndexByte(del, b[i]) >= 0 {
					continue
				}
				if table != "" {
					out = append(out, table[b[i]])
				} else {
					out = append(out, b[i])
				}
			}
			return wrap(string(out)), nil
		})
	}
	if mutable {
		if m, ok := bytearrayMethod(ba, name); ok {
			return m, true
		}
	}
	return slotWrapper(obj, name)
}

func mapEachByte(s string, f func(byte) byte) string {
	out := []byte(s)
	for i, c := range out {
		out[i] = f(c)
	}
	return string(out)
}

func allBytes(s string, f func(byte) bool) bool {
	for i := 0; i < len(s); i++ {
		if !f(s[i]) {
			return false
		}
	}
	return true
}

// bytesTitle is bytes.title(): each run of letters starts upper case.
func bytesTitle(s string) string {
	out := []byte(s)
	prevCased := false
	for i, c := range out {
		switch {
		case isASCIILower(c):
			if !prevCased {
				out[i] = c - 32
			}
			prevCased = true
		case isASCIIUpper(c):
			if prevCased {
				out[i] = c + 32
			}
			prevCased = true
		default:
			prevCased = false
		}
	}
	return string(out)
}

// bytesIsTitle is bytes.istitle().
func bytesIsTitle(s string) bool {
	cased, prevCased := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case isASCIIUpper(c):
			if prevCased {
				return false
			}
			prevCased, cased = true, true
		case isASCIILower(c):
			if !prevCased {
				return false
			}
			prevCased, cased = true, true
		default:
			prevCased = false
		}
	}
	return cased
}

// bytesSplitWhitespace is split() and rsplit() without a separator: runs of
// ASCII whitespace separate the parts, at most maxsplit times (-1: no limit).
func bytesSplitWhitespace(s string, maxsplit int, fromRight bool) []string {
	var parts []string
	if !fromRight {
		i := 0
		for maxsplit != 0 {
			for i < len(s) && isASCIISpace(s[i]) {
				i++
			}
			if i == len(s) {
				return parts
			}
			j := i
			for j < len(s) && !isASCIISpace(s[j]) {
				j++
			}
			if j == len(s) {
				return append(parts, s[i:])
			}
			parts = append(parts, s[i:j])
			i = j
			maxsplit--
		}
		for i < len(s) && isASCIISpace(s[i]) {
			i++
		}
		if i < len(s) {
			parts = append(parts, s[i:])
		}
		return parts
	}
	j := len(s)
	for maxsplit != 0 {
		for j > 0 && isASCIISpace(s[j-1]) {
			j--
		}
		if j == 0 {
			break
		}
		i := j
		for i > 0 && !isASCIISpace(s[i-1]) {
			i--
		}
		parts = append(parts, s[i:j])
		j = i
		maxsplit--
	}
	if maxsplit == 0 {
		for j > 0 && isASCIISpace(s[j-1]) {
			j--
		}
		if j > 0 {
			parts = append(parts, s[:j])
		}
	}
	for i, k := 0, len(parts)-1; i < k; i, k = i+1, k-1 {
		parts[i], parts[k] = parts[k], parts[i]
	}
	return parts
}

// bytesSplitSep is split(sep) and rsplit(sep) at most maxsplit times.
func bytesSplitSep(s, sep string, maxsplit int, fromRight bool) []string {
	if maxsplit < 0 {
		return strings.Split(s, sep)
	}
	if !fromRight {
		return strings.SplitN(s, sep, maxsplit+1)
	}
	var parts []string
	for maxsplit > 0 {
		i := strings.LastIndex(s, sep)
		if i < 0 {
			break
		}
		parts = append(parts, s[i+len(sep):])
		s = s[:i]
		maxsplit--
	}
	parts = append(parts, s)
	for i, k := 0, len(parts)-1; i < k; i, k = i+1, k-1 {
		parts[i], parts[k] = parts[k], parts[i]
	}
	return parts
}

// pyReplaceBytes is bytes.replace(old, new, count).
func pyReplaceBytes(s, old, repl string, count int) string {
	if count < 0 {
		count = -1
	}
	if old == "" {
		if count < 0 || count > len(s)+1 {
			count = len(s) + 1
		}
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if i < count {
				b.WriteString(repl)
			}
			b.WriteByte(s[i])
		}
		if count > len(s) {
			b.WriteString(repl)
		}
		return b.String()
	}
	return strings.Replace(s, old, repl, count)
}

// bytesExpandtabs is bytes.expandtabs(tabsize): columns restart after \n
// and \r.
func bytesExpandtabs(s string, tab int) string {
	var b strings.Builder
	col := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\t':
			if tab > 0 {
				n := tab - col%tab
				b.WriteString(strings.Repeat(" ", n))
				col += n
			}
		case '\n', '\r':
			b.WriteByte(c)
			col = 0
		default:
			b.WriteByte(c)
			col++
		}
	}
	return b.String()
}

// bytesHex is bytes.hex(sep, bytes_per_sep): the separator goes between
// groups counted from the right (from the left for a negative count).
func bytesHex(b string, sepV, perV Value) (Value, error) {
	const digits = "0123456789abcdef"
	sep := ""
	if sepV != nil {
		switch x := unsub(sepV).(type) {
		case strV:
			sep = string(x)
		default:
			s, ok := bytesOf(x)
			if !ok {
				return nil, typeErr("sep must be str or bytes.")
			}
			sep = s
		}
		if len([]rune(sep)) != 1 {
			return nil, valueErr("sep must be length 1.")
		}
		if sep[0] >= 0x80 {
			return nil, valueErr("sep must be ASCII.")
		}
	}
	per := 1
	if perV != nil {
		p, err := methodsCInt(perV)
		if err != nil {
			return nil, err
		}
		per = p
	}
	var out strings.Builder
	n := len(b)
	group := per
	if group < 0 {
		group = -group
	}
	for i := 0; i < n; i++ {
		if sep != "" && i > 0 && group > 0 {
			var boundary bool
			if per > 0 {
				boundary = (n-i)%group == 0
			} else {
				boundary = i%group == 0
			}
			if boundary {
				out.WriteString(sep)
			}
		}
		out.WriteByte(digits[b[i]>>4])
		out.WriteByte(digits[b[i]&15])
	}
	return strV(out.String()), nil
}

// bytesFromhex is the class method bytes.fromhex(string) (bytearray.fromhex
// when mutable): pairs of hexadecimal digits, ASCII whitespace allowed
// before each pair.
func bytesFromhex(mutable bool) classMethodFn {
	return func(c *callCtx, cls *classV, args []Value, kw *dictV) (Value, error) {
		if err := methodsArgsOne("fromhex", args, kw); err != nil {
			return nil, err
		}
		s, ok := unsub(args[0]).(strV)
		if !ok {
			return nil, typeErr("fromhex() argument must be str, not %s", typeName(args[0]))
		}
		rs := []rune(string(s))
		hexval := func(r rune) int {
			switch {
			case r >= '0' && r <= '9':
				return int(r - '0')
			case r >= 'a' && r <= 'f':
				return int(r-'a') + 10
			case r >= 'A' && r <= 'F':
				return int(r-'A') + 10
			}
			return -1
		}
		var out []byte
		for i := 0; i < len(rs); {
			if rs[i] < 0x80 && isASCIISpace(byte(rs[i])) {
				i++
				continue
			}
			hi := hexval(rs[i])
			if hi < 0 {
				return nil, valueErr("non-hexadecimal number found in fromhex() arg at position %d", i)
			}
			if i+1 >= len(rs) || hexval(rs[i+1]) < 0 {
				return nil, valueErr("non-hexadecimal number found in fromhex() arg at position %d", i+1)
			}
			out = append(out, byte(hi<<4|hexval(rs[i+1])))
			i += 2
		}
		var v Value = bytesV(out)
		builtin := "bytes"
		if mutable {
			v, builtin = &bytearrayV{b: out}, "bytearray"
		}
		return fromClassMethodResult(c, cls, builtin, v)
	}
}

// bytesMaketrans is the static method bytes.maketrans(frm, to).
func bytesMaketrans(args []Value, kw *dictV) (Value, error) {
	if err := posOnly("maketrans", 2, 2, args, kw); err != nil {
		return nil, err
	}
	frm, err := bytesLikeArg(args[0])
	if err != nil {
		return nil, err
	}
	to, err := bytesLikeArg(args[1])
	if err != nil {
		return nil, err
	}
	if len(frm) != len(to) {
		return nil, valueErr("maketrans arguments must have same length")
	}
	table := make([]byte, 256)
	for i := range table {
		table[i] = byte(i)
	}
	for i := 0; i < len(frm); i++ {
		table[frm[i]] = to[i]
	}
	return bytesV(table), nil
}

// byteValue is an int argument that must be one byte.
func byteValue(v Value) (byte, error) {
	n, err := methodsIndex(v)
	if err != nil {
		return 0, err
	}
	if !n.IsInt64() || n.Int64() < 0 || n.Int64() > 255 {
		return 0, valueErr("byte must be in range(0, 256)")
	}
	return byte(n.Int64()), nil
}

// bytearrayMethod is a method only bytearray has.
func bytearrayMethod(ba *bytearrayV, name string) (Value, bool) {
	bound := func(fn func(args []Value, kw *dictV) (Value, error)) (Value, bool) {
		return methodsBound(ba, name, fn), true
	}
	switch name {
	case "append":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("append", args, kw); err != nil {
				return nil, err
			}
			c, err := byteValue(args[0])
			if err != nil {
				return nil, err
			}
			ba.b = append(ba.b, c)
			return none, nil
		})
	case "extend":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("extend", args, kw); err != nil {
				return nil, err
			}
			if s, ok := bytesOf(unsub(args[0])); ok {
				ba.b = append(ba.b, s...)
				return none, nil
			}
			if _, isStr := unsub(args[0]).(strV); isStr {
				return nil, typeErr("expected iterable of integers; got: 'str'")
			}
			items, err := toSliceAs(args[0], typeErr("can't extend bytearray with %s", typeName(args[0])))
			if err != nil {
				return nil, err
			}
			add := make([]byte, len(items))
			for i, it := range items {
				if add[i], err = byteValue(it); err != nil {
					return nil, err
				}
			}
			ba.b = append(ba.b, add...)
			return none, nil
		})
	case "insert":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsPos("insert", 2, 2, args, kw); err != nil {
				return nil, err
			}
			i, err := methodsSsize(args[0])
			if err != nil {
				return nil, err
			}
			c, err := byteValue(args[1])
			if err != nil {
				return nil, err
			}
			n := len(ba.b)
			if i < 0 {
				i = max(i+n, 0)
			}
			i = min(i, n)
			ba.b = append(ba.b[:i], append([]byte{c}, ba.b[i:]...)...)
			return none, nil
		})
	case "pop":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsPos("pop", 0, 1, args, kw); err != nil {
				return nil, err
			}
			if len(ba.b) == 0 {
				return nil, newPyErr("IndexError", "pop from empty bytearray")
			}
			i := len(ba.b) - 1
			if len(args) == 1 {
				var err error
				if i, err = methodsSsize(args[0]); err != nil {
					return nil, err
				}
				if i < 0 {
					i += len(ba.b)
				}
				if i < 0 || i >= len(ba.b) {
					return nil, newPyErr("IndexError", "pop index out of range")
				}
			}
			c := ba.b[i]
			ba.b = append(ba.b[:i], ba.b[i+1:]...)
			return mkInt(int64(c)), nil
		})
	case "remove":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsOne("remove", args, kw); err != nil {
				return nil, err
			}
			c, err := byteValue(args[0])
			if err != nil {
				return nil, err
			}
			i := strings.IndexByte(string(ba.b), c)
			if i < 0 {
				return nil, valueErr("value not found in bytearray")
			}
			ba.b = append(ba.b[:i], ba.b[i+1:]...)
			return none, nil
		})
	case "reverse":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("bytearray.reverse", args, kw); err != nil {
				return nil, err
			}
			for i, j := 0, len(ba.b)-1; i < j; i, j = i+1, j-1 {
				ba.b[i], ba.b[j] = ba.b[j], ba.b[i]
			}
			return none, nil
		})
	case "clear":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("bytearray.clear", args, kw); err != nil {
				return nil, err
			}
			ba.b = nil
			return none, nil
		})
	case "copy":
		return bound(func(args []Value, kw *dictV) (Value, error) {
			if err := methodsArgsNone("bytearray.copy", args, kw); err != nil {
				return nil, err
			}
			return &bytearrayV{b: append([]byte(nil), ba.b...)}, nil
		})
	}
	return nil, false
}
