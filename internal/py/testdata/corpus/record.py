# Runs each program of programs.jsonl beside this script as __main__ (compiled with filename
# "<prog>") and rewrites the file with what happened under "expect": what it printed, and for
# an exception that escaped it, its class and str(), the (filename, function, line) entries of
# its traceback that lie in the program, the classes of __cause__ and __context__, and for a
# SyntaxError its message and position.  Run with the CPython the package follows (3.12).
import io
import json
import os
import signal
import sys
import traceback
import warnings

warnings.simplefilter('ignore')
sys.setrecursionlimit(1000)


class Timeout(BaseException):
    pass


def alarm(signum, frame):
    raise Timeout()


signal.signal(signal.SIGALRM, alarm)


def safe_str(e):
    try:
        return str(e)
    except Exception as x:  # noqa: BLE001
        return '<str failed: %s>' % type(x).__name__


def run(src):
    signal.alarm(10)
    out = io.StringIO()
    old = sys.stdout
    sys.stdout = out
    res = {}
    try:
        exec(compile(src, '<prog>', 'exec'), {'__name__': '__main__'})
    except BaseException as e:  # noqa: BLE001
        res['exc'] = [type(e).__name__, safe_str(e)]
        res['tb'] = [[f.f_code.co_filename, f.f_code.co_name, ln if ln is not None else 0]
                     for f, ln in traceback.walk_tb(e.__traceback__) if f.f_code.co_filename == '<prog>']
        if e.__cause__ is not None:
            res['cause'] = type(e.__cause__).__name__
        if e.__context__ is not None:
            res['context'] = type(e.__context__).__name__
        if isinstance(e, SyntaxError):
            res['syn'] = [e.msg, e.lineno, e.offset]
    finally:
        sys.stdout = old
        signal.alarm(0)
    res['out'] = out.getvalue()
    return res


path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(os.path.abspath(__file__)), 'programs.jsonl')
with open(path, encoding='utf-8') as f:
    cases = [json.loads(line) for line in f]
with open(path, 'w', encoding='utf-8') as f:
    for c in cases:
        c['expect'] = run(c['src'])
        f.write(json.dumps(c, ensure_ascii=False) + '\n')
