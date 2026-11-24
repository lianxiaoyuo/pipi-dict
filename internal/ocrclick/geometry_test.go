package ocrclick

import "testing"

func TestCrop(t *testing.T) {
	b := Rect{-200, -100, 1000, 500}
	s := Rect{-1920, 0, 1920, 1080}
	r, ok := Crop(-100, 8, b, s)
	if !ok || r.Top != 0 || r.Left != -200 || r.Right != 92 {
		t.Fatalf("%+v %v", r, ok)
	}
	if _, ok = Crop(1000, 10, b, s); ok {
		t.Fatal("outside")
	}
}
func TestHit(t *testing.T) {
	words := []Word{{"working", 10, 10, 60, 20}, {"world", 80, 10, 50, 20}, {"https://example", 0, 40, 100, 20}}
	for _, c := range []struct {
		x, y float64
		want string
	}{{20, 15, "working"}, {90, 15, "world"}, {75, 15, ""}, {20, 45, ""}, {20, 30, ""}} {
		if got := Hit(words, c.x, c.y); got != c.want {
			t.Fatalf("%v: %s", c, got)
		}
	}
}
func TestFallbackPolicy(t *testing.T) {
	for _, s := range []string{"protected_control", "no_element", "timeout", "helper_error", "no_selection", "ok"} {
		if Fallback(s, false, true) {
			t.Fatal(s)
		}
	}
	if !Fallback("text_pattern_unavailable", false, true) || Fallback("word_range_unavailable", true, true) || Fallback("no_text", false, false) {
		t.Fatal("policy")
	}
}
