"""Record what pyexpat reports for generated documents, parsed as xml.sax's
expat reader drives it without namespace processing.

Run with CPython 3.12.15 from this directory:
    python3 record.py > /dev/null && ls -l expat.jsonl.gz

Each line of expat.jsonl.gz is {"src", "chunks", "log", "result"}: the
document, the sizes in characters of the str pieces it is fed in, the
handler calls with the position each reports, and the outcome ("ok", the
ExpatError's code, line and column, or a handler's exception).
"""

import gzip
import json
import random
from xml.parsers import expat

NAMES = ['a', 'div', 'py:if', 'py:for', 'x-y', 'h1', 'p', 'xmlns:py', '_k', 'b.c', 'ns:tag:x', '\u00e9l', '\u4e2d']
TEXTS = ['hello', ' ', '\n', '  \n\t', '${x}', '$y', 'a &amp; b', '&lt;&gt;', '&nbsp;', '&copy;x', '&#65;',
         '&#x1F600;', '\r\n', '\r', 'caf\u00e9', '\u2028', '\u00a0', ']]', '>', '\U0001F600', '\ufeff',
         'a\r\n\r\nb', '&quot;&apos;']
BAD_TEXTS = ['&bogus;', '&#0;', ']]>', '&amp', '&', '<', '\x0b']
ATTRS = ['a="1"', "b='2'", 'c="x &amp; y"', 'd="&nbsp;"', 'e="\n\t v "', 'f="&#10;"', 'py:attrs="x"',
         'g="&undefined;"', 'i="${a}"', 'j="&#x20;&#32;"', "k='\"'", 'l="\r\nq"', 'm="a > b"']
BAD_ATTRS = ['h="a<b"', 'a="dup"', 'n=x', 'o="&"']
DTD = '<!DOCTYPE kajiki SYSTEM "kajiki.dtd">'


def element(r, depth):
    name = r.choice(NAMES)
    attrs = ' '.join(r.sample(ATTRS, r.randint(0, 3)) + ([r.choice(BAD_ATTRS)] if r.random() < 0.03 else []))
    head = '<' + name + (' ' + attrs if attrs else '')
    if depth > 3 or r.random() < 0.25:
        return head + (' />' if r.random() < 0.5 else '/>')
    parts = []
    for _ in range(r.randint(0, 5)):
        k = r.random()
        if k < 0.02:
            parts.append(r.choice(BAD_TEXTS))
        elif k < 0.35:
            parts.append(r.choice(TEXTS))
        elif k < 0.6:
            parts.append(element(r, depth + 1))
        elif k < 0.7:
            parts.append('<!--' + r.choice(['x', ' a b ', '! drop', '-', 'a--b']) + '-->')
        elif k < 0.8:
            parts.append('<?' + r.choice(['py', 'python', 'xml', 'php', 'py ']) +
                         r.choice([' x = 1 ', '% y = 2', '', '\n  if a:\n    b\n', ' a ? b ']) + '?>')
        elif k < 0.9:
            parts.append('<![CDATA[' + r.choice(['a < b', ']]', ' ${x} ', '\n', '&amp;']) + ']]>')
        else:
            parts.append(r.choice(TEXTS) + r.choice(TEXTS))
    return head + '>' + ''.join(parts) + '</' + name + '>'


def document(r):
    prolog = ''
    if r.random() < 0.1:
        prolog = r.choice(['<?xml version="1.0"?>', "<?xml version='1.0' encoding='latin-1'?>",
                           '<?xml version="1.0" standalone="yes"?>', '<?xml version="1.1" encoding="x y"?>',
                           ' <?xml version="1.0"?>', '<?xml version="1.0" encoding="utf-16"?>'])
    misc = r.choice(['', '\n', '<!-- c -->\n', '', '\n\n', '<!-- c -->'])
    if r.random() < 0.05:
        misc = r.choice(['<?pi x?>', '<!DOCTYPE html>', '<!doctype html>'])
    doc = prolog + DTD + misc + element(r, 0) + r.choice(['', '\n', '\n<!-- end -->', '', '\n\n'])
    if r.random() < 0.03:
        doc += r.choice([' junk', '<a/>', '<?pi?>'])
    if r.random() < 0.1:
        doc = prolog + r.choice(['<!-- ' + DTD + ' -->', '<!DOCTYPE d [<!ENTITY e "v&#38;w"> <!ENTITY f "<b>z</b>">'
                                 ' <!ATTLIST a z CDATA "dz" t NMTOKENS " p  q "> %pe; <!ENTITY % pe "x">]>']) + \
            element(r, 0).replace('hello', '&e;&f;')
    if r.random() < 0.1:
        i = r.randrange(len(doc) + 1)
        doc = doc[:i] + r.choice(['<', '>', '&', '"', "'", '</', '/>', '\x01', '', 'x']) + doc[i + r.randint(0, 3):]
    return doc


def run(src, chunks):
    log = []
    p = expat.ParserCreate()

    def at(*entry):
        log.append(list(entry) + [p.CurrentLineNumber, p.CurrentColumnNumber])

    def start(name, attrs):
        at('start', name, [[k, v] for k, v in attrs.items()])

    def skipped(name, is_pe):
        at('skipped', name, is_pe)
        if name.startswith('bogus'):
            raise KeyError(name + ';')

    p.StartElementHandler = start
    p.EndElementHandler = lambda name: at('end', name)
    p.CharacterDataHandler = lambda data: at('data', data)
    p.ProcessingInstructionHandler = lambda target, data: at('pi', target, data)
    p.CommentHandler = lambda data: at('comment', data)
    p.StartCdataSectionHandler = lambda: at('startcdata')
    p.EndCdataSectionHandler = lambda: at('endcdata')
    p.StartDoctypeDeclHandler = lambda name, sysid, pubid, internal: at('doctype', name, sysid, pubid, internal)
    p.ExternalEntityRefHandler = lambda context, base, sysid, pubid: at('extref', context, sysid, pubid) or 1
    p.SkippedEntityHandler = skipped
    p.SetParamEntityParsing(expat.XML_PARAM_ENTITY_PARSING_UNLESS_STANDALONE)
    try:
        pos = 0
        for n in chunks:
            p.Parse(src[pos:pos + n], False)
            pos += n
        p.Parse(b'', True)
    except expat.ExpatError as e:
        return log, ['error', e.code, e.lineno, e.offset, str(e)]
    except Exception as e:
        return log, ['exception', type(e).__name__, str(e)]
    return log, ['ok']


def main():
    r = random.Random(20261125)
    with gzip.open('expat.jsonl.gz', 'wt', encoding='utf-8') as out:
        for i in range(3000):
            src = document(r)
            chunks = [len(src)]
            if i % 7 == 0 and len(src) > 2:
                cut = r.randrange(1, len(src))
                chunks = [cut, len(src) - cut]
            log, result = run(src, chunks)
            out.write(json.dumps({'src': src, 'chunks': chunks, 'log': log, 'result': result},
                                 ensure_ascii=True) + '\n')


main()
