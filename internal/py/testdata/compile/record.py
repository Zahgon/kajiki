# Records what CPython's compile() does with each case of cases.jsonl beside this script and
# rewrites the file with the outcome under "expect": {"ok": true}, or the SyntaxError subclass
# with its message, location, text and str().  Run with the CPython the package follows (3.12).
import json
import os
import sys
import warnings

warnings.simplefilter('ignore')


def outcome(src, mode, filename):
    try:
        compile(src, filename, mode)
        return {'ok': True}
    except SyntaxError as e:
        return {'cls': type(e).__name__, 'msg': e.msg, 'lineno': e.lineno, 'offset': e.offset,
                'end_lineno': e.end_lineno, 'end_offset': e.end_offset, 'text': e.text, 'str': str(e)}
    except Exception as e:  # noqa: BLE001
        return {'cls': type(e).__name__, 'str': str(e)}


path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(os.path.abspath(__file__)), 'cases.jsonl')
with open(path, encoding='utf-8') as f:
    cases = [json.loads(line) for line in f]
with open(path, 'w', encoding='utf-8') as f:
    for c in cases:
        c['expect'] = outcome(c['src'], c['mode'], c.get('filename', '<string>'))
        f.write(json.dumps(c, ensure_ascii=False) + '\n')
