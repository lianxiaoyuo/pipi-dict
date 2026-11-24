package windowgeom

import "testing"

func TestHitTestTopLeftAndAxes(t *testing.T) {
	const l, t0, r, b = 100, 200, 600, 700
	tests := []struct {
		x, y int
		want uintptr
	}{
		{105, 205, 13}, {105, 380, 10}, {370, 205, 12},
		{595, 380, 11}, {370, 695, 15}, {370, 230, 2},
		{370, 420, 0}, {540, 230, 0}, {581, 240, 0},
		{99, 199, 0},
	}
	for _, c := range tests {
		got := HitTestScaled(c.x, c.y, l, t0, r, b, 1.0)
		if got != c.want {
			t.Errorf("(%d,%d) got %d want %d", c.x, c.y, got, c.want)
		}
	}
}
func TestHitTestHiDPI(t *testing.T) {
	if got := HitTestScaled(215, 315, 200, 300, 850, 900, 2.0); got != 13 {
		t.Fatalf("scaled corner expected, got %d", got)
	}
}
