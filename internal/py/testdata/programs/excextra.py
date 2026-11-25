e = ValueError("v")
e.add_note("note1")
print(e.__notes__)
try:
    e.add_note(5)
except TypeError as x:
    print(x)
print(NameError('x', name='y').name, AttributeError('x', name='n', obj=5).obj, AttributeError('q').name)
try:
    KeyError("k", name=1)
except TypeError as x:
    print(x)
print(Exception.__bases__, ValueError.__bases__, int.__bases__, object.__bases__, bool.__bases__)
e.foo = 3
print(e.__dict__, Exception.__init__)
print(vars(type("T", (), {"a": 1})))
class C:
    x = 1
print(C.__dict__.keys(), list(vars(C)))
class D(C):
    y = 2
print(list(vars(D)))
class E(Exception): pass
print(list(vars(E)))
class S(str): pass
print(list(vars(S)))
class Sl:
    __slots__ = ("a",)
print(list(vars(Sl)))
print(C().__weakref__, str(C.__dict__)[:20], repr(C.__dict__)[:20])
