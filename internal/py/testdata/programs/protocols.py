def attempt(label, fn):
    try:
        r = fn()
        print(label, "->", repr(r))
    except Exception as e:
        print(label, "!!", type(e).__name__, e)
class BadBool:
    def __bool__(self): return 1
class BadLen:
    def __len__(self): return -1
class BigLen:
    def __len__(self): return 2**70
class StrLen:
    def __len__(self): return "3"
class BadHash:
    def __hash__(self): return "x"
class BadIter:
    def __iter__(self): return 5
class BadRepr:
    def __repr__(self): return 5
class BadStr:
    def __str__(self): return 5
class BadIndex:
    def __index__(self): return 1.5
class NoCM: pass
class Eq:
    def __init__(self, v): self.v = v
    def __eq__(self, o): return NotImplemented
class Lt:
    def __lt__(self, o): return NotImplemented
class Sub(int):
    def __radd__(self, o): return "Sub.__radd__"
    def __add__(self, o): return "Sub.__add__"
class AddOnly:
    def __init__(self, v): self.v = v
    def __add__(self, o): return AddOnly(self.v + o)
    def __repr__(self): return f"AddOnly({self.v})"
class GetItem:
    def __getitem__(self, k): return k
    def __setitem__(self, k, v): print("set", k, v)
    def __delitem__(self, k): print("del", k)
class Contains:
    def __iter__(self): return iter([1, 2, 3])
attempt("bool", lambda: bool(BadBool()))
attempt("if", lambda: 1 if BadBool() else 0)
attempt("len", lambda: len(BadLen()))
attempt("biglen", lambda: len(BigLen()))
attempt("strlen", lambda: len(StrLen()))
attempt("boollen", lambda: bool(BadLen()))
attempt("hash", lambda: hash(BadHash()))
attempt("iter", lambda: iter(BadIter()))
attempt("for", lambda: [x for x in BadIter()])
attempt("repr", lambda: repr(BadRepr()))
attempt("str", lambda: str(BadStr()))
attempt("print", lambda: print(BadStr()))
attempt("fstr", lambda: f"{BadStr()}")
attempt("index", lambda: [1, 2][BadIndex()])
attempt("with", lambda: exec("with NoCM(): pass", {"NoCM": NoCM}))
attempt("call", lambda: NoCM()())
attempt("eq", lambda: Eq(1) == Eq(1))
attempt("ne", lambda: Eq(1) != Eq(2))
attempt("lt", lambda: Lt() < Lt())
attempt("gt", lambda: Lt() > 1)
attempt("radd", lambda: 1 + Sub(2))
attempt("add", lambda: Sub(2) + 1)
a = AddOnly(1)
b = a
a += 5
attempt("iadd fallback", lambda: (a, b))
g = GetItem()
attempt("getitem slice", lambda: g[1:2])
attempt("getitem ext", lambda: g[1:2, ::3, ...])
g["k"] = 1
del g[0:1]
attempt("contains iter", lambda: 2 in Contains())
attempt("contains getitem", lambda: 5 in GetItem() if False else "skip")
attempt("unpack", lambda: [*Contains()])
attempt("int", lambda: int(NoCM()))
attempt("float", lambda: float(NoCM()))
attempt("noattr", lambda: NoCM().missing)
attempt("classnoattr", lambda: NoCM.missing)
attempt("modnoattr", lambda: __import__("sys").nothing_here)
attempt("setattr int", lambda: setattr(1, "x", 2))
attempt("del missing", lambda: exec("del zz"))
attempt("unbound", lambda: exec("def f():\n    x\n    x = 1\nf()"))
attempt("free", lambda: exec("def f():\n    def g(): return y\n    g()\n    y = 1\nf()"))
attempt("next nonit", lambda: next([1]))
attempt("iter int", lambda: iter(5))
attempt("call int", lambda: (5)())
attempt("sub str", lambda: "a" - "b")
attempt("neg str", lambda: -"a")
attempt("lt mixed", lambda: 1 < "a")
attempt("getitem int", lambda: (1)[0])
attempt("setitem tuple", lambda: exec("t = (1,)\nt[0] = 2"))
attempt("del tuple", lambda: exec("t = (1,)\ndel t[0]"))
attempt("str idx", lambda: "abc"["x"])
attempt("list idx", lambda: [1][1])
attempt("dict key", lambda: {}["missing"])
attempt("tuple key", lambda: {}[(1, 2)])
attempt("unhash", lambda: {[]: 1})
attempt("unhash set", lambda: {[1]})
attempt("kw notstr", lambda: (lambda **k: k)(**{1: 2}))
attempt("chain", lambda: 1 < 2 < 3 > 2 != 5)
x = [0]
def side():
    x[0] += 1
    return x[0]
attempt("chain once", lambda: (0 < side() < 5, x[0]))
i = 0
while (j := i * 2) < 6:
    i += 1
print("walrus while", i, j)
attempt("star gen", lambda: (lambda *a: a)(*(c for c in "ab")))
