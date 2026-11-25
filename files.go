// Copyright (c) 2010 Rick Copeland and contributors.
// Ported from Python (kajiki/xml_template.py, kajiki/text.py, kajiki/loader.py) to Go, 2026-11-25.
// SPDX-License-Identifier: MIT

package kajiki

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// readFile reads a file as builtins.open(name, "rb").read() does, failing
// with the OSError subclass Python raises.
func readFile(name string) ([]byte, error) {
	data, err := os.ReadFile(name)
	if err == nil {
		return data, nil
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return nil, py.Raise("OSError", py.String(err.Error()))
	}
	class := "OSError"
	switch {
	case errors.Is(err, fs.ErrNotExist):
		class = "FileNotFoundError"
	case errors.Is(err, fs.ErrPermission):
		class = "PermissionError"
	case errno == syscall.EISDIR:
		class = "IsADirectoryError"
	}
	return nil, py.Raise(class, py.Int(int64(errno)), py.String(errnoText(errno)), py.String(name))
}

// errnoText is os.strerror for the errors opening a template can give.
func errnoText(errno syscall.Errno) string {
	switch errno {
	case syscall.ENOENT:
		return "No such file or directory"
	case syscall.EACCES:
		return "Permission denied"
	case syscall.EISDIR:
		return "Is a directory"
	case syscall.ENOTDIR:
		return "Not a directory"
	case syscall.ENAMETOOLONG:
		return "File name too long"
	}
	return errno.Error()
}

// codecsOpenRead is codecs.open(filename, encoding=encoding).read(): the
// file decoded as it is, "\r\n" kept. "" is the None of a missing file
// name.
func codecsOpenRead(filename, encoding string) (string, error) {
	if filename == "" {
		return "", py.Raise("TypeError", py.String("expected str, bytes or os.PathLike object, not NoneType"))
	}
	data, err := readFile(filename)
	if err != nil {
		return "", err
	}
	return decodeFile(data, encoding, false)
}

// decodeFile decodes the bytes of a file with the codec encoding; with
// final false, as a stream reader that has not been told the data ended, a
// truncated character at the end is left out.
func decodeFile(data []byte, encoding string, final bool) (string, error) {
	text, _, err := py.Decode(data, encoding, final)
	return text, err
}
