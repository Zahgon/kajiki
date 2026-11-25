// Ported from CPython 3.12 (Python/compile.c, Python/symtable.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Private name mangling: in a class body and the functions nested in it, the compiler reads and
// writes the names __spam as _ClassName__spam.

package py

// mangleTree mangles the private names of a compiled tree in place: the
// names variables, attributes, parameters, imports, except clauses and
// capture patterns use inside class bodies. The names functions and classes
// bind are mangled where they are stored, as their __name__ keeps the name
// written.
func mangleTree(n *Node, private string) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "ClassDef":
		for _, l := range [][]*Node{n.L3, n.L1} {
			for _, c := range l {
				mangleTree(c, private)
			}
		}
		for _, k := range n.L2 {
			mangleTree(k.A, private)
		}
		for _, s := range n.Body {
			mangleTree(s, n.ID)
		}
		return
	case "Name", "Attribute", "arg", "ExceptHandler", "MatchAs", "MatchStar", "MatchMapping":
		if !n.NoID {
			n.ID = mangle(private, n.ID)
		}
	case "alias":
		n.ID = mangle(private, n.ID)
		if !n.NoAsname {
			n.Asname = mangle(private, n.Asname)
		}
	case "Global", "Nonlocal":
		for i, name := range n.Names {
			n.Names[i] = mangle(private, name)
		}
	case "keyword":
		// A keyword argument's name is not mangled.
		mangleTree(n.A, private)
		return
	}
	for _, f := range Fields(n) {
		mangleTree(f.Node, private)
		for _, c := range f.Nodes {
			mangleTree(c, private)
		}
	}
}
