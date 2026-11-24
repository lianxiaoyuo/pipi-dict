package bubblegeom

func Clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

// TopLeftGrip sits inside the VISIBLE circle, not the transparent square
// outside the elliptical window region. Its upper-left position is deliberate:
// the lower-right corner no longer resizes the bubble.
func TopLeftGrip(x, y, size int) bool {
	if size <= 0 || x < 0 || y < 0 || x >= size || y >= size {
		return false
	}
	return x >= size*15/100 && x <= size*43/100 &&
		y >= size*15/100 && y <= size*43/100 &&
		x+y <= size*66/100
}

// FromTopLeftDrag keeps the lower-right of the floating bubble stationary.
// Users may drag horizontally OR vertically to resize the round mascot.
// For non-circular panel windows, Win32 handles independent X/Y sizing.
func FromTopLeftDrag(startSize, dx, dy, minimum, maximum int) int {
	delta := dx
	if abs(dy) > abs(dx) {
		delta = dy
	}
	return Clamp(startSize-delta, minimum, maximum)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
