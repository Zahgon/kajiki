"""Record what the library functions Kajiki relies on give for generated inputs: helpers.json.

Run with the original importable as kajiki (rec/py.sh from the task root mounts it):
    rec/py.sh gold/testdata python3 record_helpers.py

shlex.split (the tags of text templates), html.unescape (script and style content), pathlib joining and
suffixes (FileLoader), kajiki.util.default_alias_for, and kajiki.doctype's extract_dtd and matching.
"""
import html
import json
import random
import shlex
from pathlib import Path

from kajiki.doctype import DocumentTypeDeclaration, extract_dtd
from kajiki.util import default_alias_for


def main():
    r = random.Random(20261125)
    out = {'shlex': [], 'unescape': [], 'path': [], 'alias': [], 'dtd': []}
    alpha = ['a', 'b', ' ', '\t', "'", '"', '\\', '\n', 'é', '#', '$', 'as']
    for _ in range(3000):
        s = ''.join(r.choice(alpha) for _ in range(r.randint(0, 12)))
        try:
            out['shlex'].append([s, shlex.split(s), None])
        except ValueError as e:
            out['shlex'].append([s, None, str(e)])
    refs = ['&amp;', '&lt', '&notin;', '&notit;', '&#65;', '&#x41;', '&#0;', '&#128;', '&#xD800;', '&#1114112;',
            '&#99999999999999999999;', '&#11;', '&#xFFFE;', '&#13;', '&bogus;', '&', '&#', '&#x;', '&amp', 'x',
            ' ', '&ampx', '&lt;3', '&NotNestedLessLess;', '&#x1F600', '&#9;']
    for _ in range(2000):
        s = ''.join(r.choice(refs) for _ in range(r.randint(0, 5)))
        out['unescape'].append([s, html.unescape(s)])
    parts = ['', '.', '..', 'a', 'b/', '/x', '//y', '///z', 'c//d', './e', 'f/.', 'g.txt', '.hidden', 'h.', 'i.j.k']
    for _ in range(1500):
        base, name = r.choice(parts), r.choice(parts) + r.choice(['', '/n.xml', '.html5'])
        p = Path(base) / name
        out['path'].append([base, name, str(p), p.name, p.suffix])
    for _ in range(500):
        name = r.choice(parts) + r.choice(parts)
        out['alias'].append([name, default_alias_for(name)])
    doctypes = ['<!DOCTYPE html>', '<!DOCTYPE HTML>', '<!doctype html>', '<!DOCTYPE  html >', '<!DOCTYPE html\n>',
                '<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">',
                '<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML+RDFa 1.0//EN" "http://www.w3.org/MarkUp/DTD/xhtml-rdfa-1.dtd">',
                '<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN"\n  "http://www.w3.org/TR/html4/strict.dtd">',
                '<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Frameset//EN" "http://www.w3.org/TR/html4/frameset.dtd">',
                '<!DOCTYPE x SYSTEM "y">', '']
    for _ in range(600):
        markup = (r.choice(['', '<?xml version="1.0"?>', ' <?xml ?>', '<!-- c -->']) + r.choice(doctypes) +
                  r.choice(['', '\n', '<a/>', '<!DOCTYPE html>']))
        dtd, pos, rest = extract_dtd(markup)
        m = DocumentTypeDeclaration.matching(dtd)
        out['dtd'].append([markup, dtd, pos, rest, m.name if m else None, m.rendering_mode if m else None])
    with open('helpers.json', 'w', encoding='utf-8') as f:
        json.dump(out, f, ensure_ascii=True)


main()
