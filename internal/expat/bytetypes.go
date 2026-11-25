// Ported from libexpat 2.8.5 (expat/lib/xmltok_impl.h, asciitab.h, iasciitab.h, latin1tab.h, utf8tab.h) to Go,
// 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Licensed under the MIT license.
// SPDX-License-Identifier: MIT

package expat

// Byte types: the tokenizer's classification of a character by its first
// code unit.
const (
	btNonxml   = iota // not allowed in XML, e.g. U+FFFF
	btMalform         // illegal in the encoding
	btLt              // <
	btAmp             // &
	btRsqb            // ]
	btLead2           // lead byte of a 2-byte UTF-8 sequence
	btLead3           // lead byte of a 3-byte UTF-8 sequence
	btLead4           // lead of a 4-byte sequence (UTF-16: high surrogate)
	btTrail           // trailing unit
	btCr              // \r
	btLf              // \n
	btGt              // >
	btQuot            // "
	btApos            // '
	btEquals          // =
	btQuest           // ?
	btExcl            // !
	btSol             // /
	btSemi            // ;
	btNum             // #
	btLsqb            // [
	btS               // white space
	btNmstrt          // name start letter other than a hex digit
	btColon           // :
	btHex             // hex letter
	btDigit           // digit
	btName            // . and U+00B7
	btMinus           // -
	btOther           // known not to be a name character
	btNonascii        // might be a name or name start character
	btPercnt          // %
	btLpar            // (
	btRpar            // )
	btAst             // *
	btPlus            // +
	btComma           // ,
	btVerbar          // |
)

// asciiTypes is asciitab.h: the types of the bytes 0x00-0x7F.
var asciiTypes = [128]byte{
	btNonxml, btNonxml, btNonxml, btNonxml,
	btNonxml, btNonxml, btNonxml, btNonxml,
	btNonxml, btS, btLf, btNonxml,
	btNonxml, btCr, btNonxml, btNonxml,
	btNonxml, btNonxml, btNonxml, btNonxml,
	btNonxml, btNonxml, btNonxml, btNonxml,
	btNonxml, btNonxml, btNonxml, btNonxml,
	btNonxml, btNonxml, btNonxml, btNonxml,
	btS, btExcl, btQuot, btNum,
	btOther, btPercnt, btAmp, btApos,
	btLpar, btRpar, btAst, btPlus,
	btComma, btMinus, btName, btSol,
	btDigit, btDigit, btDigit, btDigit,
	btDigit, btDigit, btDigit, btDigit,
	btDigit, btDigit, btColon, btSemi,
	btLt, btEquals, btGt, btQuest,
	btOther, btHex, btHex, btHex,
	btHex, btHex, btHex, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btLsqb,
	btOther, btRsqb, btOther, btNmstrt,
	btOther, btHex, btHex, btHex,
	btHex, btHex, btHex, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btOther,
	btVerbar, btOther, btOther, btOther,
}

// latin1Types is latin1tab.h: the types of the Latin-1 bytes 0x80-0xFF.
var latin1Types = [128]byte{
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btNmstrt, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btOther, btOther, btOther,
	btOther, btNmstrt, btOther, btName,
	btOther, btOther, btNmstrt, btOther,
	btOther, btOther, btOther, btOther,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btOther,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btOther,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
	btNmstrt, btNmstrt, btNmstrt, btNmstrt,
}

// utf8Types is utf8tab.h: the types of the UTF-8 bytes 0x80-0xFF.
var utf8Types = [128]byte{
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btTrail, btTrail, btTrail, btTrail,
	btLead2, btLead2, btLead2, btLead2,
	btLead2, btLead2, btLead2, btLead2,
	btLead2, btLead2, btLead2, btLead2,
	btLead2, btLead2, btLead2, btLead2,
	btLead2, btLead2, btLead2, btLead2,
	btLead2, btLead2, btLead2, btLead2,
	btLead2, btLead2, btLead2, btLead2,
	btLead2, btLead2, btLead2, btLead2,
	btLead3, btLead3, btLead3, btLead3,
	btLead3, btLead3, btLead3, btLead3,
	btLead3, btLead3, btLead3, btLead3,
	btLead3, btLead3, btLead3, btLead3,
	btLead4, btLead4, btLead4, btLead4,
	btLead4, btNonxml, btNonxml, btNonxml,
	btNonxml, btNonxml, btNonxml, btNonxml,
	btNonxml, btNonxml, btMalform, btMalform,
}
