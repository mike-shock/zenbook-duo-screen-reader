package reader

import "math"

// State is the sole source of truth shared by both windows. Page is one-based.
type State struct {
	Revision  uint64  `json:"revision"`
	Document  string  `json:"document"`
	Page      int     `json:"page"`
	Total     int     `json:"total"`
	Mode      string  `json:"mode"`
	Zoom      float64 `json:"zoom"`
	Fit       string  `json:"fit"`
	Font      int     `json:"font"`
	Theme     string  `json:"theme"`
	Swap      bool    `json:"swap"`
	Cover     bool    `json:"cover"`
	RTL       bool    `json:"rtl"`
	Rotation  int     `json:"rotation"`
	Scroll    float64 `json:"scroll"`
	Anchor    string  `json:"anchor"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Bookmarks []int   `json:"bookmarks"`
}

func Default() State {
	return State{Page: 1, Mode: "spread", Zoom: 1, Fit: "page", Font: 22, Theme: "paper", Bookmarks: []int{}}
}
func (s *State) Normalize() {
	if s.Total < 0 {
		s.Total = 0
	}
	if s.Page < 1 {
		s.Page = 1
	}
	if s.Total > 0 && s.Page > s.Total {
		s.Page = s.Total
	}
	if s.Mode != "scroll" {
		s.Mode = "spread"
	}
	if s.Fit != "width" {
		s.Fit = "page"
	}
	if math.IsNaN(s.Zoom) || math.IsInf(s.Zoom, 0) || s.Zoom < 0.5 {
		s.Zoom = 0.5
	}
	if s.Zoom > 3 {
		s.Zoom = 3
	}
	if s.Font < 14 {
		s.Font = 14
	}
	if s.Font > 40 {
		s.Font = 40
	}
	if s.Theme != "dark" && s.Theme != "sepia" {
		s.Theme = "paper"
	}
	if s.Scroll < 0 {
		s.Scroll = 0
	}
	if s.Scroll > 1 {
		s.Scroll = 1
	}
	s.Rotation = ((s.Rotation % 360) + 360) % 360
}
func (s State) Pair() [2]int {
	p := [2]int{s.Page, s.Page + 1}
	if s.Mode == "spread" && s.Cover {
		if s.Page == 1 {
			p = [2]int{0, 1}
		} else {
			p = [2]int{s.Page, s.Page + 1}
		}
	}
	for i := range p {
		if p[i] < 1 || (s.Total > 0 && p[i] > s.Total) {
			p[i] = 0
		}
	}
	if s.RTL {
		p[0], p[1] = p[1], p[0]
	}
	if s.Swap {
		p[0], p[1] = p[1], p[0]
	}
	return p
}
func (s *State) Move(direction int) {
	step := 1
	if s.Mode == "spread" {
		step = 2
	}
	if s.Mode == "spread" && s.Cover {
		if s.Page == 1 && direction > 0 {
			step = 1
		}
		if s.Page == 2 && direction < 0 {
			step = 1
		}
	}
	next := s.Page + direction*step
	if next < 1 || s.Total > 0 && next > s.Total {
		return
	}
	s.Page = next
	s.Scroll = 0
	s.Anchor = ""
}
