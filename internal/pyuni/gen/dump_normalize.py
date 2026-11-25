#!/usr/bin/env python3
"""Reference data for the NFKC and titlecase test, computed from CPython 3.12.

Writes testdata/normalize.sha256, the sha256 of one line per code point (surrogates left out)
holding str.title() and unicodedata.normalize('NFKC') of the character, and testdata/
normalize.json, NFKC of random strings mixing combining marks, Hangul jamo and syllables and
compatibility characters, so that canonical ordering and composition across characters are
covered. Run inside the pinned image from gold/internal/pyuni:

    rec/py.sh gold/internal/pyuni python3 gen/dump_normalize.py testdata/normalize.sha256 testdata/normalize.json
"""
import hashlib
import json
import random
import sys
import unicodedata


def esc(s):
    return " ".join(f"{ord(c):x}" for c in s)


h = hashlib.sha256()
for c in range(0x110000):
    if 0xD800 <= c <= 0xDFFF:
        continue
    ch = chr(c)
    h.update(f"{c:x} [{esc(ch.title())}] [{esc(unicodedata.normalize('NFKC', ch))}]\n".encode())
with open(sys.argv[1], "w") as f:
    f.write(h.hexdigest() + "\n")

pool = ([chr(c) for c in range(0x300, 0x370)] + [chr(c) for c in range(0x1100, 0x1200, 3)] +
        [chr(c) for c in range(0xAC00, 0xAC00 + 11172, 397)] + list("aeiouAEIOUnNcCsSkK") +
        list("ﬁﬃ½²ⅫＡｚ㎏ſÅÅक़Ωẛཱཱིུ̈́") +
        [chr(c) for c in range(0x591, 0x5c8, 5)] + [chr(c) for c in range(0x1dc0, 0x1e00, 7)])
rnd = random.Random(15)
cases = []
for _ in range(600):
    s = "".join(rnd.choice(pool) for _ in range(rnd.randint(1, 8)))
    cases.append([s, unicodedata.normalize("NFKC", s)])
with open(sys.argv[2], "w") as f:
    json.dump({"python": sys.version.split()[0], "unicode": unicodedata.unidata_version, "cases": cases}, f,
              indent=0)
    f.write("\n")
