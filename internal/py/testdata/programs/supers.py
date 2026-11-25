class A:
    def __init__(self):
        print("A init")
    @classmethod
    def make(cls):
        print("A.make", cls.__name__)
        return cls()
    def __init_subclass__(cls, **kw):
        print("init_subclass", cls.__name__, kw)
class B(A, flag=1):
    def __init__(self):
        super(B, self).__init__()
        print("B init")
    @classmethod
    def make(cls):
        print("B.make")
        return super().make()
class C(B):
    def __new__(cls, *a):
        print("C new", cls.__name__)
        return super().__new__(cls)
B.make()
C()
class S(str):
    def __new__(cls, v):
        return super().__new__(cls, v.upper())
    def shout(self): return self + "!"
s = S("hi")
print(s, s.shout(), type(s).__name__, isinstance(s, str), len(s), s == "HI", repr(s), s.lower(), type(s.lower()).__name__)
class I(int):
    def __repr__(self): return f"I({int(self)})"
i = I(5)
print(i, i + 1, type(i + 1).__name__, repr(i), i * 2, str(i), f"{i}")
class L(list):
    def push(self, x): self.append(x); return self
l = L([1]); l.push(2)
print(l, len(l), type(l).__name__, l[0], l + [3], type(l + [3]).__name__)
class D(dict):
    def __missing__(self, k): return k * 2
d = D(a=1)
print(d["a"], d["zz"], d, type(d).__name__)
class T(tuple):
    pass
print(T((1, 2)), T((1,)) + (2,))
class E(Exception):
    def __init__(self, msg, code):
        super().__init__(msg)
        self.code = code
try:
    raise E("boom", 7)
except E as e:
    print(e, e.code, e.args, repr(e))
class Base:
    def who(self): return "Base"
class M1(Base):
    def who(self): return "M1+" + super().who()
class M2(Base):
    def who(self): return "M2+" + super().who()
class Diamond(M1, M2):
    def who(self): return "D+" + super().who()
print(Diamond().who())
try:
    class Bad(M1, Base, M2): pass
except TypeError as e:
    print(e)
class F(float): pass
print(F(1.5) + 1, F(2.0), repr(F(0.1)))
