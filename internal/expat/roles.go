// Ported from libexpat 2.8.5 (expat/lib/xmlrole.c, xmlrole.h) to Go, 2026-11-22.
// Copyright (c) 1997-2000 Thai Open Source Software Center Ltd
// Copyright (c) 2000-2026 Expat development team
// Licensed under the MIT license.
// SPDX-License-Identifier: MIT

package expat

// Roles: what a prolog token means in its context.
const (
	roleError = iota - 1
	roleNone
	roleXMLDecl
	roleInstanceStart
	roleDoctypeNone
	roleDoctypeName
	roleDoctypeSystemID
	roleDoctypePublicID
	roleDoctypeInternalSubset
	roleDoctypeClose
	roleGeneralEntityName
	roleParamEntityName
	roleEntityNone
	roleEntityValue
	roleEntitySystemID
	roleEntityPublicID
	roleEntityComplete
	roleEntityNotationName
	roleNotationNone
	roleNotationName
	roleNotationSystemID
	roleNotationNoSystemID
	roleNotationPublicID
	roleAttributeName
	roleAttributeTypeCdata
	roleAttributeTypeID
	roleAttributeTypeIDRef
	roleAttributeTypeIDRefs
	roleAttributeTypeEntity
	roleAttributeTypeEntities
	roleAttributeTypeNmtoken
	roleAttributeTypeNmtokens
	roleAttributeEnumValue
	roleAttributeNotationValue
	roleAttlistNone
	roleAttlistElementName
	roleImpliedAttributeValue
	roleRequiredAttributeValue
	roleDefaultAttributeValue
	roleFixedAttributeValue
	roleElementNone
	roleElementName
	roleContentAny
	roleContentEmpty
	roleContentPcdata
	roleGroupOpen
	roleGroupClose
	roleGroupCloseRep
	roleGroupCloseOpt
	roleGroupClosePlus
	roleGroupChoice
	roleGroupSequence
	roleContentElement
	roleContentElementRep
	roleContentElementOpt
	roleContentElementPlus
	rolePI
	roleComment
	roleTextDecl
	roleIgnoreSect
	roleInnerParamEntityRef
	roleParamEntityRef
)

// prologHandler is one state of the prolog grammar.
type prologHandler func(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int

// prologState is expat's PROLOG_STATE.
type prologState struct {
	handler        prologHandler
	level          int
	roleNone       int
	includeLevel   int
	documentEntity bool
	inEntityValue  bool
}

func (s *prologState) init() {
	*s = prologState{handler: prolog0, documentEntity: true}
}

func (s *prologState) initExternalEntity() {
	s.handler = externalSubset0
	s.documentEntity = false
	s.includeLevel = 0
}

// tokenRole is XmlTokenRole.
func (s *prologState) tokenRole(tok int, b []byte, ptr, end int, enc *encoding) int {
	return s.handler(s, tok, b, ptr, end, enc)
}

func (s *prologState) setTopLevel() {
	if s.documentEntity {
		s.handler = internalSubset
	} else {
		s.handler = externalSubset1
	}
}

// declKeyword reports whether the token "<!NAME" at ptr names keyword.
func declKeyword(b []byte, ptr, end int, enc *encoding, keyword string) bool {
	return enc.nameMatchesASCII(b, ptr+2*enc.minBPC, end, keyword)
}

func common(s *prologState, tok int) int {
	if !s.documentEntity && tok == tokParamEntityRef {
		return roleInnerParamEntityRef
	}
	s.handler = roleErrorState
	return roleError
}

func roleErrorState(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	return roleNone
}

func prolog0(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		s.handler = prolog1
		return roleNone
	case tokXMLDecl:
		s.handler = prolog1
		return roleXMLDecl
	case tokPI:
		s.handler = prolog1
		return rolePI
	case tokComment:
		s.handler = prolog1
		return roleComment
	case tokBOM:
		return roleNone
	case tokDeclOpen:
		if !declKeyword(b, ptr, end, enc, "DOCTYPE") {
			break
		}
		s.handler = doctype0
		return roleDoctypeNone
	case tokInstanceStart:
		s.handler = roleErrorState
		return roleInstanceStart
	}
	return common(s, tok)
}

func prolog1(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNone
	case tokPI:
		return rolePI
	case tokComment:
		return roleComment
	case tokBOM:
		return roleNone
	case tokDeclOpen:
		if !declKeyword(b, ptr, end, enc, "DOCTYPE") {
			break
		}
		s.handler = doctype0
		return roleDoctypeNone
	case tokInstanceStart:
		s.handler = roleErrorState
		return roleInstanceStart
	}
	return common(s, tok)
}

func prolog2(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNone
	case tokPI:
		return rolePI
	case tokComment:
		return roleComment
	case tokInstanceStart:
		s.handler = roleErrorState
		return roleInstanceStart
	}
	return common(s, tok)
}

func doctype0(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleDoctypeNone
	case tokName, tokPrefixedName:
		s.handler = doctype1
		return roleDoctypeName
	}
	return common(s, tok)
}

func doctype1(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleDoctypeNone
	case tokOpenBracket:
		s.handler = internalSubset
		return roleDoctypeInternalSubset
	case tokDeclClose:
		s.handler = prolog2
		return roleDoctypeClose
	case tokName:
		if enc.nameMatchesASCII(b, ptr, end, "SYSTEM") {
			s.handler = doctype3
			return roleDoctypeNone
		}
		if enc.nameMatchesASCII(b, ptr, end, "PUBLIC") {
			s.handler = doctype2
			return roleDoctypeNone
		}
	}
	return common(s, tok)
}

func doctype2(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleDoctypeNone
	case tokLiteral:
		s.handler = doctype3
		return roleDoctypePublicID
	}
	return common(s, tok)
}

func doctype3(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleDoctypeNone
	case tokLiteral:
		s.handler = doctype4
		return roleDoctypeSystemID
	}
	return common(s, tok)
}

func doctype4(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleDoctypeNone
	case tokOpenBracket:
		s.handler = internalSubset
		return roleDoctypeInternalSubset
	case tokDeclClose:
		s.handler = prolog2
		return roleDoctypeClose
	}
	return common(s, tok)
}

func doctype5(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleDoctypeNone
	case tokDeclClose:
		s.handler = prolog2
		return roleDoctypeClose
	}
	return common(s, tok)
}

func internalSubset(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNone
	case tokDeclOpen:
		switch {
		case declKeyword(b, ptr, end, enc, "ENTITY"):
			s.handler = entity0
			return roleEntityNone
		case declKeyword(b, ptr, end, enc, "ATTLIST"):
			s.handler = attlist0
			return roleAttlistNone
		case declKeyword(b, ptr, end, enc, "ELEMENT"):
			s.handler = element0
			return roleElementNone
		case declKeyword(b, ptr, end, enc, "NOTATION"):
			s.handler = notation0
			return roleNotationNone
		}
	case tokPI:
		return rolePI
	case tokComment:
		return roleComment
	case tokParamEntityRef:
		return roleParamEntityRef
	case tokCloseBracket:
		s.handler = doctype5
		return roleDoctypeNone
	case tokNone:
		return roleNone
	}
	return common(s, tok)
}

func externalSubset0(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	s.handler = externalSubset1
	if tok == tokXMLDecl {
		return roleTextDecl
	}
	return externalSubset1(s, tok, b, ptr, end, enc)
}

func externalSubset1(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokCondSectOpen:
		s.handler = condSect0
		return roleNone
	case tokCondSectClose:
		if s.includeLevel == 0 {
			break
		}
		s.includeLevel--
		return roleNone
	case tokPrologS:
		return roleNone
	case tokCloseBracket:
	case tokNone:
		if s.includeLevel != 0 {
			break
		}
		return roleNone
	default:
		return internalSubset(s, tok, b, ptr, end, enc)
	}
	return common(s, tok)
}

func entity0(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokPercent:
		s.handler = entity1
		return roleEntityNone
	case tokName:
		s.handler = entity2
		return roleGeneralEntityName
	}
	return common(s, tok)
}

func entity1(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokName:
		s.handler = entity7
		return roleParamEntityName
	}
	return common(s, tok)
}

func entity2(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokName:
		if enc.nameMatchesASCII(b, ptr, end, "SYSTEM") {
			s.handler = entity4
			return roleEntityNone
		}
		if enc.nameMatchesASCII(b, ptr, end, "PUBLIC") {
			s.handler = entity3
			return roleEntityNone
		}
	case tokLiteral:
		s.handler = declClose
		s.roleNone = roleEntityNone
		return roleEntityValue
	}
	return common(s, tok)
}

func entity3(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokLiteral:
		s.handler = entity4
		return roleEntityPublicID
	}
	return common(s, tok)
}

func entity4(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokLiteral:
		s.handler = entity5
		return roleEntitySystemID
	}
	return common(s, tok)
}

func entity5(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokDeclClose:
		s.setTopLevel()
		return roleEntityComplete
	case tokName:
		if enc.nameMatchesASCII(b, ptr, end, "NDATA") {
			s.handler = entity6
			return roleEntityNone
		}
	}
	return common(s, tok)
}

func entity6(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokName:
		s.handler = declClose
		s.roleNone = roleEntityNone
		return roleEntityNotationName
	}
	return common(s, tok)
}

func entity7(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokName:
		if enc.nameMatchesASCII(b, ptr, end, "SYSTEM") {
			s.handler = entity9
			return roleEntityNone
		}
		if enc.nameMatchesASCII(b, ptr, end, "PUBLIC") {
			s.handler = entity8
			return roleEntityNone
		}
	case tokLiteral:
		s.handler = declClose
		s.roleNone = roleEntityNone
		return roleEntityValue
	}
	return common(s, tok)
}

func entity8(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokLiteral:
		s.handler = entity9
		return roleEntityPublicID
	}
	return common(s, tok)
}

func entity9(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokLiteral:
		s.handler = entity10
		return roleEntitySystemID
	}
	return common(s, tok)
}

func entity10(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleEntityNone
	case tokDeclClose:
		s.setTopLevel()
		return roleEntityComplete
	}
	return common(s, tok)
}

func notation0(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNotationNone
	case tokName:
		s.handler = notation1
		return roleNotationName
	}
	return common(s, tok)
}

func notation1(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNotationNone
	case tokName:
		if enc.nameMatchesASCII(b, ptr, end, "SYSTEM") {
			s.handler = notation3
			return roleNotationNone
		}
		if enc.nameMatchesASCII(b, ptr, end, "PUBLIC") {
			s.handler = notation2
			return roleNotationNone
		}
	}
	return common(s, tok)
}

func notation2(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNotationNone
	case tokLiteral:
		s.handler = notation4
		return roleNotationPublicID
	}
	return common(s, tok)
}

func notation3(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNotationNone
	case tokLiteral:
		s.handler = declClose
		s.roleNone = roleNotationNone
		return roleNotationSystemID
	}
	return common(s, tok)
}

func notation4(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNotationNone
	case tokLiteral:
		s.handler = declClose
		s.roleNone = roleNotationNone
		return roleNotationSystemID
	case tokDeclClose:
		s.setTopLevel()
		return roleNotationNoSystemID
	}
	return common(s, tok)
}

func attlist0(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokName, tokPrefixedName:
		s.handler = attlist1
		return roleAttlistElementName
	}
	return common(s, tok)
}

func attlist1(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokDeclClose:
		s.setTopLevel()
		return roleAttlistNone
	case tokName, tokPrefixedName:
		s.handler = attlist2
		return roleAttributeName
	}
	return common(s, tok)
}

var attributeTypes = []string{"CDATA", "ID", "IDREF", "IDREFS", "ENTITY", "ENTITIES", "NMTOKEN", "NMTOKENS"}

func attlist2(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokName:
		for i, t := range attributeTypes {
			if enc.nameMatchesASCII(b, ptr, end, t) {
				s.handler = attlist8
				return roleAttributeTypeCdata + i
			}
		}
		if enc.nameMatchesASCII(b, ptr, end, "NOTATION") {
			s.handler = attlist5
			return roleAttlistNone
		}
	case tokOpenParen:
		s.handler = attlist3
		return roleAttlistNone
	}
	return common(s, tok)
}

func attlist3(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokNmtoken, tokName, tokPrefixedName:
		s.handler = attlist4
		return roleAttributeEnumValue
	}
	return common(s, tok)
}

func attlist4(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokCloseParen:
		s.handler = attlist8
		return roleAttlistNone
	case tokOr:
		s.handler = attlist3
		return roleAttlistNone
	}
	return common(s, tok)
}

func attlist5(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokOpenParen:
		s.handler = attlist6
		return roleAttlistNone
	}
	return common(s, tok)
}

func attlist6(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokName:
		s.handler = attlist7
		return roleAttributeNotationValue
	}
	return common(s, tok)
}

func attlist7(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokCloseParen:
		s.handler = attlist8
		return roleAttlistNone
	case tokOr:
		s.handler = attlist6
		return roleAttlistNone
	}
	return common(s, tok)
}

// attlist8 expects the default value.
func attlist8(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokPoundName:
		switch {
		case enc.nameMatchesASCII(b, ptr+enc.minBPC, end, "IMPLIED"):
			s.handler = attlist1
			return roleImpliedAttributeValue
		case enc.nameMatchesASCII(b, ptr+enc.minBPC, end, "REQUIRED"):
			s.handler = attlist1
			return roleRequiredAttributeValue
		case enc.nameMatchesASCII(b, ptr+enc.minBPC, end, "FIXED"):
			s.handler = attlist9
			return roleAttlistNone
		}
	case tokLiteral:
		s.handler = attlist1
		return roleDefaultAttributeValue
	}
	return common(s, tok)
}

func attlist9(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleAttlistNone
	case tokLiteral:
		s.handler = attlist1
		return roleFixedAttributeValue
	}
	return common(s, tok)
}

func element0(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleElementNone
	case tokName, tokPrefixedName:
		s.handler = element1
		return roleElementName
	}
	return common(s, tok)
}

func element1(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleElementNone
	case tokName:
		if enc.nameMatchesASCII(b, ptr, end, "EMPTY") {
			s.handler = declClose
			s.roleNone = roleElementNone
			return roleContentEmpty
		}
		if enc.nameMatchesASCII(b, ptr, end, "ANY") {
			s.handler = declClose
			s.roleNone = roleElementNone
			return roleContentAny
		}
	case tokOpenParen:
		s.handler = element2
		s.level = 1
		return roleGroupOpen
	}
	return common(s, tok)
}

func element2(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleElementNone
	case tokPoundName:
		if enc.nameMatchesASCII(b, ptr+enc.minBPC, end, "PCDATA") {
			s.handler = element3
			return roleContentPcdata
		}
	case tokOpenParen:
		s.level = 2
		s.handler = element6
		return roleGroupOpen
	case tokName, tokPrefixedName:
		s.handler = element7
		return roleContentElement
	case tokNameQuestion:
		s.handler = element7
		return roleContentElementOpt
	case tokNameAsterisk:
		s.handler = element7
		return roleContentElementRep
	case tokNamePlus:
		s.handler = element7
		return roleContentElementPlus
	}
	return common(s, tok)
}

func element3(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleElementNone
	case tokCloseParen:
		s.handler = declClose
		s.roleNone = roleElementNone
		return roleGroupClose
	case tokCloseParenAsterisk:
		s.handler = declClose
		s.roleNone = roleElementNone
		return roleGroupCloseRep
	case tokOr:
		s.handler = element4
		return roleElementNone
	}
	return common(s, tok)
}

func element4(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleElementNone
	case tokName, tokPrefixedName:
		s.handler = element5
		return roleContentElement
	}
	return common(s, tok)
}

func element5(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleElementNone
	case tokCloseParenAsterisk:
		s.handler = declClose
		s.roleNone = roleElementNone
		return roleGroupCloseRep
	case tokOr:
		s.handler = element4
		return roleElementNone
	}
	return common(s, tok)
}

func element6(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleElementNone
	case tokOpenParen:
		s.level++
		return roleGroupOpen
	case tokName, tokPrefixedName:
		s.handler = element7
		return roleContentElement
	case tokNameQuestion:
		s.handler = element7
		return roleContentElementOpt
	case tokNameAsterisk:
		s.handler = element7
		return roleContentElementRep
	case tokNamePlus:
		s.handler = element7
		return roleContentElementPlus
	}
	return common(s, tok)
}

func element7(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	closeGroup := func(role int) int {
		s.level--
		if s.level == 0 {
			s.handler = declClose
			s.roleNone = roleElementNone
		}
		return role
	}
	switch tok {
	case tokPrologS:
		return roleElementNone
	case tokCloseParen:
		return closeGroup(roleGroupClose)
	case tokCloseParenAsterisk:
		return closeGroup(roleGroupCloseRep)
	case tokCloseParenQuestion:
		return closeGroup(roleGroupCloseOpt)
	case tokCloseParenPlus:
		return closeGroup(roleGroupClosePlus)
	case tokComma:
		s.handler = element6
		return roleGroupSequence
	case tokOr:
		s.handler = element6
		return roleGroupChoice
	}
	return common(s, tok)
}

func condSect0(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNone
	case tokName:
		if enc.nameMatchesASCII(b, ptr, end, "INCLUDE") {
			s.handler = condSect1
			return roleNone
		}
		if enc.nameMatchesASCII(b, ptr, end, "IGNORE") {
			s.handler = condSect2
			return roleNone
		}
	}
	return common(s, tok)
}

func condSect1(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNone
	case tokOpenBracket:
		s.handler = externalSubset1
		s.includeLevel++
		return roleNone
	}
	return common(s, tok)
}

func condSect2(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return roleNone
	case tokOpenBracket:
		s.handler = externalSubset1
		return roleIgnoreSect
	}
	return common(s, tok)
}

func declClose(s *prologState, tok int, b []byte, ptr, end int, enc *encoding) int {
	switch tok {
	case tokPrologS:
		return s.roleNone
	case tokDeclClose:
		s.setTopLevel()
		return s.roleNone
	}
	return common(s, tok)
}
