// Ported from CPython 3.12.15 (Lib/shlex.py) to Go, 2026-11-25.
// Copyright 2001 Python Software Foundation.
// SPDX-License-Identifier: PSF-2.0

package kajiki

import (
	"strings"

	"github.com/jackrosenthal/kajiki-go/internal/py"
)

// shlexSplit is shlex.split(s): POSIX shell-like splitting with
// whitespace_split set and no comment characters. An unclosed quotation
// or a backslash at the end is the ValueError shlex raises.
func shlexSplit(s string) ([]string, error) {
	const (
		whitespace = " \t\r\n"
		quotes     = `'"`
		escape     = `\`
		escaped    = `"`
		// states besides a quote or the escape character
		stateIdle = ' '
		stateWord = 'a'
		stateEOF  = 0
	)
	in := []rune(s)
	pos := 0
	state := rune(stateIdle)
	var parts []string
	for state != stateEOF {
		var token strings.Builder
		quoted := false
		escapedState := rune(stateIdle)
	read:
		for {
			var next rune
			eof := pos >= len(in)
			if !eof {
				next = in[pos]
				pos++
			}
			switch {
			case state == stateIdle:
				switch {
				case eof:
					state = stateEOF
					break read
				case strings.ContainsRune(whitespace, next):
					if token.Len() > 0 || quoted {
						break read
					}
				case strings.ContainsRune(escape, next):
					escapedState = stateWord
					state = next
				case strings.ContainsRune(quotes, next):
					state = next
				default:
					token.WriteRune(next)
					state = stateWord
				}
			case strings.ContainsRune(quotes, state):
				quoted = true
				switch {
				case eof:
					return nil, py.Raise("ValueError", py.String("No closing quotation"))
				case next == state:
					state = stateWord
				case strings.ContainsRune(escape, next) && strings.ContainsRune(escaped, state):
					escapedState = state
					state = next
				default:
					token.WriteRune(next)
				}
			case strings.ContainsRune(escape, state):
				if eof {
					return nil, py.Raise("ValueError", py.String("No escaped character"))
				}
				// In quotes only the quote itself or the escape character
				// may be escaped.
				if strings.ContainsRune(quotes, escapedState) && next != state && next != escapedState {
					token.WriteRune(state)
				}
				token.WriteRune(next)
				state = escapedState
			default:
				switch {
				case eof:
					state = stateEOF
					break read
				case strings.ContainsRune(whitespace, next):
					state = stateIdle
					if token.Len() > 0 || quoted {
						break read
					}
				case strings.ContainsRune(quotes, next):
					state = next
				case strings.ContainsRune(escape, next):
					escapedState = stateWord
					state = next
				default:
					token.WriteRune(next)
				}
			}
		}
		if token.Len() > 0 || quoted {
			parts = append(parts, token.String())
		}
	}
	return parts, nil
}
