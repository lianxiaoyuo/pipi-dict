package panelgeom

import "math"

type Rect struct{ X, Y, W, H int }

func (r Rect) Right() int  { return r.X + r.W }
func (r Rect) Bottom() int { return r.Y + r.H }

type Layout struct {
	Logo, Title, Collapse, SearchCard, SearchInput, ResultCard, Word, Pron, Origin, Divider, Detail,
	BookTitle, BookCount, SavedList, Primary, Secondary, EmptyState Rect
}

func Compute(width, height int, scale float64) Layout {
	if scale < 1 {
		scale = 1
	}
	d := func(v int) int { return int(math.Round(float64(v) * scale)) }
	pad := d(18)
	inner := width - 2*pad
	gap := d(9)
	actionsY := height - d(51)
	primaryW := (inner - gap) * 54 / 100
	cardY := d(115)
	cardBottom := actionsY - d(12)
	if cardBottom < cardY+d(110) {
		cardBottom = cardY + d(110)
	}
	detailY := d(208)
	detailBottom := cardBottom - d(13)
	if detailBottom < detailY+d(16) {
		detailBottom = detailY + d(16)
	}
	listY := d(59)
	listBottom := actionsY - d(11)
	if listBottom < listY+d(80) {
		listBottom = listY + d(80)
	}
	return Layout{
		Logo: Rect{pad, d(15), d(28), d(28)}, Title: Rect{pad + d(40), d(12), inner - d(89), d(36)},
		Collapse:   Rect{width - pad - d(32), d(13), d(32), d(30)},
		SearchCard: Rect{pad, d(59), inner, d(44)}, SearchInput: Rect{pad + d(43), d(70), inner - d(55), d(22)},
		ResultCard: Rect{pad, cardY, inner, cardBottom - cardY},
		Word:       Rect{pad + d(15), d(130), inner - d(135), d(33)},
		Pron:       Rect{pad + d(15), d(167), inner - d(120), d(22)},
		Origin:     Rect{width - pad - d(76), d(168), d(66), d(21)},
		Divider:    Rect{pad + d(14), d(197), inner - d(28), d(1)},
		Detail:     Rect{pad + d(15), detailY, inner - d(30), detailBottom - detailY},
		BookTitle:  Rect{pad + d(2), d(58), inner - d(120), d(33)},
		BookCount:  Rect{width - pad - d(116), d(67), d(109), d(23)},
		SavedList:  Rect{pad, listY, inner, listBottom - listY},
		Primary:    Rect{pad, actionsY, primaryW, d(37)},
		Secondary:  Rect{pad + primaryW + gap, actionsY, inner - primaryW - gap, d(37)},
		EmptyState: Rect{pad, d(130), inner, d(120)},
	}
}
