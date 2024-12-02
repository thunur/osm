package graph

import (
	"fmt"
	"strings"

	geo "github.com/natevvv/osm-ship-routing/pkg/geometry"
)

// Implementation for static graphs
type AdjacencyArrayGraph struct {
	Nodes   []geo.Point
	arcs    []Arc
	Offsets []int
}

// Create an AdjacencyArrayGraph from the given graph
func NewAdjacencyArrayFromGraph(g Graph) *AdjacencyArrayGraph {
	nodes := make([]geo.Point, 0)
	arcs := make([]Arc, 0)
	offsets := make([]int, g.NodeCount()+1)

	for i := 0; i < g.NodeCount(); i++ {
		// add node
		nodes = append(nodes, *g.GetNode(i))

		// add all edges of node
		arcs = append(arcs, g.GetArcsFrom(i)...)

		// set stop-offset
		offsets[i+1] = len(arcs)
	}

	aag := AdjacencyArrayGraph{Nodes: nodes, arcs: arcs, Offsets: offsets}
	return &aag
}

// Get the node for the given id
func (aag *AdjacencyArrayGraph) GetNode(id NodeId) *geo.Point {
	if id < 0 || id >= aag.NodeCount() {
		panic(fmt.Sprintf("NodeId %d is not contained in the graph.", id))
	}
	return &aag.Nodes[id]
}

// get all nodes of the graph
func (aag *AdjacencyArrayGraph) GetNodes() []geo.Point {
	return aag.Nodes
}

// Get the Arcs for the given node id
func (aag *AdjacencyArrayGraph) GetArcsFrom(id NodeId) []Arc {
	if id < 0 || id >= aag.NodeCount() {
		panic(fmt.Sprintf("NodeId %d is not contained in the graph.", id))
	}
	return aag.arcs[aag.Offsets[id]:aag.Offsets[id+1]]
}

func (aag *AdjacencyArrayGraph) SortArcs() {
	// 由于我们现在使用 RoadType 而不是 arcFlag，
	// 我们按照道路类型的优先级排序
	for i := 0; i < aag.NodeCount(); i++ {
		start := aag.Offsets[i]
		end := aag.Offsets[i+1]
		for j := start; j < end-1; j++ {
			for k := j + 1; k < end; k++ {
				// 按道路类型优先级排序（高速公路优先）
				if getRoadTypePriority(aag.arcs[j].RoadType) < getRoadTypePriority(aag.arcs[k].RoadType) {
					// 交换位置
					aag.arcs[j], aag.arcs[k] = aag.arcs[k], aag.arcs[j]
				}
			}
		}
	}
}

// 添加辅助函数来获取道路类型的优先级
func getRoadTypePriority(roadType string) int {
	switch roadType {
	case "motorway":
		return 5
	case "trunk":
		return 4
	case "primary":
		return 3
	case "secondary":
		return 2
	case "tertiary":
		return 1
	default:
		return 0
	}
}

// Returns the number of Nodes in the graph
func (aag *AdjacencyArrayGraph) NodeCount() int {
	return len(aag.Nodes)
}

// Returns the total number of arcs in the graph
func (aag *AdjacencyArrayGraph) ArcCount() int {
	return len(aag.arcs)
}

// Returns a human readable string of the graph
func (aag *AdjacencyArrayGraph) AsString() string {
	var sb strings.Builder

	// write number of nodes and number of edges
	sb.WriteString(fmt.Sprintf("%v\n", aag.NodeCount()))
	sb.WriteString(fmt.Sprintf("%v\n", aag.ArcCount()))

	sb.WriteString("#Nodes\n")
	// list all nodes structured as "id lat lon"
	for i := 0; i < aag.NodeCount(); i++ {
		node := aag.GetNode(i)
		sb.WriteString(fmt.Sprintf("%v %v %v\n", i, node.Lat(), node.Lon()))
	}

	sb.WriteString("#Edges\n")
	// list all edges structured as "fromId targetId distance"
	for i := 0; i < aag.NodeCount(); i++ {
		for _, arc := range aag.GetArcsFrom(i) {
			sb.WriteString(fmt.Sprintf("%v %v %v\n", i, arc.Destination(), arc.Cost()))
		}
	}
	return sb.String()
}

// 修改 SetArcFlags 方法
func (aag *AdjacencyArrayGraph) SetArcFlags(id NodeId, flag bool) {
	// 在新的实现中，我们不再使用 arcFlag
	// 这个方法可以用来更新道路类型
	// 暂时保留为空实现或者可以删除这个方法
}
