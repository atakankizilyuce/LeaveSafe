package location

import (
	"math"
	"testing"
)

// TestDistanceM checks the haversine against distances that can be looked up
// independently, so an error in the formula shows up as a wrong number rather
// than as a plausible one.
func TestDistanceM(t *testing.T) {
	tests := []struct {
		name              string
		lat1, lon1        float64
		lat2, lon2        float64
		wantM, toleranceM float64
	}{
		{
			name: "identical points",
			lat1: 41.0, lon1: 29.0,
			lat2: 41.0, lon2: 29.0,
			wantM: 0, toleranceM: 0.001,
		},
		{
			name: "Istanbul to Ankara",
			lat1: 41.0082, lon1: 28.9784,
			lat2: 39.9334, lon2: 32.8597,
			wantM: 351000, toleranceM: 3000,
		},
		{
			name: "London to Paris",
			lat1: 51.5074, lon1: -0.1278,
			lat2: 48.8566, lon2: 2.3522,
			wantM: 343500, toleranceM: 3000,
		},
		{
			name: "one degree of latitude at the equator",
			lat1: 0, lon1: 0,
			lat2: 1, lon2: 0,
			wantM: 111195, toleranceM: 200,
		},
		{
			name: "across the antimeridian",
			lat1: 0, lon1: 179.9,
			lat2: 0, lon2: -179.9,
			wantM: 22239, toleranceM: 100,
		},
		{
			name: "a short walk",
			lat1: 41.0, lon1: 29.0,
			lat2: 41.0072, lon2: 29.0,
			wantM: 800, toleranceM: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := haversineM(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if math.Abs(got-tt.wantM) > tt.toleranceM {
				t.Errorf("distance = %.0f m, want %.0f ± %.0f", got, tt.wantM, tt.toleranceM)
			}
		})
	}
}

func TestDistanceM_IsSymmetric(t *testing.T) {
	a := &Fix{Latitude: 41.0082, Longitude: 28.9784}
	b := &Fix{Latitude: 39.9334, Longitude: 32.8597}

	if math.Abs(DistanceM(a, b)-DistanceM(b, a)) > 0.001 {
		t.Error("distance is not symmetric")
	}
}

func TestDistanceM_NilIsZero(t *testing.T) {
	a := &Fix{Latitude: 41.0, Longitude: 29.0}
	if DistanceM(a, nil) != 0 || DistanceM(nil, a) != 0 || DistanceM(nil, nil) != 0 {
		t.Error("a missing fix should measure zero rather than a distance from null island")
	}
}
