class B: pass
class F(B, SyntaxError):
    def __str__(self): return "F:" + super().__str__()
print(str(F("bad", ("/a/f.hy", 2, 3, "txt"))))
class Object:
    def __hash__(self): return super().__hash__()
    def __eq__(self, o): return type(self) is type(o) and super().__eq__(o)
    def __ne__(self, o): return not self == o
    def __repr__(self): return "M(" + super().__repr__() + ")"
class Symbol(Object, str): pass
class List(Object, list): pass
class Tup(Object, tuple): pass
print(hash(Symbol("x")) == hash("x"), Symbol("x") == Symbol("x"), Symbol("x") == "x", Symbol("x") in [Symbol("x")], Symbol("x") in List([Symbol("x")]))
print(repr(Symbol("ab")), repr(List([1])), Symbol("a") != Symbol("b"), object.__ne__)
print("x" + Symbol("a"), Symbol("a") + Symbol("b"), type(Symbol("a") + "b").__name__, (1,) + Tup((2,)), List([1]) + [2])
print({Symbol("k"): 1}["k"], Symbol("q").upper(), len(Symbol("abc")), Symbol("abc")[1], list(Symbol("ab")))
print(object.__eq__(1, 1), object.__ne__(1, 2), object().__eq__(3))
