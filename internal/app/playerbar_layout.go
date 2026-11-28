package app

import (
	"fmt"
	"strconv"

	"github.com/e12jassir/iutui/internal/i18n"
	"github.com/mattn/go-runewidth"
)

// PlayerBarRects captures exact inclusive screen X bounds for every clickable
// control in the bottom player panel so rendering and mouse clicks share one
// single layout calculation.
type PlayerBarRects struct {
	InnerW int

	// Row 1 (y == Height-4): Queue badge on the right when no Toast is shown.
	QueueText string
	HasQueue  bool
	QueueX0   int
	QueueX1   int

	// Row 3 (y == Height-2): Left zone (Like, Save, Subscribe).
	LikeX0 int
	LikeX1 int
	SaveX0 int
	SaveX1 int
	SubX0  int
	SubX1  int

	// Row 3 (y == Height-2): Center zone (Prev, -10s, Play/Pause, +10s, Next).
	PrevX0      int
	PrevX1      int
	Back10X0    int
	Back10X1    int
	PlayPauseX0 int
	PlayPauseX1 int
	Fwd10X0     int
	Fwd10X1     int
	NextX0      int
	NextX1      int

	// Row 3 (y == Height-2): Right zone (Vol-, Vol display, Vol+).
	ShowVol   bool
	VolStr    string
	VolDownX0 int
	VolDownX1 int
	VolUpX0   int
	VolUpX1   int

	CenterStartCol int
	RightStartCol  int
}

// PlayerBarLayout computes exact positions for all player bar buttons and badges.
func (s *State) PlayerBarLayout() PlayerBarRects {
	w := s.Width
	if w < 12 {
		w = 12
	}
	innerW := max(10, w-2)

	vol := int(s.PlayerStatus.Volume + 0.5)
	if vol <= 0 {
		vol = s.AudioVolume
	}
	if vol <= 0 {
		vol = 75
	}
	volStr := fmt.Sprintf("%d%%", vol)

	r := PlayerBarRects{
		InnerW: innerW,
		VolStr: volStr,
	}

	vs := s.Catalog().Views

	// Row 1 queue badge (hidden only while an active Toast occupies the right slot).
	if s.Toast == "" {
		queueCount := len(s.Queue)
		if s.PlayerStatus.PlaylistCount > queueCount {
			queueCount = s.PlayerStatus.PlaylistCount
		}
		if queueCount > 1 {
			r.QueueText = i18n.Format(vs.PlayerQueueCountBadge, map[string]string{"n": strconv.Itoa(queueCount)})
		} else {
			r.QueueText = vs.PlayerQueueBadge
		}
		badgeMax := max(10, innerW/3)
		textW := runewidth.StringWidth(r.QueueText)
		if textW > badgeMax {
			textW = badgeMax
		}
		renderedW := textW + 2 // leading & trailing space
		r.HasQueue = true
		r.QueueX0 = 1 + max(0, innerW-renderedW)
		r.QueueX1 = innerW
	}

	// Row 3 left zone starts at screen column 2 (after border at 0 and " " at 1).
	// Tokens: "󰋕" (1) + "  " (2) + "󰥔" (1) + "  " (2) + subLabel (subW).
	subLabel := vs.PlayerSubBadge
	if s.IsActiveSubscribed() {
		subLabel = vs.PlayerSubActiveBadge
	}
	subW := max(1, runewidth.StringWidth(subLabel))
	r.LikeX0, r.LikeX1 = 1, 3
	r.SaveX0, r.SaveX1 = 4, 6
	r.SubX0, r.SubX1 = 7, 7+subW+1
	leftEndCol := 7 + subW // col inside innerW (1 leading + 6 + subW)

	// Center zone: "󰒮  󰑟  󰏤  󰑝  󰒭" => 1 + 2 + 1 + 2 + 1 + 2 + 1 + 2 + 1 = 13 cols.
	const centerW = 13
	rightText := "󰖀  " + volStr + "  󰕾 "
	rightW := runewidth.StringWidth(rightText)

	centerCol := (innerW - centerW) / 2
	if centerCol < leftEndCol+2 {
		centerCol = leftEndCol + 2
	}
	if centerCol+centerW+2+rightW <= innerW {
		r.ShowVol = true
		r.RightStartCol = innerW - rightW
	} else {
		centerCol = max(leftEndCol+2, (innerW-centerW)/2)
	}
	r.CenterStartCol = centerCol

	// Screen X = 1 (left panel border) + inner column.
	cx := 1 + centerCol
	r.PrevX0, r.PrevX1 = cx-1, cx+1
	r.Back10X0, r.Back10X1 = cx+2, cx+4
	r.PlayPauseX0, r.PlayPauseX1 = cx+5, cx+7
	r.Fwd10X0, r.Fwd10X1 = cx+8, cx+10
	r.NextX0, r.NextX1 = cx+11, cx+13

	if r.ShowVol {
		rx := 1 + r.RightStartCol
		r.VolDownX0, r.VolDownX1 = rx-1, rx+1
		upCol := rx + runewidth.StringWidth("󰖀  "+volStr+"  ")
		r.VolUpX0, r.VolUpX1 = upCol-1, upCol+1
	}

	return r
}
