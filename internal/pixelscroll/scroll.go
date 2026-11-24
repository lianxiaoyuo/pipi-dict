package pixelscroll

// Clamp keeps a pixel-based viewport inside the content, even after cards
// change height or the user resizes the window.
func Clamp(position, content, viewport int) int {
	limit := content - viewport
	if limit < 0 {
		limit = 0
	}
	if position < 0 {
		return 0
	}
	if position > limit {
		return limit
	}
	return position
}

// Wheel translates high-resolution mouse/touchpad deltas into pixel motion.
// It keeps fractional motion instead of snapping to entire variable-height cards.
func Wheel(position, remainder, delta, pixelsPerNotch, content, viewport int) (int, int) {
	scaled := remainder + delta*pixelsPerNotch
	moved := scaled / 120
	remainder = scaled - moved*120
	next := Clamp(position-moved, content, viewport)
	if (next == 0 && delta > 0) || (next == Clamp(content, content, viewport) && delta < 0) {
		remainder = 0
	}
	return next, remainder
}

// At locates the first row intersected by the viewport in prefix offsets.
func At(starts []int, pos int) int {
	if len(starts) == 0 {
		return -1
	}
	lo, hi := 0, len(starts)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if starts[mid] <= pos {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return 0
	}
	return lo - 1
}
