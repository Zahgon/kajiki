def f():
    try:
        1 / 0
    except ZeroDivisionError as e:
        raise ValueError("wrapped") from e
try:
    f()
except ValueError as e:
    print(repr(e), repr(e.__cause__), e.__suppress_context__, repr(e.__context__))
try:
    try:
        raise KeyError("k")
    except KeyError:
        raise TypeError("t")
except TypeError as e:
    print(repr(e.__context__), e.__cause__, e.__suppress_context__)
try:
    raise RuntimeError("x") from None
except RuntimeError as e:
    print(e.__cause__, e.__suppress_context__)
def g():
    try:
        return 1
    finally:
        print("finally")
print(g())
def h():
    for i in range(3):
        try:
            if i == 1:
                continue
            print("body", i)
        finally:
            print("fin", i)
h()
try:
    try:
        raise ValueError(1)
    finally:
        print("cleanup")
except ValueError as e:
    print("caught", e.args)
try:
    pass
except Exception:
    pass
else:
    print("else")
class MyErr(Exception):
    def __init__(self, a, b):
        super().__init__(a)
        self.b = b
    def __str__(self):
        return f"MyErr<{self.args} {self.b}>"
try:
    raise MyErr(1, 2)
except MyErr as e:
    print(e, repr(e), e.args)
try:
    raise Exception
except Exception as e:
    print(repr(e))
e = None
try:
    raise IndexError
except (KeyError, IndexError) as err:
    print("tuple catch", type(err).__name__)
try:
    err
except NameError as ne:
    print(ne)
import sys
try:
    raise ValueError("v")
except ValueError:
    print(sys.exc_info()[0].__name__, sys.exception())
print(sys.exception())
try:
    raise 5
except TypeError as e:
    print(e)
try:
    try:
        1/0
    except:
        raise
except ZeroDivisionError as e:
    print("reraise", e)
assert True
try:
    assert 1 == 2, "nope"
except AssertionError as e:
    print("assert", e)
