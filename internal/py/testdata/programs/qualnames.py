def outer():
    def inner(): pass
    lam = lambda: 0
    lst = [lambda: i for i in range(1)]
    gen = (lambda: 0 for _ in range(1))
    class K:
        def m(self): pass
        lm = [lambda: 0 for _ in range(1)]
    return inner, lam, lst[0], next(gen), K, K.m, K.lm[0]
for f in outer():
    print(f.__qualname__, f.__name__)
print((x for x in []).__qualname__, outer.__code__.co_name)
def g(a, b=2, *c, d, **e):
    x = 1
    print(sorted(locals()))
    return locals()
print(g(1, d=4))
def h():
    v = 1
    def i():
        return v
    return i
print(h().__closure__ is not None, type(h().__closure__).__name__ if h().__closure__ else None)
class A:
    print(__qualname__, __module__, sorted(locals()))
print(vars().get("A") is A)
def nl():
    x = 1
    def f():
        nonlocal x
        del x
    f()
    try:
        return x
    except NameError as e:
        return str(e)
print(nl())
