package routing

import (
	"log"
	"math"
	"strings"

	"github.com/natevvv/osm-ship-routing/pkg/geometry"
	"github.com/natevvv/osm-ship-routing/pkg/graph"
	"github.com/natevvv/osm-ship-routing/pkg/graph/path"
)

// 路由配置
type RouteConfig struct {
	VehicleType     string // 车辆类型：car, truck 等
	AvoidTolls      bool   // 是否避开收费路段
	PreferHighway   bool   // 是否偏好高速公路
	ConsiderTraffic bool   // 是否考虑交通状况
	MaxSpeed        int    // 最大速度限制 (km/h)
}

// 路由结果
type Route struct {
	Origin      geometry.Point   // 起点
	Destination geometry.Point   // 终点
	Exists      bool             // 是否存在可行路径
	Waypoints   []geometry.Point // 路径点序列
	Length      int              // 路径长度（米）
}

// 路由器
type Router struct {
	graph           graph.Graph
	contractedGraph graph.Graph
	shortcuts       []path.Shortcut
	nodeOrdering    [][]int
	navigator       path.Navigator
	searchSpace     []geometry.Point
}

// 创建新的路由器
func NewRouter(g, contractedGraph graph.Graph,
	shortcuts []path.Shortcut, nodeOrdering [][]int,
	navigator *string) *Router {

	r := &Router{
		graph:           g,
		contractedGraph: contractedGraph,
		shortcuts:       shortcuts,
		nodeOrdering:    nodeOrdering,
	}

	if navigator != nil {
		if !r.SetNavigator(*navigator) {
			log.Fatal("Could not set navigator")
		}
	}

	return r
}

// 设置导航算法
func (r *Router) SetNavigator(navigatorType string) bool {
	switch navigatorType {
	case "contraction-hierarchies":
		r.navigator = path.NewContractionHierarchies(r.graph, path.NewUniversalDijkstra(r.graph), path.ContractionOptions{})
	case "dijkstra":
		r.navigator = path.NewUniversalDijkstra(r.graph)
	case "astar":
		dijkstra := path.NewUniversalDijkstra(r.graph)
		dijkstra.SetUseHeuristic(true)
		r.navigator = dijkstra
	case "bidirectional-dijkstra":
		dijkstra := path.NewUniversalDijkstra(r.graph)
		dijkstra.SetBidirectional(true)
		r.navigator = dijkstra
	default:
		return false
	}
	return true
}

// 计算路径
func (r *Router) ComputeRoute(origin, destination geometry.Point, config RouteConfig) Route {
	originNode := r.findNearestNode(origin)
	destNode := r.findNearestNode(destination)

	// 创建一个临时的权重映射
	weights := make(map[int]map[int]int)
	for source := 0; source < r.graph.NodeCount(); source++ {
		weights[source] = make(map[int]int)
		arcs := r.graph.GetArcsFrom(source)
		for _, arc := range arcs {
			weights[source][arc.To] = r.getEdgeWeight(source, arc.To, config)
		}
	}

	// 使用自定义权重计算路径
	distance := r.navigator.ComputeShortestPathWithWeights(originNode, destNode, weights)
	if distance == math.MaxInt32 {
		return Route{Origin: origin, Destination: destination, Exists: false}
	}

	path := r.navigator.GetPath(originNode, destNode)
	r.searchSpace = r.GetSearchSpace()

	// 计算实际距离（而不是时间权重）
	actualDistance := 0
	for i := 0; i < len(path)-1; i++ {
		arcs := r.graph.GetArcsFrom(path[i])
		for _, arc := range arcs {
			if arc.To == path[i+1] {
				actualDistance += arc.Distance
				break
			}
		}
	}

	return Route{
		Origin:      origin,
		Destination: destination,
		Exists:      true,
		Length:      actualDistance,
		Waypoints:   r.buildWaypoints(path),
	}
}

// 获取所有节点
func (r *Router) GetNodes() []geometry.Point {
	return r.graph.GetNodes()
}

// 获取搜索空间
func (r *Router) GetSearchSpace() []geometry.Point {
	searchItems := r.navigator.GetSearchSpace()
	points := make([]geometry.Point, 0, len(searchItems))
	for _, item := range searchItems {
		if point := r.graph.GetNode(item.NodeId()); point != nil {
			points = append(points, *point)
		}
	}
	return points
}

// 查找最近节点
func (r *Router) findNearestNode(point geometry.Point) int {
	minDist := math.MaxFloat64
	nearestNode := 0
	for i := 0; i < r.graph.NodeCount(); i++ {
		if node := r.graph.GetNode(i); node != nil {
			dist := point.DistanceTo(*node)
			if dist < minDist {
				minDist = dist
				nearestNode = i
			}
		}
	}
	return nearestNode
}

// 构建路径点
func (r *Router) buildWaypoints(path []int) []geometry.Point {
	waypoints := make([]geometry.Point, 0, len(path))
	for _, nodeID := range path {
		if point := r.graph.GetNode(nodeID); point != nil {
			waypoints = append(waypoints, *point)
		}
	}
	return waypoints
}

// 在 Router 中添加速度计算
func (r *Router) getEdgeWeight(from, to int, config RouteConfig) int {
	distance := r.graph.GetArcsFrom(from)[to].Distance

	// 根据道路类型和车辆类型计算基础速度
	baseSpeed := r.getBaseSpeed(from, to, config.VehicleType)
	if config.PreferHighway {
		// 如果偏好高速，给高速路更低的权重
		baseSpeed = baseSpeed * 1.2
	}

	// 应用速度限制
	if config.MaxSpeed > 0 && baseSpeed > float64(config.MaxSpeed) {
		baseSpeed = float64(config.MaxSpeed)
	}

	// 转换距离为时间权重（秒）
	timeWeight := int(float64(distance) / (baseSpeed * 0.277778)) // 转换 km/h 到 m/s

	return timeWeight
}

// 根据道路类型和车辆类型获取基础速度
func (r *Router) getBaseSpeed(from, to int, vehicleType string) float64 {
	// 获取道路类型（假设存储在边的属性中）
	arcs := r.graph.GetArcsFrom(from)
	for _, arc := range arcs {
		if arc.To == to {
			// 根据道路类型和车辆类型确定基础速度
			switch vehicleType {
			case "car":
				switch getRoadType(arc) {
				case "motorway":
					return 120.0 // 高速公路
				case "trunk":
					return 100.0 // 主干道
				case "primary":
					return 80.0 // 一级公路
				case "secondary":
					return 60.0 // 二级公路
				case "tertiary":
					return 40.0 // 三级公路
				default:
					return 30.0 // 其他道路
				}
			case "truck":
				switch getRoadType(arc) {
				case "motorway":
					return 100.0
				case "trunk":
					return 80.0
				case "primary":
					return 60.0
				default:
					return 40.0
				}
			case "bus":
				switch getRoadType(arc) {
				case "motorway":
					return 100.0
				case "trunk":
					return 80.0
				default:
					return 50.0
				}
			case "motorcycle":
				switch getRoadType(arc) {
				case "motorway":
					return 100.0
				default:
					return 60.0
				}
			case "bicycle":
				return 25.0 // 所有道路类型的自行车速度基本相同
			default:
				return 60.0 // 默认速度
			}
		}
	}
	return 60.0 // 如果找不到对应的边，返回默认速度
}

// 获取道路类型
func getRoadType(arc graph.Arc) string {
	if arc.RoadType == "" {
		return "unclassified"
	}
	return strings.ToLower(arc.RoadType) // 转换为小写以匹配标准格式
}
