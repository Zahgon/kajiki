// Ported from CPython 3.12 (Objects/unicodeobject.c, Objects/bytesobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// str.encode and bytes.decode, on package codecs.

package py

import ()

func setCodecAttrs(x *excV, e *codecError, object Value) {
	x.setField("encoding", strV(e.Encoding))
	x.setField("object", object)
	x.setField("start", mkInt(int64(e.Start)))
	x.setField("end", mkInt(int64(e.End)))
	x.setField("reason", strV(e.Reason))
}

// optStrArg reads an optional str argument of a codec call; def stands for
// a missing one.
func optStrArg(v Value, def, fn, param string) (string, error) {
	if v == nil {
		return def, nil
	}
	s, ok := unsub(v).(strV)
	if !ok {
		return "", typeErr("%s() argument '%s' must be str, not %s", fn, param, methodsBadArgType(v))
	}
	return string(s), nil
}

// decodeBytes is bytes.decode(encoding, errors) as the str and bytes
// builtins reach it; nil arguments are the defaults.
func decodeBytes(b string, enc, errs Value) (Value, error) {
	e, err := optStrArg(enc, "utf-8", "str", "encoding")
	if err != nil {
		return nil, err
	}
	h, err := optStrArg(errs, "strict", "str", "errors")
	if err != nil {
		return nil, err
	}
	out, err := codecsDecode([]byte(b), e, h)
	if err != nil {
		return nil, fromLeaf(err)
	}
	return strV(out), nil
}

// encodeStr is str.encode(encoding, errors).
func encodeStr(s, enc, errs string) ([]byte, error) {
	out, err := codecsEncode(s, enc, errs)
	if err != nil {
		return nil, fromLeaf(err)
	}
	return out, nil
}
