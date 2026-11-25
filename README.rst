Kajiki provides fast well-formed XML templates
==============================================

Because Kajiki's input is XML, it can ensure that your HTML/XML output is
well-formed. The Genshi-like syntax, based on XML attributes or tags, is simply
beautiful and easily understood (or ignored) by web designers.
Kajiki's template inheritance works like Jinja's blocks.

This is Kajiki's Go implementation, the module
``github.com/jackrosenthal/kajiki-go``. Templates hold Python: expressions,
directive arguments and code blocks are Python 3.12, and the module carries its
own implementation of the language, so a template renders exactly as it does
with the Python library on CPython 3.12, with the same escaping, the same
errors and the same reported template lines.

Example
=======

.. code:: go

    package main

    import (
        "fmt"

        kajiki "github.com/jackrosenthal/kajiki-go"
    )

    func main() {
        tpl, err := kajiki.XMLTemplate(`<html>
        <head><title>$title</title></head>
        <body>
            <h1>$title</h1>
            <ul>
                <li py:for="x in range(repetitions)">$title</li>
            </ul>
        </body>
    </html>`, kajiki.XMLOptions{})
        if err != nil {
            panic(err)
        }
        out, err := tpl.Render(
            kajiki.Var{Name: "title", Value: kajiki.String("Kajiki is teh awesome!")},
            kajiki.Var{Name: "repetitions", Value: kajiki.Int(3)},
        )
        if err != nil {
            panic(err)
        }
        fmt.Println(out)
    }

Text templates, loaders (``NewMockLoader``, ``NewFileLoader``) and message
extraction for translation (``Extract``) are part of the same package; the
documentation in ``docs/`` describes the template language.

Development
===========

``go build ./...``, ``go vet ./...`` and ``go test ./...`` build, check and
test the module; it has no dependencies outside the Go standard library.

Links
=====

Documentation_

Kajiki is licensed under an MIT-style license_.

The git repository and `issue tracker`_ are at GitHub_.


.. _Documentation: https://docs.kajiki.io/
.. _license: https://github.com/jackrosenthal/kajiki/blob/master/LICENSE.rst
.. _`issue tracker`: https://github.com/jackrosenthal/kajiki/issues
.. _GitHub: https://github.com/jackrosenthal/kajiki
