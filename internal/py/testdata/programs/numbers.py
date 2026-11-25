print(2 ** 100, -7 // 2, -7 % 2, 7 % -2, divmod(-7, 2), 1 / 3, 2 / 2, 0.1 + 0.2, 1e300 * 10, -1e300 * 10)
print(int("ff", 16), int("0b101", 0), int(" 12 "), int(3.99), int(-3.99), round(0.5), round(1.5), round(2.675, 2))
print(float("inf"), float("-inf"), float("nan"), 1e16, 1.0e-5, 123456789012345678.0, 1/7)
print(3 + 4j, (1 + 2j) * (3 - 1j), abs(3 + 4j), complex(1, 2), (2j) ** 2)
print(True + True, True * 3, 10 // 3.0, 10 % 3.5, -0.0, 0.0 == -0.0, 5 // 0.5)
print(min(1, 2.5, -1), max([3, 7, 2]), sum([0.1] * 10), abs(-2.5), pow(2, 10, 1000), pow(2, -1))
print(1 < 2.5, 3 == 3.0, hash(1) == hash(1.0), (1, 2) < (1, 3), [1, 2] == [1, 2])
for bad in ["1 / 0", "1 % 0", "int('x')", "float('y')", "2 ** 10000.0", "[] < 1"]:
    try:
        eval(bad)
    except Exception as e:
        print(type(e).__name__, e)
print(0x10, 0o17, 0b11, 1_000_000, 1_0.5, 0.1e1_0)
print(7 & 3, 7 | 8, 7 ^ 2, ~7, 1 << 70, -1 >> 1, 255 >> 4)
