// Package screenlayout keeps existing user placements visible across monitor changes.
// Windows virtual desktops may have negative X/Y coordinates.
package screenlayout

type Rect struct{ X, Y, W, H int32 }

func VisibleAtLeast(window, desktop Rect, minimum int32) bool {
	if window.W <= 0 || window.H <= 0 || desktop.W <= 0 || desktop.H <= 0 {
		return false
	}
	if minimum < 1 {
		minimum = 1
	}
	left := max(window.X, desktop.X)
	right := min(window.X+window.W, desktop.X+desktop.W)
	top := max(window.Y, desktop.Y)
	bottom := min(window.Y+window.H, desktop.Y+desktop.H)
	return right-left >= min(minimum, window.W) && bottom-top >= min(minimum, window.H)
}
