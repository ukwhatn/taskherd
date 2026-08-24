package tui

import "strings"

// OSC 8 is the terminal escape that turns a run of text into a clickable hyperlink:
//
//	ESC ] 8 ; <params> ; <uri> ST   text   ESC ] 8 ; ; ST
//
// A terminal that does not implement it ignores both sequences and prints the text unchanged,
// which is why the board can emit them without asking what it is running in.
const (
	osc8Open  = "\x1b]8;;"
	osc8Close = "\x1b\\"
)

// hyperlink wraps text so a terminal that supports OSC 8 opens url when the text is clicked.
//
// The wrapping is invisible to width measurement (lipgloss skips escape sequences), so a wrapped
// row still lines up inside a bordered card.
func hyperlink(url, text string) string {
	if url == "" || text == "" {
		return text
	}
	safe := sanitizeURI(url)
	if safe == "" {
		return text
	}
	return osc8Open + safe + osc8Close + text + osc8Open + osc8Close
}

// sanitizeURI drops the characters that would end the escape sequence early or steer the terminal
// somewhere else, and returns "" for a URI that is nothing but those.
//
// Link URLs are user input, and an escape or a control byte reaching the terminal inside one would
// be interpreted as a command rather than printed. Bytes above ASCII are left alone: a percent
// encoded or IRI-style URL is the user's to write, and no terminal reads them as control.
func sanitizeURI(raw string) string {
	if strings.ContainsFunc(raw, isControl) {
		raw = strings.Map(func(r rune) rune {
			if isControl(r) {
				return -1
			}
			return r
		}, raw)
	}
	return strings.TrimSpace(raw)
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}
