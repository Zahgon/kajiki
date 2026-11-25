#!/usr/bin/env python3
"""Record what CPython 3.12's re module does with the vectors of vectors.jsonl beside this script,
rewriting the file in place (run in the pinned CPython 3.12 image).

Each vector is a pattern, its flags, some texts and operations on them. Recording adds "compile"
(groups, groupindex, flags and the compiled code without the INFO block, or the exception class
and str) and one "res" per operation: match/search/fullmatch with pos and endpos, finditer spans,
split, subn and expand, or the exception the operation raised.
"""
import json
import os
import re
import sys
import warnings
from re import _compiler, _parser
from re._constants import INFO

warnings.simplefilter("ignore")


def exc(e):
    if isinstance(e, re.error):
        return {"err": "error", "msg": str(e)}
    return {"err": type(e).__name__, "msg": str(e)}


def code_of(pattern, flags):
    p = _parser.parse(pattern, flags)
    code = _compiler._code(p, flags)
    if code and code[0] == INFO:
        code = code[code[1] + 1:]
    return [int(c) for c in code]


def enc_match(m):
    if m is None:
        return None
    n = m.re.groups
    return {
        "spans": [list(m.span(g)) for g in range(n + 1)],
        "li": m.lastindex,
        "lg": m.lastgroup,
        "groups": list(m.groups()),
        "gd": [[k, v] for k, v in m.groupdict().items()],
        "repr": repr(m),
        "pos": m.pos,
        "endpos": m.endpos,
    }


def run_op(rx, texts, op):
    t = texts[op["t"]]
    kind = op["op"]
    try:
        if kind in ("match", "search", "fullmatch"):
            fn = getattr(rx, kind)
            if "endpos" in op:
                return enc_match(fn(t, op["pos"], op["endpos"]))
            return enc_match(fn(t, op["pos"]))
        if kind == "finditer":
            return [[list(m.span(g)) for g in range(rx.groups + 1)] for m in rx.finditer(t)]
        if kind == "split":
            return rx.split(t, op["maxsplit"])
        if kind == "sub":
            return {"ok": rx.subn(op["repl"], t, op["count"])}
        if kind == "expand":
            m = rx.search(t)
            if m is None:
                return None
            return {"ok": m.expand(op["repl"])}
    except Exception as e:  # noqa: BLE001 - every exception is part of the behaviour
        return exc(e)
    raise ValueError(kind)


def record(v):
    out = {k: v[k] for k in ("id", "pattern", "flags", "texts", "ops")}
    try:
        rx = re.compile(v["pattern"], v["flags"])
    except Exception as e:  # noqa: BLE001
        out["compile"] = exc(e)
        out["ops"] = []
        return out
    out["compile"] = {
        "groups": rx.groups,
        "groupindex": [[k, i] for k, i in rx.groupindex.items()],
        "flags": rx.flags,
        "code": code_of(v["pattern"], v["flags"]),
    }
    for op in out["ops"]:
        op["res"] = run_op(rx, v["texts"], op)
    return out


path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(os.path.abspath(__file__)), "vectors.jsonl")
with open(path, encoding="utf-8") as f:
    vectors = [json.loads(line) for line in f if line.strip()]
with open(path, "w", encoding="utf-8") as f:
    for v in vectors:
        f.write(json.dumps(record(v), ensure_ascii=True) + "\n")
