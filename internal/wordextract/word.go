// Package wordextract applies strict, local-only validation to text reported by
// external UI Automation text providers. It never copies or sends page content.
package wordextract

import (
	"regexp"
	"strings"
)

var english = regexp.MustCompile(`^[a-z]+(?:['-][a-z]+)*$`)

// FromTextUnit accepts exactly one English word returned by a UIA TextRange.
// Reject entire sentences, secrets, paths, URLs, and arbitrarily large text.
func FromTextUnit(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.NewReplacer("’", "'", "‘", "'", "‐", "-", "‑", "-", "\u00a0", " ").Replace(text)
	text = strings.Trim(text, " \t\r\n.,!?;:\"'()[]{}“”「」<>—–")
	if (len(text) < 2 && text != "a" && text != "i") || len(text) > 80 || !english.MatchString(text) {
		return ""
	}
	return text
}

// IsSingleWord validates manually typed/clipboard words before they are saved
// or sent to a third-party translation endpoint. No sentence or URL accepted.
func IsSingleWord(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if len(normalized) > 80 || len(normalized) == 0 {
		return false
	}
	return english.MatchString(normalized)
}
