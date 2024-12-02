package geometry

import "math"

const earthRadius = 6371e3 // unit meter

type Point [2]float64

// Create a new Point with latitude and longitude (both in degree)
func NewPoint(lat, lon float64) *Point {
	return &Point{lat, lon}
}

func MakePoint(lat, lon float64) Point {
	return Point{lat, lon}
}

func NewPointFromBearing(initialPoint *Point, bearing float64, distance float64) *Point {
	phi := math.Asin(math.Sin(initialPoint.Phi())*math.Cos(distance/earthRadius) + math.Cos(initialPoint.Phi())*math.Sin(distance/earthRadius)*math.Cos(bearing))
	lambda := initialPoint.Lambda() + math.Atan2(math.Sin(bearing)*math.Sin(distance/earthRadius)*math.Cos(initialPoint.Phi()),
		math.Cos(distance/earthRadius)-math.Sin(initialPoint.Phi())*math.Sin(phi))
	return NewPoint(Rad2Deg(phi), Rad2Deg(lambda))
}

// latitude in degree
func (p *Point) Lat() float64 {
	return p[0]
}

// longitude in degree
func (p *Point) Lon() float64 {
	return p[1]
}

// Latitude in radian
func (p *Point) Phi() float64 {
	return Deg2Rad(p.Lat())
}

// Longitude in radian
func (p *Point) Lambda() float64 {
	return Deg2Rad(p.Lon())
}

func (p *Point) X() float64 {
	return earthRadius * math.Sin(p.Phi()) * math.Cos(p.Lambda())
}

func (p *Point) Y() float64 {
	return earthRadius * math.Sin(p.Phi()) * math.Sin(p.Lambda())
}

func (p *Point) Z() float64 {
	return earthRadius * math.Cos(p.Phi())
}

// The great circle distance
func (first *Point) Haversine(second *Point) float64 {
	// one can reduce one function call/calculation by directly substraction the latitudes/longitudes and then convert to radian:
	// (point.Lat() - p.Lat()) * math.Pi / 180
	// (point.Lon() - p.Lon()) * math.Pi / 180
	// But this is probably not worth to improve
	deltaPhi := second.Phi() - first.Phi()
	deltaLambda := second.Lambda() - first.Lambda()

	a := math.Pow(math.Sin(deltaPhi/2), 2) + math.Cos(first.Phi())*math.Cos(second.Phi())*math.Pow(math.Sin(deltaLambda/2), 2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadius * c
}

func (first *Point) IntHaversine(second *Point) int {
	return int(first.Haversine(second))
}

// Calculate the distance with the Spherical Law of Cosines.
// This is a roughly simpler formula (which may improve the performance).
// In other tests however, it took a  bit longer
func (first *Point) SphericalCosineDistance(second *Point) float64 {
	// may be better to only calculate Phi only once for each point and store in local variable. But probably no big improvements
	return math.Acos(math.Sin(first.Phi())*math.Sin(second.Phi())+math.Cos(first.Phi())*math.Cos(second.Phi())*math.Cos(second.Lambda()-first.Lambda())) * earthRadius
}

// Bearing from one point to another point in degree
func (first *Point) Bearing(second *Point) float64 {
	y := math.Sin(second.Lambda()-first.Lambda()) * math.Cos(second.Phi())
	x := math.Cos(first.Phi())*math.Sin(second.Phi()) -
		math.Sin(first.Phi())*math.Cos(second.Phi())*math.Cos(second.Lambda()-first.Lambda())
	theta := math.Atan2(y, x)
	bearing := math.Mod(Rad2Deg(theta)+360, 360) // bearing in degrees
	return bearing
}

// Half-way point along a great circle path between two points
func (first *Point) Midpoint(second *Point) *Point {
	Bx := math.Cos(second.Phi()) * math.Cos(second.Lambda()-first.Lambda())
	By := math.Cos(second.Phi()) * math.Sin(second.Lambda()-first.Lambda())
	phi := math.Atan2(math.Sin(first.Phi())+math.Sin(second.Phi()),
		math.Sqrt(math.Pow(math.Cos(first.Phi())+Bx, 2)+math.Pow(By, 2)))
	lambda := first.Lambda() + math.Atan2(By, math.Cos(first.Phi())+Bx)
	return NewPoint(Rad2Deg(phi), Rad2Deg(lambda))
	// The longitude can be normalised to −180…+180 using (lon+540)%360-180
}

func (first *Point) LatitudeOnLineAtLon(second *Point, lon float64) float64 {
	lambda := Deg2Rad(lon)
	phi := first.Phi() + ((lambda-first.Lambda())/(second.Lambda()-first.Lambda()))*(second.Phi()-first.Phi())
	return Rad2Deg(phi)
}

func (first *Point) LatOfCrossingPoint(second *Point, lon float64) float64 {
	phi := first.Phi() + ((second.Phi()-first.Phi())/(second.Lambda()-first.Lambda()))*(Deg2Rad(lon)-first.Lambda())
	return Rad2Deg(phi)
}

func (first *Point) GreatCircleLatOfCrossingPoint(second *Point, lon float64) float64 {
	tanPhi := math.Tan(first.Phi())*(math.Sin(Deg2Rad(lon)-second.Lambda())/math.Sin(first.Lambda()-second.Lambda())) - math.Tan(second.Phi())*(math.Sin(Deg2Rad(lon)-first.Lambda())/math.Sin(first.Lambda()-second.Lambda()))
	phi := math.Atan(tanPhi)
	return Rad2Deg(phi)
}

func (p Point) DistanceTo(other Point) float64 {
	const R = 6371.0 // 地球半径，单位：公里

	lat1 := p[0] * math.Pi / 180.0
	lat2 := other[0] * math.Pi / 180.0
	dLat := lat2 - lat1
	dLon := (other[1] - p[1]) * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}
