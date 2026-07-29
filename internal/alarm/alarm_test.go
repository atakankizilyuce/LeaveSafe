package alarm

import "testing"

func TestClampLevel(t *testing.T) {
	cases := []struct {
		name  string
		level float64
		want  float64
	}{
		{"silent", 0, 0},
		{"half", 0.5, 0.5},
		{"full", 1, 1},
		// A percentage above 100 in the escalation config used to reach the OS
		// audio APIs unchanged, which is rejected outright by some and wrapped by
		// others.
		{"above full", 1.5, 1},
		{"far above full", 42, 1},
		{"negative", -0.25, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampLevel(tc.level); got != tc.want {
				t.Errorf("clampLevel(%v) = %v, want %v", tc.level, got, tc.want)
			}
		})
	}
}
