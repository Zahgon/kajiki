// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"bytes"
	"runtime"
	"testing"
)

// TestDroppedGeneratorsReleased leaves many generators paused inside try,
// with and except blocks and drops them: once they are collected their
// bodies end (the goroutines of their coroutines go away) without running
// any of their finally blocks, __exit__ methods or handlers, and the
// exception state of the code around them is untouched.
func TestDroppedGeneratorsReleased(t *testing.T) {
	var out bytes.Buffer
	Init(Config{Stdout: &out})
	before := runtime.NumGoroutine()
	g := execSource(t, `
import sys
class CM:
    def __enter__(self): return self
    def __exit__(self, *a): print('exit')
def gen(i):
    try:
        with CM():
            try:
                raise ValueError(i)
            except ValueError:
                yield i
                yield i
    finally:
        print('finally')
def outer(i):
    yield from gen(i)
keep = []
for i in range(500):
    g = outer(i) if i % 2 else gen(i)
    next(g)
    keep.append(g)
`)
	if paused := runtime.NumGoroutine() - before; paused < 500 {
		t.Fatalf("only %d goroutines for 500 paused generators", paused)
	}
	if err := DelItem(g, String("keep")); err != nil {
		t.Fatal(err)
	}
	if err := DelItem(g, String("g")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5 && runtime.NumGoroutine()-before > 50; i++ {
		runtime.GC()
		releaseDropped()
	}
	if left := runtime.NumGoroutine() - before; left > 50 {
		t.Errorf("%d of the paused generators' goroutines are left", left)
	}
	if out.Len() != 0 {
		t.Errorf("dropping generators ran Python code: %q", out.String())
	}
	code, err := Compile("r = sys.exc_info()\n", "<t>", "exec")
	if err != nil {
		t.Fatal(err)
	}
	if err := code.Exec(g); err != nil {
		t.Fatal(err)
	}
	if got := reprText(t, lookup(t, g, "r")); got != "(None, None, None)" {
		t.Errorf("exception state after the drops: %s", got)
	}
}
