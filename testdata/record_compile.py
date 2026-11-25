"""Record the compile phase of the original Kajiki for generated templates: compile.jsonl.gz.

Run with the original importable as kajiki (rec/py.sh from the task root mounts it):
    rec/py.sh gold/testdata python3 record_compile.py

Each line is {"id", "kind", "src", "opts", "result"}: an XML or text template from the seeded generators below,
the options it is compiled with, and either {"ok": {"py", "lines", "syntax"}} (the generated module, the
py_linenos map from_ir builds, and the SyntaxError class and msg compiling it gives, or null) or {"error": {...}}
(the class and str() of the exception, with filename, linenum and colnum for KajikiTemplateError).
"""
import gzip
import json
import random

from kajiki import ir, text, xml_template as xt
from kajiki.template import KajikiTemplateError

TAGS = ['div', 'span', 'p', 'ul', 'li', 'a', 'em', 'table', 'tr', 'td', 'section', 'b', 'h2', 'label',
        'form', 'option', 'select', 'title', 'head', 'body', 'html', 'pre', 'dl', 'dt', 'dd']
VOID = ['br', 'img', 'input', 'hr', 'meta', 'link', 'col', 'area', 'base', 'param']
WORDS = ['hello', 'world', 'price', 'Total:', 'item', 'café', 'naïve', 'x', 'A&amp;B', 'a &lt; b',
         ' spaced ', 'tab\tbed', '50%', 'q?', '中文', 'O\'Neil', '"quoted"', '#hash', '{brace}',
         'semi;colon', 'end.', ' em', '-->']
NAMES = ['x', 'name', 'items', 'user', 'n', 'total', 'row', 'i', 'title', 'data', 'flag', 'val', 'k', 'v']
SPACES = [' ', '\n', '\n  ', '\n    ', '  ', '\t', '\r\n', '\n\n', ' ', '　', ' \n ', '']

EXPRS = [
    'x', 'name.upper()', 'items[0]', 'user.name', 'n + 1', 'total * 2', "'%s!' % name", 'len(items)',
    "{'a': 1}['a']", "'}'", "'{x}'.format(x=1)", "f'{n:>5}'", "f'{name!r}'", 'lambda: 1', '[i for i in items]',
    '(1, 2)', "{'k': {'n': 1}}", 'n if flag else 0', "'a' 'b'", 'a.b.c', 'x[1:2]', '-n', 'not flag',
    "\"it's\"", "'\\\\'", "'}' + '{'", "{1, 2}", "dict(a=1)", 'n &gt; 1', 'n &lt; 2', 'a &amp; b',
    'n == 1', '(\n  n + 1\n)', "f'{{x}}'", 'x  ', ' x', '[\n1,\n2]', 'v or k',
]
BAD_EXPRS = ['x +', "'abc", '(', 'x y', '', 'yield 1', '1 +* 2', 'a b c', "f'{'", 'def', '"', '[1, 2']

PIS = [
    'x = 1', '% import math', '% counter = 0', 'total = sum(items)', '#\n    a = 1\n    b = 2',
    'if flag:\n    y = 1\nelse:\n    y = 2', 'for i in range(3):\n  pass', '% def helper(v):\n    return v * 2',
    'x = """a\nb"""', '  n = 3', '#\n  a = 1\n b = 2', '', '%', 'import string', '# comment only',
    'x = {"a": "}"}', 'name = "<b>"', '%x = 1\ny = 2',
]


class XMLGen:
    def __init__(self, r):
        self.r = r

    def ws(self):
        return self.r.choice(SPACES)

    def expr(self):
        if self.r.random() < 0.06:
            return self.r.choice(BAD_EXPRS)
        return self.r.choice(EXPRS)

    def text(self):
        r = self.r
        parts = []
        for _ in range(r.randint(1, 4)):
            k = r.random()
            if k < 0.35:
                parts.append(r.choice(WORDS))
            elif k < 0.55:
                parts.append('${' + self.expr() + '}')
            elif k < 0.65:
                parts.append('$' + r.choice(NAMES) + r.choice(['', '.attr', '.a.b', '.', '_x']))
            elif k < 0.7:
                parts.append(r.choice(['$$', '$', '$ ', '$5', '$${x}', '${', '$$$x']))
            elif k < 0.8:
                parts.append(r.choice(['&nbsp;', '&amp;', '&lt;', '&gt;', '&#160;', '&#x41;', '&copy;', '&quot;',
                                       '&apos;', '&hellip;', '&nbsp', '&rarr;', '&#10;', '&#13;']))
            elif k < 0.82:
                parts.append(r.choice(['&bogus;', '&#0;', '&nosuchentity;']))
            else:
                parts.append(self.ws())
            parts.append(r.choice(['', ' ', '', '\n']))
        return ''.join(parts)

    def attr_value(self):
        r = self.r
        k = r.random()
        if k < 0.4:
            return r.choice(WORDS + ['', 'a b', '1', 'x\ty', 'a\nb', '&#10;', '&nbsp;', '&quot;q&quot;'])
        if k < 0.7:
            return r.choice(['', 'pre-']) + '${' + self.expr() + '}' + r.choice(['', ' post'])
        return '$' + r.choice(NAMES) + r.choice(['', ' and $n', '$$'])

    def attrs(self, extra=None):
        r = self.r
        names = r.sample(['class', 'id', 'title', 'href', 'name', 'value', 'checked', 'selected', 'data-x',
                          'style', 'alt', 'z', 'A', 'é', 'xmlns:py', 'for', 'if', 'content', 'attrs', 'strip',
                          'xml:lang', 'lang', 'a:b', 'c:b', 'py', 'case', 'each'], r.randint(0, 4))
        out = []
        for n in names:
            if n == 'xmlns:py':
                out.append('xmlns:py="http://genshi.edgewall.org/"')
            else:
                out.append('%s="%s"' % (n, self.attr_value().replace('"', '&quot;').replace('<', '&lt;')))
        if extra:
            out.extend(extra)
        r.shuffle(out)
        return out

    def open_tag(self, tag, attrs):
        return '<' + tag + ''.join(' ' + a for a in attrs)

    def element(self, depth, tag=None, extra=None):
        r = self.r
        tag = tag or r.choice(TAGS + VOID[:3])
        attrs = self.attrs(extra)
        k = r.random()
        if k < 0.12:
            attrs.append('py:attrs="%s"' % r.choice(["{'a': x}", 'extra', "[('b', 1)]", 'None', "dict(c=n)"]))
        elif k < 0.2:
            attrs.append('py:content="%s"' % r.choice(['x', 'name.title()', "''", 'None', 'n * 2']))
        elif k < 0.28:
            attrs.append('py:strip="%s"' % r.choice(['', 'flag', 'not flag', 'True', 'n &gt; 1']))
        if tag in VOID and r.random() < 0.5:
            return self.open_tag(tag, attrs) + r.choice(['/>', ' />', '></' + tag + '>'])
        return self.open_tag(tag, attrs) + '>' + self.content(depth + 1) + '</' + tag + '>'

    def directive_attr(self):
        r = self.r
        return r.choice([
            'py:if="%s"' % r.choice(['flag', 'n &gt; 0', 'x', 'items', 'not flag']),
            'py:for="%s"' % r.choice(['i in items', 'k, v in data.items()', 'i in range(3)', '(a, b) in pairs']),
            'py:with="%s"' % r.choice(['a=1', 'a=1;b=a+1', 'x = n * 2', 'a = 1\nb = 2', 's=\'=\'', 'a==b']),
            'py:replace="%s"' % r.choice(['x', 'name', 'None']),
            'py:strip=""',
            'py:def="%s"' % r.choice(['f()', 'g(a, b=1)', 'h(*args, **kw)']),
            'py:block="%s"' % r.choice(['body', 'header', 'b1']),
            'py:switch="%s"' % r.choice(['n', 'x']),
            'py:case="%s"' % r.choice(['1', "'a'", 'n']),
            'py:else=""',
            'py:call="%s"' % r.choice(['f(%caller)', 'g(1, %caller)']),
        ])

    def directive_tag(self, depth):
        r = self.r
        k = r.randrange(12)
        inner = lambda: self.content(depth + 1)
        if k == 0:
            s = '<py:if test="%s">%s</py:if>' % (r.choice(['flag', 'x', 'n == 1']), inner())
            if r.random() < 0.6:
                s += r.choice(['', '', ' ', '\n']) + '<py:else>%s</py:else>' % inner()
            return s
        if k == 1:
            return '<py:for each="%s">%s</py:for>' % (r.choice(['i in items', 'k in data', 'x in range(n)']), inner())
        if k == 2:
            cases = ''.join(r.choice(['', '\n  ', ' ']) + '<py:case value="%s">%s</py:case>' % (r.choice(['1', '2', "'a'"]), inner())
                            for _ in range(r.randint(0, 3)))
            if r.random() < 0.5:
                cases += '<py:else>%s</py:else>' % inner()
            if r.random() < 0.15:
                cases += r.choice(['text', '<b>x</b>', '${x}'])
            return '<py:switch test="%s">%s%s</py:switch>' % (r.choice(['n', 'x']), cases, r.choice(['', '\n']))
        if k == 3:
            return '<py:with vars="%s">%s</py:with>' % (r.choice(['a=1', 'a=1;b=2', 'x=n\ny=2']), inner())
        if k == 4:
            return '<py:def function="%s">%s</py:def>' % (r.choice(['f()', 'g(a)', 'item(x, y=2)']), inner())
        if k == 5:
            return '<py:call function="%s" args="%s">%s</py:call>' % (r.choice(['f(%caller)', 'g(%caller)']),
                                                                     r.choice(['', 'a', 'a, b']), inner())
        if k == 6:
            return '<py:replace value="%s"/>' % r.choice(['x', 'n + 1'])
        if k == 7:
            return '<py:block name="%s">%s</py:block>' % (r.choice(['content', 'nav']), inner())
        if k == 8:
            return '<py:match on="%s">%s</py:match>' % (r.choice(['x', 'n']), ''.join(
                r.choice(['', '\n']) + '<py:case match="%s">%s</py:case>' % (r.choice(['1', '[a, b]', '_', "{'k': v}", 'str() as s']), inner())
                for _ in range(r.randint(0, 3))))
        if k == 9:
            return '<py:include href="%s"/>' % r.choice(['inc.xml', 'a/b.html'])
        if k == 10:
            alias = r.choice(['', ' alias="lib"'])
            return '<py:import href="%s"%s/>' % (r.choice(['lib.xml', 'dir/util.html', 'x.y.z', '.hidden']), alias)
        return '<py:nop>%s</py:nop>' % inner()

    def content(self, depth):
        r = self.r
        if depth > 4:
            return self.text() if r.random() < 0.7 else ''
        parts = []
        for _ in range(r.randint(0, 5)):
            k = r.random()
            if k < 0.3:
                parts.append(self.text())
            elif k < 0.55:
                extra = [self.directive_attr() for _ in range(r.choice([0, 0, 1, 1, 2]))]
                extra = list(dict.fromkeys(extra))
                parts.append(self.element(depth, extra=extra))
            elif k < 0.7:
                parts.append(self.directive_tag(depth))
            elif k < 0.76:
                parts.append('<!--%s-->' % r.choice([' note ', '! hidden ', 'x', ' ${x} ', ' <!DOCTYPE html> ']))
            elif k < 0.85:
                parts.append('<?%s %s?>' % (r.choice(['py', 'python', 'py']), r.choice(PIS)))
            elif k < 0.9:
                parts.append('<![CDATA[%s]]>' % r.choice(['a < b', ' ${x} ', '\n', '&amp;', '']))
            elif k < 0.95:
                tag = r.choice(['script', 'style'])
                body = r.choice(['var a = 1 &lt; 2;', 'if (a &amp;&amp; b) {}', '${x}', '<![CDATA[ a < b ]]>',
                                 '', ' x &gt; ${n} ', '<!-- c -->', 'a\nb'])
                parts.append('<%s>%s</%s>' % (tag, body, tag))
            else:
                parts.append(self.ws())
        return ''.join(parts)

    def document(self):
        r = self.r
        prolog = ''
        if r.random() < 0.08:
            prolog += r.choice(['<?xml version="1.0"?>', '<?xml version="1.0" encoding="utf-8"?>', ' <?xml version="1.0"?>',
                                '<?xml version="1.0" standalone="yes"?>'])
            prolog += r.choice(['', '\n'])
        if r.random() < 0.35:
            prolog += r.choice([
                '<!DOCTYPE html>', '<!DOCTYPE HTML>', '<!doctype html>', '<!DOCTYPE  html >',
                '<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">',
                '<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN"\n  "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">',
                '<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">',
                '<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML+RDFa 1.0//EN" "http://www.w3.org/MarkUp/DTD/xhtml-rdfa-1.dtd">',
                '<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">',
                '<!DOCTYPE foo SYSTEM "foo.dtd">', '<!DOCTYPE html [<!ENTITY e "v">]>',
            ])
            prolog += r.choice(['', '\n', '\n\n'])
        if r.random() < 0.1:
            prolog += '<!-- %s -->' % r.choice(['lead', '<!DOCTYPE html>']) + r.choice(['', '\n'])
        root_tag = r.choice(TAGS + ['py:nop', 'html'])
        extra = ['xmlns:py="http://genshi.edgewall.org/"'] if r.random() < 0.6 else []
        if r.random() < 0.15:
            extra.append(self.directive_attr())
        if r.random() < 0.08:
            extra.append('py:extends="%s"' % r.choice(['base.xml', 'layout.html']))
        if root_tag.startswith('py:'):
            root = '<%s>%s</%s>' % (root_tag, self.content(1), root_tag)
        else:
            root = self.element(0, tag=root_tag, extra=extra)
        doc = prolog + root + r.choice(['', '\n', '\n<!-- tail -->', '\n\n'])
        if r.random() < 0.08:
            i = r.randrange(len(doc) + 1)
            doc = doc[:i] + r.choice(['<', '&', '</x>', '<y>', '"', '>', ']]>', '\x01']) + doc[i:]
        return doc

    def quirky(self):
        r = self.r
        k = r.randrange(14)
        if k == 0:  # comment before the root: no directive expansion, no merging
            return '<!-- lead -->' + self.element(0, tag=r.choice(TAGS), extra=[self.directive_attr()])
        if k == 1:
            return '<!-- x --><div><py:call function="f(%%caller)" args="a">%s<b>x</b></py:call></div>' % r.choice(['', ' ', 'text'])
        if k == 2:
            return '<div>%s</div>' % r.choice(['<py:text>x</py:text>', '<py:comment/>', '<py:pi/>', '<py:x:if test="a">y</py:x:if>',
                                              '<py:foo>z</py:foo>', '<py:xml a="1">q</py:xml>', '<py:>e</py:>'])
        if k == 3:
            return '<div>${"$x" + y} and ${\'$$\'} $a.b. ${{"k": "$v"}["k"]}</div>'
        if k == 4:
            return '<div>\r\n  <?py #\r\n    a = 1\r\n    b = 2 ?>\x0c x \u2028y\u0085 <?py\tx=1\x0cy=2?></div>'
        if k == 5:
            return ('<html py:extends="base.xml"><py:block name="a">A ${x}</py:block><p py:block="b">B</p>'
                    '<py:def function="f(x)"><py:def function="g()">inner $x</py:def>${g()}</py:def></html>')
        if k == 6:
            return '<div py:with="%s">$a</div>' % r.choice(['a=1;b=2', ' a = 1 ; b=a==1', 'a=1\nb=2', 'x;a=1', '=1',
                                                          'a=(1;2)', 'a, b = 1, 2', 'a=1;', ';a=2', 'a ==1', 'a\t=\t1'])
        if k == 7:
            return '<ul><li py:for="i in x" py:if="i" py:attrs="{1: 2}" py:strip="i" py:content="i" class="c">t</li></ul>'
        if k == 8:
            return '<py:switch test="x"><py:case value="1">a</py:case>\n<py:else>b</py:else></py:switch>'
        if k == 9:
            return '<div py:switch="x"><span py:case="1">a</span><span py:else="">b</span></div>'
        if k == 10:
            return '<py:match on="x"><py:case match="1">a</py:case><li py:case="2">b</li></py:match>'
        if k == 11:
            return '<div><script>${a &amp;&amp; b} &lt;x&gt; &amp;nbsp; &#38;lt;</script><style>p &gt; a {}</style></div>'
        if k == 12:
            return '<div>&nbsp;x&nbsp; \u00a0y\u3000 <![CDATA[ z ]]> <b/> w&#32;</div>'
        return '<div py:strip="%s" py:attrs="%s" id="$x">%s</div>' % (r.choice(['', 'x', '1 if a else 0']),
                                                                    r.choice(['a', "{'b': None}"]), self.text())

    def options(self):
        r = self.r
        o = {}
        if r.random() < 0.4:
            o['mode'] = r.choice(['xml', 'html', 'html5'])
        if r.random() < 0.15:
            o['is_fragment'] = True
        if r.random() < 0.15:
            o['autoblocks'] = r.sample(['div', 'p', 'span', 'body', 'title', 'li'], r.randint(1, 2))
        if r.random() < 0.15:
            o['cdata_scripts'] = False
        if r.random() < 0.2:
            o['strip_text'] = True
        if r.random() < 0.2:
            o['filename'] = r.choice(['page.html', 'dir/x.xml', 't.kajiki'])
        return o


TEXT_WORDS = ['Hello', 'world', 'total:', 'x', 'a b', '50%', '{brace}', '}', '{', 'q?', 'caf\u00e9', '\t', '  ', '#',
         'line\\', 'esc\\n', '\\$', 'back\\\\slash', 'a\\\\', '%', '%%', '% x', '{%', '%}', '"q"', "'s'"]
TEXT_EXPRS = ['x', 'n + 1', "'}'", "{'a': 1}['a']", "f'{n}'", "f'{n:{w}}'", 'items[0]', '[\n  1,\n  2\n]',
         '(a,\n b)', "'''x\ny'''", 'len(items)', '{1: {2: 3}}', 'x if y else z', '# c\n x', "'a' # c",
         '"{"', "f'{{'", 'lambda: {}', '1 +', "'abc", '(', 'a b', '\\\n x', '', ' x ', 'x\n', "b'{'",
         '[i for i in {1, 2}]', "u'x'", "rb'\\d'", '0x1F', '1e3j', '...', 'not x', 'a.b().c']
TEXT_TAGS = ['for', 'if', 'else', 'switch', 'case', 'def', 'call', 'py', 'include', 'import', 'extends', 'block',
        'end', 'FOR', 'Py', 'unknown', 'while', 'body', 'expr']


class TextGen:
    def __init__(self, r):
        self.r = r

    def text(self):
        r = self.r
        out = []
        for _ in range(r.randint(1, 4)):
            k = r.random()
            if k < 0.4:
                out.append(r.choice(TEXT_WORDS))
            elif k < 0.6:
                out.append('${' + r.choice(TEXT_EXPRS) + '}')
            elif k < 0.7:
                out.append('$' + r.choice(['x', 'a.b', 'n_1', 'Y', 'a.', '_']))
            elif k < 0.75:
                out.append(r.choice(['$$', '$', '$ ', '$1', '$$x', '${', '$}']))
            else:
                out.append(r.choice([' ', '\n', '\r\n', '\t', '\n\n']))
        return ''.join(out)

    def tag(self, name, body, bare=None):
        r = self.r
        if bare is None:
            bare = r.random() < 0.6
        if bare:
            return r.choice(['', '  ', '\t', '\n']) + '%' + name + body + '\n'
        return r.choice(['{%', '{%-']) + name + body + r.choice(['%}', '-%}', ' -%}']) + r.choice(['', ' ', '\t ', '\n'])

    def block(self, depth):
        r = self.r
        if depth > 3:
            return self.text()
        parts = []
        for _ in range(r.randint(1, 5)):
            k = r.random()
            if k < 0.4:
                parts.append(self.text())
            elif k < 0.5:
                parts.append(self.tag('for', r.choice([' i in items', ' k, v in d.items()', 'x in y', ' (a, b) in p'])) +
                             self.block(depth + 1) + self.tag('end', ''))
            elif k < 0.6:
                s = self.tag('if', r.choice([' x', ' n > 1', 'flag', ' a == {']) ) + self.block(depth + 1)
                if r.random() < 0.5:
                    s += self.tag('else', '') + self.block(depth + 1)
                parts.append(s + self.tag('end', ''))
            elif k < 0.66:
                s = self.tag('switch', ' n')
                for _ in range(r.randint(0, 3)):
                    s += self.tag('case', r.choice([' 1', ' 2', " 'a'"])) + self.block(depth + 1)
                if r.random() < 0.5:
                    s += self.tag('else', '') + self.block(depth + 1)
                parts.append(s + self.tag('end', ''))
            elif k < 0.72:
                parts.append(self.tag('def', r.choice([' f()', ' g(a, b=1)', ' h(*a)'])) + self.block(depth + 1) +
                             self.tag('end', ''))
            elif k < 0.76:
                parts.append(self.tag('call', r.choice([' (x) f(%caller)', ' () g(1, %caller)', ' f(%caller)', '(a, b)h(%caller)'])) +
                             self.block(depth + 1) + self.tag('end', ''))
            elif k < 0.84:
                if r.random() < 0.5:
                    parts.append(self.tag('py', r.choice([' x = 1', ' % y = 2', ' import math', '  z = [1,\\\n2]', ' %'])))
                else:
                    parts.append(self.tag('py', '') + r.choice(['x = 1\n', '%import string\n', 'if a:\n    b = 1\n',
                                                                '  a = 1\n  b = 2\n', 'x = $y\n', '#\n    q = 1\n', '']) +
                                 self.tag('end', ''))
            elif k < 0.88:
                parts.append(self.tag(r.choice(['include', 'import', 'extends']),
                                      r.choice([' "a.txt"', " b.txt", ' c.txt as lib', ' "d e.txt"', '', ' a b',
                                                " 'unclosed", ' x as', ' x of y', ' e\\\\f.txt'])))
            elif k < 0.92:
                parts.append(self.tag('block', r.choice([' body', ' head', ' b1 '])) + self.block(depth + 1) +
                             self.tag('end', ''))
            elif k < 0.96:
                parts.append(self.tag(r.choice(TEXT_TAGS), r.choice(['', ' x', ' a b'])))
            else:
                parts.append(r.choice(['\n%\n', '% not a tag\n', '%5\n', '{%%}', '{% x %}', '\\\n', '\\', '\n\n%for x in y\n$x\n%end\n']))
        return ''.join(parts)


def compile_case(case):
    o = case['opts']
    filename = o.get('filename', '<string>')
    if case['kind'] == 'xml':
        doc = xt._Parser(filename, case['src']).parse()
        doc = xt._DomTransformer(doc, strip_text=o.get('strip_text', False)).transform()
        tree = xt._Compiler(filename, doc, mode=o.get('mode'), is_fragment=o.get('is_fragment', False),
                            autoblocks=o.get('autoblocks'), cdata_scripts=o.get('cdata_scripts', True)).compile()
    else:
        tree = text._Parser(text._Scanner(filename, case['src']), o.get('autoescape', False)).parse()
        tree.filename = filename
    py_lines = list(ir.generate_python(tree))
    py_text = '\n'.join(map(str, py_lines))
    linenos = []
    last, n = 0, 1
    for line in py_lines:
        lno = max(last, line._lineno or 0)
        for _ in range(str(line).count('\n') + 1):
            linenos.append([n, lno])
            n += 1
        last = lno
    syntax = None
    try:
        compile(py_text, '<string>', 'exec')
    except SyntaxError as e:
        syntax = [type(e).__name__, e.msg]
    return {'py': py_text, 'lines': linenos, 'syntax': syntax}


def main():
    cases = []
    for i in range(2500):
        r = random.Random(31 * 100003 + i)
        g = XMLGen(r)
        src = g.quirky() if r.random() < 0.25 else g.document()
        opts = g.options()
        if 'autoblocks' in opts and r.random() < 0.5:
            src = src.replace('<div', '<div py:autoblock="%s"' % r.choice(['False', 'true', 'maybe']), 1)
        cases.append({'id': 'x%d' % i, 'kind': 'xml', 'src': src, 'opts': opts})
    for i in range(1500):
        r = random.Random(31 * 1000003 + i)
        src = TextGen(r).block(0)
        opts = {}
        if r.random() < 0.3:
            opts['autoescape'] = True
        if r.random() < 0.2:
            opts['filename'] = r.choice(['t.txt', 'dir/x.txt'])
        cases.append({'id': 't%d' % i, 'kind': 'text', 'src': src, 'opts': opts})
    with gzip.open('compile.jsonl.gz', 'wt', encoding='utf-8') as out:
        for case in cases:
            try:
                res = {'ok': compile_case(case)}
            except RecursionError:
                continue
            except Exception as e:
                err = {'class': type(e).__name__, 'str': str(e)}
                if isinstance(e, KajikiTemplateError):
                    err.update(filename=e.filename, linenum=e.linenum, colnum=e.colnum)
                res = {'error': err}
            case['result'] = res
            out.write(json.dumps(case, ensure_ascii=True) + '\n')


main()
