# Writes cases.txt beside this script: for each function of the math module (and complex * and /)
# below, arguments drawn from a fixed random sequence and the result CPython gives, as
# "name arg... result" with every float in hex, or "name arg... error ClassName" when it raises.
# The values depend on the C library: record with the CPython the package follows (3.12) on
# 64-bit ARM Linux (glibc 2.36).
import math
import os
import random

R = random.Random(2026)


def anyfloat():
    k = R.random()
    if k < 0.3:
        return R.uniform(-2, 2)
    if k < 0.55:
        return R.uniform(-40, 40)
    if k < 0.8:
        return float('%re%d' % (R.uniform(1, 10), R.randint(-300, 300))) * R.choice([1, -1])
    if k < 0.9:
        return float(R.randint(-20, 20))
    return R.choice([0.0, -0.0, 1.0, -1.0, 0.5, math.inf, -math.inf, math.nan, 5e-324, 1e308, 710.0, -745.5])


ONE = ['exp', 'exp2', 'expm1', 'log', 'log2', 'log10', 'log1p', 'sinh', 'cosh', 'tanh', 'asinh', 'acosh', 'atanh',
       'gamma', 'lgamma', 'sqrt', 'degrees', 'radians']
TWO = ['pow', 'atan2', 'hypot', 'fmod', 'remainder', 'copysign']


def fmt(v):
    return v.hex()


lines = []
for name in ONE:
    for _ in range(120):
        x = anyfloat()
        if name in ('acosh',) and R.random() < 0.7:
            x = abs(x) + 1
        if name in ('atanh',) and R.random() < 0.7:
            x = R.uniform(-1, 1)
        try:
            r = fmt(getattr(math, name)(x))
        except (ValueError, OverflowError) as e:
            r = 'error ' + type(e).__name__
        lines.append('%s %s %s' % (name, fmt(x), r))
for name in TWO:
    for _ in range(200):
        x, y = anyfloat(), anyfloat()
        if name == 'pow' and R.random() < 0.5:
            x, y = abs(x), R.uniform(-30, 30)
        try:
            r = fmt(getattr(math, name)(x, y))
        except (ValueError, OverflowError) as e:
            r = 'error ' + type(e).__name__
        lines.append('%s %s %s %s' % (name, fmt(x), fmt(y), r))
for _ in range(200):
    a = complex(R.uniform(-10, 10), R.uniform(-10, 10))
    b = complex(R.uniform(-10, 10), R.uniform(-10, 10))
    m, q = a * b, a / b
    lines.append('cmul %s %s %s %s %s %s' % (fmt(a.real), fmt(a.imag), fmt(b.real), fmt(b.imag), fmt(m.real), fmt(m.imag)))
    lines.append('cdiv %s %s %s %s %s %s' % (fmt(a.real), fmt(a.imag), fmt(b.real), fmt(b.imag), fmt(q.real), fmt(q.imag)))
with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'cases.txt'), 'w') as f:
    f.write('\n'.join(lines) + '\n')
