# Records the tokens tokenize.generate_tokens yields for each case of cases.jsonl beside this
# script, reading the source with io.StringIO(src).readline, and rewrites the file with them
# under "expect": a list of [type, string, start, end, line], and the class and str() of the
# exception that ended the stream, if any.  Run with the CPython the package follows (3.12).
import io
import json
import os
import sys
import tokenize


def run(src):
    toks = []
    try:
        for t in tokenize.generate_tokens(io.StringIO(src).readline):
            toks.append([t.type, t.string, list(t.start), list(t.end), t.line])
    except Exception as e:  # noqa: BLE001
        return {'toks': toks, 'err': type(e).__name__, 'str': str(e)}
    return {'toks': toks}


path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(os.path.abspath(__file__)), 'cases.jsonl')
with open(path, encoding='utf-8') as f:
    cases = [json.loads(line) for line in f]
with open(path, 'w', encoding='utf-8') as f:
    for c in cases:
        c['expect'] = run(c['src'])
        f.write(json.dumps(c, ensure_ascii=False) + '\n')
