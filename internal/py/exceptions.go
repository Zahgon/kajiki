// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Lookups of the builtin exception classes by name, for Go code.

package py

// namedExcClass is the builtin exception class of the given name (or of an
// alias such as IOError), or nil.
func namedExcClass(name string) *classV {
	if k, ok := classes[name]; ok && isExcClass(k) {
		return k
	}
	if alias, ok := excAliases[name]; ok {
		return classes[alias]
	}
	return nil
}

// excIsInstance reports whether e is an instance of the named builtin
// exception class.
func excIsInstance(e *excV, class string) bool {
	k := namedExcClass(class)
	return k != nil && excClass(e).subclassOf(k)
}
