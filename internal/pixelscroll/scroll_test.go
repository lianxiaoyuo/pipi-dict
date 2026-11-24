package pixelscroll

import "testing"

func TestSmoothWheelPreservesHighResolutionDeltas(t *testing.T) {
	pos, rem := 250, 0
	for i := 0; i < 8; i++ {
		pos, rem = Wheel(pos, rem, 15, 60, 1000, 200)
	}
	if pos != 190 || rem != 0 {
		t.Fatalf("got pos=%d rem=%d", pos, rem)
	}
}
func TestWheelNoSnapAcrossDifferentCardHeights(t *testing.T) {
	starts := []int{0, 78, 225, 316, 490}
	pos, _ := Wheel(225, 0, -30, 60, 800, 200)
	if pos != 240 || At(starts, pos) != 2 {
		t.Fatalf("got pos %d", pos)
	}
}
func TestWheelBounds(t *testing.T) {
	if p, _ := Wheel(0, 0, 120, 60, 900, 500); p != 0 {
		t.Fatal(p)
	}
	if p, _ := Wheel(400, 0, -120, 60, 900, 500); p != 400 {
		t.Fatal(p)
	}
	if p := Clamp(600, 900, 500); p != 400 {
		t.Fatal(p)
	}
}
func TestAt(t *testing.T) {
	starts := []int{0, 90, 190, 300}
	for _, tt := range []struct{ pos, want int }{{0, 0}, {89, 0}, {90, 1}, {299, 2}, {300, 3}, {1000, 3}} {
		if x := At(starts, tt.pos); x != tt.want {
			t.Fatalf("at %d: %d != %d", tt.pos, x, tt.want)
		}
	}
}

// High-volume wheel gestures must never exceed content bounds or jump by a card.
func TestRapidTrackpadStress(t *testing.T) {
	pos, rem := 0, 0
	content, viewport := 153287, 672
	last := pos
	for i := 0; i < 15000; i++ {
		delta := -15
		if i%11 == 0 {
			delta = -30
		}
		pos, rem = Wheel(pos, rem, delta, 68, content, viewport)
		if pos < last || pos-last > 17 || pos > content-viewport {
			t.Fatalf("step %d: pos=%d last=%d rem=%d", i, pos, last, rem)
		}
		last = pos
	}
	for i := 0; i < 15000; i++ {
		pos, rem = Wheel(pos, rem, 30, 68, content, viewport)
		if pos < 0 || pos > content-viewport {
			t.Fatalf("step %d: pos=%d", i, pos)
		}
	}
}
