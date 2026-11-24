// Package ocrclick contains the local-only screen crop and word hit testing.
package ocrclick

import (
	"hoverdict/internal/wordextract"
	"math"
)

type Rect struct{ Left, Top, Right, Bottom int32 }

func (r Rect) Contains(x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}
func Intersect(a, b Rect) Rect {
	return Rect{max(a.Left, b.Left), max(a.Top, b.Top), min(a.Right, b.Right), min(a.Bottom, b.Bottom)}
}
func Crop(x, y int32, bounds, screen Rect) (Rect, bool) {
	r := Intersect(Intersect(bounds, screen), Rect{x - 192, y - 54, x + 192, y + 54})
	return r, r.Contains(x, y) && r.Right-r.Left >= 8 && r.Bottom-r.Top >= 8
}

type Word struct {
	Text       string
	X, Y, W, H float64
}

// Only the word actually containing the click is accepted. No nearest-line
// guessing: clicking whitespace must not turn into an unrelated query.
func Hit(words []Word, x, y float64) string {
	best := ""
	area := math.Inf(1)
	for _, w := range words {
		if w.W <= 0 || w.H <= 0 || math.IsNaN(w.X+w.Y+w.W+w.H) || math.IsInf(w.X+w.Y+w.W+w.H, 0) {
			continue
		}
		if x < w.X || x >= w.X+w.W || y < w.Y || y >= w.Y+w.H {
			continue
		}
		if s := wordextract.FromTextUnit(w.Text); s != "" && w.W*w.H < area {
			best = s
			area = w.W * w.H
		}
	}
	return best
}
func Fallback(status string, selected bool, verified bool) bool {
	return !selected && verified && (status == "text_pattern_unavailable" || status == "word_range_unavailable" || status == "no_text")
}
