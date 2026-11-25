// Ported from CPython 3.12 (Lib/importlib/_bootstrap.py, Lib/importlib/_bootstrap_external.py,
// Objects/moduleobject.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// Module objects and the import system: sys.modules, the modules written in Go (registered with
// RegisterModule), packages of them, relative imports and the from-list handling of import
// statements.

package py

import (
	"sort"
	"strconv"
	"strings"
	"sync"
)

// moduleV is a module object; its attributes live in dict.
type moduleV struct {
	dict *dictV
	// builtin marks a module written in Go (its repr says "built-in").
	builtin bool
}

func (*moduleV) pyType() string { return "module" }

// newModuleV is types.ModuleType(name): the dict holds __name__, __doc__,
// __package__, __loader__ and __spec__, the last four None.
func newModuleV(name string) *moduleV {
	d := newDict()
	d.setStr("__name__", strV(name))
	d.setStr("__doc__", none)
	d.setStr("__package__", none)
	d.setStr("__loader__", none)
	d.setStr("__spec__", none)
	return &moduleV{dict: d}
}

// name is the module's __name__ for messages, "?" when it is not a str.
func (m *moduleV) name() (string, bool) {
	if v, ok := m.dict.getStr("__name__"); ok {
		if s, ok := unsub(v).(strV); ok {
			return string(s), true
		}
	}
	return "?", false
}

func (m *moduleV) repr(*reprState) (string, error) {
	name, _ := m.name()
	q := strRepr(name)
	if spec, ok := m.dict.getStr("__spec__"); ok {
		if s, isSpec := spec.(*moduleSpecV); isSpec && s.origin == "" && s.searchLocations != nil {
			locs, err := pyRepr(s.searchLocations)
			if err != nil {
				return "", err
			}
			return "<module " + q + " (namespace) from " + locs + ">", nil
		}
	}
	if f, ok := m.dict.getStr("__file__"); ok {
		r, err := pyRepr(f)
		if err != nil {
			return "", err
		}
		return "<module " + q + " from " + r + ">", nil
	}
	if m.builtin {
		return "<module " + q + " (built-in)>", nil
	}
	return "<module " + q + ">", nil
}

func (m *moduleV) getAttr(name string) (Value, error) {
	if v, ok := m.dict.getStr(name); ok {
		return v, nil
	}
	switch name {
	case "__dict__":
		return m.dict, nil
	case "__class__":
		return classes["module"], nil
	}
	if ga, ok := m.dict.getStr("__getattr__"); ok {
		return callValue(nil, ga, []Value{strV(name)}, nil)
	}
	modName, ok := m.name()
	if !ok {
		return nil, newPyErr("AttributeError", "module has no attribute '"+name+"'")
	}
	e := newPyErr("AttributeError", "module '"+modName+"' has no attribute '"+name+"'")
	e.setField("name", strV(name))
	e.setField("obj", m)
	return nil, e
}

func (m *moduleV) setAttr(name string, v Value) error {
	m.dict.setStr(name, v)
	return nil
}

func (m *moduleV) delAttr(name string) error {
	if _, ok := m.dict.getStr(name); !ok {
		modName, _ := m.name()
		return newPyErr("AttributeError", "module '"+modName+"' has no attribute '"+name+"'")
	}
	m.dict.delStr(name)
	return nil
}

// moduleSpecV is an importlib.machinery.ModuleSpec.
type moduleSpecV struct {
	name   string
	loader Value
	// origin is the file the module comes from, "built-in" for a Go
	// module, "" (None) for a namespace package.
	origin string
	// searchLocations is submodule_search_locations: the __path__ list of a
	// package, nil for a plain module.
	searchLocations *listV
	cached          string
}

var moduleSpecClass = &classV{name: "ModuleSpec", module: "importlib._bootstrap", base: objectClass}

func init() {
	classes["ModuleSpec"] = moduleSpecClass
	classes["module"] = &classV{name: "module", base: objectClass,
		construct: func(c *callCtx, args []Value, kw *dictV) (Value, error) {
			buf, err := methodsArgsKw("module", []string{"name", "doc"}, 2, args, kw)
			if err != nil {
				return nil, err
			}
			if buf[0] == nil {
				return nil, typeErr("module() missing required argument 'name' (pos 1)")
			}
			name, ok := unsub(buf[0]).(strV)
			if !ok {
				return nil, typeErr("module.__init__() argument 'name' must be str, not %s", typeName(buf[0]))
			}
			m := newModuleV(string(name))
			if buf[1] != nil {
				m.dict.setStr("__doc__", buf[1])
			}
			return m, nil
		}}
	classes["BuiltinImporter"] = builtinImporterClass
}

func (*moduleSpecV) pyType() string { return "ModuleSpec" }

func optStr(s string) Value {
	if s == "" {
		return none
	}
	return strV(s)
}

func (s *moduleSpecV) parent() string {
	if s.searchLocations != nil {
		return s.name
	}
	if i := strings.LastIndexByte(s.name, '.'); i >= 0 {
		return s.name[:i]
	}
	return ""
}

func (s *moduleSpecV) getAttr(name string) (Value, error) {
	switch name {
	case "name":
		return strV(s.name), nil
	case "loader":
		return s.loader, nil
	case "origin":
		return optStr(s.origin), nil
	case "submodule_search_locations":
		if s.searchLocations == nil {
			return none, nil
		}
		return s.searchLocations, nil
	case "cached":
		return optStr(s.cached), nil
	case "parent":
		return strV(s.parent()), nil
	case "has_location":
		return pyBool(s.origin != "" && s.origin != "built-in"), nil
	case "loader_state":
		return none, nil
	}
	return nil, nil
}

func (s *moduleSpecV) repr(st *reprState) (string, error) {
	loader, err := reprWith(s.loader, st)
	if err != nil {
		return "", err
	}
	out := "ModuleSpec(name=" + strRepr(s.name) + ", loader=" + loader
	if s.origin != "" {
		out += ", origin=" + strRepr(s.origin)
	}
	if s.searchLocations != nil {
		locs, err := reprWith(s.searchLocations, st)
		if err != nil {
			return "", err
		}
		out += ", submodule_search_locations=" + locs
	}
	return out + ")", nil
}

// ---------------------------------------------------------------- registries

var (
	registryMu sync.Mutex
	// goModules holds the initialisers of the modules written in Go.
	goModules = map[string][]func(m *moduleV) error{}
)

// builtinImporterClass is importlib's BuiltinImporter, the loader of the
// modules written in Go.
var builtinImporterClass = &classV{name: "BuiltinImporter", module: "_frozen_importlib", base: objectClass}

// RegisterModule makes a module written in Go importable: attrs returns
// its attributes and is called on the module's first import. A module
// registered more than once has the attributes of every registration. A
// dotted name is a submodule; its parent must be registered too.
func RegisterModule(name string, attrs func() map[string]Value) {
	RegisterModuleInit(name, func(m Value) error {
		a := attrs()
		names := make([]string, 0, len(a))
		for k := range a {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			m.(*moduleV).dict.setStr(k, a[k])
		}
		return nil
	})
}

// RegisterModuleInit makes a module written in Go importable: init fills
// the module object (made by NewModule, already in sys.modules) on its first
// import; an error it returns fails the import and removes the module from
// sys.modules again.
func RegisterModuleInit(name string, init func(m Value) error) {
	registryMu.Lock()
	defer registryMu.Unlock()
	goModules[name] = append(goModules[name], func(m *moduleV) error { return init(m) })
}

// hasGoSubmodules reports whether a Go module has submodules registered.
func hasGoSubmodules(name string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	for n := range goModules {
		if strings.HasPrefix(n, name+".") {
			return true
		}
	}
	return false
}

func goModuleInits(name string) ([]func(m *moduleV) error, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	inits, ok := goModules[name]
	return inits, ok
}

// ---------------------------------------------------------------- finding

// findSpec is importlib.util.find_spec without the parent import: the spec
// of the module name among the Go modules, nil when there is none.
func findSpec(name string) *moduleSpecV {
	if _, ok := goModuleInits(name); !ok {
		return nil
	}
	spec := &moduleSpecV{name: name, loader: builtinImporterClass, origin: "built-in"}
	if hasGoSubmodules(name) {
		// A Go module with registered submodules is a package.
		spec.searchLocations = newList(nil)
	}
	return spec
}

// ---------------------------------------------------------------- loading

func notFound(name string) *excV {
	e := newPyErr("ModuleNotFoundError", "No module named '"+name+"'")
	e.setField("name", strV(name))
	return e
}

func notAPackage(name, parent string) *excV {
	e := newPyErr("ModuleNotFoundError", "No module named '"+name+"'; '"+parent+"' is not a package")
	e.setField("name", strV(name))
	return e
}

// importModule is importlib._bootstrap._gcd_import(name): the module of
// the absolute name, loaded with its parents when it is not in sys.modules.
func importModule(name string) (Value, error) {
	ensureInit()
	if m, ok := interp.modules.getStr(name); ok {
		if isNoneV(m) {
			e := newPyErr("ModuleNotFoundError", "import of "+name+" halted; None in sys.modules")
			e.setField("name", strV(name))
			return nil, e
		}
		return m, nil
	}
	var path Value
	var parentMod Value
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		parentName := name[:i]
		p, err := importModule(parentName)
		if err != nil {
			return nil, err
		}
		// Importing the parent may have imported the module itself.
		if m, ok := interp.modules.getStr(name); ok {
			return m, nil
		}
		parentMod = p
		pp, err := getattrOpt(p, "__path__")
		if err != nil {
			return nil, err
		}
		if pp == nil {
			return nil, notAPackage(name, parentName)
		}
		path = pp
	}
	if path == nil && parentMod != nil {
		return nil, notAPackage(name, name[:strings.LastIndexByte(name, '.')])
	}
	spec := findSpec(name)
	if spec == nil {
		return nil, notFound(name)
	}
	m, err := loadSpec(spec)
	if err != nil {
		return nil, err
	}
	if parentMod != nil {
		if err := pySetattr(parentMod, name[strings.LastIndexByte(name, '.')+1:], m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// loadSpec is importlib._bootstrap._load: the module made from the spec,
// its attributes set as _init_module_attrs sets them, entered in
// sys.modules and run; a failure removes it from sys.modules.
func loadSpec(spec *moduleSpecV) (Value, error) {
	m := newModuleV(spec.name)
	d := m.dict
	d.setStr("__spec__", spec)
	d.setStr("__loader__", spec.loader)
	d.setStr("__package__", strV(spec.parent()))
	if spec.searchLocations != nil {
		d.setStr("__path__", spec.searchLocations)
	}
	if spec.origin != "" && spec.origin != "built-in" {
		d.setStr("__file__", strV(spec.origin))
		d.setStr("__cached__", strV(spec.cached))
	}
	interp.modules.setStr(spec.name, m)
	importing[m] = true
	err := execSpec(spec, m)
	delete(importing, m)
	if err != nil {
		if _, ok := interp.modules.getStr(spec.name); ok {
			interp.modules.delStr(spec.name)
		}
		return nil, err
	}
	// The module's code may have replaced itself in sys.modules.
	if r, ok := interp.modules.getStr(spec.name); ok {
		return r, nil
	}
	return m, nil
}

// execSpec runs the initialisers of a Go module on it.
func execSpec(spec *moduleSpecV, m *moduleV) error {
	m.builtin = true
	inits, _ := goModuleInits(spec.name)
	for _, init := range inits {
		if err := init(m); err != nil {
			return fromGoError(err)
		}
	}
	return nil
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

// ---------------------------------------------------------------- import statements

// resolveName is importlib._bootstrap._resolve_name with the package found
// as _calc___package__ finds it in the importing module's globals.
func resolveName(name string, globals *dictV, level int) (string, error) {
	pkg, err := calcPackage(globals)
	if err != nil {
		return "", err
	}
	if pkg == "" {
		return "", newPyErr("ImportError", "attempted relative import with no known parent package")
	}
	parts := strings.Split(pkg, ".")
	if level-1 >= len(parts) {
		return "", newPyErr("ImportError", "attempted relative import beyond top-level package")
	}
	base := strings.Join(parts[:len(parts)-(level-1)], ".")
	if name == "" {
		return base, nil
	}
	return base + "." + name, nil
}

// calcPackage is _calc___package__: __package__, else __spec__.parent,
// else __name__ (or its parent for a module that is not a package).
func calcPackage(globals *dictV) (string, error) {
	if p, ok := globals.getStr("__package__"); ok && !isNoneV(p) {
		s, isStr := unsub(p).(strV)
		if !isStr {
			return "", typeErr("package must be a string")
		}
		return string(s), nil
	}
	if spec, ok := globals.getStr("__spec__"); ok && !isNoneV(spec) {
		p, err := pyGetattr(spec, "parent")
		if err != nil {
			return "", err
		}
		if s, ok := unsub(p).(strV); ok {
			return string(s), nil
		}
	}
	n, ok := globals.getStr("__name__")
	if !ok {
		return "", newPyErr("KeyError", "'__name__' not in globals")
	}
	name, isStr := unsub(n).(strV)
	if !isStr {
		return "", typeErr("__name__ must be a string")
	}
	if _, isPkg := globals.getStr("__path__"); isPkg {
		return string(name), nil
	}
	s := string(name)
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[:i], nil
	}
	return "", nil
}

// importAbsolute imports the module a statement names: name relative to
// the globals' package for level > 0.
func importAbsolute(name string, globals *dictV, level int) (string, Value, error) {
	if level > 0 {
		full, err := resolveName(name, globals, level)
		if err != nil {
			return "", nil, err
		}
		name = full
	}
	if name == "" {
		return "", nil, valueErr("Empty module name")
	}
	m, err := importModule(name)
	return name, m, err
}

// handleFromlist is _handle_fromlist: the submodules of a package named in
// a from-list are imported when the package has no attribute of that name.
func handleFromlist(m Value, names []string) error {
	path, err := getattrOpt(m, "__path__")
	if err != nil || path == nil {
		return err
	}
	modName, err := pyGetattr(m, "__name__")
	if err != nil {
		return err
	}
	base, _ := unsub(modName).(strV)
	for _, x := range names {
		if x == "*" {
			all, err := getattrOpt(m, "__all__")
			if err != nil {
				return err
			}
			if all != nil {
				items, err := toSlice(all)
				if err != nil {
					return err
				}
				sub := make([]string, 0, len(items))
				for _, it := range items {
					s, ok := unsub(it).(strV)
					if !ok {
						return typeErr("Item in %s.__all__ must be str, not %s", string(base), typeName(it))
					}
					sub = append(sub, string(s))
				}
				if err := handleFromlist(m, sub); err != nil {
					return err
				}
			}
			continue
		}
		has, err := getattrOpt(m, x)
		if err != nil {
			return err
		}
		if has != nil {
			continue
		}
		full := string(base) + "." + x
		if _, err := importModule(full); err != nil {
			if x, ok := err.(*excV); ok && excIsInstance(x, "ModuleNotFoundError") {
				if n, ok := x.fields["name"]; ok && pyEqualStr(n, full) {
					continue
				}
			}
			return err
		}
	}
	return nil
}

func pyEqualStr(v Value, s string) bool {
	t, ok := unsub(v).(strV)
	return ok && string(t) == s
}

// execImport runs import a.b.c [as d].
func (f *frame) execImport(n *Node) error {
	for _, a := range n.L1 {
		_, m, err := importAbsolute(a.ID, f.vm.globals, 0)
		if err != nil {
			return err
		}
		var bound Value
		if a.NoAsname {
			// import a.b.c binds a, the top-level package.
			top := a.ID
			if i := strings.IndexByte(top, '.'); i >= 0 {
				top = top[:i]
			}
			t, err := importModule(top)
			if err != nil {
				return err
			}
			bound = t
		} else {
			// import a.b.c as d binds a.b.c, found as attribute c of a.b.
			bound = m
			if i := strings.LastIndexByte(a.ID, '.'); i >= 0 {
				parent, err := importModule(a.ID[:i])
				if err != nil {
					return err
				}
				v, err := importFromAttr(parent, a.ID[i+1:])
				if err != nil {
					return err
				}
				bound = v
			}
		}
		if err := f.storeName(importBinding(a), bound); err != nil {
			return err
		}
	}
	return nil
}

// importFromAttr is the IMPORT_FROM instruction: getattr(m, name), else the
// submodule m.name from sys.modules, else ImportError.
func importFromAttr(m Value, name string) (Value, error) {
	v, err := getattrOpt(m, name)
	if err != nil || v != nil {
		return v, err
	}
	pkgName := "<unknown module name>"
	if n, err := getattrOpt(m, "__name__"); err == nil && n != nil {
		if s, ok := unsub(n).(strV); ok {
			pkgName = string(s)
			if sub, ok := interp.modules.getStr(pkgName + "." + name); ok {
				return sub, nil
			}
		}
	}
	loc := "unknown location"
	var file Value = none
	if fv, err := getattrOpt(m, "__file__"); err == nil && fv != nil {
		if s, ok := unsub(fv).(strV); ok {
			loc, file = string(s), fv
		}
	}
	msg := "cannot import name '" + name + "' from '" + pkgName + "' (" + loc + ")"
	if initializing(m) {
		msg = "cannot import name '" + name + "' from partially initialized module '" + pkgName +
			"' (most likely due to a circular import) (" + loc + ")"
	}
	e := newPyErr("ImportError", msg)
	e.setField("name", strV(pkgName))
	e.setField("path", file)
	return nil, e
}

// initializing reports whether a module is still running its code: it is
// in sys.modules but its import has not finished.
func initializing(m Value) bool {
	mv, ok := m.(*moduleV)
	if !ok {
		return false
	}
	_, running := importing[mv]
	return running
}

// importing holds the modules whose code is running.
var importing = map[*moduleV]bool{}

// execImportFrom runs from m import names.
func (f *frame) execImportFrom(n *Node) error {
	name := n.ID
	if n.NoID {
		name = ""
	}
	_, m, err := importAbsolute(name, f.vm.globals, n.Level)
	if err != nil {
		return err
	}
	names := make([]string, len(n.L1))
	for i, a := range n.L1 {
		names[i] = a.ID
	}
	if err := handleFromlist(m, names); err != nil {
		return err
	}
	for _, a := range n.L1 {
		if a.ID == "*" {
			if err := importStar(m, f); err != nil {
				return err
			}
			continue
		}
		v, err := importFromAttr(m, a.ID)
		if err != nil {
			return err
		}
		if err := f.storeName(importBinding(a), v); err != nil {
			return err
		}
	}
	return nil
}

// importStar binds the public names of m in f's namespace: those __all__
// lists, else those not starting with an underscore.
func importStar(m Value, f *frame) error {
	all, err := getattrOpt(m, "__all__")
	if err != nil {
		return err
	}
	if all == nil {
		d, err := pyGetattr(m, "__dict__")
		if err != nil {
			return err
		}
		dict, ok := d.(*dictV)
		if !ok {
			return newPyErr("ImportError", "from-import-* object has no __dict__ and no __all__")
		}
		keys := append([]Value(nil), dict.keys...)
		vals := append([]Value(nil), dict.vals...)
		for i, k := range keys {
			s, ok := unsub(k).(strV)
			if !ok {
				return typeErr("Key in %s.__dict__ must be str, not %s", moduleName(m), typeName(k))
			}
			if strings.HasPrefix(string(s), "_") {
				continue
			}
			if err := f.storeName(string(s), vals[i]); err != nil {
				return err
			}
		}
		return nil
	}
	items, err := toSlice(all)
	if err != nil {
		return err
	}
	for _, it := range items {
		s, ok := unsub(it).(strV)
		if !ok {
			return typeErr("Item in %s.__all__ must be str, not %s", moduleName(m), typeName(it))
		}
		v, err := pyGetattr(m, string(s))
		if err != nil {
			return err
		}
		if err := f.storeName(string(s), v); err != nil {
			return err
		}
	}
	return nil
}

func moduleName(m Value) string {
	if n, err := getattrOpt(m, "__name__"); err == nil && n != nil {
		if s, ok := unsub(n).(strV); ok {
			return string(s)
		}
	}
	return "?"
}

// builtinImport is __import__(name, globals=None, locals=None, fromlist=(),
// level=0): the module named (relative to the globals' package for level >
// 0), and the top-level package of a dotted name when fromlist is empty.
func builtinImport(c *callCtx, args []Value, kw *dictV) (Value, error) {
	buf, err := methodsArgsKwRequired("__import__", []string{"name", "globals", "locals", "fromlist", "level"}, 1, 5, args, kw)
	if err != nil {
		return nil, err
	}
	name, ok := unsub(buf[0]).(strV)
	if !ok {
		return nil, typeErr("__import__() argument 1 must be str, not %s", typeName(buf[0]))
	}
	level := 0
	if buf[4] != nil {
		n, err := methodsSsize(buf[4])
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, valueErr("level must be >= 0")
		}
		level = n
	}
	g := newDict()
	if d, ok := buf[1].(*dictV); ok {
		g = d
	}
	full, m, err := importAbsolute(string(name), g, level)
	if err != nil {
		return nil, err
	}
	var fromlist []string
	if buf[3] != nil && !isNoneV(buf[3]) {
		items, err := toSlice(buf[3])
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			s, ok := unsub(it).(strV)
			if !ok {
				return nil, typeErr("Item in ``from list'' must be str, not %s", typeName(it))
			}
			fromlist = append(fromlist, string(s))
		}
	}
	if len(fromlist) > 0 {
		return m, handleFromlist(m, fromlist)
	}
	if level == 0 {
		top := full
		if i := strings.IndexByte(top, '.'); i >= 0 {
			top = top[:i]
		}
		return importModule(top)
	}
	// A relative import without a from-list returns the package the name
	// starts in.
	cut := len(full) - len(string(name))
	if name == "" {
		return m, nil
	}
	first := string(name)
	if i := strings.IndexByte(first, '.'); i >= 0 {
		first = first[:i]
	}
	return importModule(full[:cut] + first)
}
