package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path"
	"runtime"
	"time"

	"github.com/natevvv/osm-ship-routing/pkg/geometry"
	"github.com/natevvv/osm-ship-routing/pkg/graph"
	p "github.com/natevvv/osm-ship-routing/pkg/graph/path"
	"github.com/natevvv/osm-ship-routing/pkg/road"
)

func main() {
	buildRoadGraph := flag.String("roadgraph", "", "构建道路网络图")
	contractGraph := flag.String("contract", "", "收缩给定的图")

	// 收缩选项
	contractionLimit := flag.Float64("contraction-limit", 100, "限制收缩级别")
	contractionWorkers := flag.Int("contraction-workers", 6, "设置收缩工作线程数")
	bidirectional := flag.Bool("bidirectional", false, "使用双向计算")
	useHeuristic := flag.Bool("use-heuristic", false, "使用A*搜索进行收缩")
	maxNumSettledNodes := flag.Int("max-settled-nodes", math.MaxInt, "设置每次收缩允许的最大节点数")

	// CH顺序选项
	noLazyUpdate := flag.Bool("no-lazy-update", false, "禁用延迟更新")
	noEdgeDifference := flag.Bool("no-edge-difference", false, "禁用边差异")
	noProcessedNeighbors := flag.Bool("no-processed-neighbors", false, "禁用已处理邻居")
	periodic := flag.Bool("periodic", false, "定期重新计算收缩优先级")
	updateNeighbors := flag.Bool("update-neighbors", false, "更新已收缩节点的邻居优先级")

	// 调试选项
	dijkstraDebugLevel := flag.Int("dijkstra-debug", 0, "设置Dijkstra算法的调试级别")
	chDebugLevel := flag.Int("ch-debug", 1, "设置CH算法的调试级别")

	flag.Parse()

	if *buildRoadGraph != "" {
		roadFile := *buildRoadGraph
		createRoadGraph(roadFile)
	}

	if *contractGraph != "" {
		_, filename, _, ok := runtime.Caller(0)
		if !ok {
			log.Fatal("Error")
		}

		directory := path.Dir(filename)
		graphFile := path.Join(directory, "..", "..", "graphs", *contractGraph)

		oo := p.MakeOrderOptions().
			SetLazyUpdate(!*noLazyUpdate).
			SetEdgeDifference(!*noEdgeDifference).
			SetProcessedNeighbors(!*noProcessedNeighbors).
			SetPeriodic(*periodic).
			SetUpdateNeighbors(*updateNeighbors)

		options := p.ContractionOptions{
			Bidirectional:      *bidirectional,
			UseHeuristic:       *useHeuristic,
			MaxNumSettledNodes: *maxNumSettledNodes,
			ContractionLimit:   *contractionLimit,
			ContractionWorkers: *contractionWorkers,
		}

		debugOptions := struct {
			dijkstra int
			ch       int
		}{dijkstra: *dijkstraDebugLevel, ch: *chDebugLevel}

		createContractedGraph(graphFile, oo, options, debugOptions)
	}
}

func createRoadGraph(roadFile string) {
	start := time.Now()
	roads := loadRoadJson(roadFile)
	elapsed := time.Since(start)
	fmt.Printf("[TIME] 加载道路数据: %s\n", elapsed)

	start = time.Now()
	g := graph.NewAdjacencyListGraph()

	// 为每个道路点创建图节点
	pointToNode := make(map[geometry.Point]int)
	for _, segment := range roads {
		for _, point := range segment.Points {
			if _, exists := pointToNode[point]; !exists {
				g.AddNode(point)
				nodeID := g.NodeCount() - 1
				pointToNode[point] = nodeID
			}
		}
	}

	// 添加边
	for _, segment := range roads {
		for i := 0; i < len(segment.Points)-1; i++ {
			from := pointToNode[segment.Points[i]]
			to := pointToNode[segment.Points[i+1]]

			// 计算两点间的距离作为权重
			weight := segment.Points[i].DistanceTo(segment.Points[i+1])

			// 添加边，包含道路类型信息
			g.AddArc(from, to, int(weight*1000))
			g.SetRoadType(from, to, segment.Type.String())

			// 如果不是单向路，添加反向边
			if !segment.OneWay {
				g.AddArc(to, from, int(weight*1000))
				g.SetRoadType(to, from, segment.Type.String())
			}
		}
	}

	elapsed = time.Since(start)
	fmt.Printf("[TIME] 构建图: %s\n", elapsed)
	fmt.Printf("节点数量: %d\n", g.NodeCount())
	fmt.Printf("边数量: %d\n", g.ArcCount())

	// 导出基础图
	start = time.Now()
	graph.WriteFmi(g, "road_graph.fmi")
	elapsed = time.Since(start)
	fmt.Printf("[TIME] 导出基础图: %s\n", elapsed)

	// 创建并导出收缩层次结构
	start = time.Now()
	ch := p.NewContractionHierarchies(g, p.NewUniversalDijkstra(g), p.MakeDefaultContractionOptions())
	ch.SetDebugLevel(1)

	// 设置有效的排序选项
	orderOptions := p.MakeOrderOptions().
		SetLazyUpdate(true).         // 使用延迟更新
		SetEdgeDifference(true).     // 考虑边差异
		SetProcessedNeighbors(true). // 考虑已处理的邻居
		SetPeriodic(true)            // 定期更新

	ch.Precompute(nil, orderOptions)
	ch.WriteContractionResult()
	elapsed = time.Since(start)
	fmt.Printf("[TIME] 导出收缩层次结构: %s\n", elapsed)
}

func loadRoadJson(file string) []*road.Segment {
	bytes, err := os.ReadFile(file)
	if err != nil {
		panic(err)
	}

	var roads []*road.Segment
	err = json.Unmarshal(bytes, &roads)
	if err != nil {
		panic(err)
	}

	return roads
}

func createContractedGraph(graphFile string, oo p.OrderOptions, options p.ContractionOptions, debugOptions struct {
	dijkstra int
	ch       int
}) {
	log.Printf("Read graph file\n")
	alg := graph.NewAdjacencyListFromFmiFile(graphFile)
	dijkstra := p.NewUniversalDijkstra(alg)
	dijkstra.SetDebugLevel(debugOptions.dijkstra)
	log.Printf("Initialize Contraction Hierarchies\n")
	ch := p.NewContractionHierarchies(alg, dijkstra, options)
	ch.SetDebugLevel(debugOptions.ch)
	ch.SetPrecomputationMilestones([]float64{0, 10, 20, 30, 40, 50, 60, 70, 80, 90, 91, 92, 93, 94, 95, 96, 97, 98, 99, 99.99})
	log.Printf("Initialized Contraction Hierarchies, start precomputation\n")
	ch.Precompute(nil, oo)
	log.Printf("Finished computation\n")
	ch.WriteContractionResult()
	log.Printf("Finished Contraction\n")
}
