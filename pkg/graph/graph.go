package graph

import (
	"fmt"
	"strings"

	geo "github.com/natevvv/osm-ship-routing/pkg/geometry"
)

type NodeId = int

type Graph interface {
	GetNode(id NodeId) *geo.Point
	GetNodes() []geo.Point
	GetArcsFrom(id NodeId) []Arc
	NodeCount() int
	ArcCount() int
	AsString() string
	SortArcs()
}

type DynamicGraph interface {
	Graph
	AddNode(n geo.Point)
	AddArc(from, to NodeId, distance int) bool
}

type Edge struct {
	From     NodeId
	To       NodeId
	Distance int
	arcFlag  bool
}

type Arcs = []Arc

func NewEdge(to, from NodeId, distance int, arcFlag bool) *Edge {
	return &Edge{To: to, From: from, Distance: distance, arcFlag: arcFlag}
}

func MakeEdge(from, to NodeId, distance int, arcFlag bool) Edge {
	return Edge{To: to, From: from, Distance: distance, arcFlag: arcFlag}
}

func NewArc(to NodeId, distance int, roadType string) *Arc {
	return &Arc{To: to, Distance: distance, RoadType: roadType}
}

func MakeArc(to NodeId, distance int, roadType string) Arc {
	return Arc{To: to, Distance: distance, RoadType: roadType}
}

func (e Edge) Destination() NodeId {
	return e.To
}

func (e Edge) Cost() int {
	return e.Distance
}

func (e Edge) Invert() Edge {
	return Edge{From: e.To, To: e.From, Distance: e.Distance}
}

func (e Edge) ArcFlag() bool {
	return e.arcFlag
}

func (e *Edge) SetArcFlag(flag bool) {
	e.arcFlag = flag
}

func (a Arc) Destination() NodeId {
	return a.To
}

func (a Arc) Cost() int {
	return a.Distance
}

func GraphAsString(g Graph) string {
	var sb strings.Builder

	// write number of nodes and number of edges
	sb.WriteString(fmt.Sprintf("%v\n", g.NodeCount()))
	sb.WriteString(fmt.Sprintf("%v\n", g.ArcCount()))

	// list all nodes structured as "id lat lon"
	for i := 0; i < g.NodeCount(); i++ {
		node := g.GetNode(i)
		sb.WriteString(fmt.Sprintf("%v %v %v\n", i, node.Lat(), node.Lon()))
	}

	// list all edges structured as "fromId targetId distance"
	for i := 0; i < g.NodeCount(); i++ {
		for _, arc := range g.GetArcsFrom(i) {
			sb.WriteString(fmt.Sprintf("%v %v %v\n", i, arc.Destination(), arc.Cost()))
		}
	}
	return sb.String()
}
