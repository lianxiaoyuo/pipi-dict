package clicktext

import "testing"

func TestClassifyNoFakeParagraph(t *testing.T) {
	samples := []string{".0", "0", "…", ".", "19.0", "\t", "", "a .0", " 1 "}
	for _, input := range samples {
		w, p := Classify(true, input)
		if w != "" || p != "" {
			t.Errorf("%q became word=%q paragraph=%q", input, w, p)
		}
	}
}
func TestClassifyWordAndParagraph(t *testing.T) {
	cases := []struct {
		input           string
		selected        bool
		word, paragraph string
	}{
		{" conversations ", false, "conversations", ""},
		{"conversations", true, "conversations", ""},
		{"ChatGPT accessibility", true, "", "ChatGPT accessibility"},
		{"ChatGPT accessibility", false, "", ""},
		{"It's a text selection.", true, "", "It's a text selection."},
		{"109", true, "", ""},
	}
	for _, tc := range cases {
		w, p := Classify(tc.selected, tc.input)
		if w != tc.word || p != tc.paragraph {
			t.Errorf("%q: got %q/%q, want %q/%q", tc.input, w, p, tc.word, tc.paragraph)
		}
	}
}
