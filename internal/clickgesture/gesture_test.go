package clickgesture

import "testing"

func TestGestures(t *testing.T) {
	tests := []struct {
		dx, dy int32
		ms     uint32
		kind   Kind
	}{
		{0, 0, 50, Click}, {7, 2, 88, Click}, {15, 15, 20, Click},
		{16, 1, 50, Ignore}, {16, 1, 100, Selection}, {24, 1, 210, Selection},
		{0, 50, 100, Selection}, {2450, 0, 900, Ignore}, {0, 1800, 200, Ignore},
		{-1, 0, 100, Ignore},
	}
	for _, tt := range tests {
		if got := Classify(tt.dx, tt.dy, tt.ms); got != tt.kind {
			t.Errorf("%v got %v, want %v", tt, got, tt.kind)
		}
	}
}
