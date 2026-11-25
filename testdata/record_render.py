"""Record what the original Kajiki prints for generated runtime cases: render.jsonl.gz.

Run with the original importable as kajiki (rec/py.sh from the task root mounts it):
    rec/py.sh gold/testdata python3 record_render.py

Each case is {"id", "files", "steps", "lines"}: templates made from strings, files and loaders, renders with
contexts, extract calls, and the lines printed for them (out, error/msg/at, extract/m, missing), as
rec/record.py prints them. Each case runs in its own process, in an empty run directory holding its files.
Cases whose output holds an object address are left out.
"""
import gzip
import io
import json
import os
import random
import re
import shutil
import tempfile
import traceback

import kajiki
import kajiki.i18n
import kajiki.template
from kajiki.template import KajikiSyntaxError, KajikiTemplateError

ADDRESS = re.compile(r' at 0x[0-9a-fA-F]+')
CLASSES = []
_from_ir = kajiki.template.from_ir


def _recording_from_ir(*args, **kwargs):
    tpl = _from_ir(*args, **kwargs)
    CLASSES.append(tpl)
    return tpl


kajiki.template.from_ir = _recording_from_ir


class Crash(Exception):
    pass


# The case builders of gen/common.py.

def F(x):
    return {'f': x if isinstance(x, str) else repr(float(x))}


def D(*pairs):
    return {'d': [list(p) for p in pairs]}


def LIT(text):
    return {'lit': text}


def _pairs(ctx):
    if ctx is None:
        return []
    if isinstance(ctx, dict):
        return [[k, v] for k, v in ctx.items()]
    return [list(p) for p in ctx]


def _opts(opts):
    return {k: (_pairs(v) if k == 'base_globals' else v) for k, v in opts.items() if v is not None}


def xml(name, src, **opts):
    return {'op': 'xml', 'name': name, 'src': src, 'opts': _opts(opts)}


def text(name, src, **opts):
    return {'op': 'text', 'name': name, 'src': src, 'opts': _opts(opts)}


def xml_file(name, **opts):
    return {'op': 'xml', 'name': name, 'opts': _opts(opts)}


def text_file(name, **opts):
    return {'op': 'text', 'name': name, 'opts': _opts(opts)}


def write(path, text_):
    return {'op': 'write', 'path': path, 'text': text_}


def file_bytes(path, data):
    return {'path': path, 'hex': data.hex()}


def mock(name, modules):
    return {'op': 'mock', 'name': name, 'modules': [[k, v] for k, v in modules.items()]}


def fileloader(name, path, **opts):
    return {'op': 'fileloader', 'name': name, 'path': list(path), 'opts': _opts(opts)}


def load(loader, path, name):
    return {'op': 'load', 'loader': loader, 'path': path, 'name': name}


def render(tpl, ctx=None):
    return {'op': 'render', 'tpl': tpl, 'ctx': _pairs(ctx)}


def extract(src, **opts):
    return {'op': 'extract', 'src': src, 'opts': _opts(opts)}


def file(path, text_):
    return {'path': path, 'text': text_}


def case(steps, files=None):
    return {'files': list(files or []), 'steps': list(steps)}


# The generator.

NS = 'xmlns:py="http://genshi.edgewall.org/"'
WORDS = ['alpha', 'Beta', 'g&mma', '<d>', '"q"', "it's", 'café', '', ' sp ', 'x\ny', 'TAB\t', ' nb']


def word(r):
    return r.choice(WORDS)


def scalar(r):
    k = r.randrange(8)
    if k == 0:
        return r.randint(-5, 50)
    if k == 1:
        return F(r.choice([0.5, -0.0, 1e16, 2.5, 1 / 3]))
    if k == 2:
        return None
    if k == 3:
        return r.choice([True, False])
    if k == 4:
        return [word(r), r.randint(0, 3)]
    if k == 5:
        return LIT(r.choice(['<b>x</b>', 'a & b', '']))
    return word(r)


def fam_exprs(r, i):
    tag = r.choice(['div', 'section', 'p'])
    mode = r.choice([None, 'xml', 'html', 'html5', 'xhtml'])
    src = (f'<{tag} {NS} class="${{cls}}" data-n="${{n}}" title="pre-${{v}}-post">\n'
           f'  <span py:content="v">x</span><b py:replace="w"/>\n'
           f'  ${{v}} ${{w.upper()}} $$ ${{n * 2 if n else "none"}}\n'
           f'  <i py:strip="flag">${{type(v).__name__}}</i><em py:attrs="extra" id="e">e</em>\n'
           f'  <br/><input type="checkbox" checked="${{flag}}" py:attrs="{{\'disabled\': flag, \'selected\': None}}"/>\n'
           f'</{tag}>')
    steps = [xml('t', src, mode=mode)]
    for _ in range(r.randint(1, 3)):
        steps.append(render('t', {'cls': scalar(r), 'n': r.choice([0, 1, 7, None]), 'v': scalar(r),
                                  'w': word(r), 'flag': r.choice([True, False, 0, 1]),
                                  'extra': r.choice([D(['b', 1], ['a', None]), [['z', 'q'], ['a', 2]], None,
                                                    D(['checked', True], ['x', '<y>']), [['a', 1], [2, 'b']]])}))
    return case(steps)


def fam_switch_with(r, i):
    src = (f'<ul {NS}>\n'
           f'  <py:switch test="kind">\n'
           f'    <py:case value="1"><li>one $kind</li></py:case>\n'
           f'    <py:case value="\'a\'"><li>letter</li></py:case>\n'
           f'    <py:else><li>other ${{repr(kind)}}</li></py:else>\n'
           f'  </py:switch>\n'
           f'  <li py:with="a = n * 2; b = a + 1" class="${{a}}">${{a}}-${{b}}</li>\n'
           f'  <py:with vars="{r.choice(["x = 1", "x=n;y=x==n", "p, q = n, n"])}"><li>${{x if defined("x") else "-"}}</li></py:with>\n'
           f'  <li>${{value_of("a", "no-a")}} ${{defined("kind")}}</li>\n'
           f'  <li py:for="k in range(n)" py:if="k % 2">k=$k</li>\n'
           f'  <li py:if="n &gt; 2">big</li><li py:else="">small</li>\n'
           f'</ul>')
    steps = [xml('t', src)]
    for _ in range(r.randint(1, 3)):
        steps.append(render('t', {'kind': r.choice([1, F(1.0), True, 'a', 2, None]), 'n': r.choice([0, 1, 3, 4])}))
    return case(steps)


def fam_defs(r, i):
    src = (f'<div {NS}>\n'
           f'  <py:def function="item(label, *, n=1, sep=\'-\')"><b>$label$sep$n</b></py:def>\n'
           f'  <py:def function="wrap(x)"><py:def function="inner(y=2)">[$x:$y]</py:def>${{inner()}}${{inner(5)}}</py:def>\n'
           f'  <p>${{item("a", n=3, sep=":")}}</p>\n'
           f'  <p>${{wrap(n)}}</p>\n'
           f'  <py:def function="calls(c)">${{c(1)}}|${{c(\'two\')}}</py:def><p py:call="calls(%caller)" args="z">C[$z]</p>\n'
           f'  <p>${{len(list(wrap(1)))}} ${{"".join(wrap(2))}}</p>\n'
           f'  <p py:if="mode == 1">${{item("k")}}</p>\n'
           f'  <p py:if="mode == 2">${{wrap(1) + "x"}}</p>\n'
           f'  <p py:if="mode == 3">${{len(wrap(1))}}</p>\n'
           f'</div>')
    steps = [xml('t', src, filename=r.choice(['defs.html', None]))]
    for _ in range(r.randint(1, 3)):
        steps.append(render('t', {'n': r.choice([0, 'x', F(2.5)]), 'mode': r.choice([0, 1, 2, 3])}))
    return case(steps)


def fam_inherit(r, i):
    parent = (f'<html {NS}>\n<head><title py:block="title">Parent ${{t}}</title></head>\n'
              f'<body>\n<py:block name="body"><p>parent body ${{helper()}}</p></py:block>\n'
              f'<py:def function="helper()">P-helper</py:def>\n'
              f'<p>${{local is self}} ${{defined("child")}}</p>\n</body>\n</html>')
    child = (f'<html {NS} py:extends="parent.html">\n'
             f'<py:block name="title">Child ${{t}} / ${{parent_block()}}</py:block>\n'
             f'<py:block name="body">{r.choice(["<p>child body</p>", "${parent_block()}<p>more</p>", "${helper()}"])}</py:block>\n'
             f'<py:def function="helper()">C-helper</py:def>\n'
             f'</html>')
    files = [file('parent.html', r.choice([parent, '<!DOCTYPE html>\n' + parent])), file('child.html', child),
             file('inc.html', f'<span {NS}>inc ${{t}} ${{repr(local.__class__.__name__)}}</span>'),
             file('lib.html', f'<div {NS}><py:def function="tag(x)"><b>$x</b></py:def></div>')]
    main = (f'<div {NS}>\n  <py:include href="inc.html"/>\n  <py:import href="lib.html" alias="L"/>\n'
            f'  ${{L.tag(t)}} <py:import href="lib.html"/>${{lib.tag(2)}} ${{inc is not None}}\n</div>')
    files.append(file('main.html', main))
    steps = [fileloader('fl', ['.', 'missing'], force_mode=r.choice([None, 'xml', 'html']), reload=r.random() < 0.3),
             load('fl', r.choice(['child.html', 'main.html', 'nope.html']), 't')]
    for _ in range(r.randint(1, 2)):
        steps.append(render('t', {'t': word(r)}))
    return case(steps, files)


def fam_mock(r, i):
    base = xml('b', f'<div {NS}><h1 py:block="head">H ${{x}}</h1><py:block name="foot">F</py:block></div>')
    child = xml('c', f'<py:extends href="base" {NS}><py:block name="head">C ${{x}}|${{parent_block()}}</py:block></py:extends>')
    inc = xml('i', f'<em {NS}>I ${{x}}</em>', is_fragment=True)
    user = xml('u', f'<p {NS}><py:include href="inc"/> <py:include href="{r.choice(["inc", "zzz"])}"/></p>')
    steps = [base, child, inc, user, mock('m', {'base': 'b', 'inc': 'i', 'child': 'c', 'user': 'u'})]
    steps.append(render(r.choice(['c', 'u']), {'x': scalar(r)}))
    steps.append(render('u', {'x': 1}))
    return case(steps)


def fam_text(r, i):
    src = r.choice([
        '%for i in range(n)\n${i}:${w}\\\n%end\nTail $$ ${w.upper()} $w.__class__.__name__\n',
        '{%if n > 1%}big{%else%}small{%end%} ${n}\n%def f(x)\n[$x]\n%end\n${f(n)}${f("<")}\n',
        '%switch n\n%case 1\none\n%case 2\ntwo\n%else\nother\n%end\n%py y = n * 3\nY=$y\n',
        '%py\nz = [k * 2 for k in range(n)]\n%end\n${z} {%- for k in z -%}${k},{%- end %}\n',
        '%call (a) wrap(%caller)\n<$a>\n%end\n%def wrap(c)\n${c(1)}${c(2)}\n%end\n',
        'line one\n  %if n\n${1/0 if n == 2 else n}\n%end\n${w + 1}\n',
    ])
    steps = [text('t', src, autoescape=r.random() < 0.5, filename=r.choice([None, 't.txt']))]
    for _ in range(r.randint(1, 3)):
        steps.append(render('t', {'n': r.choice([0, 1, 2, 3]), 'w': word(r)}))
    return case(steps)


def fam_errors(r, i):
    src = (f'<div {NS}>\n'
           f'  <?py %\ndef boom(v):\n    return 10 // v\n?>\n'
           f'  <py:def function="early(v)">\n    <i>${{boom(v)}}</i>\n  </py:def>\n'
           f'  <p>${{n}}</p>\n'
           f'  <p py:if="mode == 1">${{boom(n)}}</p>\n'
           f'  <p py:if="mode == 2">${{early(n)}}</p>\n'
           f'  <py:def function="outer()"><py:def function="inner()">${{boom(n)}}</py:def>${{inner()}}</py:def>\n'
           f'  <p py:if="mode == 3">${{outer()}}</p>\n'
           f'  <?py\nif mode == 4:\n    q = [1][n]\n?>\n'
           f'  <p py:if="mode == 5" title="${{boom(n)}}">t</p>\n'
           f'  <p py:if="mode == 6">${{undefined_name}}</p>\n'
           f'  <p py:for="k in n">${{k}}</p>\n'
           f'</div>')
    steps = [xml('t', src, filename=r.choice(['err.html', None]))]
    for _ in range(r.randint(1, 3)):
        steps.append(render('t', {'n': r.choice([0, 1, 5]), 'mode': r.randint(0, 6)}))
    return case(steps)


def fam_i18n(r, i):
    src = (f'<div {NS}>\n'
           f'  <?py %\ndef gettext(s):\n    return "[" + s.strip() + "]"\n?>\n'
           f'  <p title="not translated">Hello ${{name}}, welcome!</p>\n'
           f'  <span>  spaced text  </span><script>var a = "no";</script>\n'
           f'  <!-- comment --><b>${{_("x")}} ${{gettext("y")}}</b>\n'
           f'</div>')
    steps = [xml('t', src, strip_text=r.random() < 0.3,
                 base_globals=r.choice([None, {'gettext': None}]))]
    steps.append(render('t', {'name': word(r)}))
    steps.append(extract(src, strip_text=r.random() < 0.5, mode=r.choice([None, 'html'])))
    return case(steps)


def fam_files(r, i):
    body = (f'<div {NS}>\r\n  <p>café ${{x}}</p>\r\n  <py:for each="k in range(n)">[$k]\r\n</py:for>\r\n</div>')
    enc = r.choice([None, 'utf-8', 'latin-1', 'utf-16', 'cp1252', 'bogus'])
    data = body.encode(enc if enc not in (None, 'bogus') else 'utf-8')
    if r.random() < 0.2:
        data = data + b'\xc3'
    files = [file_bytes('t.html', data), file('t.txt', '%for k in range(n)\r\n$k:${x}\r\n%end\r\n')]
    steps = [xml_file('a', filename=r.choice(['t.html', 'missing.html', 'sub']), encoding=enc),
             text_file('b', filename=r.choice(['t.txt', 'nope.txt']), encoding=r.choice([None, 'ascii'])),
             fileloader('fl', ['.'], force_mode=r.choice([None, 'html5', 'text', 'xhtml'])),
             load('fl', r.choice(['t.html', 't.txt']), 'c')]
    for name in 'abc':
        steps.append(render(name, {'x': r.choice(['<&>', 'v', 1]), 'n': r.randint(0, 2)}))
    return case(steps, files + [file('sub/x.txt', 'x')])


def fam_loader_opts(r, i):
    files = [file('pages/main.html', f'<html {NS}><body><div>${{title}}</div><py:include href="part.html"/><p>${{g}}</p>\n<script>a &lt; b</script></body></html>'),
             file('pages/part.html', f'<section {NS}> part ${{title}} </section>'),
             file('pages/t.txt', '${title} %% ${g}\n'),
             file('pages/u.xml', f'<div {NS}><title>T</title><div>D</div></div>')]
    opts = dict(force_mode=r.choice([None, 'html', 'xml']), autoescape_text=r.random() < 0.5,
                xml_autoblocks=r.choice([None, ['div'], ['title', 'div']]), reload=r.random() < 0.5)
    if r.random() < 0.5:
        opts['is_fragment'] = r.random() < 0.5
    if r.random() < 0.4:
        opts['strip_text'] = r.random() < 0.5
    if r.random() < 0.4:
        opts['cdata_scripts'] = r.random() < 0.5
    if r.random() < 0.4:
        opts['base_globals'] = {'g': r.choice(['G1', 2])}
    steps = [fileloader('fl', ['pages', '.'], **opts), load('fl', r.choice(['main.html', 't.txt', 'u.xml']), 't'),
             render('t', {'title': r.choice(['<T>', 'x'])})]
    if opts['reload']:
        steps += [write('pages/part.html', f'<em {NS}>new ${{title}}</em>'), load('fl', 'main.html', 'u'),
                  render('u', {'title': 'z'})]
    return case(steps, files)


def fam_modes(r, i):
    mode = r.choice([None, 'xml', 'html', 'html5', 'HTML', 'xhtml', 'html4'])
    doctype = r.choice(['', '<!DOCTYPE html>\n', '<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">\n',
                        '<!-- <!DOCTYPE html> -->\n'])
    src = (doctype + f'<html {NS}><head><meta charset="${{cs}}"/><script/><style>p &gt; a {{}}</style>'
           f'<script>if (a &amp;&amp; ${{n}} &lt; 2) {{}}</script></head>\n'
           f'<body><br/><img src="${{cs}}"/><input checked="${{n}}" disabled="${{None}}"/><p/>'
           f'<![CDATA[ raw ${{n}} < ]]><textarea></textarea></body></html>')
    steps = [xml('t', src, mode=mode, is_fragment=r.random() < 0.2, cdata_scripts=r.choice([None, False]))]
    steps.append(render('t', {'cs': r.choice(['utf-8', '"q"']), 'n': r.choice([0, 1, True])}))
    return case(steps)


def fam_blocks(r, i):
    src = (f'<div {NS}>\n<header>H ${{x}}</header>\n<nav py:autoblock="{r.choice(["False", "True", "maybe"])}">N</nav>\n'
           f'<section>\n  text  \n  ${{x}}  more\n</section>\n<py:block name="extra">${{x * 2}}</py:block>\n</div>')
    steps = [xml('t', src, autoblocks=r.choice([None, ['header'], ['nav', 'section']]), strip_text=r.random() < 0.5)]
    steps.append(render('t', {'x': r.choice([1, 'ab', None])}))
    return case(steps)


def fam_syntax(r, i):
    bad = r.choice(['${x +}', '<?py if x ?>', '<p py:for="x in">a</p>', '<p py:if="x =">a</p>',
                    '<?py\n  a = 1\n b = 2\n?>', '<py:def function="f(a, a)">x</py:def>', '<p py:with="a=1;b">q</p>',
                    '<?py return 1 ?>', '<p>${yield 1}</p>', '<p py:attrs="{1:}">x</p>', '<?py % x = (?>'])
    src = f'<div {NS}>\n  <p>${{x}}</p>\n  {bad}\n</div>'
    return case([xml('t', src), render('t', {'x': 1})])


def fam_text_errors(r, i):
    src = r.choice([
        'a\n%for i in range(3)\n${10 // (i - n)}\n%end\nz\n',
        '%def f(v)\n${v[5]}\n%end\nline\n${f(s)}\n',
        'x ${s.upper() + n}\n',
        '%py\nq = {}\n%end\n${q[s]}\n',
        '{%if n%}${s}{%else%}${undefined}{%end%}\n',
        '$s $ bad\n',
        '%if n\nno end\n',
        '%include x.txt\n',
    ])
    steps = [text('t', src, filename=r.choice([None, 'e.txt']), autoescape=r.random() < 0.5)]
    for _ in range(2):
        steps.append(render('t', {'n': r.choice([0, 1, 2]), 's': r.choice(['abc', '<x>'])}))
    return case(steps)


def fam_i18n2(r, i):
    src = (f'<div {NS}>\n<p>One  ${{a}}  two</p>\n<p title="t">  Three &amp; four  </p>\n'
           f'<py:if test="a">Five</py:if><span py:strip="">Six</span>\n<!-- not this --></div>')
    steps = [xml('t', src, strip_text=r.random() < 0.5, base_globals=r.choice([None, {'k': 1}]))]
    steps.append(render('t', {'a': 1, 'gettext': None} if r.random() < 0.3 else {'a': 'A'}))
    steps.append(extract(src, strip_text=r.random() < 0.5, mode=r.choice([None, 'html', 'xml']),
                         is_fragment=r.random() < 0.5))
    steps.append(extract(r.choice(['<a>&bogus;</a>', '<a><b></a>', '<a>${x</a>', '<a>x</a><b/>'])))
    return case(steps)



FAMILIES = [fam_exprs, fam_switch_with, fam_defs, fam_inherit, fam_mock, fam_text, fam_errors, fam_i18n, fam_files,
            fam_loader_opts, fam_modes, fam_blocks, fam_syntax, fam_text_errors, fam_i18n2]



# The recorder (rec/record.py's printing).

def value(v):
    """A context value from its JSON description (see gen/common.py)."""
    if v is None or isinstance(v, (bool, str, int)):
        return v
    if isinstance(v, list):
        return [value(x) for x in v]
    if not isinstance(v, dict) or len(v) != 1:
        raise Crash(f'not a value description: {v!r}'[:200])
    (kind, x), = v.items()
    if kind == 'f':
        return float(x)
    if kind == 'i':
        return int(x)
    if kind == 't':
        return tuple(value(i) for i in x)
    if kind == 'd':
        return dict(zip([value(k) for k, _ in x], [value(i) for _, i in x]))
    if kind == 's':
        return set(value(i) for i in x)
    if kind == 'b':
        return bytes.fromhex(x)
    if kind == 'lit':
        return kajiki.util.literal(x)
    raise Crash(f'unknown value kind {kind!r}')


def variables(pairs):
    return dict(zip([n for n, _ in pairs], [value(v) for _, v in pairs]))


def dumps(s):
    return json.dumps(s, ensure_ascii=True)


def rewritten_codes():
    codes = []
    for tpl in CLASSES:
        for _, meth in tpl.__methods__:
            codes.append(meth._func.__code__)
    return codes


class Case:
    def __init__(self, case):
        self.case = case
        self.lines = []
        self.templates = {}
        self.loaders = {}

    def p(self, s):
        self.lines.append(s)

    def error(self, e):
        self.p('error ' + type(e).__name__)
        if isinstance(e, KajikiSyntaxError):
            cause = e.__cause__
            if not isinstance(cause, SyntaxError):
                raise Crash('KajikiSyntaxError without a SyntaxError cause')
            if GENERATED_LINE.search(cause.msg):
                raise Crash('a SyntaxError message naming a generated line')
            self.p('msg ' + dumps(cause.msg))
            return
        self.p('msg ' + dumps(str(e)))
        if isinstance(e, KajikiTemplateError):
            self.p(f'at {dumps(e.filename)} {e.linenum} {e.colnum}')
            return
        codes = rewritten_codes()
        where = ('', 0)
        for frame, lineno in traceback.walk_tb(e.__traceback__):
            if any(frame.f_code is c for c in codes):
                where = (frame.f_code.co_filename, lineno if lineno is not None else 0)
        self.p(f'at {dumps(where[0])} {where[1]} 0')

    def run(self):
        for step in self.case['steps']:
            getattr(self, 'op_' + step['op'])(step)

    def op_xml(self, s):
        o = s.get('opts', {})
        kw = {}
        if 'filename' in o:
            kw['filename'] = o['filename']
        if 'mode' in o:
            kw['mode'] = o['mode']
        if o.get('is_fragment'):
            kw['is_fragment'] = True
        if 'encoding' in o:
            kw['encoding'] = o['encoding']
        if 'autoblocks' in o:
            kw['autoblocks'] = list(o['autoblocks'])
        if o.get('cdata_scripts') is False:
            kw['cdata_scripts'] = False
        if o.get('strip_text'):
            kw['strip_text'] = True
        if 'base_globals' in o:
            kw['base_globals'] = variables(o['base_globals'])
        try:
            if 'src' in s:
                tpl = kajiki.XMLTemplate(source=s['src'], **kw)
            else:
                tpl = kajiki.XMLTemplate(source=None, **kw)
        except Exception as e:
            self.error(e)
            return
        self.templates[s['name']] = tpl

    def op_text(self, s):
        o = s.get('opts', {})
        kw = {}
        if 'filename' in o:
            kw['filename'] = o['filename']
        if o.get('autoescape'):
            kw['autoescape'] = True
        if 'encoding' in o:
            kw['encoding'] = o['encoding']
        try:
            if 'src' in s:
                tpl = kajiki.TextTemplate(source=s['src'], **kw)
            else:
                tpl = kajiki.TextTemplate(source=None, **kw)
        except Exception as e:
            self.error(e)
            return
        self.templates[s['name']] = tpl

    def op_mock(self, s):
        modules = {}
        for path, name in s['modules']:
            if name not in self.templates:
                self.p('missing ' + name)
                return
            modules[path] = self.templates[name]
        self.loaders[s['name']] = kajiki.MockLoader(modules)

    def op_fileloader(self, s):
        o = s.get('opts', {})
        kw = {}
        if o.get('reload'):
            kw['reload'] = True
        if 'force_mode' in o:
            kw['force_mode'] = o['force_mode']
        if o.get('autoescape_text'):
            kw['autoescape_text'] = True
        if 'xml_autoblocks' in o:
            kw['xml_autoblocks'] = list(o['xml_autoblocks'])
        for key in ('is_fragment', 'strip_text', 'cdata_scripts'):
            if key in o:
                kw[key] = bool(o[key])
        if 'base_globals' in o:
            kw['base_globals'] = variables(o['base_globals'])
        self.loaders[s['name']] = kajiki.FileLoader(list(s['path']), **kw)

    def op_load(self, s):
        loader = self.loaders.get(s['loader'])
        if loader is None:
            self.p('missing ' + s['loader'])
            return
        try:
            tpl = loader.import_(s['path'])
        except Exception as e:
            self.error(e)
            return
        self.templates[s['name']] = tpl

    def op_render(self, s):
        tpl = self.templates.get(s['tpl'])
        if tpl is None:
            self.p('missing ' + s['tpl'])
            return
        try:
            out = tpl(variables(s.get('ctx', []))).render()
        except Exception as e:
            self.error(e)
            return
        self.p('out ' + dumps(out))

    def op_extract(self, s):
        o = s.get('opts', {})
        options = {}
        if o.get('strip_text'):
            options['strip_text'] = True
        if o.get('mode'):
            options['mode'] = o['mode']
        if o.get('is_fragment'):
            options['is_fragment'] = True
        try:
            messages = list(kajiki.i18n.extract(io.BytesIO(s['src'].encode('utf-8')), ['_'], [], options))
        except Exception as e:
            self.error(e)
            return
        self.p(f'extract {len(messages)}')
        for lineno, funcname, message, comments in messages:
            self.p(f'm {lineno} {dumps(funcname)} {dumps(message)} {dumps(list(comments))}')

    def op_write(self, s):
        path = s['path']
        if os.path.dirname(path):
            os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, 'w', encoding='utf-8', newline='') as f:
            f.write(s['text'])


def record(c, run):
    for f in c['files']:
        path = os.path.join(run, f['path'])
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, 'wb') as fh:
            fh.write(bytes.fromhex(f['hex']) if 'hex' in f else f['text'].encode('utf-8'))
    r, w = os.pipe()
    pid = os.fork()
    if pid == 0:
        os.close(r)
        os.chdir(run)
        rc = 0
        k = Case(c)
        try:
            k.run()
            if any(ADDRESS.search(line) for line in k.lines):
                rc = 'address'
        except BaseException as e:
            rc = 'crash: %s' % type(e).__name__
        with os.fdopen(w, 'w') as fh:
            json.dump({'rc': rc, 'lines': k.lines}, fh)
        os._exit(0)
    os.close(w)
    with os.fdopen(r) as fh:
        data = json.load(fh)
    os.waitpid(pid, 0)
    return data


def main():
    with gzip.open('render.jsonl.gz', 'wt', encoding='utf-8') as out:
        for fam in FAMILIES:
            g = 'kj_' + fam.__name__[4:]
            for i in range(40):
                c = fam(random.Random('gold-kj-render-%s-%d' % (g, i)), i)
                run = tempfile.mkdtemp()
                try:
                    row = record(c, run)
                finally:
                    shutil.rmtree(run)
                if row['rc'] != 0:
                    continue
                out.write(json.dumps({'id': '%s_%04d' % (g, i), 'files': c['files'], 'steps': c['steps'],
                                      'lines': row['lines']}, ensure_ascii=True) + '\n')


main()
