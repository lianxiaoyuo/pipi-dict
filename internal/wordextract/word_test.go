package wordextract

import "testing"

func TestFromTextUnit(t *testing.T) {
	tests := map[string]string{
		" Opportunity ": "opportunity", "resilient ": "resilient", "'can't'": "can't",
		"high-quality ": "high-quality", "(OVERCOME)": "overcome", "it’s": "it's",
		"hello world": "", "https://example.com": "", "password=secret": "", "a": "a", "I": "i",
		"你好": "", "123456": "", "foo_bar": "", "line\nsecret": "",
		"word. Next": "", "abc@example.com": "",
	}
	for in, want := range tests {
		if got := FromTextUnit(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestIsSingleWord(t *testing.T) {
	for _, word := range []string{"hello", "can't", "high-quality", "x"} {
		if !IsSingleWord(word) {
			t.Errorf("valid word %q rejected", word)
		}
	}
	for _, text := range []string{"two words", "http://example.com", "a\nsecret", "one.two", "abc@domain.com", "", "秘密"} {
		if IsSingleWord(text) {
			t.Errorf("unsafe non-word %q accepted", text)
		}
	}
}
