// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/loader.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"os"
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/py"
	"github.com/jackrosenthal/kajiki-go/internal/pyre"
)

// A loader's cache (modules) and reload flag (_reload) are Python
// attributes, as in the original; a FileLoader's settings are Go state.

// fileLoader is what FileLoader adds to Loader.
type fileLoader struct {
	path           []string
	forceMode      string
	autoescapeText bool
	xmlAutoblocks  []string
	// options are the template_options, in the order they were given.
	options []templateOption
}

// templateOption is a keyword argument passed on to the template
// functions.
type templateOption struct {
	name  string
	value py.Value
}

// The loader classes, made in init.
var loaderClass, mockLoaderClass, fileLoaderClass py.Value

func init() {
	loaderClass = newClass("loader", "Loader", map[string]py.Value{
		"import_": py.NewFunction(py.FuncSpec{Name: "import_", Qualname: "Loader.import_", Module: "kajiki.loader",
			Params: []string{"self", "name"}, VarKw: "kwargs"}, func(a []py.Value) (py.Value, error) {
			return loaderImport(a[0], a[1], keywordsOf(a[2]))
		}),
		"default_alias_for": method("loader", "Loader", "default_alias_for", []string{"self", "name"},
			func(a []py.Value) (py.Value, error) {
				name, ok := py.AsString(a[1])
				if !ok {
					return nil, py.Raise("TypeError", py.String("expected str, bytes or os.PathLike object, not "+
						py.TypeName(a[1])))
				}
				return py.String(defaultAliasFor(name)), nil
			}),
		"load": property(method("loader", "Loader", "load", []string{"self"},
			func(a []py.Value) (py.Value, error) { return py.GetAttr(a[0], "import_") })),
	})
	mockLoaderClass = subclass("MockLoader", loaderClass, nil)
	fileLoaderClass = subclass("FileLoader", loaderClass, map[string]py.Value{
		"_load": py.NewFunction(py.FuncSpec{Name: "_load", Qualname: "FileLoader._load", Module: "kajiki.loader",
			Params: []string{"self", "name", "encoding"}, Defaults: []py.Value{py.String("utf-8")}, VarKw: "kwargs"},
			fileLoad),
	})
}

func property(fn py.Value) py.Value {
	p, err := py.Call(py.Builtin("property"), []py.Value{fn}, nil)
	if err != nil {
		panic(err)
	}
	return p
}

// subclass makes the class name of kajiki.loader deriving from base.
func subclass(name string, base py.Value, attrs map[string]py.Value) py.Value {
	names := []string{"__module__"}
	values := []py.Value{py.String("kajiki.loader")}
	for k, v := range attrs {
		names = append(names, k)
		values = append(values, v)
	}
	cls, err := py.NewClass(name, []py.Value{base}, py.StrDict(names, values))
	if err != nil {
		panic(err)
	}
	return cls
}

// newLoader is Loader.__init__(reload) for an instance of cls, with the Go
// state of a FileLoader.
func newLoader(cls py.Value, reload bool, file *fileLoader) (py.Value, error) {
	l, err := py.NewInstance(cls, nil)
	if err != nil {
		return nil, err
	}
	if err := py.SetAttr(l, "_reload", py.Bool(reload)); err != nil {
		return nil, err
	}
	if err := py.SetAttr(l, "modules", py.EmptyDict()); err != nil {
		return nil, err
	}
	if file != nil {
		if err := py.SetNative(l, file); err != nil {
			return nil, err
		}
	}
	return l, nil
}

// newMockLoader is MockLoader(modules): its cache starts with the modules,
// whose loader it becomes.
func newMockLoader(modules py.Value) (py.Value, error) {
	l, err := newLoader(mockLoaderClass, false, nil)
	if err != nil {
		return nil, err
	}
	cache, err := py.GetAttr(l, "modules")
	if err != nil {
		return nil, err
	}
	if _, err := py.CallMethod(cache, "update", modules); err != nil {
		return nil, err
	}
	values, err := py.CallMethod(cache, "values")
	if err != nil {
		return nil, err
	}
	return l, iterate(values, func(v py.Value) error { return py.SetAttr(v, "loader", l) })
}

// loaderImport is Loader.import_: the cached template unless the loader
// reloads, else the template _load makes, cached with its loader set.
func loaderImport(self, name py.Value, kwargs []py.Keyword) (py.Value, error) {
	cache, err := py.GetAttr(self, "modules")
	if err != nil {
		return nil, err
	}
	mod, err := py.CallMethod(cache, "get", name)
	if err != nil {
		return nil, err
	}
	reload, err := py.GetAttr(self, "_reload")
	if err != nil {
		return nil, err
	}
	if r, err := py.Truth(reload); err != nil {
		return nil, err
	} else if !r {
		if t, err := py.Truth(mod); err != nil || t {
			return mod, err
		}
	}
	load, err := py.GetAttr(self, "_load")
	if err != nil {
		return nil, err
	}
	if mod, err = py.Call(load, []py.Value{name}, kwargs); err != nil {
		return nil, err
	}
	if err := py.SetAttr(mod, "loader", self); err != nil {
		return nil, err
	}
	return mod, py.SetItem(cache, name, mod)
}

// fileLoad is FileLoader._load: the file found on the path, read as text
// in encoding, made into a template by the forced mode or the extension,
// with the template options and the keyword arguments of the call.
func fileLoad(a []py.Value) (py.Value, error) {
	fl, _ := py.Native(a[0]).(*fileLoader)
	name, ok := py.AsString(a[1])
	if !ok {
		return nil, py.Raise("TypeError", py.String("unsupported operand type(s) for /: 'PosixPath' and '"+
			py.TypeName(a[1])+"'"))
	}
	encoding, ok := py.AsString(a[2])
	if !ok {
		return nil, py.Raise("TypeError", py.String("TextIOWrapper() argument 'encoding' must be str or None, not "+
			py.TypeName(a[2])))
	}
	options := append([]templateOption(nil), fl.options...)
	for _, kw := range keywordsOf(a[3]) {
		options = setOption(options, kw.Name, kw.Value)
	}
	resource, err := fl.findResource(name)
	if err != nil {
		return nil, err
	}
	source, err := readText(resource, encoding)
	if err != nil {
		return nil, err
	}
	switch {
	case fl.forceMode == "text":
		return textTemplateCall(source, resource, fl.autoescapeText, options)
	case fl.forceMode != "":
		return xmlTemplateCall(source, resource, fl.forceMode, fl.xmlAutoblocks, options)
	}
	switch ext := strings.TrimLeft(pathSuffix(pathName(resource)), "."); ext {
	case "txt":
		return textTemplateCall(source, resource, fl.autoescapeText, options)
	case "xml":
		return xmlTemplateCall(source, resource, "", nil, options)
	case "html", "html5":
		return xmlTemplateCall(source, resource, ext, nil, options)
	default:
		return nil, py.Raise("KeyError", py.String(ext))
	}
}

// setOption is options[name] = value on the ordered options.
func setOption(options []templateOption, name string, value py.Value) []templateOption {
	for i := range options {
		if options[i].name == name {
			options[i].value = value
			return options
		}
	}
	return append(options, templateOption{name, value})
}

// textTemplateCall is TextTemplate(source=..., filename=..., autoescape=...,
// **options): TextTemplate takes none of the template options.
func textTemplateCall(source, filename string, autoescape bool, options []templateOption) (py.Value, error) {
	if len(options) > 0 {
		return nil, py.Raise("TypeError", py.String("TextTemplate() got an unexpected keyword argument '"+
			options[0].name+"'"))
	}
	return textTemplate(&source, filename, autoescape, "utf-8")
}

// xmlTemplateCall is XMLTemplate(source=..., filename=..., [mode=...,
// autoblocks=...,] **options).
func xmlTemplateCall(source, filename, mode string, autoblocks []string, options []templateOption) (py.Value, error) {
	a := xmlArgs{source: &source, filename: filename, mode: mode, autoblocks: autoblocks, cdataScripts: true}
	for _, o := range options {
		var err error
		switch o.name {
		case "is_fragment":
			a.isFragment, err = py.Truth(o.value)
		case "strip_text":
			a.stripText, err = py.Truth(o.value)
		case "cdata_scripts":
			a.cdataScripts, err = py.Truth(o.value)
		case "base_globals":
			a.baseGlobals = o.value
		}
		if err != nil {
			return nil, err
		}
	}
	return xmlTemplate(a)
}

// findResource is FileLoader._find_resource: the first directory of the
// path holding a file of that name.
func (fl *fileLoader) findResource(name string) (string, error) {
	for _, base := range fl.path {
		p := joinPath(base, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	parts := make([]string, len(fl.path))
	for i, p := range fl.path {
		parts[i] = pyre.StrRepr(p)
	}
	return "", py.Raise("FileNotFoundError", py.String(name+" not found in any of ["+strings.Join(parts, ", ")+"]"))
}

// readText is Path.read_text(encoding): the file decoded, with universal
// newlines.
func readText(path, encoding string) (string, error) {
	data, err := readFile(path)
	if err != nil {
		return "", err
	}
	text, err := decodeFile(data, encoding, true)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), nil
}

// joinPath is str(Path(base) / name): the paths joined as posixpath.join
// does, then normalized as pathlib does (empty and "." parts dropped, a
// root of exactly two slashes kept).
func joinPath(base, name string) string {
	p := name
	if !strings.HasPrefix(name, "/") {
		switch {
		case base == "":
		case strings.HasSuffix(base, "/"):
			p = base + name
		default:
			p = base + "/" + name
		}
	}
	root := ""
	switch {
	case strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///"):
		root = "//"
	case strings.HasPrefix(p, "/"):
		root = "/"
	}
	var parts []string
	for _, part := range strings.Split(p[len(root):], "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	if root == "" && len(parts) == 0 {
		return "."
	}
	return root + strings.Join(parts, "/")
}

// pathName is PurePath.name.
func pathName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	if p == "." {
		return ""
	}
	return p
}

// pathSuffix is the suffix of a file name as PurePath.suffix finds it.
func pathSuffix(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 && i < len(name)-1 {
		return name[i:]
	}
	return ""
}
