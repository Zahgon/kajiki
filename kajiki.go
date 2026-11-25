// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/__init__.py, kajiki/xml_template.py, kajiki/text.py, kajiki/loader.py,
// kajiki/i18n.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

// Package kajiki is Kajiki, the XML and text template engine whose templates
// hold Python: expressions, directive arguments and code blocks are Python 3.12,
// evaluated with CPython's semantics.
package kajiki

import (
	"errors"
	"slices"

	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// XMLOptions holds the keyword arguments of XMLTemplate. The zero value gives
// the original's defaults.
type XMLOptions struct {
	// Filename is filename; "" is None.
	Filename string
	// Mode is mode: any string, as Python passes it on ("xml", "html" and
	// "html5" are the ones Kajiki documents); "" is None.
	Mode string
	// IsFragment is is_fragment.
	IsFragment bool
	// Encoding is encoding, used when the source is read from Filename; "" is
	// "utf-8".
	Encoding string
	// Autoblocks is autoblocks; nil is None.
	Autoblocks []string
	// NoCDATAScripts is cdata_scripts=False.
	NoCDATAScripts bool
	// StripText is strip_text.
	StripText bool
	// BaseGlobals is base_globals, in order; nil is None.
	BaseGlobals []Var
}

// TextOptions holds the keyword arguments of TextTemplate. The zero value gives
// the original's defaults.
type TextOptions struct {
	// Filename is filename; "" is None.
	Filename string
	// Autoescape is autoescape.
	Autoescape bool
	// Encoding is encoding, used when the source is read from Filename; "" is
	// "utf-8".
	Encoding string
}

// Template is a template class, as XMLTemplate, TextTemplate and the loaders
// return it.
type Template struct {
	impl any
}

func encodingOr(encoding string) string {
	if encoding == "" {
		return "utf-8"
	}
	return encoding
}

func (o XMLOptions) args(source *string) xmlArgs {
	a := xmlArgs{source: source, filename: o.Filename, mode: o.Mode, isFragment: o.IsFragment,
		encoding: encodingOr(o.Encoding), autoblocks: o.Autoblocks, cdataScripts: !o.NoCDATAScripts,
		stripText: o.StripText}
	if o.BaseGlobals != nil {
		a.baseGlobals = varsDict(o.BaseGlobals)
	}
	return a
}

// templateResult wraps a template class, or turns the failure into an
// *Error.
func templateResult(cls py.Value, err error) (*Template, error) {
	if err != nil {
		return nil, newError(err)
	}
	return &Template{impl: cls}, nil
}

// XMLTemplate is kajiki.XMLTemplate(source=source, **options).
func XMLTemplate(source string, opts XMLOptions) (*Template, error) {
	return templateResult(xmlTemplate(opts.args(&source)))
}

// XMLTemplateFile is kajiki.XMLTemplate(source=None, **options): the source is
// read from opts.Filename.
func XMLTemplateFile(opts XMLOptions) (*Template, error) {
	return templateResult(xmlTemplate(opts.args(nil)))
}

// TextTemplate is kajiki.TextTemplate(source=source, **options).
func TextTemplate(source string, opts TextOptions) (*Template, error) {
	return templateResult(textTemplate(&source, opts.Filename, opts.Autoescape, encodingOr(opts.Encoding)))
}

// TextTemplateFile is kajiki.TextTemplate(source=None, **options): the source is
// read from opts.Filename.
func TextTemplateFile(opts TextOptions) (*Template, error) {
	return templateResult(textTemplate(nil, opts.Filename, opts.Autoescape, encodingOr(opts.Encoding)))
}

// Render is template(context).render(), context being the dict of the
// variables in order (a later variable of the same name wins).
func (t *Template) Render(context ...Var) (string, error) {
	inst, err := py.Call(t.impl.(py.Value), []py.Value{varsDict(context)}, nil)
	if err != nil {
		return "", newError(err)
	}
	out, err := py.CallMethod(inst, "render")
	if err != nil {
		return "", newError(err)
	}
	s, err := py.Str(out)
	if err != nil {
		return "", newError(err)
	}
	return s, nil
}

// Loader is a template loader.
type Loader struct {
	impl any
}

// NewMockLoader is kajiki.MockLoader(modules).
func NewMockLoader(modules map[string]*Template) *Loader {
	paths := make([]string, 0, len(modules))
	for p := range modules {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	d := py.EmptyDict()
	for _, p := range paths {
		if err := py.SetItem(d, py.String(p), modules[p].impl.(py.Value)); err != nil {
			panic(err)
		}
	}
	l, err := newMockLoader(d)
	if err != nil {
		panic(err)
	}
	return &Loader{impl: l}
}

// FileLoaderOptions holds the keyword arguments of FileLoader. The zero value
// gives the original's defaults.
type FileLoaderOptions struct {
	// Reload is reload.
	Reload bool
	// ForceMode is force_mode: any string, as Python passes it on ("text",
	// "xml", "html" and "html5" are the ones Kajiki documents); "" is None.
	ForceMode string
	// AutoescapeText is autoescape_text.
	AutoescapeText bool
	// XMLAutoblocks is xml_autoblocks; nil is None.
	XMLAutoblocks []string

	// The remaining fields are the template_options the loader passes to every
	// template it builds; nil leaves that keyword out.

	// IsFragment is the template option is_fragment.
	IsFragment *bool
	// StripText is the template option strip_text.
	StripText *bool
	// CDATAScripts is the template option cdata_scripts.
	CDATAScripts *bool
	// BaseGlobals is the template option base_globals, in order.
	BaseGlobals []Var
}

// NewFileLoader is kajiki.FileLoader(path, **options), path being the list of
// directories searched in order.
func NewFileLoader(path []string, opts FileLoaderOptions) *Loader {
	fl := &fileLoader{path: slices.Clone(path), forceMode: opts.ForceMode, autoescapeText: opts.AutoescapeText,
		xmlAutoblocks: opts.XMLAutoblocks}
	for _, o := range []struct {
		name string
		set  *bool
	}{{"is_fragment", opts.IsFragment}, {"strip_text", opts.StripText}, {"cdata_scripts", opts.CDATAScripts}} {
		if o.set != nil {
			fl.options = append(fl.options, templateOption{o.name, py.Bool(*o.set)})
		}
	}
	if opts.BaseGlobals != nil {
		fl.options = append(fl.options, templateOption{"base_globals", varsDict(opts.BaseGlobals)})
	}
	l, err := newLoader(fileLoaderClass, opts.Reload, fl)
	if err != nil {
		panic(err)
	}
	return &Loader{impl: l}
}

// Import is loader.import_(name), which loader.load(name) also is.
func (l *Loader) Import(name string) (*Template, error) {
	return templateResult(py.CallMethod(l.impl.(py.Value), "import_", py.String(name)))
}

// Message is one message kajiki.i18n.extract yields: (lineno, funcname,
// message, comments).
type Message struct {
	Lineno   int
	Funcname string
	Text     string
	Comments []string
}

// ExtractOptions holds the options mapping of kajiki.i18n.extract. The zero
// value is an empty mapping.
type ExtractOptions struct {
	// StripText is options["strip_text"].
	StripText bool
	// Mode is options["mode"]; "" leaves the key out.
	Mode string
	// IsFragment is options["is_fragment"].
	IsFragment bool
}

// Extract is list(kajiki.i18n.extract(io.BytesIO(source), keywords,
// comment_tags, options)), run as it runs without Babel installed.
func Extract(source []byte, opts ExtractOptions) ([]Message, error) {
	text, err := decodeFile(source, "utf-8", true)
	if err != nil {
		return nil, newError(err)
	}
	messages, err := extractMessages(text, opts.StripText, opts.Mode, opts.IsFragment)
	if err != nil {
		return nil, newError(err)
	}
	return messages, nil
}

// newError is the *Error for an exception: KajikiTemplateError and its
// subclasses carry their location, any other exception is located by its
// traceback.
func newError(err error) *Error {
	var pe *py.Error
	if !errors.As(err, &pe) {
		return &Error{Class: "RuntimeError", Message: err.Error()}
	}
	e := &Error{Class: pe.Class, Message: pe.Text}
	exc := pe.Value()
	if !isInstance(exc, kajikiTemplateError) {
		e.Filename, e.Line = templateLocation(err)
		return e
	}
	if isInstance(exc, kajikiSyntaxError) {
		if cause, err := py.GetAttr(exc, "__cause__"); err == nil {
			e.SyntaxMsg = strAttr(cause, "msg")
		}
	}
	e.Filename = strAttr(exc, "filename")
	e.Line = intAttr(exc, "linenum")
	e.Column = intAttr(exc, "colnum")
	return e
}

// isInstance is isinstance(v, cls) for one of the exception classes.
func isInstance(v, cls py.Value) bool {
	is, err := py.IsInstance(v, cls)
	return err == nil && is
}

// strAttr is the str attribute name of v, "" when it has none.
func strAttr(v py.Value, name string) string {
	a, err := py.GetAttr(v, name)
	if err != nil {
		return ""
	}
	s, _ := py.AsString(a)
	return s
}

// intAttr is the int attribute name of v, 0 when it has none.
func intAttr(v py.Value, name string) int {
	a, err := py.GetAttr(v, name)
	if err != nil {
		return 0
	}
	i, ok := py.AsInt(a)
	if !ok {
		return 0
	}
	return int(i.Int64())
}
