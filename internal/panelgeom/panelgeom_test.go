package panelgeom

import "testing"

func TestPanelsStayInsideWindowAndDoNotOverlap(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		for _, size := range [][2]int{{336, 346}, {368, 365}, {520, 490}, {760, 650}} {
			w, h := int(float64(size[0])*scale), int(float64(size[1])*scale)
			l := Compute(w, h, scale)
			rects := []Rect{l.Logo, l.Title, l.Collapse, l.SearchCard, l.SearchInput, l.ResultCard, l.Word, l.Pron, l.Origin, l.Divider, l.Detail, l.BookTitle, l.BookCount, l.SavedList, l.Primary, l.Secondary}
			for _, r := range rects {
				if r.X < 0 || r.Y < 0 || r.W <= 0 || r.H <= 0 || r.Right() > w || r.Bottom() > h {
					t.Fatalf("scale %.2f %dx%d invalid rectangle %+v", scale, w, h, r)
				}
			}
			if l.ResultCard.Bottom() > l.Primary.Y || l.SavedList.Bottom() > l.Primary.Y || l.SearchCard.Bottom() > l.ResultCard.Y || l.Detail.Bottom() > l.ResultCard.Bottom() {
				t.Fatalf("overlap at %.2f %dx%d: %+v", scale, w, h, l)
			}
			if l.Primary.Right() >= l.Secondary.X || l.Secondary.Bottom() > h {
				t.Fatalf("bad buttons scale %.2f %dx%d", scale, w, h)
			}
		}
	}
}

// The native single-line EDIT text is top-aligned inside its own HWND.
// Positioning a text-height EDIT window centrally in the search card keeps
// the displayed text centered at all supported display scales.
func TestSearchTextControlVerticallyCentered(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		l := Compute(int(480*scale), int(520*scale), scale)
		if l.SearchInput.Y < l.SearchCard.Y || l.SearchInput.Bottom() > l.SearchCard.Bottom() {
			t.Fatalf("search input outside card at scale %v: %+v %+v", scale, l.SearchCard, l.SearchInput)
		}
		cardCenter2 := l.SearchCard.Y*2 + l.SearchCard.H
		inputCenter2 := l.SearchInput.Y*2 + l.SearchInput.H
		diff := cardCenter2 - inputCenter2
		if diff < -2 || diff > 2 {
			t.Fatalf("input not vertically centered at scale %v (difference=%d)", scale, diff)
		}
	}
}
