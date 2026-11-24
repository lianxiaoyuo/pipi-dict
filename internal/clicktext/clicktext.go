// Package clicktext classifies text returned by third-party accessibility
// providers. Invalid/partial selections must never masquerade as paragraphs.
package clicktext

import (
	"hoverdict/internal/textsegments"
	"hoverdict/internal/wordextract"
	"unicode"
)

// Classify returns exactly one of word or paragraph. A normal click can
// produce a single word only. A deliberate selection can produce a word or
// a paragraph; punctuation-only and numeric fragments are rejected.
func Classify(selection bool, raw string) (word, paragraph string) {
	cleaned := textsegments.Clean(raw)
	if w := wordextract.FromTextUnit(cleaned); w != "" {
		return w, ""
	}
	if !selection || len(cleaned) == 0 || len(cleaned) > textsegments.MaxTextBytes {
		return "", ""
	}
	letters := 0
	for _, r := range cleaned {
		if unicode.IsLetter(r) {
			letters++
			if letters >= 2 {
				break
			}
		}
	}
	if letters < 2 {
		return "", ""
	}
	return "", cleaned
}
