// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/i18n.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

// extractMessages is kajiki.i18n.extract run without Babel: the template is
// parsed and compiled as XMLTemplate does it (named "<string>", in mode,
// xml when it is ""), and the translatable texts of its intermediate
// representation are the messages, in the order the nodes stream.
func extractMessages(source string, stripText bool, mode string, isFragment bool) ([]Message, error) {
	doc, err := newXMLParser("<string>", source).parse()
	if err != nil {
		return nil, err
	}
	if err := transformDocument(doc, stripText); err != nil {
		return nil, err
	}
	if mode == "" {
		mode = "xml"
	}
	tree, err := newCompiler("<string>", doc, xmlCompileOptions{mode: mode, isFragment: isFragment,
		cdataScripts: true}).compile()
	if err != nil {
		return nil, err
	}
	var messages []Message
	for n := range tree.nodes() {
		if t, ok := n.(*translatableTextNode); ok && !isBlank(t.text) {
			messages = append(messages, Message{Lineno: t.lineno, Funcname: "_", Text: t.text, Comments: []string{}})
		}
	}
	return messages, nil
}
