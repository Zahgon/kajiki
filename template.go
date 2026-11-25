// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/template.py, kajiki/i18n.py, kajiki/__init__.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"strings"
	"sync"

	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// The Python side of a template: a template class is a subclass of
// _Template made by Template() from the class the generated module
// defines, holding its exposed functions as TplFuncs in __methods__; an
// instance holds the globals dict its functions run in. The state of both
// lives in Python attributes, as in the original, so that template code
// sees and changes the same objects.
var (
	kajikiModule  py.Value // the kajiki module the generated code and __kj__ refer to
	objClass      py.Value // kajiki.template._obj
	templateClass py.Value // kajiki.template._Template
	tplFuncClass  py.Value // kajiki.template.TplFunc
	gettextFunc   py.Value // kajiki.i18n.gettext
	emptyAttrSet  py.Value // html_utils.HTML_EMPTY_ATTRS
)

// init makes the classes, in an order that lets each refer to the ones
// before it.
func init() {
	flattenerClass = newClass("util", "flattener", flattenerMethods())
	gettextFunc = method("i18n", "", "gettext", []string{"s"}, func(a []py.Value) (py.Value, error) { return a[0], nil })
	objClass = newClass("template", "_obj", nil)
	templateClass = newClass("template", "_Template", templateMethods())
	tplFuncClass = newClass("template", "TplFunc", tplFuncMethods())
	items := make([]py.Value, len(htmlEmptyAttrs))
	for i, a := range htmlEmptyAttrs {
		items[i] = py.String(a)
	}
	var err error
	if emptyAttrSet, err = py.Set(items...); err != nil {
		panic(err)
	}
	kajikiModule = py.NewModule("kajiki")
	for name, v := range map[string]py.Value{"Template": templateFunc, "expose": expose, "flattener": flattenerClass} {
		if err := py.SetAttr(kajikiModule, name, v); err != nil {
			panic(err)
		}
	}
}

// selfMethod makes a method of _Template taking self and params.
func selfMethod(name string, params []string, body func(self py.Value, a []py.Value) (py.Value, error)) py.Value {
	return method("template", "_Template", name, append([]string{"self"}, params...),
		func(a []py.Value) (py.Value, error) { return body(a[0], a[1:]) })
}

func templateMethods() map[string]py.Value {
	initFn := py.NewFunction(py.FuncSpec{Name: "__init__", Qualname: "_Template.__init__", Module: "kajiki.template",
		Params: []string{"self", "context"}, Defaults: []py.Value{py.None()}},
		func(a []py.Value) (py.Value, error) { return nil, templateInit(a[0], a[1]) })
	return map[string]py.Value{
		"__methods__":  py.Tuple(),
		"loader":       py.None(),
		"base_globals": py.None(),
		"filename":     py.None(),
		"__init__":     initFn,
		"__iter__":     selfMethod("__iter__", nil, templateIter),
		"render":       selfMethod("render", nil, render),
		"_gettext": selfMethod("_gettext", []string{"s"}, func(self py.Value, a []py.Value) (py.Value, error) {
			g, err := py.GetAttr(self, "__globals__")
			if err != nil {
				return nil, err
			}
			fn, err := py.GetItem(g, py.String("gettext"))
			if err != nil {
				return nil, err
			}
			return py.Call(fn, a, nil)
		}),
		"_push_with":    selfMethod("_push_with", []string{"locals_", "vars"}, pushWith),
		"_pop_with":     selfMethod("_pop_with", nil, popStack("_with_stack")),
		"_extend":       selfMethod("_extend", []string{"parent"}, extend),
		"_push_switch":  selfMethod("_push_switch", []string{"expr"}, pushSwitch),
		"_pop_switch":   selfMethod("_pop_switch", nil, popSwitch),
		"_case":         selfMethod("_case", []string{"obj"}, switchCase),
		"_import":       selfMethod("_import", []string{"name", "alias", "gbls"}, importTemplate),
		"_escape":       selfMethod("_escape", []string{"value"}, func(_ py.Value, a []py.Value) (py.Value, error) { return escape(a[0]) }),
		"_render_attrs": selfMethod("_render_attrs", []string{"attrs", "mode"}, renderAttrs),
		"_collect":      selfMethod("_collect", []string{"it"}, collectParts),
		"defined": selfMethod("defined", []string{"name"}, func(self py.Value, a []py.Value) (py.Value, error) {
			ctx, err := py.GetAttr(self, "_context")
			if err != nil {
				return nil, err
			}
			in, err := py.Contains(ctx, a[0])
			return py.Bool(in), err
		}),
	}
}

// kjMethods are the methods _Template.__init__ puts on __kj__, by the name
// the generated code calls them.
var kjMethods = []struct{ name, method string }{
	{"extend", "_extend"}, {"push_switch", "_push_switch"}, {"pop_switch", "_pop_switch"}, {"case", "_case"},
	{"import_", "_import"}, {"escape", "_escape"}, {"gettext", "_gettext"}, {"render_attrs", "_render_attrs"},
	{"push_with", "_push_with"}, {"pop_with", "_pop_with"}, {"collect", "_collect"},
}

// templateInit is _Template.__init__: the globals are Kajiki's names, then
// the class's base_globals (the user's, then the generated module's), the
// template functions bound to the instance, the context, and gettext
// under "_" and the globals' get as value_of.
func templateInit(self, context py.Value) error {
	if py.IsNone(context) {
		context = py.EmptyDict()
	}
	if err := py.SetAttr(self, "_context", context); err != nil {
		return err
	}
	base, err := py.GetAttr(self, "base_globals")
	if err != nil {
		return err
	}
	if t, err := py.Truth(base); err != nil {
		return err
	} else if !t {
		base = py.EmptyDict()
	}
	defined := py.NewFunction(py.FuncSpec{Name: "<lambda>", Qualname: "_Template.__init__.<locals>.<lambda>",
		Module: "kajiki.template", Params: []string{"x"}}, func(a []py.Value) (py.Value, error) {
		g, err := py.GetAttr(self, "__globals__")
		if err != nil {
			return nil, err
		}
		in, err := py.Contains(g, a[0])
		return py.Bool(in), err
	})
	g := py.StrDict([]string{"local", "self", "defined", "literal", "Markup", "gettext", "__builtins__", "__kj__"},
		[]py.Value{self, self, defined, literal, literal, gettextFunc, py.ModuleDict(py.BuiltinsModule()), kajikiModule})
	if err := py.SetAttr(self, "__globals__", g); err != nil {
		return err
	}
	if _, err := py.CallMethod(g, "update", base); err != nil {
		return err
	}
	methods, err := py.GetAttr(self, "__methods__")
	if err != nil {
		return err
	}
	err = py.ForEach(methods, func(m py.Value) error {
		k, v, err := unpackPair(m)
		if err != nil {
			return err
		}
		if v, err = py.CallMethod(v, "bind_instance", self); err != nil {
			return err
		}
		name, ok := py.AsString(k)
		if !ok {
			return py.Raise("TypeError", py.String("attribute name must be string, not '"+py.TypeName(k)+"'"))
		}
		if err := py.SetAttr(self, name, v); err != nil {
			return err
		}
		return py.SetItem(g, k, v)
	})
	if err != nil {
		return err
	}
	kj, err := py.NewInstance(objClass, nil)
	if err != nil {
		return err
	}
	for _, m := range kjMethods {
		bound, err := py.GetAttr(self, m.method)
		if err != nil {
			return err
		}
		if err := py.SetAttr(kj, m.name, bound); err != nil {
			return err
		}
	}
	for _, a := range []struct {
		name  string
		value py.Value
	}{{"__kj__", kj}, {"_switch_stack", py.List()}, {"_with_stack", py.List()}} {
		if err := py.SetAttr(self, a.name, a.value); err != nil {
			return err
		}
	}
	if _, err := py.CallMethod(g, "update", context); err != nil {
		return err
	}
	gettext, err := py.GetItem(g, py.String("gettext"))
	if err != nil {
		return err
	}
	if err := py.SetItem(g, py.String("_"), gettext); err != nil {
		return err
	}
	get, err := py.GetAttr(g, "get")
	if err != nil {
		return err
	}
	return py.SetItem(g, py.String("value_of"), get)
}

// unpackPair is `k, v = item` with Python's errors.
func unpackPair(item py.Value) (k, v py.Value, err error) {
	vals, err := py.Unpack(item, 2)
	if err != nil {
		return nil, nil, err
	}
	return vals[0], vals[1], nil
}

// templateIter is _Template.__iter__, a generator: str() of each chunk of
// __main__'s output.
func templateIter(self py.Value, _ []py.Value) (py.Value, error) {
	var chunks *py.Iterator
	return py.NewIterator(func() (py.Value, bool, error) {
		leave, err := py.EnterRecursiveCall()
		defer leave()
		if err != nil {
			return nil, false, err
		}
		if chunks == nil {
			m, err := py.GetAttr(self, "__main__")
			if err != nil {
				return nil, false, err
			}
			out, err := py.Call(m, nil, nil)
			if err != nil {
				return nil, false, err
			}
			if chunks, err = py.Iter(out); err != nil {
				return nil, false, err
			}
		}
		chunk, ok, err := chunks.Next()
		if err != nil || !ok {
			return nil, false, err
		}
		s, err := py.Str(chunk)
		if err != nil {
			return nil, false, err
		}
		return py.String(s), true, nil
	}), nil
}

// render is _Template.render: "".join(self).
func render(self py.Value, _ []py.Value) (py.Value, error) {
	return py.CallMethod(py.String(""), "join", self)
}

func pushWith(self py.Value, a []py.Value) (py.Value, error) {
	var saved []py.Value
	err := py.ForEach(a[1], func(k py.Value) error {
		v, err := py.CallMethod(a[0], "get", k, py.Tuple())
		saved = append(saved, v)
		return err
	})
	if err != nil {
		return nil, err
	}
	stack, err := py.GetAttr(self, "_with_stack")
	if err != nil {
		return nil, err
	}
	_, err = py.CallMethod(stack, "append", py.List(saved...))
	return nil, err
}

// popStack makes the method that pops the stack attribute name.
func popStack(name string) func(self py.Value, a []py.Value) (py.Value, error) {
	return func(self py.Value, _ []py.Value) (py.Value, error) {
		stack, err := py.GetAttr(self, name)
		if err != nil {
			return nil, err
		}
		return py.CallMethod(stack, "pop")
	}
}

func pushSwitch(self py.Value, a []py.Value) (py.Value, error) {
	stack, err := py.GetAttr(self, "_switch_stack")
	if err != nil {
		return nil, err
	}
	_, err = py.CallMethod(stack, "append", a[0])
	return nil, err
}

func popSwitch(self py.Value, a []py.Value) (py.Value, error) {
	_, err := popStack("_switch_stack")(self, a)
	return nil, err
}

// switchCase is _Template._case: obj == the switch value.
func switchCase(self py.Value, a []py.Value) (py.Value, error) {
	stack, err := py.GetAttr(self, "_switch_stack")
	if err != nil {
		return nil, err
	}
	top, err := py.GetItem(stack, py.Int(-1))
	if err != nil {
		return nil, err
	}
	return py.RichCompare("==", a[0], top)
}

// importTemplate is _Template._import: the template is loaded as a
// fragment and instantiated with the importer's globals as its context,
// and stored in them under alias.
func importTemplate(self py.Value, a []py.Value) (py.Value, error) {
	name, alias, gbls := a[0], a[1], a[2]
	loader, err := py.GetAttr(self, "loader")
	if err != nil {
		return nil, err
	}
	cls, err := py.CallMethodKw(loader, "import_", []py.Value{name}, []py.Keyword{{Name: "is_fragment", Value: py.Bool(true)}})
	if err != nil {
		return nil, err
	}
	if py.IsNone(alias) {
		if alias, err = py.CallMethod(loader, "default_alias_for", name); err != nil {
			return nil, err
		}
	}
	r, err := py.Call(cls, []py.Value{gbls}, nil)
	if err != nil {
		return nil, err
	}
	return r, py.SetItem(gbls, alias, r)
}

// extend is _Template._extend: the parent is instantiated with the child's
// context; the child's template functions override the parent's, the
// parent's functions the child lacks are reached through trampolines, and
// the names parent, child, self and local are set on both sides.
func extend(self py.Value, a []py.Value) (py.Value, error) {
	parent := a[0]
	if _, ok := py.AsString(parent); ok {
		loader, err := py.GetAttr(self, "loader")
		if err != nil {
			return nil, err
		}
		if parent, err = py.CallMethod(loader, "import_", parent); err != nil {
			return nil, err
		}
	}
	ctx, err := py.GetAttr(self, "_context")
	if err != nil {
		return nil, err
	}
	pInst, err := py.Call(parent, []py.Value{ctx}, nil)
	if err != nil {
		return nil, err
	}
	pGlobals, err := py.GetAttr(pInst, "__globals__")
	if err != nil {
		return nil, err
	}
	g, err := py.GetAttr(self, "__globals__")
	if err != nil {
		return nil, err
	}
	err = templateFuncsIn(g, func(k, v py.Value) error { return py.SetItem(pGlobals, k, v) })
	if err != nil {
		return nil, err
	}
	err = templateFuncsIn(pGlobals, func(k, v py.Value) error {
		if in, err := py.Contains(g, k); err != nil {
			return err
		} else if !in {
			if err := py.SetItem(g, k, v); err != nil {
				return err
			}
		}
		name, ok := py.AsString(k)
		if !ok {
			return py.Raise("TypeError", py.String("attribute name must be string, not '"+py.TypeName(k)+"'"))
		}
		if has, err := py.HasAttr(self, name); err != nil || has {
			return err
		}
		f, err := newTrampoline(name)
		if err != nil {
			return err
		}
		bound, err := py.CallMethod(f, "bind_instance", self)
		if err != nil {
			return err
		}
		return py.SetAttr(self, name, bound)
	})
	if err != nil {
		return nil, err
	}
	selfV, err := py.GetItem(g, py.String("self"))
	if err != nil {
		return nil, err
	}
	for _, s := range []struct {
		d    py.Value
		name string
		v    py.Value
	}{{pGlobals, "child", self}, {pGlobals, "local", pInst}, {pGlobals, "self", selfV}, {g, "parent", pInst}, {g, "local", self}} {
		if err := py.SetItem(s.d, py.String(s.name), s.v); err != nil {
			return nil, err
		}
	}
	return pInst, nil
}

// templateFuncsIn calls fn for the TplFuncs of the dict d, __main__
// excepted, in order.
func templateFuncsIn(d py.Value, fn func(k, v py.Value) error) error {
	items, err := py.CallMethod(d, "items")
	if err != nil {
		return err
	}
	return py.ForEach(items, func(item py.Value) error {
		k, v, err := unpackPair(item)
		if err != nil {
			return err
		}
		if s, ok := py.AsString(k); ok && s == "__main__" {
			return nil
		}
		if is, err := py.IsInstance(v, tplFuncClass); err != nil || !is {
			return err
		}
		return fn(k, v)
	})
}

// escape is _Template._escape: None and flatteners pass, an object with
// __html__ gives its result, anything else is str() with &, <, > and "
// escaped.
func escape(value py.Value) (py.Value, error) {
	if py.IsNone(value) {
		return value, nil
	}
	if is, err := py.IsInstance(value, flattenerClass); err != nil || is {
		return value, err
	}
	if has, err := py.HasAttr(value, "__html__"); err != nil {
		return nil, err
	} else if has {
		return py.CallMethod(value, "__html__")
	}
	s, err := py.Str(value)
	if err != nil {
		return nil, err
	}
	return py.String(htmlEscaper.Replace(s)), nil
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// renderAttrs is _Template._render_attrs, a generator: the attributes of a
// mapping or a sequence of pairs, sorted, an HTML boolean attribute given
// as True or False written (or dropped) by its name, None values dropped.
func renderAttrs(self py.Value, a []py.Value) (py.Value, error) {
	attrs, mode := a[0], a[1]
	var pairs []py.Value
	started := false
	return py.NewIterator(func() (py.Value, bool, error) {
		leave, err := py.EnterRecursiveCall()
		defer leave()
		if err != nil {
			started, pairs = true, nil
			return nil, false, err
		}
		if !started {
			started = true
			if has, err := py.HasAttr(attrs, "items"); err != nil {
				return nil, false, err
			} else if has {
				if attrs, err = py.CallMethod(attrs, "items"); err != nil {
					return nil, false, err
				}
			}
			if py.IsNone(attrs) {
				return nil, false, nil
			}
			s, err := py.Call(py.Builtin("sorted"), []py.Value{attrs}, nil)
			if err != nil {
				return nil, false, err
			}
			if pairs, err = py.ToList(s); err != nil {
				return nil, false, err
			}
		}
		for len(pairs) > 0 {
			item := pairs[0]
			pairs = pairs[1:]
			out, err := renderAttr(self, item, mode)
			if err != nil {
				pairs = nil
				return nil, false, err
			}
			if out != nil {
				return out, true, nil
			}
		}
		return nil, false, nil
	}), nil
}

// renderAttr renders one (k, v) pair of renderAttrs, nil when it is
// dropped.
func renderAttr(self, item, mode py.Value) (py.Value, error) {
	k, v, err := unpackPair(item)
	if err != nil {
		return nil, err
	}
	empty, err := py.Contains(emptyAttrSet, k)
	if err != nil {
		return nil, err
	}
	if empty {
		if b, err := py.Contains(py.Tuple(py.Bool(true), py.Bool(false)), v); err != nil {
			return nil, err
		} else if b {
			if t, err := py.Truth(v); err != nil {
				return nil, err
			} else if t {
				v = k
			} else {
				v = py.None()
			}
		}
	}
	if py.IsNone(v) {
		return nil, nil
	}
	html, err := py.CallMethod(mode, "startswith", py.String("html"))
	if err != nil {
		return nil, err
	}
	if isHTML, _ := py.AsBool(html); isHTML && empty {
		lower, err := py.CallMethod(k, "lower")
		if err != nil {
			return nil, err
		}
		return py.BinaryOp("Add", py.String(" "), lower)
	}
	key, err := py.Format(k, "")
	if err != nil {
		return nil, err
	}
	escaped, err := py.CallMethod(self, "_escape", v)
	if err != nil {
		return nil, err
	}
	value, err := py.Format(escaped, "")
	if err != nil {
		return nil, err
	}
	return py.String(" " + key + `="` + value + `"`), nil
}

// collectParts is _Template._collect: the parts of an attribute's value as
// one str, flatteners accumulated, Nones dropped; None when nothing is
// left.
func collectParts(_ py.Value, a []py.Value) (py.Value, error) {
	var result []string
	err := py.ForEach(a[0], func(part py.Value) error {
		if py.IsNone(part) {
			return nil
		}
		if is, err := py.IsInstance(part, flattenerClass); err != nil {
			return err
		} else if is {
			if part, err = py.CallMethod(part, "accumulate_str"); err != nil {
				return err
			}
		}
		s, err := py.Str(part)
		result = append(result, s)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return py.None(), nil
	}
	return py.String(strings.Join(result, "")), nil
}

// templateFunc is kajiki.Template(ns): a _Template subclass named after ns
// whose __methods__ are the exposed functions found through dir(ns).
var templateFunc = py.NewFunction(py.FuncSpec{Name: "Template", Module: "kajiki.template", Params: []string{"ns"}},
	func(a []py.Value) (py.Value, error) {
		ns := a[0]
		names, err := py.Dir(ns)
		if err != nil {
			return nil, err
		}
		var methods []py.Value
		for _, name := range names {
			value, err := py.GetAttr(ns, name)
			if err != nil {
				return nil, err
			}
			exposed, err := py.GetAttrDefault(value, "exposed", py.Bool(false))
			if err != nil {
				return nil, err
			}
			if t, err := py.Truth(exposed); err != nil {
				return nil, err
			} else if !t {
				continue
			}
			fn, err := py.GetAttrDefault(value, "__func__", value)
			if err != nil {
				return nil, err
			}
			tf, err := py.Call(tplFuncClass, []py.Value{fn}, nil)
			if err != nil {
				return nil, err
			}
			methods = append(methods, py.Tuple(py.String(name), tf))
		}
		clsName, err := py.GetAttr(ns, "__name__")
		if err != nil {
			return nil, err
		}
		ns2 := py.StrDict([]string{"__methods__", "__module__"}, []py.Value{py.List(methods...), py.String("kajiki.template")})
		return py.Call(py.Builtin("type"), []py.Value{clsName, py.Tuple(templateClass), ns2}, nil)
	})

// tplFunc is the Go state of a TplFunc: the function re-bound to the
// instance's globals once it is first called, or the name a trampoline
// forwards to the parent template.
type tplFunc struct {
	bound      py.Value
	trampoline string
}

func tplFuncMethods() map[string]py.Value {
	return map[string]py.Value{
		"__init__": py.NewFunction(py.FuncSpec{Name: "__init__", Qualname: "TplFunc.__init__", Module: "kajiki.template",
			Params: []string{"self", "func", "inst"}, Defaults: []py.Value{py.None()}},
			func(a []py.Value) (py.Value, error) { return nil, initTplFunc(a[0], a[1], a[2], "") }),
		"bind_instance": method("template", "TplFunc", "bind_instance", []string{"self", "inst"},
			func(a []py.Value) (py.Value, error) {
				fn, err := py.GetAttr(a[0], "_func")
				if err != nil {
					return nil, err
				}
				b, err := py.NewInstance(tplFuncClass, nil)
				if err != nil {
					return nil, err
				}
				st, _ := py.Native(a[0]).(*tplFunc)
				return b, initTplFunc(b, fn, a[1], st.trampoline)
			}),
		"__repr__": method("template", "TplFunc", "__repr__", []string{"self"}, tplFuncRepr),
		"__call__": py.NewFunction(py.FuncSpec{Name: "__call__", Qualname: "TplFunc.__call__", Module: "kajiki.template",
			Params: []string{"self"}, VarArgs: "args", VarKw: "kwargs"}, callTplFunc),
	}
}

func initTplFunc(self, fn, inst py.Value, trampoline string) error {
	for _, a := range []struct {
		name  string
		value py.Value
	}{{"_func", fn}, {"_inst", inst}, {"_bound_func", py.None()}} {
		if err := py.SetAttr(self, a.name, a.value); err != nil {
			return err
		}
	}
	return py.SetNative(self, &tplFunc{trampoline: trampoline})
}

func tplFuncRepr(a []py.Value) (py.Value, error) {
	fn, err := py.GetAttr(a[0], "_func")
	if err != nil {
		return nil, err
	}
	name, err := py.GetAttr(fn, "__name__")
	if err != nil {
		return nil, err
	}
	nameRepr, err := py.Repr(name)
	if err != nil {
		return nil, err
	}
	inst, err := py.GetAttr(a[0], "_inst")
	if err != nil {
		return nil, err
	}
	if t, err := py.Truth(inst); err != nil {
		return nil, err
	} else if t {
		instRepr, err := py.Repr(inst)
		if err != nil {
			return nil, err
		}
		return py.String("<bound tpl_function " + nameRepr + " of " + instRepr + ">"), nil
	}
	return py.String("<unbound tpl_function " + nameRepr + ">"), nil
}

// callTplFunc is TplFunc.__call__: the function runs in the instance's
// globals (re-bound with types.FunctionType at the first call) and its
// result is wrapped in a flattener. The Go functions standing for Kajiki's
// Python frames count towards the recursion limit as those frames do.
func callTplFunc(a []py.Value) (py.Value, error) {
	leave, err := py.EnterRecursiveCall()
	defer leave()
	if err != nil {
		return nil, err
	}
	self := a[0]
	args, _ := py.SeqItems(a[1])
	kwargs := keywordsOf(a[2])
	st, _ := py.Native(self).(*tplFunc)
	if st.bound == nil {
		inst, err := py.GetAttr(self, "_inst")
		if err != nil {
			return nil, err
		}
		g, err := py.GetAttr(inst, "__globals__")
		if err != nil {
			return nil, err
		}
		if st.trampoline != "" {
			st.bound = trampolineCall(g, st.trampoline)
		} else {
			fn, err := py.GetAttr(self, "_func")
			if err != nil {
				return nil, err
			}
			if st.bound, err = py.RebindFunction(fn, g); err != nil {
				return nil, err
			}
		}
		if err := py.SetAttr(self, "_bound_func", st.bound); err != nil {
			return nil, err
		}
	}
	r, err := py.Call(st.bound, args, kwargs)
	if err != nil {
		return nil, err
	}
	return newFlattener(r)
}

// newTrampoline is the TplFunc _extend makes for a function only the parent
// has: called on the child, it calls the function of the same name of the
// child's global parent.
func newTrampoline(name string) (py.Value, error) {
	t, err := py.NewInstance(tplFuncClass, nil)
	if err != nil {
		return nil, err
	}
	fn := py.NewFunction(py.FuncSpec{Name: "trampoline", Qualname: "_Template._extend.<locals>._.<locals>.trampoline",
		Module: "kajiki.template", VarArgs: "a", VarKw: "kw"}, func([]py.Value) (py.Value, error) {
		return nil, py.Raise("NameError", py.String("name 'parent' is not defined"))
	})
	return t, initTplFunc(t, fn, py.None(), name)
}

// trampolineCall is the trampoline bound to the globals g: global parent,
// then getattr(parent, name)(*a, **kw).
func trampolineCall(g py.Value, name string) py.Value {
	return py.NewFunction(py.FuncSpec{Name: "trampoline", Qualname: "_Template._extend.<locals>._.<locals>.trampoline",
		Module: "kajiki.template", VarArgs: "a", VarKw: "kw"}, func(b []py.Value) (py.Value, error) {
		parent, err := globalName(g, "parent")
		if err != nil {
			return nil, err
		}
		fn, err := py.GetAttr(parent, name)
		if err != nil {
			return nil, err
		}
		args, _ := py.SeqItems(b[0])
		return py.Call(fn, args, keywordsOf(b[1]))
	})
}

// globalName looks name up as a function running in the globals g does:
// in g, then in its builtins.
func globalName(g py.Value, name string) (py.Value, error) {
	key := py.String(name)
	if in, err := py.Contains(g, key); err != nil {
		return nil, err
	} else if in {
		return py.GetItem(g, key)
	}
	v, err := py.GetAttr(py.BuiltinsModule(), name)
	if err == nil || !py.ErrorIs(err, "AttributeError") {
		return v, err
	}
	return nil, py.Raise("NameError", py.String("name '"+name+"' is not defined"))
}

// rewritten holds the codes annotate_lnotab gave the template functions of
// every template class made: an exception is reported where the innermost
// traceback entry running one of them is.
var rewritten struct {
	sync.Mutex
	codes map[*py.Code]bool
}

// fromIR is template.from_ir: the module the intermediate representation
// generates is run, its template class gets base_globals (a copy of the
// user's updated with the module's namespace), py_text and filename, and
// the code of its functions the template's lines.
func fromIR(tree *templateNode, baseGlobals py.Value) (py.Value, error) {
	text, linenos := moduleSource(tree)
	dct := py.StrDict([]string{"kajiki"}, []py.Value{kajikiModule})
	code, err := py.Compile(text, "<string>", "exec")
	if err == nil {
		err = code.Exec(dct)
	}
	if err != nil {
		return nil, syntaxError(err, text)
	}
	tpl, err := py.GetItem(dct, py.String("template"))
	if err != nil {
		return nil, err
	}
	if baseGlobals == nil {
		baseGlobals = py.EmptyDict()
	}
	base, err := py.CallMethod(baseGlobals, "copy")
	if err != nil {
		return nil, err
	}
	if _, err := py.CallMethod(base, "update", dct); err != nil {
		return nil, err
	}
	for _, a := range []struct {
		name  string
		value py.Value
	}{{"base_globals", base}, {"py_text", py.String(text)}, {"filename", py.String(tree.filename)}} {
		if err := py.SetAttr(tpl, a.name, a.value); err != nil {
			return nil, err
		}
	}
	return tpl, annotateLnotab(tpl, tree.filename, linenos)
}

// syntaxError turns the SyntaxError (IndentationError, TabError) running
// the generated module raised into KajikiSyntaxError, caused by it.
func syntaxError(err error, text string) error {
	pe, ok := err.(*py.Error)
	if !ok {
		return err
	}
	si, ok := pe.Syntax()
	if !ok {
		return err
	}
	ke := newTemplateError(kajikiSyntaxError, si.Msg, text, si.Filename, si.Lineno, si.Offset)
	if kpe, ok := ke.(*py.Error); ok {
		if err := py.SetAttr(kpe.Value(), "__cause__", pe.Value()); err != nil {
			return err
		}
	}
	return ke
}

// annotateLnotab is _Template.annotate_lnotab: each template function's
// code is replaced by one reporting the template's file name, and for each
// line of the generated module the template line linenos maps it to.
func annotateLnotab(tpl py.Value, filename string, linenos []lineMapping) error {
	lines := make(map[int]int, len(linenos))
	for _, l := range linenos {
		lines[l.pyLine] = l.tplLine
	}
	methods, err := py.GetAttr(tpl, "__methods__")
	if err != nil {
		return err
	}
	return py.ForEach(methods, func(m py.Value) error {
		_, tf, err := unpackPair(m)
		if err != nil {
			return err
		}
		fn, err := py.GetAttr(tf, "_func")
		if err != nil {
			return err
		}
		code, ok := py.FunctionCode(fn)
		if !ok {
			return nil
		}
		annotated := code.WithLines(filename, lines[code.FirstLine()], lines)
		if err := py.SetAttr(fn, "__code__", annotated.Value()); err != nil {
			return err
		}
		rewritten.Lock()
		defer rewritten.Unlock()
		if rewritten.codes == nil {
			rewritten.codes = map[*py.Code]bool{}
		}
		rewritten.codes[annotated] = true
		return nil
	})
}

// lineMapping is one entry of from_ir's py_linenos: a line of the
// generated module and the template line it reports.
type lineMapping struct {
	pyLine, tplLine int
}

// moduleSource is the part of from_ir that writes the module: its text and
// the map of its lines to template lines, where each line reports the
// largest template line of the lines up to it.
func moduleSource(tree *templateNode) (string, []lineMapping) {
	lines := generatePython(tree)
	texts := make([]string, len(lines))
	var linenos []lineMapping
	last, pyLineno := 0, 1
	for i, l := range lines {
		texts[i] = l.String()
		lno := max(last, l.lineno)
		for range strings.Count(texts[i], "\n") + 1 {
			linenos = append(linenos, lineMapping{pyLineno, lno})
			pyLineno++
		}
		last = lno
	}
	return strings.Join(texts, "\n"), linenos
}

// templateLocation is where an exception is reported: the file name and
// line of the innermost traceback entry running a template function's
// code; "" and 0 when there is none.
func templateLocation(err error) (string, int) {
	rewritten.Lock()
	defer rewritten.Unlock()
	filename, line := "", 0
	for _, f := range py.Traceback(err) {
		if f.Code == nil || !rewritten.codes[f.Code] {
			continue
		}
		filename, line = f.Filename, 0
		if f.HasLine {
			line = f.Lineno
		}
	}
	return filename, line
}
