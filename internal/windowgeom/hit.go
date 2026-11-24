package windowgeom

// HitTest returns Win32 non-client hit-test codes or zero for a normal client
// point. Window bounds and cursor coordinates use physical screen pixels.
// The top-left corner changes width and height; edges change one dimension.
// HitTestScaled handles fractional monitor DPI scaling (e.g. 125%).
func HitTestScaled(x, y, left, top, right, bottom int, scale float64) uintptr {
	if scale < 1 {
		scale = 1
	}
	if x < left || x >= right || y < top || y >= bottom {
		return 0
	}
	corner, edge := int(24*scale+0.5), int(9*scale+0.5)
	dl, dr := x-left, right-x
	dt, db := y-top, bottom-y
	switch {
	case dl < corner && dt < corner:
		return 13
	case dr <= corner && dt < corner:
		return 14
	case dl < corner && db <= corner:
		return 16
	case dr <= corner && db <= corner:
		return 17
	case dl < edge:
		return 10
	case dr <= edge:
		return 11
	case dt < edge:
		return 12
	case db <= edge:
		return 15
	case dt < int(58*scale+0.5) && dr > int(70*scale+0.5):
		return 2
	default:
		return 0
	}
}
