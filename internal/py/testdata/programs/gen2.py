def inner():
    try:
        x = yield 1
        print("inner got", x)
        y = yield 2
        print("inner got", y)
    except KeyError as e:
        print("inner caught", repr(e))
        yield "recovered"
    finally:
        print("inner finally")
    return "inner-ret"
def outer():
    r = yield from inner()
    print("outer r", r)
    yield "outer-last"
g = outer()
print(next(g))
print(g.send("A"))
print(g.throw(KeyError("kk")))
print(next(g))
try:
    next(g)
except StopIteration as e:
    print("done", e.value)
def gen_close():
    try:
        yield 1
    except GeneratorExit:
        print("got GeneratorExit")
        raise
gc = gen_close()
next(gc)
gc.close()
print(gc.close())
def bad_close():
    try:
        yield 1
    except GeneratorExit:
        yield 2
bc = bad_close()
next(bc)
try:
    bc.close()
except RuntimeError as e:
    print("RuntimeError", e)
def stop_in_gen():
    yield 1
    raise StopIteration("x")
try:
    list(stop_in_gen())
except RuntimeError as e:
    print(e, type(e.__cause__).__name__)
g2 = (i for i in range(3))
print(next(g2), list(g2), next(g2, "default"))
def counter():
    i = 0
    while True:
        cmd = yield i
        if cmd == "reset":
            i = 0
        else:
            i += 1
c = counter()
print(next(c), next(c), c.send("reset"), next(c))
def running():
    yield g3.send(None)
g3 = running()
try:
    next(g3)
except ValueError as e:
    print(e)
gen = (x for x in [1])
print(gen.__name__, gen.__qualname__)
def gf(): yield
print(gf.__name__, gf().__name__, type(gf()).__name__)
print(list(zip(range(3), (c for c in "abc"))))
def yield_in_try():
    for i in range(3):
        try:
            yield i
        finally:
            print("cleanup", i)
for v in yield_in_try():
    if v == 1:
        break
print("after break")
