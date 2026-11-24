package bubblegeom

import "testing"

func TestTopLeftGrip(t *testing.T) {
	for _, p := range [][2]int{{9, 9}, {13, 12}, {11, 13}} {
		if !TopLeftGrip(p[0], p[1], 40) {
			t.Fatalf("%v expected top-left hit", p)
		}
	}
	for _, p := range [][2]int{{35, 35}, {20, 20}, {-1, 12}, {0, 0}, {50, 13}} {
		if TopLeftGrip(p[0], p[1], 40) {
			t.Fatalf("%v unexpected grip", p)
		}
	}
}

func TestResizeFromTopLeft(t *testing.T) {
	for _, tc := range []struct{ start, dx, dy, want int }{
		{42, -18, 0, 60}, {42, 0, -12, 54}, {42, 10, 0, 32},
		{42, 0, 9, 33}, {42, 900, 0, 32}, {42, -900, 0, 88},
	} {
		got := FromTopLeftDrag(tc.start, tc.dx, tc.dy, 32, 88)
		if got != tc.want {
			t.Fatalf("%+v got %d", tc, got)
		}
	}
}
