// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/doctype.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/pyre"
)

// doctypeDecl is a DocumentTypeDeclaration: a known doctype and the
// rendering mode it selects.
type doctypeDecl struct {
	name          string
	renderingMode string
	regex         *pyre.Pattern
}

// newDoctypeDecl builds the declaration with its regex, made from its
// string form as Kajiki makes it: spaces match any white space and ".",
// "[" and "]" are escaped (other characters, like the "+" of XHTML+RDFa,
// keep their regex meaning), matched ignoring case.
func newDoctypeDecl(name, fpi string, uri *string, renderingMode, kind string) *doctypeDecl {
	parts := []string{"<!DOCTYPE", "html"}
	if kind != "" {
		parts = append(parts, kind)
	}
	if fpi != "" {
		parts = append(parts, `"`+fpi+`"`)
	}
	if uri != nil && *uri != "" {
		parts = append(parts, `"`+*uri+`"`)
	}
	s := strings.Join(parts, " ") + ">"
	s = strings.NewReplacer(" ", `\s+`, ".", `\.`, "[", `\[`, "]", `\]`).Replace(s)
	return &doctypeDecl{name: name, renderingMode: renderingMode, regex: pyre.MustCompile(s, pyre.IGNORECASE)}
}

func strp(s string) *string { return &s }

// knownDoctypes is DocumentTypeDeclaration.by_uri in its order. Two entries
// share the string "<!DOCTYPE html>"; the first one, html5, is the one
// that matches.
var knownDoctypes = []*doctypeDecl{
	newDoctypeDecl("html5", "", strp(""), "html5", ""),
	newDoctypeDecl("xhtml5", "", nil, "xml", ""),
	newDoctypeDecl("xhtml1transitional", "-//W3C//DTD XHTML 1.0 Transitional//EN",
		strp("http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd"), "xml", "PUBLIC"),
	newDoctypeDecl("xhtml1strict", "-//W3C//DTD XHTML 1.0 Strict//EN",
		strp("http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd"), "xml", "PUBLIC"),
	newDoctypeDecl("xhtml1rdfa", "-//W3C//DTD XHTML+RDFa 1.0//EN",
		strp("http://www.w3.org/MarkUp/DTD/xhtml-rdfa-1.dtd"), "xml", "PUBLIC"),
	newDoctypeDecl("xhtml11", "-//W3C//DTD XHTML 1.1//EN",
		strp("http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd"), "xml", "PUBLIC"),
	newDoctypeDecl("xhtml1frameset", "-//W3C//DTD XHTML 1.0 Frameset//EN",
		strp("http://www.w3.org/TR/xhtml1/DTD/xhtml1-frameset.dtd"), "xml", "PUBLIC"),
	newDoctypeDecl("xhtmlbasic11", "-//W3C//DTD XHTML Basic 1.1//EN",
		strp("http://www.w3.org/TR/xhtml-basic/xhtml-basic11.dtd"), "xml", "PUBLIC"),
	newDoctypeDecl("xhtmlmobile12", "-//WAPFORUM//DTD XHTML Mobile 1.2//EN",
		strp("http://www.openmobilealliance.org/tech/DTD/xhtml-mobile12.dtd"), "xml", "PUBLIC"),
	newDoctypeDecl("html4transitional", "-//W3C//DTD HTML 4.01 Transitional//EN",
		strp("http://www.w3.org/TR/html4/loose.dtd"), "html", "PUBLIC"),
	newDoctypeDecl("html4strict", "-//W3C//DTD HTML 4.01//EN",
		strp("http://www.w3.org/TR/html4/strict.dtd"), "html", "PUBLIC"),
	newDoctypeDecl("html4frameset", "-//W3C//DTD HTML 4.01 Frameset//EN",
		strp("http://www.w3.org/TR/html4/frameset.dtd"), "html", "PUBLIC"),
}

// matchingDoctype is DocumentTypeDeclaration.matching: the first known
// doctype whose regex matches at the start of dtd, or nil.
func matchingDoctype(dtd string) *doctypeDecl {
	text := []rune(dtd)
	for _, d := range knownDoctypes {
		if d.regex.Match(text, 0) != nil {
			return d
		}
	}
	return nil
}

var (
	// anyDoctype is DocumentTypeDeclaration.REGEX.
	anyDoctype = pyre.MustCompile(`<!DOCTYPE[^>]+>`, 0)
	// xmlDeclaration is doctype.XML_DECLARATION.
	xmlDeclaration = pyre.MustCompile(`<\?xml .*?\?>`, 0)
)

// extractDTD is extract_dtd: the first doctype found anywhere in markup
// (even inside a comment), where it was, and markup without it; with no
// doctype, the place for one is after an XML declaration at the very
// start, or the start.
func extractDTD(markup string) (dtd string, position int, rest string) {
	text := []rune(markup)
	m := anyDoctype.Search(text, 0)
	if m == nil {
		if d := xmlDeclaration.Match(text, 0); d != nil {
			return "", len(string(text[:d.End(0)])), markup
		}
		return "", 0, markup
	}
	found, _ := m.Group(0)
	return found, len(string(text[:m.Start(0)])), strings.Replace(markup, found, "", 1)
}
