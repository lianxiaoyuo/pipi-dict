// Package clickgesture distinguishes ordinary click jitter from an intentional
// mouse text selection. Thresholds are in physical pixels and milliseconds.
package clickgesture

type Kind uint8

const (
	Ignore Kind = iota
	Click
	Selection
)

func Classify(dx, dy int32, durationMS uint32) Kind {
	if dx < 0 || dy < 0 || dx > 2400 || dy > 1600 {
		return Ignore
	}
	if dx < 16 && dy < 16 {
		return Click
	}
	if durationMS >= 70 {
		return Selection
	}
	return Ignore
}
