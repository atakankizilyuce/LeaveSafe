package location

import "math"

// earthRadiusM is the mean radius of the Earth. Distances here are compared
// against accuracy radii measured in hundreds of meters, so the error from
// treating the Earth as a sphere is far below the noise.
const earthRadiusM = 6371008.8

// DistanceM returns the great-circle distance in meters between two fixes.
func DistanceM(a, b *Fix) float64 {
	if a == nil || b == nil {
		return 0
	}
	return haversineM(a.Latitude, a.Longitude, b.Latitude, b.Longitude)
}

func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δφ := (lat2 - lat1) * math.Pi / 180
	Δλ := (lon2 - lon1) * math.Pi / 180

	h := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)

	return 2 * earthRadiusM * math.Asin(math.Sqrt(math.Min(1, h)))
}
