package path

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/natevvv/osm-ship-routing/pkg/graph"
	"github.com/natevvv/osm-ship-routing/pkg/queue"
	"github.com/natevvv/osm-ship-routing/pkg/slice"
)

// Provides the precomputation and query for shortest paths with the use of Contraction Hierarchies.
// Implements the Navigator Interface.
type ContractionHierarchies struct {
	g                  graph.Graph          // graph to work on (for precomputation, this ha sto be a graph.DynamicGraph)
	dg                 graph.DynamicGraph   // Dynamic graph which is used for precomputation (to add shortcuts)
	dijkstra           *UniversalDijkstra   // the dijkstra algorithm to perform the searches
	contractionWorkers []*UniversalDijkstra // the elements/workers who perform the (potential parallel) contraction of nodes

	nodeOrdering    [][]graph.NodeId     // the node ordering (in which order the nodes were contracted)
	orderOfNode     []int                // the order of the node ("reverse" node ordering). At which position the specified node was contracted
	contractedNodes slice.FixedSizeSlice // indicates if node (index = id) is contracted

	contractionLevelLimit float64              // percentage, how many nodes should get contracted
	contractionProgress   ContractionProgress  // for some debugging
	precomputedResults    []*ContractionResult // contains the Results from "previous" runs which are still valid

	shortcutMap    []map[graph.NodeId]Shortcut // slice of map of the shortcuts (from/source -> to/target -> via). Index: nodeId of source node, map key: nodeId of target node, map value: complete Shortcut description
	addedShortcuts []int                       // debug information - stores the number of how many nodes introduced the specified amount of shortcuts. Key/Index is the number of shortcuts, value is how often they were created

	debugLevel           int    // the debug level - used for printing some informaiton
	graphFilename        string // the filename were the file gets stored
	shortcutsFilename    string // the filename were the shourtcuts gets stored
	nodeOrderingFilename string // the filname were the node ordering gets stored

	// search items needed for (manual) path calculation
	forwardSearch  SearchStats
	backwardSearch SearchStats
	connection     graph.NodeId
	searchKPIs     SearchKPIs
}

// Contains the contraction progress information
type ContractionProgress struct {
	initialTime        time.Time   // store the initial time
	milestones         []float64   // storage for the target milestones
	achievedMilestones []Milestone // storage for the achieved milestones
	milestoneFilename  string      // filename where to store the milestones
}

// Contains information for a milestone. Helpful for debugging/storing progress
type Milestone struct {
	runtime   time.Duration // time needed until this milestone is reached
	shortcuts int           // shortcuts which were introduced until this milestone
}

// Describes a shortcut.
// It contains the information of the source and target node and via which node this shortcut is spanned
type Shortcut struct {
	source graph.NodeId // the source node
	target graph.NodeId // the target node
	via    graph.NodeId // over which node this shortcut is spanned
	cost   int          // cost of the shortcut
}

// Contrains the result of a (computed, virtual) node contraction
type ContractionResult struct {
	nodeId              graph.NodeId // the node which gets contracted
	shortcuts           []Shortcut   // the necessary shortcuts
	incidentEdges       int          // number of (active) incident edges
	contractedNeighbors int          // number of contracted neighbors
}

// Collection/Options how to search for the path.
// Can be used as tuning parameters
type PathFindingOptions struct {
	Manual        bool // do an independent forward and backward search and check the connection manually
	UseHeuristic  bool // TODO use astar
	StallOnDemand int  // define the level for stall on demand (See UniversalDijkstra for more information)
	SortArcs      bool // sort the arcs to only consider the active edges
}

// Colleciton/Options how to contract the nodes.
// can be used as tuning options
type ContractionOptions struct {
	Bidirectional      bool    // use bidirectional search
	UseHeuristic       bool    // use astar
	HotStart           bool    // use hot start
	MaxNumSettledNodes int     // limit the number of settled nodes. If this is reached a shortcut is added
	ContractionLimit   float64 // upper bound (in %) until which limit the nodes are contracted. If this is 99, only the contraction from level 0 to 99 is calculated
	ContractionWorkers int     // set the number of contraction workers who parallely compute the contraction
	UseCache           bool    // use a cache to store already known shortcuts for uncontracted nodes
}

// Make default contraction options
func MakeDefaultContractionOptions() ContractionOptions {
	// TODO test bidirectional=true, useHeuristic=true, maxNumSettledNodes=60 (or something else)
	return ContractionOptions{Bidirectional: false, UseHeuristic: false, HotStart: true, MaxNumSettledNodes: math.MaxInt, ContractionLimit: 100, ContractionWorkers: 1, UseCache: false}
}

// Make default path finding options
func MakeDefaultPathFindingOptions() PathFindingOptions {
	return PathFindingOptions{Manual: false, UseHeuristic: false, StallOnDemand: 2, SortArcs: false}
}

// Create a new Contraction Hierarchy.
// Before a query can get executed, the Precomputation has to be done
func NewContractionHierarchies(g graph.Graph, dijkstra *UniversalDijkstra, options ContractionOptions) *ContractionHierarchies {
	cw := make([]*UniversalDijkstra, 0, options.ContractionWorkers)
	for i := 0; i < options.ContractionWorkers; i++ {
		worker := NewUniversalDijkstra(g)
		worker.SetHotStart(options.HotStart)
		worker.SetBidirectional(options.Bidirectional)
		worker.SetUseHeuristic(options.UseHeuristic)
		worker.SetMaxNumSettledNodes(options.MaxNumSettledNodes)
		cw = append(cw, worker)
	}

	ch := &ContractionHierarchies{
		g:                     g,
		dijkstra:              dijkstra,
		contractionWorkers:    cw,
		contractionLevelLimit: options.ContractionLimit,
		graphFilename:         "contracted_graph.fmi",
		shortcutsFilename:     "shortcuts.txt",
		nodeOrderingFilename:  "node_ordering.txt",
		precomputedResults:    make([]*ContractionResult, g.NodeCount()),
	}

	ch.contractionProgress.milestoneFilename = "milestones.txt"
	return ch
}

// Create a new Contraction Hierarchy which is already initialized with the shortcuts and node ordering.
// This can directly start a new query
func NewContractionHierarchiesInitialized(g graph.Graph, dijkstra *UniversalDijkstra, shortcuts []Shortcut, nodeOrdering [][]int, pathFindingOptions PathFindingOptions) *ContractionHierarchies {
	ch := NewContractionHierarchies(g, dijkstra, MakeDefaultContractionOptions()) // use default contraction options (they are not used anyway)
	ch.SetShortcuts(shortcuts)
	ch.SetNodeOrdering(nodeOrdering)
	ch.ShortestPathSetup(pathFindingOptions)
	return ch
}

// Do the precomputation of the Contraction Hierarchies.
// This adds new Edges in the graph by adding shortcuts. The result will be a modified graph, a collection of shortcuts and the calculated node ordering.
// If givenNodeOrder is not nil, the OrderOption oo are ignored.
// givenNodeOrder predefines the order of the nodes.
// oo defines how the node ordering will be calculated.
func (ch *ContractionHierarchies) Precompute(givenNodeOrder []int, oo OrderOptions) {
	ch.contractionProgress.initialTime = time.Now()

	ch.addedShortcuts = make([]int, 0)
	ch.contractedNodes = slice.MakeFixedSizeSlice(ch.g.NodeCount())
	ch.nodeOrdering = make([][]int, 0)
	ch.orderOfNode = make([]int, ch.g.NodeCount())
	for i := range ch.orderOfNode {
		ch.orderOfNode[i] = -1
	}

	if ch.precomputedResults != nil {
		// Reset cache
		ch.precomputedResults = make([]*ContractionResult, ch.g.NodeCount())
	}

	if givenNodeOrder == nil && !oo.IsValid() {
		panic("Order Options are not valid")
	}
	dg, ok := ch.g.(graph.DynamicGraph)
	if !ok {
		panic("Given Graph is no dynamic graph. Adding Arcs will not be possible")
	}
	ch.dg = dg
	if givenNodeOrder != nil {
		// ignore the order options
		// -> just overwrite them
		oo = MakeOrderOptions().SetLazyUpdate(false).SetRandom(false).SetEdgeDifference(false).SetProcessedNeighbors(false).SetRandom(false)
	}
	if ch.debugLevel >= 1 {
		log.Printf("Compute Node Ordering\n")
	}

	minHeap := ch.computeInitialNodeOrder(givenNodeOrder, oo)
	fmt.Println("minHeap长度", len(minHeap.Queue))
	if ch.debugLevel >= 3 {
		log.Printf("Initial computed order:\n%v\n", minHeap)
	}

	if ch.debugLevel >= 1 {
		log.Printf("Contract Nodes\n")
	}

	shortcuts := ch.contractNodes(minHeap, oo, givenNodeOrder != nil)

	if ch.debugLevel >= 1 {
		log.Printf("Shortcuts:\n")
		for amount, frequency := range ch.addedShortcuts {
			if frequency > 0 {
				log.Printf("%v x %v Shortcuts\n", frequency, 2*amount)
			}
		}
	}

	// store the computed shortcuts in the map
	ch.SetShortcuts(shortcuts)

	if ch.debugLevel >= 1 {
		for i := range ch.contractionProgress.achievedMilestones {
			milestone := ch.contractionProgress.milestones[i]
			achievedMilestone := &ch.contractionProgress.achievedMilestones[i]
			runtime := achievedMilestone.runtime
			totalShortcuts := achievedMilestone.shortcuts
			timeDif, addedShortcuts := func() (time.Duration, int) {
				if i == 0 {
					return runtime, totalShortcuts
				}
				previousMilestone := &ch.contractionProgress.achievedMilestones[i-1]
				return runtime - previousMilestone.runtime, totalShortcuts - previousMilestone.shortcuts
			}()
			log.Printf("Milestone %05.2f %% - Runtime: %6.3f s, difference: %.3f s, total Shortcuts: %5v, added Shortcuts: %5v\n", milestone, float64(runtime.Microseconds())/1000000, float64(timeDif.Microseconds())/1000000, totalShortcuts, addedShortcuts)
		}
	}
}

// Setup ch to compute the shortest path
func (ch *ContractionHierarchies) ShortestPathSetup(options PathFindingOptions) {
	// match arcs with node order
	ch.matchArcsWithNodeOrder()
	// set fix options for CH search
	ch.dijkstra.SetCostUpperBound(math.MaxInt)
	ch.dijkstra.SetMaxNumSettledNodes(math.MaxInt)
	ch.dijkstra.SetConsiderArcFlags(true)
	ch.dijkstra.SetIgnoreNodes(nil)
	ch.dijkstra.SetHotStart(false)

	// set tuning options
	ch.dijkstra.SetBidirectional(!options.Manual)
	ch.dijkstra.SetUseHeuristic(options.UseHeuristic)
	ch.dijkstra.SetStallOnDemand(options.StallOnDemand)
	ch.SetSortArcs(options.SortArcs)
}

// Compute the shortest path for the given query (from origin to destination node).
// It returns the length of the path.
// If no path was found, -1 is returned
func (ch *ContractionHierarchies) ComputeShortestPath(origin, destination graph.NodeId) int {
	if ch.debugLevel >= 1 {
		log.Printf("Compute path from %v to %v\n", origin, destination)
	}

	if ch.debugLevel >= 3 {
		log.Printf("Node ordering: %v\n", ch.nodeOrdering)
	}

	if ch.dijkstra.searchOptions.bidirectional {
		return ch.dijkstra.ComputeShortestPath(origin, destination)
	}
	// if path should not get calculated bidirectional, the following will get executed
	// compute shortest path manually since two unidirectinoal dijkstras were used
	ch.searchKPIs.Reset()
	// hepler function to compute path from source to possible destinations and store searchKPIs
	oneToManySearch := func(source graph.NodeId, searchStats *SearchStats, dijkstraSearchStats *SearchStats) {
		ch.dijkstra.ComputeShortestPath(source, -1)
		searchStats.visitedNodes = make([]bool, ch.g.NodeCount())
		copy(searchStats.visitedNodes, dijkstraSearchStats.visitedNodes)
		searchStats.searchSpace = make([]*DijkstraItem, ch.g.NodeCount())
		copy(searchStats.searchSpace, dijkstraSearchStats.searchSpace)
		ch.searchKPIs.pqPops += ch.dijkstra.GetPqPops()
		ch.searchKPIs.pqUpdates += ch.dijkstra.GetPqUpdates()
		ch.searchKPIs.stalledNodes += ch.dijkstra.GetStalledNodesCount()
		ch.searchKPIs.unstalledNodes += ch.dijkstra.GetUnstalledNodesCount()
		ch.searchKPIs.relaxedEdges += ch.dijkstra.GetEdgeRelaxations()
		ch.searchKPIs.relaxationAttempts += ch.dijkstra.GetRelaxationAttempts()
	}

	oneToManySearch(origin, &ch.forwardSearch, &ch.dijkstra.forwardSearch)
	oneToManySearch(destination, &ch.backwardSearch, &ch.dijkstra.forwardSearch)

	// Find the best connection
	ch.connection = -1
	shortestLength := math.MaxInt
	for nodeId := 0; nodeId < ch.g.NodeCount(); nodeId++ {
		if ch.forwardSearch.visitedNodes[nodeId] && ch.backwardSearch.visitedNodes[nodeId] {
			length := ch.forwardSearch.searchSpace[nodeId].distance + ch.backwardSearch.searchSpace[nodeId].distance
			if length < shortestLength {
				shortestLength = length
				ch.connection = nodeId
			}
		}
	}
	if ch.connection == -1 {
		return -1
	}

	return shortestLength
}

// Get the computed path.
// A slice is returned which contains the node IDs in order from source to target
func (ch *ContractionHierarchies) GetPath(origin, destination graph.NodeId) []int {
	if ch.connection == -1 {
		return make([]int, 0)
	}
	if origin == destination {
		// origin and destination is the same -> path with one node is the result
		// this is a workaround
		return []int{origin}
	}

	path := make([]int, 0)
	if ch.dijkstra.searchOptions.bidirectional {
		path = ch.dijkstra.GetPath(origin, destination)
	} else {
		// compute path manually, since two unidirectional dijkstras were used
		for nodeId := ch.forwardSearch.searchSpace[ch.connection].predecessor; nodeId != -1; nodeId = ch.forwardSearch.searchSpace[nodeId].predecessor {
			path = append(path, nodeId)
		}
		slice.ReverseInPlace(path)
		path = append(path, ch.connection)
		for nodeId := ch.backwardSearch.searchSpace[ch.connection].predecessor; nodeId != -1; nodeId = ch.backwardSearch.searchSpace[nodeId].predecessor {
			path = append(path, nodeId)
		}
	}
	if ch.debugLevel >= 1 {
		log.Printf("Path with shortcuts: %v\n", path)
	}
	for i := 0; i < len(path)-1; i++ {
		source := path[i]
		target := path[i+1]

		if sc, exists := ch.shortcutMap[source][target]; exists {
			path = slice.Insert(path, i+1, sc.via)
			if ch.debugLevel >= 2 {
				log.Printf("Added node %v -> %v -> %v\n", source, sc.via, target)
			}
			i-- // reevaluate, if the source has a shortcut to the currently added node
		}
	}
	return path
}

// Get the search space of the path finding query
// S slice is returned which contains all settled nodes of the query (containing the search information, e.g. distance to source node, which search direction was used for this item, ...)
func (ch *ContractionHierarchies) GetSearchSpace() []*DijkstraItem {
	if ch.dijkstra.searchOptions.bidirectional {
		return ch.dijkstra.GetSearchSpace()
	}
	// compute search space manually, since two unidirectional dijkstras were used
	searchSpace := make([]*DijkstraItem, 0)
	for i := 0; i < ch.g.NodeCount(); i++ {
		if ch.forwardSearch.visitedNodes[i] {
			searchSpace = append(searchSpace, ch.forwardSearch.searchSpace[i])
		} else if ch.backwardSearch.visitedNodes[i] {
			searchSpace = append(searchSpace, ch.backwardSearch.searchSpace[i])
		}
	}
	return searchSpace
}

// Get the number of pq pops (from the priority queue)
func (ch *ContractionHierarchies) GetPqPops() int {
	if ch.dijkstra.searchOptions.bidirectional {
		return ch.dijkstra.GetPqPops()
	}
	// use manual computed pq pops when calculated the path manually
	return ch.searchKPIs.pqPops
}

// Get the number of the pq updates
func (ch *ContractionHierarchies) GetPqUpdates() int {
	if ch.dijkstra.searchOptions.bidirectional {
		return ch.dijkstra.GetPqUpdates()
	}
	// use manual computed pq updates when calculated the path manually
	return ch.searchKPIs.pqUpdates
}

// Get the number of stalled nodes (invocations)
func (ch *ContractionHierarchies) GetStalledNodesCount() int {
	if ch.dijkstra.searchOptions.bidirectional {
		return ch.dijkstra.GetStalledNodesCount()
	}
	return ch.searchKPIs.stalledNodes
}

// Get the number of unstalled nodes (invocations)
func (ch *ContractionHierarchies) GetUnstalledNodesCount() int {
	if ch.dijkstra.searchOptions.bidirectional {
		return ch.dijkstra.GetUnstalledNodesCount()
	}
	return ch.searchKPIs.unstalledNodes
}

// Get the number of relaxed edges
func (ch *ContractionHierarchies) GetEdgeRelaxations() int {
	if ch.dijkstra.searchOptions.bidirectional {
		return ch.dijkstra.GetEdgeRelaxations()
	}
	// use manual computed pq updates when calculated the path manually
	return ch.searchKPIs.relaxedEdges
}

// Get the number for how many edges the relaxation was tested (but maybe early terminated)
func (ch *ContractionHierarchies) GetRelaxationAttempts() int {
	if ch.dijkstra.searchOptions.bidirectional {
		return ch.dijkstra.GetRelaxationAttempts()
	}
	// use manual computed pq updates when calculated the path manually
	return ch.searchKPIs.relaxationAttempts
}

// get the used graph
func (ch *ContractionHierarchies) GetGraph() graph.Graph {
	return ch.g
}

// Compute an initial node order. If givenNodeOrder is not nil, the OrderOption oo are ignored.
// givenNodeOrder predefines the order of the nodes.
// oo defines how the node ordering will be calculated.
// It returns the calculated node order in a priority queue
func (ch *ContractionHierarchies) computeInitialNodeOrder(givenNodeOrder []int, oo OrderOptions) *queue.MinHeap[*OrderItem] {
	orderItems := make([]*OrderItem, ch.g.NodeCount())

	if givenNodeOrder != nil {
		for i := 0; i < ch.g.NodeCount(); i++ {
			orderItem := NewOrderItem(givenNodeOrder[i])
			orderItem.edgeDifference = i // set edge difference for maintaining different priority
			orderItem.index = i
			orderItems[orderItem.nodeId] = orderItem
			fmt.Println("orderItem", orderItem)
		}
	} else if oo.IsRandom() {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		nodeOrdering := rng.Perm(ch.g.NodeCount())

		for i := 0; i < ch.g.NodeCount(); i++ {
			orderItem := NewOrderItem(nodeOrdering[i])
			orderItem.edgeDifference = i // set edge difference for maintaining different priority
			orderItem.index = i
			orderItems[orderItem.nodeId] = orderItem
		}
	} else {
		nodes := make([]graph.NodeId, ch.g.NodeCount())
		for i := range nodes {
			nodes[i] = i
		}

		contractionResults := ch.computeNodeContractionParallel(nodes, nil, true)

		for i, result := range contractionResults {
			nodeId := result.nodeId
			edgeDifference := len(result.shortcuts) - result.incidentEdges
			contractedNeighbors := result.contractedNeighbors

			if !oo.ConsiderEdgeDifference() {
				edgeDifference = 0
			}

			if !oo.ConsiderProcessedNeighbors() {
				contractedNeighbors = 0
			}

			item := NewOrderItem(nodeId)
			item.edgeDifference = edgeDifference
			item.processedNeighbors = contractedNeighbors
			item.index = i

			if ch.debugLevel >= 2 {
				log.Printf("Add node %6v, edge difference: %3v, processed neighbors: %3v\n", nodeId, edgeDifference, contractedNeighbors)
			}

			orderItems[nodeId] = item
		}
	}

	minHeap := queue.NewMinHeap(orderItems)
	return minHeap
}

// compute an independent set from the node order (pqOrder).
func (ch *ContractionHierarchies) computeIndependentSet(minHeap *queue.MinHeap[*OrderItem], ignorePriority bool) []graph.NodeId {
	priority := minHeap.Peek().Priority()

	if ignorePriority {
		priority = math.MaxInt
	}

	independentSet := make([]graph.NodeId, 0)
	forbiddenNodes := make([]bool, ch.g.NodeCount())

	increasedPriority := false
	ignoredNode := false

	for i := 0; i < minHeap.Len(); i++ {
		item := minHeap.PeekAt(i)
		if forbiddenNodes[item.nodeId] {
			ignoredNode = true
			continue
		}
		if priority < item.Priority() {
			increasedPriority = true
		}
		if increasedPriority && ignoredNode {
			break
		}
		independentSet = append(independentSet, item.nodeId)
		forbiddenNodes[item.nodeId] = true
		for _, arc := range ch.g.GetArcsFrom(item.nodeId) {
			forbiddenNodes[arc.To] = true
		}
	}

	return independentSet
}

// update the node order for the given nodes by computing a virtual contraction for each given node
func (ch *ContractionHierarchies) updateOrderForNodes(minHeap *queue.MinHeap[*OrderItem], nodes []graph.NodeId, oo OrderOptions) {
	// add "current node" to the ignore list, because it is not contracted, yet (what is the basis for the ignore list)
	contractionResult := ch.computeNodeContractionParallel(nodes, nil, true)
	for _, result := range contractionResult {
		nodeId := result.nodeId
		edgeDifference := len(result.shortcuts) - result.incidentEdges
		contractedNeighbors := result.contractedNeighbors

		if !oo.ConsiderEdgeDifference() {
			edgeDifference = 0
		}

		if !oo.ConsiderProcessedNeighbors() {
			contractedNeighbors = 0
		}

		item := minHeap.Storage[nodeId]
		oldPrio := item.Priority()
		oldPos := item.index

		item.edgeDifference = edgeDifference
		item.processedNeighbors = contractedNeighbors
		minHeap.Update(item)

		newPrio := item.Priority()
		newPos := item.index
		if ch.debugLevel >= 2 {
			log.Printf("Updating node %v. old priority: %v, new priority: %v, old position: %v, new position: %v\n", nodeId, oldPrio, newPrio, oldPos, newPos)
		}
	}
}

// Compute contraction result for given nodes.
// Returns the result of the (virtual) contraction.
func (ch *ContractionHierarchies) computeNodeContractionParallel(nodes []graph.NodeId, ignoreList []graph.NodeId, ignoreCurrentNode bool) []*ContractionResult {
	numJobs := len(nodes)
	numWorkers := len(ch.contractionWorkers)

	jobs := make(chan graph.NodeId, numJobs)
	results := make(chan *ContractionResult, numJobs)
	errChan := make(chan error, numWorkers) // 添加错误通道
	var wg sync.WaitGroup

	// create workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		worker := ch.contractionWorkers[i] // 在循环内获取 worker
		go func(w *UniversalDijkstra, workerID int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errChan <- fmt.Errorf("worker %d panic: %v", workerID, r)
				}
			}()

			for nodeId := range jobs {
				if ch.debugLevel >= 3 {
					log.Printf("Worker %d processing Node %7v\n", workerID, nodeId)
				}

				if ch.precomputedResults != nil && ch.precomputedResults[nodeId] != nil {
					results <- ch.precomputedResults[nodeId]
					continue
				}

				ignoreNodes := make([]bool, ch.g.NodeCount())
				copy(ignoreNodes, ch.contractedNodes.Get())

				for _, node := range ignoreList {
					ignoreNodes[node] = true
				}

				if ignoreCurrentNode {
					ignoreNodes[nodeId] = true
				}

				cr := ch.computeNodeContraction(nodeId, ignoreNodes, w)
				results <- cr
			}
		}(worker, i) // 正确传递 worker 和 worker ID
	}

	// 发送任务
	go func() {
		for _, nodeId := range nodes {
			jobs <- nodeId
		}
		close(jobs)
	}()

	// 等待所有 worker 完成
	go func() {
		wg.Wait()
		close(results)
		close(errChan)
	}()

	// 收集结果
	resultMap := make(map[int]*ContractionResult, numJobs)
	for {
		select {
		case result, ok := <-results:
			if !ok {
				// results 通道已关闭，所有结果都已收集
				goto DONE
			}
			resultMap[result.nodeId] = result
			if ch.precomputedResults != nil {
				ch.precomputedResults[result.nodeId] = result
			}
		case err := <-errChan:
			if err != nil {
				log.Printf("Worker error: %v", err)
			}
		}
	}

DONE:
	// 按照原始节点顺序重建结果数组
	contractionResults := make([]*ContractionResult, numJobs)
	for i, nodeId := range nodes {
		if result, ok := resultMap[nodeId]; ok {
			contractionResults[i] = result
		} else {
			log.Printf("Warning: missing result for node %d", nodeId)
		}
	}

	return contractionResults
}

// Contract the nodes based on the given order.
// The OrderOptions oo define, if and how the nodeOrder can get updated dynamically.
func (ch *ContractionHierarchies) contractNodes(minHeap *queue.MinHeap[*OrderItem], oo OrderOptions, fixedOrder bool) []Shortcut {
	if minHeap.Len() != ch.g.NodeCount() {
		// this is a rudimentary test, if the ordering could be valid.
		// However, it misses to test if every id appears exactly once
		panic("Node ordering not valid")
	}

	level := 0
	intermediateUpdates := 0
	shortcutCounter := 0
	newShortcuts := 0

	// helper function to get the target nodes which should get contracted
	getTargetNodes := func() []graph.NodeId {
		if fixedOrder {
			// stick to initial order
			// only contract one by one
			return []graph.NodeId{minHeap.Peek().nodeId}
		} else {
			return ch.computeIndependentSet(minHeap, false)
		}
	}
	// helper function to get the maximum priority of the given contraction results
	getMaxPriority := func(cr []*ContractionResult) int {
		prio := math.MinInt
		for _, result := range cr {
			if minHeap.Storage[result.nodeId].Priority() > prio {
				prio = minHeap.Storage[result.nodeId].Priority()
			}
		}
		return prio
	}
	// helper function to find the unique shortuts from the given list. This only keeps the best shourtcuts, if multiple shortcuts for the same arc exist.
	findUniqueShortcuts := func(shortcuts []Shortcut) []Shortcut {
		// remove duplicate shortcuts (shortcuts which are found from both middle nodes). However, both nodes ignore each other so there is a different path. Only one path should remain)
		uniqueShortcuts := make([]Shortcut, 0, len(shortcuts))
		for i := 0; i < len(shortcuts); i++ {
			shortcut := shortcuts[i]
			overwritten := false
			skip := false
			for j := 0; j < len(uniqueShortcuts); j++ {
				uniqueShortcut := uniqueShortcuts[j]
				if shortcut.source == uniqueShortcut.source && shortcut.target == uniqueShortcut.target {
					//&& /*shortcut.via == otherShortcut.via &&*/
					if shortcut.cost >= uniqueShortcut.cost {
						skip = true
					} else {
						uniqueShortcuts[j] = shortcut
						overwritten = true
					}
					break
				}
			}
			if !overwritten && !skip {
				uniqueShortcuts = append(uniqueShortcuts, shortcut)
			}
		}
		return uniqueShortcuts
	}

	storeContractionProgress := len(ch.contractionProgress.milestones) > 0
	if storeContractionProgress && ch.contractionProgress.milestones[0] == 0 {
		ch.storeContractionProgressInfo(time.Since(ch.contractionProgress.initialTime), 0, 0)
	}

	shortcuts := make([]Shortcut, 0)
	for minHeap.Len() > 0 && ch.contractedNodes.Ratio()*100 <= ch.contractionLevelLimit {
		targetNodes := getTargetNodes()

		if ch.debugLevel >= 2 {
			log.Printf("Compute contraction for nodes: %v\n", targetNodes)
		}

		contractionResults := ch.computeNodeContractionParallel(targetNodes, targetNodes, true) // Ignore nodes for current level (they are not contracted, yet)
		contractNodes := make([]graph.NodeId, 0, len(targetNodes))                              // store all nodes which will get contracted
		deniedContractionItems := make([]*OrderItem, 0, len(contractionResults))                // only necessary for lazy update

		affectedNeighbors := make([]graph.NodeId, 0)
		collectedShortcuts := make([]Shortcut, 0)

		priorityThreshold := getMaxPriority(contractionResults) // only necessary for lazy update

		for _, result := range contractionResults {
			item := minHeap.Storage[result.nodeId]

			if ch.debugLevel >= 3 {
				log.Printf("Test contraction of Node %v\n", item.nodeId)
			}
			minHeap.Remove(item.index)

			if oo.IsLazyUpdate() {
				if oo.ConsiderEdgeDifference() {
					item.edgeDifference = len(result.shortcuts) - result.incidentEdges
				}

				if oo.ConsiderProcessedNeighbors() {
					item.processedNeighbors = result.contractedNeighbors
				}

				if minHeap.Len() == 0 || item.Priority() <= priorityThreshold {
					// this is still one of the smallest priorities
					if ch.orderOfNode[item.nodeId] >= 0 {
						panic("Node was already ordered?")
					}
					contractNodes = append(contractNodes, item.nodeId)
					collectedShortcuts = append(collectedShortcuts, result.shortcuts...)

					if ch.debugLevel >= 3 {
						log.Printf("Add node %v to contracted nodes\n", item.nodeId)
					}
				} else {
					if ch.debugLevel >= 3 {
						log.Printf("Denied contraction for node %v. Update order\n", item.nodeId)
					}
					deniedContractionItems = append(deniedContractionItems, item)
				}
			} else {
				// no lazy update
				contractNodes = append(contractNodes, item.nodeId)
				collectedShortcuts = append(collectedShortcuts, result.shortcuts...)
			}
		}

		if len(contractNodes) > 0 {
			if level != len(ch.nodeOrdering) {
				panic("Something went wrong with level assignment.")
			}
			ch.nodeOrdering = append(ch.nodeOrdering, contractNodes)
			for _, nodeId := range contractNodes {

				if ch.debugLevel >= 3 {
					log.Printf("Finally contract node: %v\n", nodeId)
				}

				ch.orderOfNode[nodeId] = level
				// collect all nodes which have to get updates
				for _, arc := range ch.g.GetArcsFrom(nodeId) {
					destination := arc.To
					if ch.precomputedResults != nil {
						// invalidate precomputed contraction
						ch.precomputedResults[destination] = nil
					}
					if !ch.isNodeContracted(destination) {
						affectedNeighbors = append(affectedNeighbors, destination)
					}
				}
			}
			ch.contractedNodes.Add(contractNodes...)
			level++
			intermediateUpdates = 0
		}

		finalShortcuts := findUniqueShortcuts(collectedShortcuts)
		ch.addShortcuts(finalShortcuts, &shortcuts)
		newShortcuts = len(finalShortcuts)

		if len(deniedContractionItems) > 0 {
			intermediateUpdates++
			for _, item := range deniedContractionItems {
				// re-add the items which were not contracted
				minHeap.Push(item)
			}
		}

		nextMilestoneIndex := len(ch.contractionProgress.achievedMilestones)
		shortcutCounter += newShortcuts

		if storeContractionProgress && nextMilestoneIndex < len(ch.contractionProgress.milestones) && ch.contractedNodes.Ratio()*100 >= ch.contractionProgress.milestones[nextMilestoneIndex] {
			ch.storeContractionProgressInfo(time.Since(ch.contractionProgress.initialTime), ch.contractionProgress.milestones[nextMilestoneIndex], shortcutCounter)
		}

		// update all nodes or neighbors (if required)
		if oo.IsPeriodic() && (level%1000 == 0 || intermediateUpdates > 100) {
			remainingNodes := func() []graph.NodeId {
				remainingNodes := make([]graph.NodeId, minHeap.Len())
				for i := 0; i < minHeap.Len(); i++ {
					orderItem := minHeap.PeekAt(i)
					targetNodes[i] = orderItem.nodeId
				}
				return remainingNodes
			}()
			ch.updateOrderForNodes(minHeap, remainingNodes, oo)
			intermediateUpdates = 0
		} else if oo.UpdateNeighbors() {
			uniqueNeighbors := func() []graph.NodeId {
				uniqueNodes := make([]graph.NodeId, 0)
				for _, nodeId := range affectedNeighbors {
					if !slice.Contains(uniqueNodes, nodeId) {
						uniqueNodes = append(uniqueNodes, nodeId)
					}
				}
				return uniqueNodes
			}()
			ch.updateOrderForNodes(minHeap, uniqueNeighbors, oo)
		}
	}
	ch.liftUncontractedNodes()
	return shortcuts
}

// Set all uncontracted nodes to highest level
func (ch *ContractionHierarchies) liftUncontractedNodes() {
	for i := range ch.orderOfNode {
		if ch.orderOfNode[i] == -1 {
			ch.orderOfNode[i] = math.MaxInt
		}
	}
}

// Compute a (virtual) contraction for the given node. Ignore the nodes given by ignoreNodes.
// This returns the ContractionResult, containing the necessary shortcuts, incident arcs, and already contracted neighbors
func (ch *ContractionHierarchies) computeNodeContraction(nodeId graph.NodeId, ignoreNodes []bool, contractionWorker *UniversalDijkstra) *ContractionResult {
	if ch.isNodeContracted(nodeId) {
		panic("Node already contracted.")
	}

	shortcuts := make([]Shortcut, 0)
	contractedNeighbors := 0

	contractionWorker.SetIgnoreNodes(ignoreNodes)
	contractionWorker.SetDebugLevel(ch.dijkstra.debugLevel) // copy debug level

	var runtime time.Duration = 0
	computations := 0

	arcs := ch.dg.GetArcsFrom(nodeId)
	incidentArcsAmount := len(arcs)
	if ch.debugLevel >= 4 {
		log.Printf("Incident arcs %v\n", incidentArcsAmount)
	}
	for i := 0; i < len(arcs); i++ {
		arc := arcs[i]
		source := arc.Destination()
		if ch.isNodeContracted(source) {
			if ch.debugLevel >= 4 {
				log.Printf("source %v already processed\n", source)
			}
			contractedNeighbors++
			continue
		}
		for j := i + 1; j < len(arcs); j++ {
			otherArc := arcs[j]
			target := otherArc.Destination()
			if source == target {
				// source shouldn't be the same like target
				panic("This should not happen")
			}
			if ch.isNodeContracted(target) {
				if ch.debugLevel >= 4 {
					log.Printf("target %v already processed\n", target)
				}
				continue
			}

			if ch.debugLevel >= 4 {
				log.Printf("testing for shortcut %v -> %v\n", source, target)
			}
			maxCost := arc.Cost() + otherArc.Cost()
			contractionWorker.SetCostUpperBound(maxCost)
			start := time.Now()
			length := contractionWorker.ComputeShortestPath(source, target)
			elapsed := time.Since(start)
			runtime += elapsed
			computations++

			if ch.debugLevel >= 4 {
				log.Printf("Length: %v, cost via node: %v, pq pops: %v\n", length, maxCost, ch.dijkstra.GetPqPops())
			}
			if length == -1 || length > maxCost {
				// add shortcut, since the path via this node is the fastest
				// without this node, the target is either not reachable or the path is longer
				if ch.debugLevel >= 4 {
					log.Printf("Shortcut %v -> %v via %v needed\n", source, target, nodeId)
				}
				shortcut := Shortcut{source: source, target: target, via: nodeId, cost: maxCost}
				// add reverse shortcut since this is only computed once
				// only calculate source -> target once, don't calculate target -> source
				reverseShortcut := Shortcut{source: target, target: source, via: nodeId, cost: maxCost}
				shortcuts = append(shortcuts, shortcut, reverseShortcut)
			}
		}
	}

	if ch.debugLevel >= 3 && computations > 0 {
		log.Printf("Dijkstra Runtime: %v us * %v, node %v\n", float64(int(runtime.Nanoseconds())/computations)/1000, computations, nodeId)
	}

	// Fix incident arcs: Remove number of already contracted neighbors
	incidentArcsAmount -= contractedNeighbors

	// number of shortcuts is doubled, since we have two arcs for each each (because symmetric graph)
	contractionResult := &ContractionResult{nodeId: nodeId, shortcuts: shortcuts, incidentEdges: 2 * incidentArcsAmount, contractedNeighbors: contractedNeighbors}
	return contractionResult
}

// Add the shortcuts to the graph (by adding new arcs)
func (ch *ContractionHierarchies) addShortcuts(shortcuts []Shortcut, storage *[]Shortcut) {
	addedShortcuts := 0
	for _, sc := range shortcuts {
		added := ch.addShortcut(sc, storage)
		if added {
			addedShortcuts++
		}
	}

	if addedShortcuts%2 == 1 {
		panic("shortcuts are odd. They should only be added pairwise")
	}
	addedShortcuts /= 2 // divide by 2 (to save storage space)

	if addedShortcuts < len(ch.addedShortcuts) {
		// addedShortcuts is in range of slice
		// can just add them
		ch.addedShortcuts[addedShortcuts]++
	} else {
		// addedShortcuts is currently out of range
		// increase slice first and add then
		dif := addedShortcuts - len(ch.addedShortcuts) + 1
		missingEntries := make([]int, dif)
		ch.addedShortcuts = append(ch.addedShortcuts, missingEntries...)
		ch.addedShortcuts[addedShortcuts] = 1
	}
}

// Adds a shortcut to the graph from source to target with length cost which is spanned over node defined by via.
// This adds a new arc in the graph.
func (ch *ContractionHierarchies) addShortcut(shortcut Shortcut, shortcuts *[]Shortcut) bool {
	if ch.debugLevel >= 3 {
		log.Printf("Add shortcut %+v\n", shortcut)
	}
	if ch.orderOfNode[shortcut.source] != -1 || ch.orderOfNode[shortcut.target] != -1 {
		panic("Edge Node already contracted")
	}
	added := ch.dg.AddArc(shortcut.source, shortcut.target, shortcut.cost)
	if added {
		*shortcuts = append(*shortcuts, shortcut)
		return true
	}
	return false
}

// Enable arcs which point to a node with higher level. Disable all other ones
func (ch *ContractionHierarchies) matchArcsWithNodeOrder() {
	for source := range ch.g.GetNodes() {
		arcs := ch.g.GetArcsFrom(source)
		for i := range arcs {
			arc := &arcs[i]
			target := arc.Destination()
			// 根据节点顺序和道路类型设置可用性
			isHigherLevel := ch.orderOfNode[source] < ch.orderOfNode[target] || ch.orderOfNode[target] == math.MaxInt
			if isHigherLevel {
				// 如果是更高层级的节点，保持原有道路类型
				continue
			} else {
				// 如果是更低层级的节点，设置为不可用
				arc.SetArcFlag(false)
			}
		}
	}
}

// checks whether the node given by nodeId is already contracted.
func (ch *ContractionHierarchies) isNodeContracted(node graph.NodeId) bool {
	// orderOfNode gets initialized with -1
	// if the entry is greater than that, there was already a level assigned
	return ch.orderOfNode[node] >= 0
}

// Set the shortcuts by an already available list.
// This is used when one has already a contracted graph and one need to define which arcs are shortcuts.
func (ch *ContractionHierarchies) SetShortcuts(shortcuts []Shortcut) {
	// structure: idx: source -> key: target -> value: (complete) Shortcut
	ch.shortcutMap = make([]map[graph.NodeId]Shortcut, ch.g.NodeCount())
	for _, sc := range shortcuts {
		if sc.source >= len(ch.shortcutMap) {
			panic("source is out of range.")
		}
		sourceMap := ch.shortcutMap[sc.source]
		if sourceMap == nil {
			ch.shortcutMap[sc.source] = make(map[graph.NodeId]Shortcut)
		}
		// if a shortcut already exists for this source-target pair, it gets overwritten
		// but this only happens if the second shortcut is shorter
		ch.shortcutMap[sc.source][sc.target] = sc
	}
}

// Get the calculated shortcuts.
func (ch *ContractionHierarchies) GetShortcuts() []Shortcut {
	shortcuts := make([]Shortcut, 0)

	for _, scMap := range ch.shortcutMap {
		for _, sc := range scMap {
			shortcuts = append(shortcuts, sc)
		}
	}

	return shortcuts
}

// Set the node ordering by an already available list.
// This is used when one has already a contracted graph and one need to define in which order the nodes were contracted.
// the index of the list reflects to the node id, the value to the level/position, when the node was contracted.
func (ch *ContractionHierarchies) SetNodeOrdering(nodeOrdering [][]int) {
	ch.orderOfNode = make([]int, ch.g.NodeCount())

	// initialize order of node
	for i := range ch.orderOfNode {
		ch.orderOfNode[i] = -1
	}

	for i, nodeIds := range nodeOrdering {
		for _, nodeId := range nodeIds {
			ch.orderOfNode[nodeId] = i
		}
	}

	ch.liftUncontractedNodes()
}

// Set if the arcs should be sorted
// flag indicating if the arcs are sorted according if they are enabled or not (list will contain enabled arcs, then disabled arcs)
func (ch *ContractionHierarchies) SetSortArcs(sort bool) {
	if sort {
		ch.g.SortArcs()
	}
	ch.dijkstra.SortedArcs(sort)
}

// Set the debug level
func (ch *ContractionHierarchies) SetDebugLevel(level int) {
	ch.debugLevel = level
}

// Set the precomputation milestones (which are worth a log message)
func (ch *ContractionHierarchies) SetPrecomputationMilestones(milestones []float64) {
	ch.contractionProgress.milestones = milestones
	ch.contractionProgress.achievedMilestones = make([]Milestone, 0, len(milestones))
}

func (ch *ContractionHierarchies) storeContractionProgressInfo(runtime time.Duration, milestone float64, shortcuts int) {
	ch.contractionProgress.achievedMilestones = append(ch.contractionProgress.achievedMilestones, Milestone{runtime, shortcuts})

	f, err := os.OpenFile(ch.contractionProgress.milestoneFilename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}

	defer f.Close()

	timeDif, addedShortcuts := func() (time.Duration, int) {
		if len(ch.contractionProgress.achievedMilestones) <= 1 {
			return runtime, shortcuts
		}
		previousPos := len(ch.contractionProgress.achievedMilestones) - 2
		return runtime - ch.contractionProgress.achievedMilestones[previousPos].runtime, shortcuts - int(ch.contractionProgress.achievedMilestones[previousPos].shortcuts)
	}()

	if _, err = f.WriteString(fmt.Sprintf("Milestone %05.2f %% - Runtime: %6.3f s, difference: %.3f s, total Shortcuts: %5v, added Shortcuts: %5v\n", milestone, float64(runtime.Microseconds())/1000000, float64(timeDif.Microseconds())/1000000, shortcuts, addedShortcuts)); err != nil {
		panic(err)
	}
}

// Write the graph to a file
func (ch *ContractionHierarchies) WriteGraph() {
	graph.WriteFmi(ch.g, ch.graphFilename)
}

// Write the shortcuts to a file
func (ch *ContractionHierarchies) WriteShortcuts() {
	file, cErr := os.Create(ch.shortcutsFilename)

	if cErr != nil {
		log.Fatal(cErr)
	}

	var sb strings.Builder

	shortcuts := ch.GetShortcuts()
	fmt.Println("shortcuts长度", len(shortcuts))
	for _, v := range shortcuts {
		shortcut := fmt.Sprintf("%v %v %v\n", v.source, v.target, v.via)
		sb.WriteString(shortcut)
	}

	writer := bufio.NewWriter(file)
	writer.WriteString(sb.String())
	writer.Flush()
}

// Write the node ordering to a file
func (ch *ContractionHierarchies) WriteNodeOrdering() {
	file, cErr := os.Create(ch.nodeOrderingFilename)

	if cErr != nil {
		log.Fatal(cErr)
	}

	var sb strings.Builder
	fmt.Println("nodeOrdering长度", len(ch.nodeOrdering))
	for _, cascadedNodeOrdering := range ch.nodeOrdering {
		for _, v := range cascadedNodeOrdering {
			order := fmt.Sprintf("%v ", v)
			sb.WriteString(order)
		}
		sb.WriteString("\n")
	}
	writer := bufio.NewWriter(file)
	writer.WriteString(sb.String())
	writer.Flush()
}

// Write the contraciotn resutl (graph, shortcuts, node ordering) to a file
func (ch *ContractionHierarchies) WriteContractionResult() {
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		ch.WriteGraph()
		wg.Done()
	}()
	go func() {
		ch.WriteShortcuts()
		wg.Done()
	}()
	go func() {
		ch.WriteNodeOrdering()
		wg.Done()
	}()
	wg.Wait()
}

// Read a shortcuts file and return the list of shortcuts
func ReadShortcutFile(filename string) []Shortcut {
	file, err := os.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	return ConvertToShortcuts(string(file))
}

// Parse a string to a list of shortcuts
func ConvertToShortcuts(shortcutsString string) []Shortcut {
	scanner := bufio.NewScanner(strings.NewReader(shortcutsString))

	shortcuts := make([]Shortcut, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 1 || line[0] == '#' {
			// skip empty lines and comments
			continue
		}
		var source, target, via graph.NodeId
		fmt.Sscanf(line, "%d %d %d", &source, &target, &via)
		sc := Shortcut{source: source, target: target, via: via}
		shortcuts = append(shortcuts, sc)
	}
	return shortcuts
}

// Read a node ordering file and return the node order
func ReadNodeOrderingFile(filename string) [][]int {
	file, err := os.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	return ConvertToNodeOrdering(string(file))
}

// Parse a string to a lsit which specifies the node order
func ConvertToNodeOrdering(nodeOrderingString string) [][]int {
	scanner := bufio.NewScanner(strings.NewReader(nodeOrderingString))

	nodeOrdering := make([][]int, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 1 || line[0] == '#' {
			// skip empty lines and comments
			continue
		}
		stringNodeIds := strings.Fields(line)
		nodeIds := make([]graph.NodeId, len(stringNodeIds))
		for i, sid := range stringNodeIds {
			id, err := strconv.Atoi(sid)
			if err != nil {
				log.Fatal(err)
			}
			nodeIds[i] = id
		}
		nodeOrdering = append(nodeOrdering, nodeIds)
	}
	return nodeOrdering
}

func (ch *ContractionHierarchies) ComputeShortestPathWithWeights(origin, destination int, weights map[int]map[int]int) int {
	// 使用 dijkstra 的实现
	return ch.dijkstra.ComputeShortestPathWithWeights(origin, destination, weights)
}
