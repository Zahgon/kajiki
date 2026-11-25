eg = ExceptionGroup("meep", [KeyError("k"), ValueError(1)])
print(eg, repr(eg), eg.message, eg.exceptions, type(eg).__mro__, isinstance(eg, Exception))
b = BaseExceptionGroup("b", [ValueError()])
print(type(b).__name__, repr(b))
b2 = BaseExceptionGroup("b", [KeyboardInterrupt()])
print(type(b2).__name__, str(b2))
for bad in [lambda: ExceptionGroup(1, [ValueError()]), lambda: ExceptionGroup("m", []), lambda: ExceptionGroup("m", [1]),
            lambda: ExceptionGroup("m", [KeyboardInterrupt()]), lambda: ExceptionGroup("m", 5), lambda: ExceptionGroup("m")]:
    try:
        bad()
    except Exception as e:
        print(type(e).__name__, e)
m, r = eg.split(KeyError)
print(repr(m), repr(r))
print(repr(eg.subgroup(ValueError)), eg.subgroup(OSError))
print(repr(eg.split(lambda e: isinstance(e, ValueError))))
nested = ExceptionGroup("outer", [ValueError(1), ExceptionGroup("inner", [KeyError(2), TypeError(3)])])
print(repr(nested.split(KeyError)))
def f():
    got = ""
    try:
        raise ExceptionGroup("meep", [KeyError(), ValueError()])
    except* KeyError as e:
        got += "k"
        print("k handler", repr(e))
    except* IndexError:
        got += "i"
    except* ValueError as e:
        got += "v"
        print("v handler", repr(e))
    else:
        got += "e"
    finally:
        got += "f"
    return got
print(f())
try:
    try:
        raise ValueError("naked")
    except* ValueError as e:
        print("naked caught", repr(e))
except Exception as x:
    print("outer", repr(x))
try:
    try:
        raise TypeError("t")
    except* ValueError:
        print("no")
except TypeError as x:
    print("propagated naked", repr(x))
try:
    try:
        raise ExceptionGroup("g", [TypeError("t"), ValueError("v")])
    except* ValueError:
        print("handled v")
except ExceptionGroup as x:
    print("rest", repr(x))
try:
    try:
        raise ExceptionGroup("g", [ValueError("v")])
    except* ValueError:
        raise KeyError("new")
except BaseException as x:
    print("raised in handler", repr(x), repr(x.exceptions[0].__context__) if hasattr(x, "exceptions") else "")
try:
    try:
        raise ExceptionGroup("g", [ValueError("v")])
    except* ValueError:
        raise
except BaseException as x:
    print("reraise", repr(x))
