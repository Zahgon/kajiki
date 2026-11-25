// Ported from libexpat 2.8.5 and CPython 3.12.15 (Modules/pyexpat.c) to Go, 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: MIT AND PSF-2.0

// Package expat is xml.parsers.expat as CPython 3.12 builds it on libexpat
// 2.8.5, for parsers fed with str data.
//
// It reproduces expat's tokenizer, prolog grammar and parser (encoding
// detection, UTF-8 and UTF-16, XML declarations, DTD declarations,
// parameter entities, attribute defaults and normalization, skipped
// entities, namespaces when enabled, well-formedness errors and the
// positions it reports, the input buffer and the deferred reparsing of
// partial tokens) and pyexpat's layer over it: handler attributes that may
// fail, character data buffering with buffer_text and ExpatError.
//
// A Parser is used as pyexpat's xmlparser is:
//
//	p := expat.NewParser()            // or NewParserNS('}')
//	p.StartElementHandler = func(name string, attrs []string) error { ... }
//	...
//	err := p.Parse(chunk, false)      // for each chunk
//	err = p.Parse("", true)           // at the end
//
// Handlers read p.CurrentLineNumber() and p.CurrentColumnNumber() for the
// position of the event being reported. Parse returns the first error a
// handler returned, or an *Error (ExpatError) for a well-formedness error.
package expat
