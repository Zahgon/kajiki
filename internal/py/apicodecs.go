// Ported from CPython 3.12 (Lib/codecs.py, Lib/encodings/__init__.py, Objects/unicodeobject.c) to Go,
// 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The codecs for Go code: decoding bytes as bytes.decode, a text file's read() and
// codecs.open(...).read() do.

package py

// Decode decodes data with the codec named encoding (any name or alias
// codecs.lookup accepts) and errors="strict". With final set it is the
// decoding of bytes.decode and of a text file's read(): an incomplete
// sequence at the end is a UnicodeDecodeError. Without it, it is what
// codecs.open(...).read() makes of the bytes: the StreamReader's decoder,
// which leaves an incomplete sequence at the end undecoded (consumed counts
// the bytes decoded) and reads the byte order mark of utf-8-sig, utf-16 and
// utf-32 as those readers do. The errors are LookupError ("unknown encoding:
// ..."), UnicodeDecodeError and UnicodeError, as *Error.
func Decode(data []byte, encoding string, final bool) (text string, consumed int, err error) {
	c, err := lookupCodec(encoding)
	if err != nil {
		return "", 0, toError(fromLeaf(err))
	}
	if final {
		s, err := c.Decode(data, "strict")
		if err != nil {
			return "", 0, toError(fromLeaf(err))
		}
		return s, len(data), nil
	}
	s, n, err := c.streamDecode(data)
	if err != nil {
		return "", 0, toError(fromLeaf(err))
	}
	return s, n, nil
}

// EncodingModule is the module of CPython's encodings package that
// codecs.lookup finds for the name ("utf_8" for "UTF8", "shift_jis" for
// "sjis"), whether or not this package has that codec: pyexpat's handler
// of unknown encodings needs the name search alone. An unknown name is a
// LookupError.
func EncodingModule(encoding string) (string, error) {
	mod, err := encodingModule(encoding)
	if err != nil {
		return "", toError(fromLeaf(err))
	}
	return mod, nil
}

// streamDecode is the first decode(input, errors) call of the codec's
// StreamReader on the whole input: the decoder with final=False, the byte
// order mark of utf-8-sig skipped (and an input that may still become one
// left undecoded), and that of utf-16 and utf-32 required.
func (c *codecInfo) streamDecode(data []byte) (string, int, error) {
	switch c.Module {
	case "utf_8_sig":
		bom := "\xef\xbb\xbf"
		if len(data) < 3 && bom[:len(data)] == string(data) {
			return "", 0, nil
		}
		return c.decode(data, "strict", false, byteOrderDetect)
	case "utf_16", "utf_32":
		var s string
		var n, order, unit int
		var err error
		if c.Module == "utf_16" {
			s, n, order, err = decodeUTF16(data, "strict", false, byteOrderDetect)
			unit = 2
		} else {
			s, n, order, err = decodeUTF32(data, "strict", false, byteOrderDetect)
			unit = 4
		}
		if err != nil {
			return "", 0, err
		}
		if order == byteOrderDetect && n >= unit {
			name := "UTF-16"
			if unit == 4 {
				name = "UTF-32"
			}
			return "", 0, newPyErr("UnicodeError", name+" stream does not start with BOM")
		}
		return s, n, nil
	}
	return c.decode(data, "strict", false, byteOrderDetect)
}
