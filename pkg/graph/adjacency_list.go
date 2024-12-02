package graph

import (
	"github.com/natevvv/osm-ship-routing/pkg/geometry"
)

type AdjacencyList struct {
	nodes  [][]Arc
	coords []geometry.Point
}

func NewAdjacencyList() *AdjacencyList {
	return &AdjacencyList{
		nodes:  make([][]Arc, 0),
		coords: make([]geometry.Point, 0),
	}
}

func (g *AdjacencyList) AddNodeWithCoord(p geometry.Point) int {
	g.nodes = append(g.nodes, make([]Arc, 0))
	g.coords = append(g.coords, p)
	return len(g.nodes) - 1
}

func (g *AdjacencyList) AddEdge(from, to int, weight float64, roadType string) {
	g.nodes[from] = append(g.nodes[from], Arc{
		To:       to,
		Distance: int(weight * 1000),
		RoadType: roadType,
	})
}

func (g *AdjacencyList) NumNodes() int {
	return len(g.nodes)
}

func (g *AdjacencyList) NumEdges() int {
	count := 0
	for _, edges := range g.nodes {
		count += len(edges)
	}
	return count
}

func (g *AdjacencyList) ArcCount() int {
	return g.NumEdges()
}

func (g *AdjacencyList) AsString() string {
	return GraphAsString(g)
}

func (g *AdjacencyList) GetNode(id NodeId) *geometry.Point {
	if id >= len(g.coords) {
		return nil
	}
	p := g.coords[id]
	return &p
}

func (g *AdjacencyList) GetNodes() []geometry.Point {
	return g.coords
}

func (g *AdjacencyList) GetArcsFrom(id NodeId) []Arc {
	if id >= len(g.nodes) {
		return nil
	}
	return g.nodes[id]
}

func (g *AdjacencyList) NodeCount() int {
	return len(g.nodes)
}

func (g *AdjacencyList) SortArcs() {
	// TODO: 实现边的排序
}
