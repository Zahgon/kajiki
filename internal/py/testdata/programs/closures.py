def make_adders():
    return [lambda x, i=i: x + i for i in range(3)]
print([f(10) for f in make_adders()])
def outer():
    x = 1
    def mid():
        def inner():
            return x
        return inner
    x = 2
    return mid()()
print(outer())
def shadow():
    x = "local"
    class C:
        x = "class"
        def m(self):
            return x
        y = [x for _ in range(1)]
    return C().m(), C.y
print(shadow())
x = "global"
def g1():
    def g2():
        global x
        x = "changed"
    g2()
print(x); g1(); print(x)
def nl():
    a = 1
    def f():
        nonlocal a
        a += 10
    f(); f()
    return a
print(nl())
def recur(n):
    return 1 if n <= 1 else n * recur(n - 1)
print(recur(20))
def deep(n):
    return deep(n + 1)
try:
    deep(0)
except RecursionError as e:
    print("RecursionError", e)
fns = {}
for name in ["a", "b"]:
    def f(name=name):
        return name.upper()
    fns[name] = f
print({k: v() for k, v in fns.items()})
def cell_after_del():
    v = 1
    def r(): return v
    del v
    try:
        r()
    except NameError as e:
        print(e)
cell_after_del()
def gen_closure():
    total = 0
    def add(n):
        nonlocal total
        total += n
        return total
    return add
a = gen_closure()
a(1); a(2)
print(a(3))
k = 5
def uses_k():
    return [k * j for j in range(3)]
print(uses_k())
class Ctr:
    n = 0
    def inc(self):
        Ctr.n += 1
        return self
print(Ctr().inc().inc().n)
lam = lambda: (yield)
print(type(lam()).__name__)
