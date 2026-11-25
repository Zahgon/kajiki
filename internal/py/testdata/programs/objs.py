class Desc:
    def __set_name__(self, owner, name):
        self.name = "_" + name
    def __get__(self, obj, objtype=None):
        if obj is None:
            return self
        return getattr(obj, self.name, "unset")
    def __set__(self, obj, value):
        setattr(obj, self.name, value * 2)
class Holder:
    d = Desc()
h = Holder()
print(h.d)
h.d = 5
print(h.d, h._d, type(Holder.d).__name__)
class GA:
    def __getattribute__(self, name):
        if name == "magic":
            return 42
        return super().__getattribute__(name)
    def __setattr__(self, name, value):
        super().__setattr__(name, value * 10 if isinstance(value, int) else value)
    def __delattr__(self, name):
        print("deleting", name)
        super().__delattr__(name)
ga = GA()
ga.v = 3
print(ga.magic, ga.v)
del ga.v
print(hasattr(ga, "v"))
class P:
    def __init__(self): self._x = 0
    @property
    def x(self): return self._x
    @x.setter
    def x(self, v): self._x = v
    @x.deleter
    def x(self): print("del x"); self._x = None
p = P(); p.x = 4; print(p.x); del p.x; print(p.x)
try:
    P.x.fget(None)
except AttributeError as e:
    print("AE", e)
class RO:
    @property
    def r(self): return 1
try:
    RO().r = 2
except AttributeError as e:
    print(e)
class Sl:
    __slots__ = ("a", "b")
    def __init__(self): self.a = 1
s = Sl()
print(s.a, hasattr(s, "b"))
try:
    s.c = 1
except AttributeError as e:
    print(e)
try:
    print(s.__dict__)
except AttributeError as e:
    print(e)
T = type("T", (object,), {"v": 1, "m": lambda self: self.v + 1})
print(T().m(), T.__name__, T.__mro__)
class MC(type):
    def __call__(cls, *a):
        print("MC call", a)
        return super().__call__(*a)
    @classmethod
    def __prepare__(mcls, name, bases):
        print("prepare", name)
        return {"injected": 7}
class W(metaclass=MC):
    def __init__(self, v): self.v = v
w = W(3)
print(w.v, W.injected, type(W).__name__, isinstance(W, MC))
class CG:
    def __class_getitem__(cls, item):
        return f"CG[{item}]"
print(CG[int])
class Fmt:
    def __format__(self, spec): return "F<" + spec + ">"
    def __str__(self): return "S"
    def __repr__(self): return "R"
print(f"{Fmt()} {Fmt():xyz} {Fmt()!r} {Fmt()!s}", format(Fmt(), "q"), str(Fmt()), repr(Fmt()), [Fmt()])
class Cmp:
    def __init__(self, v): self.v = v
    def __eq__(self, o): return self.v == o.v
    def __lt__(self, o): return self.v < o.v
    def __le__(self, o): return self.v <= o.v
print(Cmp(1) == Cmp(1), Cmp(1) != Cmp(2), Cmp(1) < Cmp(2), Cmp(2) > Cmp(1), Cmp(2) >= Cmp(2))
try:
    hash(Cmp(1))
except TypeError as e:
    print(e)
class Num:
    def __init__(self, v): self.v = v
    def __iadd__(self, o): self.v += o; return self
    def __neg__(self): return Num(-self.v)
    def __abs__(self): return Num(abs(self.v))
    def __invert__(self): return "inv"
    def __pos__(self): return "pos"
    def __mul__(self, o): return Num(self.v * o)
    __rmul__ = __mul__
    def __truediv__(self, o): return "div"
    def __floordiv__(self, o): return "fdiv"
    def __mod__(self, o): return "mod"
    def __pow__(self, o): return "pow"
    def __matmul__(self, o): return "matmul"
    def __and__(self, o): return "and"
    def __or__(self, o): return "or"
    def __xor__(self, o): return "xor"
    def __lshift__(self, o): return "lshift"
    def __rshift__(self, o): return "rshift"
    def __index__(self): return self.v
    def __int__(self): return self.v
    def __float__(self): return float(self.v)
n = Num(3); n += 2
print(n.v, (-n).v, abs(Num(-2)).v, ~n, +n, (n * 2).v, (2 * n).v, n / 1, n // 1, n % 1, n ** 1, n @ 1, n & 1, n | 1, n ^ 1, n << 1, n >> 1)
print([10, 20, 30, 40, 50, 60][Num(1)], int(Num(7)), float(Num(2)), hex(Num(255)), bin(Num(2)))
class It:
    def __init__(self): self.i = 0
    def __iter__(self): return self
    def __next__(self):
        self.i += 1
        if self.i > 3: raise StopIteration
        return self.i
print(list(It()), sum(It()), 2 in It(), sorted(It(), reverse=True))
class Seq:
    def __getitem__(self, i):
        if i < 4: return i * i
        raise IndexError(i)
print(list(Seq()), 9 in Seq(), list(reversed([1, 2])))
class Truth:
    def __len__(self): return 0
print(bool(Truth()), not Truth(), "yes" if Truth() else "no")
class Ctx:
    def __bool__(self): return 1
try:
    bool(Ctx())
except TypeError as e:
    print(e)
print(callable(Ctx), callable(Ctx()), isinstance(3, (str, int)), issubclass(bool, (str, int)))
o = object()
try:
    o.x = 1
except AttributeError as e:
    print(e)
class Empty: pass
e = Empty()
print(repr(e).startswith("<__main__.Empty object at 0x"), e == e, e != Empty(), vars(e))
print(getattr(e, "nope", "dflt"))
try:
    getattr(e, "nope")
except AttributeError as ex:
    print(ex)
setattr(e, "z", 9); print(e.z); delattr(e, "z"); print(hasattr(e, "z"))
class A: pass
class B(A): pass
print(B.__bases__, A.__subclasses__() if hasattr(A, "__subclasses__") else None, B.__base__)
