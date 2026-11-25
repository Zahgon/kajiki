# Records what CPython does with each program of a directory (default: programs/ beside this
# script; files named helper_*.py are modules the programs import): for prog.py it writes prog.json with the exit status, stdout, and stderr, which for an
# uncaught exception is reduced to its last line (the exception), since tracebacks show the
# interpreter's own paths. A prog.stdin file is the program's standard input.
import json
import os
import subprocess
import sys

d = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(os.path.abspath(__file__)), 'programs')
for name in sorted(os.listdir(d)):
    if not name.endswith('.py') or name.startswith('helper_'):
        continue
    path = os.path.join(d, name)
    stdin = b''
    if os.path.exists(path[:-3] + '.stdin'):
        with open(path[:-3] + '.stdin', 'rb') as f:
            stdin = f.read()
    p = subprocess.run([sys.executable, name], cwd=d, input=stdin, capture_output=True, timeout=60)
    # Paths of the program's directory are recorded relative to it.
    err = p.stderr.decode('utf-8', 'surrogateescape').replace(os.path.abspath(d) + '/', '')
    if err.startswith('Traceback (most recent call last):\n') and p.returncode == 1:
        err = 'Traceback (most recent call last):\n' + err.rstrip('\n').split('\n')[-1] + '\n'
    with open(path[:-3] + '.json', 'w') as f:
        json.dump({'status': p.returncode, 'stdout': p.stdout.decode('utf-8', 'surrogateescape'),
                   'stderr': err}, f, indent=1, sort_keys=True)
        f.write('\n')
