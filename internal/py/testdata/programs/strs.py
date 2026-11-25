name = "World"
n = 3.14159
print(f"Hello {name}! {n:.3f} {n!r:>12} {name!s:^9}|{name=}|{n = :.1f}")
print(f"{'nested ' + f'{name}'}", f"{{literal}}", f"{1+1}{2*3}")
w = 10
print(f"{n:{w}.{2}f}|", f"{12345678:,}", f"{0.25:%}", f"{42:08b}", f"{'x':*<5}")
print("%s %d %.2f %r %5s|%-5s|" % ("a", 3, 2.5, "q", "r", "l"), "%(k)s" % {"k": "v"})
print("{} {} {}".format(1, "two", 3.0), "{0}{1}{0}".format("a", "b"), "{x}".format(x=5), "{:>4}".format(7))
print(repr("tab\there"), repr('quote"s'), repr("both'\""), "multi\nline".splitlines())
print(str(b"bytes"), repr(b"\x00\xff"), "é".encode("utf-8"), b"caf\xc3\xa9".decode())
print("abc".upper(), " x ".strip(), "a-b".replace("-", "+"), "abc".startswith("ab"), ",".join(["1", "2"]))
print("Hello"[1:3], "Hello"[::-1], len("héllo"), "x" * 3, "ab" < "b", ord("€"), chr(8364))
print(f"{3!a}", ascii("ü"), f"{'ü'!a}")
