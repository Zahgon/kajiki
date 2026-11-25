class Base:
    """Base doc"""
    cnt = 0
    def __init__(self, x):
        self.x = x
        Base.cnt += 1
    def __repr__(self):
        return f"{type(self).__name__}({self.x!r})"
    def __eq__(self, other):
        return isinstance(other, Base) and self.x == other.x
    def __hash__(self):
        return hash(self.x)
    @property
    def double(self):
        return self.x * 2
    @double.setter
    def double(self, v):
        self.x = v // 2
    @classmethod
    def make(cls, v):
        return cls(v)
    @staticmethod
    def st(a):
        return a + 1
class Child(Base):
    def __init__(self, x, y=0):
        super().__init__(x)
        self.y = y
    def __repr__(self):
        return "Child:" + super().__repr__()
c = Child(3, 4)
print(c, c.double, Child.make(5), Base.st(1), Base.cnt, Child.__doc__, Base.__doc__)
c.double = 10
print(c.x, c == Child(5), {c: 1}[Child(5)])
print(Child.__mro__, Child.__name__, Child.__qualname__, Child.__module__)
print(isinstance(c, Base), issubclass(Child, Base), type(c) is Child)
class Meta(type):
    def __new__(mcls, name, bases, ns, **kw):
        ns["added"] = kw.get("flag", 0)
        return super().__new__(mcls, name, bases, ns)
class WithMeta(metaclass=Meta, flag=7):
    pass
print(WithMeta.added, type(WithMeta).__name__)
class Reg:
    subs = []
    def __init_subclass__(cls, tag=None, **kw):
        super().__init_subclass__(**kw)
        Reg.subs.append((cls.__name__, tag))
class R1(Reg, tag="one"): pass
class R2(Reg): pass
print(Reg.subs)
def deco(cls):
    cls.decorated = True
    return cls
@deco
class D:
    __slots__ = ("a",)
print(D.decorated)
d = D()
d.a = 1
try:
    d.b = 2
except AttributeError as e:
    print(e)
class A1:
    def f(self): return "A1"
class B1(A1):
    def f(self): return "B1>" + super().f()
class C1(A1):
    def f(self): return "C1>" + super().f()
class E1(B1, C1):
    def f(self): return "E1>" + super().f()
print(E1().f(), [k.__name__ for k in E1.__mro__])
class Outer:
    class Inner:
        def m(self): pass
print(Outer.Inner.__qualname__, Outer.Inner.m.__qualname__)
class N:
    def __len__(self): return 3
    def __bool__(self): return False
    def __getitem__(self, i):
        if i >= 3: raise IndexError
        return i * 10
    def __contains__(self, x): return x == 99
    def __call__(self, *a, **k): return (a, k)
n = N()
print(len(n), bool(n), list(n), 99 in n, 5 in n, n(1, z=2))
class V:
    def __init__(self, v): self.v = v
    def __add__(self, o): return V(self.v + (o.v if isinstance(o, V) else o))
    def __radd__(self, o): return V(o + self.v)
    def __lt__(self, o): return self.v < o.v
    def __repr__(self): return f"V{self.v}"
print(V(1) + V(2), 3 + V(4), V(1) + 5, sorted([V(3), V(1), V(2)]), max(V(1), V(9)))
try:
    V(1) - V(2)
except TypeError as e:
    print(e)
class G:
    def __getattr__(self, name): return name.upper()
print(G().hello)
print(getattr(G(), "x"), hasattr(c, "zzz"), vars(Child(1)))
