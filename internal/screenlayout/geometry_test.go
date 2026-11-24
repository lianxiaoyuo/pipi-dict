package screenlayout

import "testing"

func TestVirtualDesktop(t *testing.T) {
	desktop := Rect{-1920, -100, 3840, 1180}
	cases := []struct {
		r    Rect
		want bool
	}{
		{Rect{-1700, 150, 450, 530}, true},
		{Rect{1400, 80, 450, 530}, true},
		{Rect{-2500, 30, 450, 530}, false},
		{Rect{2200, 40, 450, 530}, false},
		{Rect{-1910, -90, 80, 80}, true},
		{Rect{3000, 90, 0, 20}, false},
	}
	for _, c := range cases {
		if got := VisibleAtLeast(c.r, desktop, 30); got != c.want {
			t.Errorf("rect %v got %v", c.r, got)
		}
	}
}
