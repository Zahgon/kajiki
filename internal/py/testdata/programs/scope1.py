x = 1
def f():
    global x
    x = 2
    def g():
        nonlocal_y = 3
        def h():
            nonlocal nonlocal_y
            nonlocal_y += 1
            return nonlocal_y
        return h() + h()
    return g()
print(f(), x)
def counter():
    n = 0
    def inc():
        nonlocal n
        n += 1
        return n
    return inc
c = counter()
print(c(), c(), c())
def u():
    print(y)
    y = 1
try:
    u()
except UnboundLocalError as e:
    print("UnboundLocalError:", e)
try:
    print(undefined_name)
except NameError as e:
    print("NameError:", e)
def cl():
    a = 1
    del a
    return lambda: a
try:
    cl()()
except NameError as e:
    print(type(e).__name__, e)
fs = [lambda: i for i in range(3)]
print([f() for f in fs])
print([(lambda j=i: j)() for i in range(3)])
class A:
    v = 1
    w = [v for _ in range(2)] if False else None
    def m(self):
        return v
try:
    A().m()
except NameError as e:
    print(e)
print((z := 10), z)
print([y := k * 2 for k in range(3)], y)
del x
try:
    x
except NameError as e:
    print(e)
