package reader

import "testing"

func TestNavigation(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		cover      bool
		total      int
		want       []int
	}{
		{"spread", "spread", false, 5, []int{1, 3, 5, 5}},
		{"scroll", "scroll", false, 4, []int{1, 2, 3, 4, 4}},
		{"cover", "spread", true, 6, []int{1, 2, 4, 6, 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Default()
			s.Mode = tc.mode
			s.Cover = tc.cover
			s.Total = tc.total
			for _, want := range tc.want {
				if s.Page != want {
					t.Fatalf("page=%d want=%d", s.Page, want)
				}
				s.Move(1)
			}
		})
	}
}
func TestPairs(t *testing.T) {
	s := Default()
	s.Total = 5
	s.Page = 5
	if got := s.Pair(); got != [2]int{5, 0} {
		t.Fatal(got)
	}
	s.Swap = true
	if got := s.Pair(); got != [2]int{0, 5} {
		t.Fatal(got)
	}
	s.RTL = true
	if got := s.Pair(); got != [2]int{5, 0} {
		t.Fatal(got)
	}
	s = Default()
	s.Cover = true
	s.Total = 1
	if s.Pair() != [2]int{0, 1} {
		t.Fatal(s.Pair())
	}
	s.Move(1)
	if s.Page != 1 {
		t.Fatal(s.Page)
	}
}
func TestNavigationReversible(t *testing.T) {
	for total := 1; total < 20; total++ {
		for _, mode := range []string{"spread", "scroll"} {
			s := Default()
			s.Mode = mode
			s.Total = total
			for s.Page < total {
				old := s
				s.Move(1)
				if s.Page == old.Page {
					break
				}
				s.Move(-1)
				if s.Page != old.Page {
					t.Fatalf("not reversible: %+v %+v", old, s)
				}
				s.Move(1)
			}
		}
	}
}
