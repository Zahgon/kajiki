// Ported from CPython 3.12 (Python/sysmodule.c, Python/pylifecycle.c) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0
//
// The interpreter's state: the sys and builtins modules, the module table, the input and outputs
// of input(), print() and warnings, the stack of running frames and the exceptions being handled.

package py

import (
	"bufio"
	"io"
	"math"
	"sort"
	"strings"
)

// Config is what Init starts the interpreter with.
type Config struct {
	// Stdin is what input() reads lines from, Stdout is where print()
	// writes when no file is given and Stderr is where warnings are shown.
	// Nil means no input or no output.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// LauncherFrames is the number of Python frames the program that
	// starts the interpreter would have on the stack below the code it
	// runs; they count towards the recursion limit.
	LauncherFrames int
}

// interp is the state of the one interpreter of the process.
var interp struct {
	sys      *moduleV
	builtins *moduleV
	// modules is sys.modules.
	modules *dictV
	stdin   *bufio.Reader
	stdout  io.Writer
	stderr  io.Writer
	// cur is the innermost running Python frame (nil outside Python code).
	cur *frame
	// handled is the stack of exceptions being handled by except clauses
	// and finally blocks, innermost last (sys.exception()).
	handled []*excV
	// depth is the number of Python frames running; recursionLimit is
	// sys.getrecursionlimit().
	depth, recursionLimit int
	// generators is the number of generator objects made so far.
	generators uint64
}

// defaultRecursionLimit is sys.getrecursionlimit() at start-up.
const defaultRecursionLimit = 1000

// Init (re)starts the interpreter: a fresh module table holding sys and
// builtins, and print() and input() bound to cfg's writer and reader. Go
// modules registered with RegisterModule are imported anew when Python code
// first imports them.
func Init(cfg Config) {
	interp.modules = newDict()
	interp.cur = nil
	interp.handled = nil
	interp.depth = cfg.LauncherFrames
	interp.recursionLimit = defaultRecursionLimit
	if cfg.Stdin == nil {
		cfg.Stdin = strings.NewReader("")
	}
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	interp.stdin = bufio.NewReader(cfg.Stdin)
	interp.stdout = cfg.Stdout
	interp.stderr = cfg.Stderr
	b := newModuleV("builtins")
	names := make([]string, 0, len(builtinsTable))
	for name := range builtinsTable {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name != "__name__" && name != "__doc__" {
			b.dict.setStr(name, builtinsTable[name])
		}
	}
	b.dict.setStr("__doc__", strV(builtinsDoc))
	interp.builtins = b
	interp.modules.setStr("builtins", b)
	interp.sys = newSysModule()
	interp.modules.setStr("sys", interp.sys)
}

// ensureInit starts the interpreter with an empty configuration when Init
// has not run, so that the package works without it.
func ensureInit() {
	if interp.modules == nil {
		Init(Config{})
	}
}

// strList is a list of str.
func strList(items []string) *listV {
	vals := make([]Value, len(items))
	for i, s := range items {
		vals[i] = strV(s)
	}
	return newList(vals)
}

// newSysModule builds the sys module.
func newSysModule() *moduleV {
	m := newModuleV("sys")
	d := m.dict
	set := d.setStr
	fn := func(name string, f func(c *callCtx, args []Value, kw *dictV) (Value, error)) {
		set(name, newBuiltin(name, f))
	}
	set("modules", interp.modules)
	set("platform", strV("linux"))
	set("byteorder", strV("little"))
	set("maxsize", mkInt(math.MaxInt64))
	set("maxunicode", mkInt(0x10ffff))
	set("version", strV("3.12.15 (main, Oct 14 2025, 21:26:46) [GCC 12.2.0]"))
	set("hexversion", mkInt(0x030c0ff0))
	set("api_version", mkInt(1013))
	set("copyright", strV("Copyright (c) 2001-2023 Python Software Foundation.\nAll Rights Reserved.\n\n"+
		"Copyright (c) 2000 BeOpen.com.\nAll Rights Reserved.\n\n"+
		"Copyright (c) 1995-2001 Corporation for National Research Initiatives.\nAll Rights Reserved.\n\n"+
		"Copyright (c) 1991-1995 Stichting Mathematisch Centrum, Amsterdam.\nAll Rights Reserved."))
	set("version_info", versionInfo())
	set("builtin_module_names", builtinModuleNames())
	set("float_repr_style", strV("short"))
	fn("exit", sysExit)
	fn("getrecursionlimit", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("getrecursionlimit", 0, 0, args, kw); err != nil {
			return nil, err
		}
		return mkInt(int64(interp.recursionLimit)), nil
	})
	fn("setrecursionlimit", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("setrecursionlimit", 1, 1, args, kw); err != nil {
			return nil, err
		}
		if _, isFloat := args[0].(floatV); isFloat {
			return nil, typeErr("'float' object cannot be interpreted as an integer")
		}
		n, err := methodsSsize(args[0])
		if err != nil {
			return nil, err
		}
		if n < 1 {
			return nil, valueErr("recursion limit must be greater or equal than 1")
		}
		if n <= interp.depth {
			return nil, newPyErr("RecursionError", "cannot set the recursion limit to "+itoa(n)+
				" at the recursion depth "+itoa(interp.depth)+": the limit is too low")
		}
		interp.recursionLimit = n
		return none, nil
	})
	fn("get_int_max_str_digits", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		return mkInt(maxStrDigits), posOnly("get_int_max_str_digits", 0, 0, args, kw)
	})
	fn("is_finalizing", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		return falseV, posOnly("is_finalizing", 0, 0, args, kw)
	})
	fn("getswitchinterval", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		return floatV(0.005), posOnly("getswitchinterval", 0, 0, args, kw)
	})
	set("float_info", newStructSeq("sys.float_info", []string{"max", "max_exp", "max_10_exp", "min", "min_exp",
		"min_10_exp", "dig", "mant_dig", "epsilon", "radix", "rounds"}, []Value{floatV(math.MaxFloat64),
		mkInt(1024), mkInt(308), floatV(2.2250738585072014e-308), mkInt(-1021), mkInt(-307), mkInt(15), mkInt(53),
		floatV(2.220446049250313e-16), mkInt(2), mkInt(1)}))
	set("int_info", newStructSeq("sys.int_info", []string{"bits_per_digit", "sizeof_digit", "default_max_str_digits",
		"str_digits_check_threshold"}, []Value{mkInt(30), mkInt(4), mkInt(4300), mkInt(640)}))
	fn("getdefaultencoding", constStr("getdefaultencoding", "utf-8"))
	fn("getfilesystemencoding", constStr("getfilesystemencoding", "utf-8"))
	fn("exception", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("exception", 0, 0, args, kw); err != nil {
			return nil, err
		}
		if x := currentHandled(); x != nil {
			return x, nil
		}
		return none, nil
	})
	fn("exc_info", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("exc_info", 0, 0, args, kw); err != nil {
			return nil, err
		}
		if x := currentHandled(); x != nil {
			return newTuple([]Value{excClass(x), x, x.traceback()}), nil
		}
		return newTuple([]Value{none, none, none}), nil
	})
	fn("intern", func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly("intern", 1, 1, args, kw); err != nil {
			return nil, err
		}
		if _, ok := args[0].(strV); !ok {
			return nil, typeErr("intern() argument must be str, not %s", typeName(args[0]))
		}
		return args[0], nil
	})
	return m
}

func constStr(name, s string) func(c *callCtx, args []Value, kw *dictV) (Value, error) {
	return func(c *callCtx, args []Value, kw *dictV) (Value, error) {
		if err := posOnly(name, 0, 0, args, kw); err != nil {
			return nil, err
		}
		return strV(s), nil
	}
}

// builtinModuleNames is sys.builtin_module_names: the modules compiled into
// the interpreter.
func builtinModuleNames() Value {
	names := []string{"_abc", "_ast", "_codecs", "_collections", "_functools", "_imp", "_io", "_locale",
		"_operator", "_signal", "_sre", "_stat", "_string", "_symtable", "_thread", "_tokenize",
		"_tracemalloc", "_typing", "_warnings", "_weakref", "atexit", "builtins", "errno", "faulthandler",
		"gc", "itertools", "marshal", "posix", "pwd", "sys", "time", "xxsubtype"}
	vals := make([]Value, len(names))
	for i, n := range names {
		vals[i] = strV(n)
	}
	return newTuple(vals)
}

// versionInfo is sys.version_info, a tuple of (3, 12, 15, 'final', 0)
// whose fields also have names.
func versionInfo() Value {
	return newStructSeq("sys.version_info", []string{"major", "minor", "micro", "releaselevel", "serial"},
		[]Value{mkInt(3), mkInt(12), mkInt(15), strV("final"), mkInt(0)})
}

// sysExit is sys.exit([status]): it raises SystemExit(status).
func sysExit(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("exit", 0, 1, args, kw); err != nil {
		return nil, err
	}
	return nil, newExc("SystemExit", append([]Value(nil), args...))
}

// currentHandled is the exception being handled, nil when there is none.
func currentHandled() *excV {
	if n := len(interp.handled); n > 0 {
		return interp.handled[n-1]
	}
	return nil
}

// pushHandled marks x as being handled until the returned function runs.
// The marks of one body are taken off in the reverse order of putting them
// on; a generator body's marks move with it between its yields.
func pushHandled(x *excV) func() {
	interp.handled = append(interp.handled, x)
	return func() { interp.handled = interp.handled[:len(interp.handled)-1] }
}

// streamWrite writes text through file.write, as print does.
func streamWrite(file Value, text string) error {
	w, err := pyGetattr(file, "write")
	if err != nil {
		return err
	}
	_, err = callValue(nil, w, []Value{strV(text)}, nil)
	return err
}

// newStructSeq is an instance of a struct sequence type such as
// sys.version_info: a tuple subclass whose items are also attributes, with
// the repr name(field=value, ...).
func newStructSeq(fullName string, fields []string, items []Value) Value {
	mod, name := "builtins", fullName
	if i := strings.LastIndexByte(fullName, '.'); i >= 0 {
		mod, name = fullName[:i], fullName[i+1:]
	}
	ns := newDict()
	ns.setStr("__module__", strV(mod))
	ns.setStr("__qualname__", strV(name))
	ns.setStr("n_fields", mkInt(int64(len(fields))))
	ns.setStr("n_sequence_fields", mkInt(int64(len(fields))))
	ns.setStr("n_unnamed_fields", mkInt(0))
	matchArgs := make([]Value, len(fields))
	for i, f := range fields {
		matchArgs[i] = strV(f)
		idx := i
		get := Func(f, func(args []Value, _ []Keyword) (Value, error) {
			return args[0].(*instV).base.(*tupleV).items[idx], nil
		})
		ns.setStr(f, &propertyV{fget: get})
	}
	ns.setStr("__match_args__", newTuple(matchArgs))
	ns.setStr("__repr__", Func("__repr__", func(args []Value, _ []Keyword) (Value, error) {
		vals := args[0].(*instV).base.(*tupleV).items
		parts := make([]string, len(fields))
		for i, f := range fields {
			r, err := pyRepr(vals[i])
			if err != nil {
				return nil, err
			}
			parts[i] = f + "=" + r
		}
		return strV(fullName + "(" + strings.Join(parts, ", ") + ")"), nil
	}))
	k, err := newUserClass(name, []*classV{classes["tuple"]}, ns)
	if err != nil {
		panic("struct sequence " + fullName + ": " + err.Error())
	}
	return &instV{cls: k, dict: newDict(), base: newTuple(items)}
}

// builtinPrint is print(*objects, sep=' ', end='\n', file=None,
// flush=False), writing through file.write, or to the interpreter's output
// when no file is given.
func builtinPrint(c *callCtx, args []Value, kw *dictV) (Value, error) {
	sep, end := " ", "\n"
	var file Value
	flush := false
	if kw != nil {
		for i, k := range kw.keys {
			v := kw.vals[i]
			name, _ := k.(strV)
			switch name {
			case "sep", "end":
				if isNoneV(v) {
					continue
				}
				s, ok := unsub(v).(strV)
				if !ok {
					return nil, typeErr("%s must be None or a string, not %s", name, typeName(v))
				}
				if name == "sep" {
					sep = string(s)
				} else {
					end = string(s)
				}
			case "file":
				if !isNoneV(v) {
					file = v
				}
			case "flush":
				t, err := truthy(v)
				if err != nil {
					return nil, err
				}
				flush = t
			default:
				return nil, typeErr("'%s' is an invalid keyword argument for print()", string(name))
			}
		}
	}
	write := func(text string) error {
		if file != nil {
			return streamWrite(file, text)
		}
		_, err := io.WriteString(interp.stdout, text)
		return err
	}
	for i, a := range args {
		if i > 0 {
			if err := write(sep); err != nil {
				return nil, err
			}
		}
		s, err := pyStr(a)
		if err != nil {
			return nil, err
		}
		if err := write(s); err != nil {
			return nil, err
		}
	}
	if err := write(end); err != nil {
		return nil, err
	}
	if flush && file != nil {
		if _, err := callMethodValue(file, "flush"); err != nil {
			return nil, err
		}
	}
	return none, nil
}

// callMethodValue is obj.name().
func callMethodValue(obj Value, name string) (Value, error) {
	m, err := pyGetattr(obj, name)
	if err != nil {
		return nil, err
	}
	return callValue(nil, m, nil, nil)
}

// builtinInput is input([prompt]): the prompt written to the interpreter's
// output, then a line read from its input without the newline; EOFError at
// the end of the input.
func builtinInput(c *callCtx, args []Value, kw *dictV) (Value, error) {
	if err := posOnly("input", 0, 1, args, kw); err != nil {
		return nil, err
	}
	if len(args) == 1 {
		p, err := pyStr(args[0])
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(interp.stdout, p); err != nil {
			return nil, err
		}
	}
	line, err := interp.stdin.ReadString('\n')
	if line == "" {
		if err != nil && err != io.EOF {
			return nil, err
		}
		return nil, newPyErr("EOFError", "EOF when reading a line")
	}
	return strV(strings.TrimSuffix(line, "\n")), nil
}
