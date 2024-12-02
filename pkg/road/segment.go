package road

import (
	"github.com/natevvv/osm-ship-routing/pkg/geometry"
)

type RoadType int

const (
	Unknown RoadType = iota
	Motorway
	Trunk
	Primary
	Secondary
	Tertiary
)

type Segment struct {
	ID       int64
	Type     RoadType
	Points   []geometry.Point
	Tags     map[string]string
	OneWay   bool
	MaxSpeed int // km/h
}

func (r RoadType) String() string {
	return []string{"Unknown", "Motorway", "Trunk", "Primary", "Secondary", "Tertiary"}[r]
}
