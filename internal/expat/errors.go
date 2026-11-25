// Ported from libexpat 2.8.5 (expat/lib/expat.h, xmlparse.c) and CPython 3.12.15 (Modules/pyexpat.c) to Go,
// 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: MIT AND PSF-2.0

package expat

import "fmt"

// Error codes, as xml.parsers.expat.errors numbers them.
const (
	ErrNone = iota
	ErrNoMemory
	ErrSyntax
	ErrNoElements
	ErrInvalidToken
	ErrUnclosedToken
	ErrPartialChar
	ErrTagMismatch
	ErrDuplicateAttribute
	ErrJunkAfterDocElement
	ErrParamEntityRef
	ErrUndefinedEntity
	ErrRecursiveEntityRef
	ErrAsyncEntity
	ErrBadCharRef
	ErrBinaryEntityRef
	ErrAttributeExternalEntityRef
	ErrMisplacedXMLPI
	ErrUnknownEncoding
	ErrIncorrectEncoding
	ErrUnclosedCdataSection
	ErrExternalEntityHandling
	ErrNotStandalone
	ErrUnexpectedState
	ErrEntityDeclaredInPE
	ErrFeatureRequiresXMLDTD
	ErrCantChangeFeatureOnceParsing
	ErrUnboundPrefix
	ErrUndeclaringPrefix
	ErrIncompletePE
	ErrXMLDecl
	ErrTextDecl
	ErrPublicID
	ErrSuspended
	ErrNotSuspended
	ErrAborted
	ErrFinished
	ErrSuspendPE
	ErrReservedPrefixXML
	ErrReservedPrefixXMLNS
	ErrReservedNamespaceURI
	ErrInvalidArgument
	ErrNoBuffer
	ErrAmplificationLimitBreach
	ErrNotStarted
)

var errorStrings = [...]string{
	ErrNoMemory:                     "out of memory",
	ErrSyntax:                       "syntax error",
	ErrNoElements:                   "no element found",
	ErrInvalidToken:                 "not well-formed (invalid token)",
	ErrUnclosedToken:                "unclosed token",
	ErrPartialChar:                  "partial character",
	ErrTagMismatch:                  "mismatched tag",
	ErrDuplicateAttribute:           "duplicate attribute",
	ErrJunkAfterDocElement:          "junk after document element",
	ErrParamEntityRef:               "illegal parameter entity reference",
	ErrUndefinedEntity:              "undefined entity",
	ErrRecursiveEntityRef:           "recursive entity reference",
	ErrAsyncEntity:                  "asynchronous entity",
	ErrBadCharRef:                   "reference to invalid character number",
	ErrBinaryEntityRef:              "reference to binary entity",
	ErrAttributeExternalEntityRef:   "reference to external entity in attribute",
	ErrMisplacedXMLPI:               "XML or text declaration not at start of entity",
	ErrUnknownEncoding:              "unknown encoding",
	ErrIncorrectEncoding:            "encoding specified in XML declaration is incorrect",
	ErrUnclosedCdataSection:         "unclosed CDATA section",
	ErrExternalEntityHandling:       "error in processing external entity reference",
	ErrNotStandalone:                "document is not standalone",
	ErrUnexpectedState:              "unexpected parser state - please send a bug report",
	ErrEntityDeclaredInPE:           "entity declared in parameter entity",
	ErrFeatureRequiresXMLDTD:        "requested feature requires XML_DTD support in Expat",
	ErrCantChangeFeatureOnceParsing: "cannot change setting once parsing has begun",
	ErrUnboundPrefix:                "unbound prefix",
	ErrUndeclaringPrefix:            "must not undeclare prefix",
	ErrIncompletePE:                 "incomplete markup in parameter entity",
	ErrXMLDecl:                      "XML declaration not well-formed",
	ErrTextDecl:                     "text declaration not well-formed",
	ErrPublicID:                     "illegal character(s) in public id",
	ErrSuspended:                    "parser suspended",
	ErrNotSuspended:                 "parser not suspended",
	ErrAborted:                      "parsing aborted",
	ErrFinished:                     "parsing finished",
	ErrSuspendPE:                    "cannot suspend in external parameter entity",
	ErrReservedPrefixXML:            "reserved prefix (xml) must not be undeclared or bound to another namespace name",
	ErrReservedPrefixXMLNS:          "reserved prefix (xmlns) must not be declared or undeclared",
	ErrReservedNamespaceURI:         "prefix must not be bound to one of the reserved namespace names",
	ErrInvalidArgument:              "invalid argument",
	ErrNoBuffer:                     "a successful prior call to function XML_GetBuffer is required",
	ErrAmplificationLimitBreach:     "limit on input amplification factor (from DTD and entities) breached",
	ErrNotStarted:                   "parser not started",
}

// ErrorString is xml.parsers.expat.ErrorString: the message of an error code.
func ErrorString(code int) string {
	if code <= ErrNone || code >= len(errorStrings) {
		return ""
	}
	return errorStrings[code]
}

// Error is xml.parsers.expat.ExpatError.
type Error struct {
	// Code is the error code (ErrSyntax, ...).
	Code int
	// Msg is str() of the exception: "<message>: line L, column C".
	Msg string
	// Lineno and Offset are the 1-based line and 0-based column the
	// parser reports for the error.
	Lineno, Offset int
}

func (e *Error) Error() string { return e.Msg }

// NewError builds the ExpatError pyexpat raises for code at a position.
func NewError(code, lineno, offset int) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf("%s: line %d, column %d", ErrorString(code), lineno, offset),
		Lineno: lineno, Offset: offset}
}
