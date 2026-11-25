a = [1, 2]
b = {"x": 1}
print([*a, *a], (*a,), {*a, 3}, {**b, "y": 2}, {**b, **{"x": 9}})
def f(*args, **kw): return args, kw
print(f(*a, 0, *a, **b, z=1))
first, *rest = "hello"
print(first, rest)
*init, last = [1]
print(init, last)
(a1, b1), *c1 = [(1, 2), 3, 4]
print(a1, b1, c1)
for x, (y, z) in [(1, (2, 3))]:
    print(x, y, z)
d = {}
d["k"], d["j"] = 1, 2
print(d)
lst = [0, 0, 0]
lst[0], lst[2] = "a", "c"
print(lst)
lst[1:2] = [7, 8, 9]
print(lst)
del lst[::2]
print(lst)
x = y = z = 5
print(x, y, z)
i = 0
l2 = [10, 20]
i, l2[i] = 1, 99
print(i, l2)
try:
    f(**{"a": 1}, **{"a": 2})
except TypeError as e:
    print(e)
try:
    print(*5)
except TypeError as e:
    print(e)
try:
    [*None]
except TypeError as e:
    print(e)
try:
    {**[1]}
except TypeError as e:
    print(e)
s = "a,b,c"
p, q, r = s.split(",")
print(r, q, p)
