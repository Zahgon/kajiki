// Ported from CPython 3.12 to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package py

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// programOutcome is what testdata/record.py records for a program.
type programOutcome struct {
	Status int    `json:"status"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// runMain runs a program file as __main__ and reports what CPython's
// command line would: the exit status, stdout, and stderr with an uncaught
// exception reduced to its last line.
func runMain(path string) programOutcome {
	var out, errOut bytes.Buffer
	_, name := filepath.Split(path)
	Init(Config{Stdout: &out, Stderr: &errOut})
	src, err := os.ReadFile(path)
	if err != nil {
		return programOutcome{Status: 2, Stderr: err.Error()}
	}
	m := newModuleV("__main__")
	m.dict.setStr("__file__", strV(name))
	interp.modules.setStr("__main__", m)
	code, err := Compile(string(src), name, "exec")
	if err == nil {
		err = code.Exec(m.dict)
	}
	res := programOutcome{}
	if err != nil {
		res.Status = 1
		errOut.WriteString("Traceback (most recent call last):\n" + exceptionLine(ExceptionValue(err)) + "\n")
	}
	res.Stdout, res.Stderr = out.String(), errOut.String()
	return res
}

// TestPrograms runs the programs in testdata/programs and compares each
// outcome with the one CPython recorded (testdata/record.py).
func TestPrograms(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "programs"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.py"))
	if err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Error(err)
		}
	}()
	same, total := 0, 0
	for _, f := range files {
		total++
		abs, err := filepath.Abs(f)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(strings.TrimSuffix(abs, ".py") + ".json")
		if err != nil {
			t.Errorf("%s: no .json outcome beside it", f)
			continue
		}
		var want programOutcome
		if err := json.Unmarshal(data, &want); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(filepath.Dir(abs)); err != nil {
			t.Fatal(err)
		}
		got := runMain(abs)
		if got != want {
			t.Errorf("%s: status %d, want %d\nstderr %q\n  want %q\n%s", filepath.Base(f), got.Status, want.Status,
				got.Stderr, want.Stderr, firstDifference(got.Stdout, want.Stdout))
			continue
		}
		same++
	}
	t.Logf("%d of %d programs identical", same, total)
}

// firstDifference describes the first line where two outputs differ.
func firstDifference(got, want string) string {
	if want == "" {
		return "stdout:\n" + got
	}
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl || i >= len(g) || i >= len(w) {
			return "stdout line " + strconv.Itoa(i+1) + ":\n   got " + strconv.Quote(gl) + "\n  want " + strconv.Quote(wl)
		}
	}
	return "stdout identical"
}
