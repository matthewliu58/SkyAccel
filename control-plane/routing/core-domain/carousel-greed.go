package core_domain

import (
	"container/list"
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"time"
)

// FlowOptimizationSolver implements the flow optimization problem
// max F s.t.
// 1. Flow conservation: sum f_ij = sum f_ji for all i
// 2. Capacity constraint: f_ij <= theta_a(v) * u_ij, where theta_a(v) = 1 - U'_v is dynamically computed based on CPU utilization
// 3. Latency constraint: L(P) <= theta_L(d), each destination d has its own latency bound
// 4. From s to N destinations, flowsPerDest flows per destination
type FlowOptimizationSolver struct {
	edges        []*graph.Edge
	alpha        float64 // iteration count coefficient
	beta         float64 // path removal ratio
	capacity     float64 // edge capacity u_ij (dynamically computed: numDestinations * flowsPerDest)
	flowsPerDest int     // number of flows per destination
	// CPU utilization parameters
	Tu      float64 // safe utilization threshold (node is uncongested below this value)
	Uhot    float64 // high-percentile utilization threshold (hotspot condition)
	epsilon float64 // small constant to prevent division by zero
	// Latency constraints (per-destination)
	thetaLMap  map[string]float64 // destination -> latency constraint
	kspK       int                // number of KSP paths used to compute average latency
	latencyMap map[string]float64 // edge (source->dest) -> latency (for O(1) lookup)
	rng        *rand.Rand         // random number generator (initialized once)
}

// NewFlowOptimizationSolver creates a new instance
func NewFlowOptimizationSolver(edges []*graph.Edge) *FlowOptimizationSolver {
	// Build latency map for O(1) edge latency lookup
	latencyMap := make(map[string]float64)
	for _, e := range edges {
		key := e.SourceIp + "->" + e.DestinationIp
		latencyMap[key] = e.Latency
	}

	// Initialize random number generator once (reused across iterations)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	return &FlowOptimizationSolver{
		edges:        edges,
		alpha:        2.0, // iteration count coefficient (increase for more thorough optimization)
		beta:         0.9, // path removal ratio (aggressive exploration: remove 90% of initial paths)
		capacity:     0.0, // edge capacity computed dynamically in ComputingMulti based on destination count
		flowsPerDest: 3,   // 3 flows per destination
		// CPU utilization parameters
		Tu:      60.0,  // safe utilization threshold (node is uncongested below this value)
		Uhot:    0.0,   // high-percentile utilization threshold (computed as 90th percentile at runtime)
		epsilon: 0.001, // small constant to prevent division by zero
		// Latency constraint parameters
		thetaLMap:  make(map[string]float64),
		kspK:       5, // use top 5 KSP paths to compute average latency
		latencyMap: latencyMap,
		rng:        rng,
	}
}

// EdgeFlow records flow information for an edge
type EdgeFlow struct {
	flow     float64 // current flow
	cap      float64 // original capacity u_ij
	thetaA   float64 // dynamic utilization factor [0,1]
	effCap   float64 // effective capacity = thetaA * cap
	banned   bool    // temporarily banned for exploration (not permanently removed)
	residual float64 // residual capacity = effCap - flow (forward residual)
}

// updateResidual updates the residual capacity
func (ef *EdgeFlow) updateResidual() {
	ef.residual = ef.effCap - ef.flow
}

// getReverseResidual returns the reverse residual capacity (equals current flow)
func (ef *EdgeFlow) getReverseResidual() float64 {
	return ef.flow // Backward residual = flow (standard residual network)
}

// computeThetaA computes the dynamic scaling factor theta_a(v) based on CPU utilization
// Formula: U'_v = min(1, max(U_v-Tu,0) / max(U_hot-Tu, epsilon))
//
//	theta_a(v) = 1 - U'_v
func (d *FlowOptimizationSolver) computeThetaA(cpuLoad float64) float64 {
	numerator := max(cpuLoad-d.Tu, 0.0)
	denominator := max(d.Uhot-d.Tu, d.epsilon)

	U_prime := numerator / denominator
	U_prime = min(U_prime, 1.0)

	return 1.0 - U_prime
}

// max returns the larger value
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// min returns the smaller value
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// computeThetaLForDestinations computes average latency for each destination using KSP as theta_L(d)
func (d *FlowOptimizationSolver) computeThetaLForDestinations(start string, ends []string, logger *slog.Logger) {
	// Create KSP solver (use latency as weight)
	kspSolver := NewKShortestSolverWithLatency(d.edges, d.kspK, true)

	for _, end := range ends {
		// Compute top kspK paths using KSP
		paths, err := kspSolver.Computing(start, end, "thetaL_calc", logger)
		if err != nil || len(paths) == 0 {
			// Use default value if no paths found
			d.thetaLMap[end] = 100.0
			logger.Warn("FlowOptimizationSolver: cannot find paths for destination, using default thetaL",
				slog.String("dest", end),
				slog.Float64("thetaL", d.thetaLMap[end]))
			continue
		}

		// Compute average latency using RawRTT (pure latency) for consistency with DFS calculation
		totalLatency := 0.0
		for _, path := range paths {
			totalLatency += path.RawRTT
		}
		avgLatency := totalLatency / float64(len(paths))

		// Set latency constraint to average latency (can multiply by a factor)
		d.thetaLMap[end] = avgLatency

		logger.Debug("FlowOptimizationSolver: computed thetaL for destination",
			slog.String("dest", end),
			slog.Float64("avgLatency", avgLatency),
			slog.Float64("thetaL", d.thetaLMap[end]))
	}
}

// computePercentile computes the percentile of edge loads
func (d *FlowOptimizationSolver) computePercentile(percentile float64) float64 {
	if len(d.edges) == 0 {
		return 0.0
	}

	// Extract all edge loads
	loads := make([]float64, 0, len(d.edges))
	for _, e := range d.edges {
		loads = append(loads, e.Load)
	}

	// Sort
	for i := 0; i < len(loads)-1; i++ {
		for j := i + 1; j < len(loads); j++ {
			if loads[j] < loads[i] {
				loads[i], loads[j] = loads[j], loads[i]
			}
		}
	}

	// Compute percentile position
	index := percentile / 100.0 * float64(len(loads)-1)
	lowerIndex := int(index)
	upperIndex := lowerIndex + 1

	if upperIndex >= len(loads) {
		return loads[len(loads)-1]
	}

	// Linear interpolation
	fraction := index - float64(lowerIndex)
	return loads[lowerIndex] + fraction*(loads[upperIndex]-loads[lowerIndex])
}

// PathWithInfo contains path information (for multi-destination)
type PathWithInfo struct {
	path    []string
	dest    string
	latency float64
}

// ComputingMulti performs multi-destination flow optimization
// start: source node
// ends: list of destinations (e.g., 10 destinations)
// Returns: list of paths for each destination (max flowsPerDest paths per destination)
func (d *FlowOptimizationSolver) ComputingMulti(start string, ends []string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	return ComputeMultiDestination(d, start, ends, pre, logger)
}

// buildResidualGraphWithCapacity builds a residual graph with capacities (using dynamic theta_a(v))
func (d *FlowOptimizationSolver) buildResidualGraphWithCapacity() map[string]map[string]*EdgeFlow {
	g := make(map[string]map[string]*EdgeFlow)
	for _, e := range d.edges {
		if g[e.SourceIp] == nil {
			g[e.SourceIp] = make(map[string]*EdgeFlow)
		}
		// Compute theta_a(v) dynamically based on edge's CPU load
		thetaA := d.computeThetaA(e.Load)
		effCap := thetaA * d.capacity

		ef := &EdgeFlow{
			flow:     0.0,
			cap:      d.capacity,
			thetaA:   thetaA,
			effCap:   effCap,
			banned:   false, // initialize as not banned
			residual: effCap,
		}
		g[e.SourceIp][e.DestinationIp] = ef
	}
	return g
}

const maxHops = 10 // maximum hop limit

// findFeasiblePathWithLatency finds a feasible path that satisfies latency constraint
// Uses standard residual network:
//   - Forward edge: residual = effCap - flow
//   - Backward edge: residual = flow (reverse residual)
//
// Key design decisions:
//  1. Hard constraints: residual > 0 (feasibility)
//  2. Soft constraints: banned edges add penalty to cost (not hard skip)
//  3. Reverse edges: only for flow augmentation, NOT for latency optimization
func (d *FlowOptimizationSolver) findFeasiblePathWithLatency(g map[string]map[string]*EdgeFlow, s, t string) *PathWithInfo {
	if _, ok := g[s]; !ok {
		return nil
	}

	// Get latency constraint for this destination
	thetaL, ok := d.thetaLMap[t]
	if !ok {
		thetaL = 100.0 // default value
	}

	// Penalty weight for banned edges (soft constraint, not hard skip)
	const bannedPenalty = 100.0 // Large penalty but not infinite

	visited := make(map[string]bool)
	path := []string{}

	var dfs func(u string, currentLatency float64) (bool, float64)
	dfs = func(u string, currentLatency float64) (bool, float64) {
		// Hop pruning: stop if current path length (hops = len(path)) exceeds maxHops
		if len(path) >= maxHops {
			return false, currentLatency
		}

		if u == t {
			path = append(path, u)
			return true, currentLatency
		}
		visited[u] = true
		path = append(path, u)

		// Explore forward edges (original direction)
		for v, ef := range g[u] {
			if !visited[v] && ef != nil {
				// Hard constraint: must have residual capacity
				if ef.residual <= 0 {
					continue
				}

				// Compute edge cost with soft penalty for banned edges
				edgeCost := d.getEdgeLatency(u, v)
				if ef.banned {
					edgeCost += bannedPenalty // Soft penalty instead of hard skip
				}

				if currentLatency+edgeCost <= thetaL {
					found, finalLatency := dfs(v, currentLatency+edgeCost)
					if found {
						return true, finalLatency
					}
				}
			}
		}

		// Explore backward edges (reverse direction using reverse residual)
		// IMPORTANT: Reverse edges are ONLY for flow augmentation, NOT for latency optimization
		// They allow "undoing" previously allocated flow without affecting path latency
		for v := range nodesInGraph(g) {
			if v == u || visited[v] {
				continue
			}
			// Check if there's an edge from v to u (so u->v is the reverse)
			if g[v] != nil && g[v][u] != nil {
				revEf := g[v][u]
				// Hard constraint: must have reverse residual capacity
				if revEf.getReverseResidual() <= 0 {
					continue
				}

				// Reverse edge cost: only add banned penalty, NOT edge latency
				// This ensures reverse edges don't artificially reduce path latency
				edgeCost := 0.0 // Reverse edges don't contribute to latency
				if revEf.banned {
					edgeCost += bannedPenalty // Soft penalty for banned reverse edges
				}

				if currentLatency+edgeCost <= thetaL {
					found, finalLatency := dfs(v, currentLatency+edgeCost)
					if found {
						return true, finalLatency
					}
				}
			}
		}

		path = path[:len(path)-1]
		visited[u] = false // Backtrack: restore visited state
		return false, currentLatency
	}

	found, finalLatency := dfs(s, 0.0)
	if found {
		return &PathWithInfo{
			path:    path,
			dest:    t,
			latency: finalLatency,
		}
	}
	return nil
}

// nodesInGraph returns all unique nodes in the graph
func nodesInGraph(g map[string]map[string]*EdgeFlow) map[string]bool {
	nodes := make(map[string]bool)
	for from, edges := range g {
		nodes[from] = true
		for to := range edges {
			nodes[to] = true
		}
	}
	return nodes
}

// getEdgeLatency gets the latency of an edge (O(1) lookup using latencyMap)
func (d *FlowOptimizationSolver) getEdgeLatency(from, to string) float64 {
	key := from + "->" + to
	if latency, ok := d.latencyMap[key]; ok {
		return latency
	}
	// Return infinity if edge not found - this will cause latency constraint to fail
	// This prevents silent failures where missing edges get a fake latency value
	return math.MaxFloat64
}

// allocateFlow allocates flow along a path
// Standard residual network update:
//   - Forward edge: flow += f, residual = effCap - flow
//   - Backward residual is implicitly flow (no need to store separately)
func (d *FlowOptimizationSolver) allocateFlow(g map[string]map[string]*EdgeFlow, path []string, flow float64) {
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		if g[from] != nil && g[from][to] != nil {
			g[from][to].flow += flow
			g[from][to].updateResidual()
		}
	}
}

// releaseFlow releases flow along a path
// Standard residual network update - reverses allocateFlow
func (d *FlowOptimizationSolver) releaseFlow(g map[string]map[string]*EdgeFlow, path []string, flow float64) {
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		if g[from] != nil && g[from][to] != nil {
			g[from][to].flow -= flow
			if g[from][to].flow < 0 {
				g[from][to].flow = 0
			}
			g[from][to].updateResidual()
		}
	}
}

// validatePaths validates paths against the current residual graph state
// Returns only paths that are still feasible (all edges have sufficient residual capacity)
func (d *FlowOptimizationSolver) validatePaths(paths *list.List, g map[string]map[string]*EdgeFlow) *list.List {
	valid := list.New()
	for e := paths.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)
		if d.isPathFeasible(pi.path, g) {
			valid.PushBack(pi)
		}
	}
	return valid
}

// isPathFeasible checks if a path is feasible in the current graph state
func (d *FlowOptimizationSolver) isPathFeasible(path []string, g map[string]map[string]*EdgeFlow) bool {
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		if g[from] == nil || g[from][to] == nil {
			return false
		}
		ef := g[from][to]
		if ef.banned || ef.residual <= 0 {
			return false
		}
	}
	return true
}

// banPathEdges temporarily bans all edges in a path (for exploration)
func (d *FlowOptimizationSolver) banPathEdges(g map[string]map[string]*EdgeFlow, path []string) {
	for i := 0; i < len(path)-1; i++ {
		d.banEdge(g, path[i], path[i+1])
	}
}

// banEdge temporarily bans a single edge (sets banned flag instead of removing)
func (d *FlowOptimizationSolver) banEdge(g map[string]map[string]*EdgeFlow, from, to string) {
	if g[from] != nil && g[from][to] != nil {
		g[from][to].banned = true
	}
}

// unbanAllEdges unbans all edges in the graph (called at the end of each iteration)
func (d *FlowOptimizationSolver) unbanAllEdges(g map[string]map[string]*EdgeFlow) {
	for _, edges := range g {
		for _, ef := range edges {
			if ef != nil {
				ef.banned = false
			}
		}
	}
}

// allDestinationsFull checks if all destinations have reached max flowsPerDest
func (d *FlowOptimizationSolver) allDestinationsFull(destFlowCount map[string]int, destinations []string) bool {
	for _, dest := range destinations {
		if destFlowCount[dest] < d.flowsPerDest {
			return false
		}
	}
	return true
}

// objective function: maximize path count + diversity - latency
// Score = path_count * pathWeight + diversity_score * diversityWeight - latency_penalty
// Uses current residual graph state to compute actual path costs
func (d *FlowOptimizationSolver) objective(lst *list.List, g map[string]map[string]*EdgeFlow) float64 {
	if lst.Len() == 0 {
		return 0.0
	}

	// Weights for balancing objectives
	// pathWeight should be dominant to prioritize max F (paper objective)
	const pathWeight = 10.0
	const diversityWeight = 1.0
	const latencyWeight = 0.01
	const congestionWeight = 1.0 // penalty for using congested edges

	// Count edge usage for diversity calculation
	edgeUsage := make(map[string]int)
	totalLatency := 0.0
	totalCongestion := 0.0

	for e := lst.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)

		// Recompute latency from current graph state (not cached value)
		pathLatency := d.computePathLatency(pi.path, g)
		totalLatency += pathLatency

		// Count each edge in the path and compute congestion penalty
		path := pi.path
		for i := 0; i < len(path)-1; i++ {
			from := path[i]
			to := path[i+1]
			edgeKey := from + "->" + to
			edgeUsage[edgeKey]++

			// Congestion penalty: based on current residual capacity
			if g[from] != nil && g[from][to] != nil {
				ef := g[from][to]
				// Congestion = 1 - (residual / effCap) -> 0 = uncongested, 1 = fully congested
				if ef.effCap > 0 {
					congestion := 1.0 - (ef.residual / ef.effCap)
					totalCongestion += congestion
				}
			}
		}
	}

	// Diversity score: edges used fewer times contribute more
	// 1/count for each edge, so shared edges reduce score
	diversityScore := 0.0
	for _, count := range edgeUsage {
		diversityScore += 1.0 / float64(count)
	}

	// Combine metrics with weights
	// pathCount is dominant (10x weight) to prioritize max F as per paper
	pathCount := float64(lst.Len())
	latencyPenalty := totalLatency * latencyWeight
	congestionPenalty := totalCongestion * congestionWeight

	// Total score favors: more paths (primary), more diversity (secondary),
	// lower latency (tertiary), lower congestion (quaternary)
	return pathCount*pathWeight + diversityScore*diversityWeight - latencyPenalty - congestionPenalty
}

// computePathLatency computes latency of a path using current graph state
func (d *FlowOptimizationSolver) computePathLatency(path []string, g map[string]map[string]*EdgeFlow) float64 {
	latency := 0.0
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		// Use edge latency from latencyMap (original edge property)
		// This is more stable than using dynamic residual-based weights
		latency += d.getEdgeLatency(from, to)
	}
	return latency
}

// copyPathList copies a path list
func (d *FlowOptimizationSolver) copyPathList(l *list.List) *list.List {
	nl := list.New()
	for e := l.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)
		nl.PushBack(&PathWithInfo{
			path:    pi.path,
			dest:    pi.dest,
			latency: pi.latency,
		})
	}
	return nl
}

// mostFrequentPathInfo counts path frequencies and returns the most frequent path
func (d *FlowOptimizationSolver) mostFrequentPathInfo(lst *list.List) *PathWithInfo {
	cnt := make(map[string]int)
	maxCnt := 0
	var bestPath *PathWithInfo

	for e := lst.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)
		key := ""
		for _, s := range pi.path {
			key += s + "|"
		}
		cnt[key]++
		if cnt[key] > maxCnt {
			maxCnt = cnt[key]
			bestPath = pi
		}
	}
	return bestPath
}

// Computing single destination interface (compatible with legacy interface)
func (d *FlowOptimizationSolver) Computing(start, end string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	return d.ComputingMulti(start, []string{end}, pre, logger)
}

// ComputeMultiDestination package-level multi-destination flow optimization function (following onewan-multi.go style)
func ComputeMultiDestination(solver *FlowOptimizationSolver, start string, ends []string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	if len(ends) == 0 {
		return nil, fmt.Errorf("no destinations provided")
	}

	if len(ends) == 1 {
		return solver.Computing(start, ends[0], pre, logger)
	}

	// Validate source node exists in graph
	nodes := make(map[string]struct{})
	for _, e := range solver.edges {
		nodes[e.SourceIp] = struct{}{}
		nodes[e.DestinationIp] = struct{}{}
	}

	if _, ok := nodes[start]; !ok {
		return nil, fmt.Errorf("start node %s not found in graph", start)
	}

	// Validate all destinations exist in graph
	validEnds := make([]string, 0, len(ends))
	for _, end := range ends {
		if _, ok := nodes[end]; ok {
			validEnds = append(validEnds, end)
		} else {
			logger.Warn("FlowOptimization multi: destination not in graph, skipping",
				slog.String("pre", pre), slog.String("end", end))
		}
	}

	if len(validEnds) == 0 {
		return nil, fmt.Errorf("no valid destinations found in graph from %s", start)
	}

	// Dynamically compute edge capacity: capacity = numDestinations * flowsPerDest
	solver.capacity = float64(len(validEnds) * solver.flowsPerDest)

	// Dynamically compute U_hot: 90th percentile of all edge loads
	solver.Uhot = solver.computePercentile(90.0)

	// Compute average latency for each destination using KSP as theta_L(d)
	solver.computeThetaLForDestinations(start, validEnds, logger)

	logger.Info("FlowOptimizationSolver: parameters calculated dynamically",
		slog.String("pre", pre),
		slog.Int("numDestinations", len(validEnds)),
		slog.Int("flowsPerDest", solver.flowsPerDest),
		slog.Float64("capacity", solver.capacity),
		slog.Float64("U_hot (90th percentile)", solver.Uhot))

	// 1. Build residual graph with capacities
	resGraph := solver.buildResidualGraphWithCapacity()

	// 2. Find feasible paths for each destination (satisfy latency constraint and flow limit)
	allPaths := list.New()
	destFlowCount := make(map[string]int) // Track flow count per destination

	for _, end := range validEnds {
		// Max flowsPerDest paths per destination
		for i := 0; i < solver.flowsPerDest; i++ {
			pathInfo := solver.findFeasiblePathWithLatency(resGraph, start, end)
			if pathInfo == nil {
				logger.Warn("Cannot find enough paths for destination",
					slog.String("dest", end), slog.Int("found", i))
				break
			}
			allPaths.PushBack(pathInfo)
			destFlowCount[end]++
			solver.allocateFlow(resGraph, pathInfo.path, 1.0) // Allocate 1 unit of flow
		}
	}

	if allPaths.Len() == 0 {
		return nil, fmt.Errorf("no feasible path found from %s to any destination", start)
	}

	// 3. Record all historical paths
	P := list.New()
	for e := allPaths.Front(); e != nil; e = e.Next() {
		P.PushBack(e.Value)
	}

	// 4. Initialize optimal solution
	bestPaths := solver.copyPathList(allPaths)
	bestScore := solver.objective(allPaths, resGraph)

	// Record initial size before removing paths (for maxIter calculation)
	initialSize := allPaths.Len()

	// 5. Remove beta*|S| newest paths and release their flows
	removeNum := int(solver.beta * float64(allPaths.Len()))
	for i := 0; i < removeNum && allPaths.Len() > 0; i++ {
		oldestElm := allPaths.Front()
		oldestPathInfo := oldestElm.Value.(*PathWithInfo)
		// Release flow before removing path
		solver.releaseFlow(resGraph, oldestPathInfo.path, 1.0)
		destFlowCount[oldestPathInfo.dest]--
		allPaths.Remove(oldestElm)
	}

	// 6. Iterative optimization
	// Use initial size for maxIter calculation (alpha * |S_0|), not the reduced size
	maxIter := int(solver.alpha * float64(initialSize))
	for i := 0; i < maxIter; i++ {
		if allPaths.Len() == 0 {
			break
		}

		// 7. Remove oldest path and release flow
		oldestElm := allPaths.Front()
		oldestPathInfo := oldestElm.Value.(*PathWithInfo)
		allPaths.Remove(oldestElm)
		destFlowCount[oldestPathInfo.dest]--
		solver.releaseFlow(resGraph, oldestPathInfo.path, 1.0)

		// 8. Find most frequent path in current solution (not historical paths)
		freqPathInfo := solver.mostFrequentPathInfo(allPaths)

		// 9. Ban edges of frequent path + first edge of oldest path
		if freqPathInfo != nil {
			solver.banPathEdges(resGraph, freqPathInfo.path)
		}
		if len(oldestPathInfo.path) >= 2 {
			solver.banEdge(resGraph, oldestPathInfo.path[0], oldestPathInfo.path[1])
		}

		// 10. Find new path for random destination (respect flow limit)
		// Shuffle destinations to ensure fair selection (not biased by array order)
		shuffledEnds := make([]string, len(validEnds))
		copy(shuffledEnds, validEnds)
		solver.rng.Shuffle(len(shuffledEnds), func(i, j int) {
			shuffledEnds[i], shuffledEnds[j] = shuffledEnds[j], shuffledEnds[i]
		})

		for _, end := range shuffledEnds {
			// Check if destination has reached max flow limit
			if destFlowCount[end] >= solver.flowsPerDest {
				continue
			}

			newPathInfo := solver.findFeasiblePathWithLatency(resGraph, start, end)
			if newPathInfo != nil {
				allPaths.PushBack(newPathInfo)
				P.PushBack(newPathInfo)
				destFlowCount[end]++
				solver.allocateFlow(resGraph, newPathInfo.path, 1.0)
				break
			}
		}

		// 11. Update optimal solution
		// Critical fix: Evaluate on a stable graph state (after unban)
		// First unban all edges to get a stable state for evaluation
		solver.unbanAllEdges(resGraph)

		// Validate current solution against stable graph before evaluation
		validPaths := solver.validatePaths(allPaths, resGraph)
		if validPaths.Len() > 0 {
			currentScore := solver.objective(validPaths, resGraph)
			if currentScore > bestScore {
				bestPaths = solver.copyPathList(validPaths)
				bestScore = currentScore
			}
		}
	}

	// Convert results to PathInfo
	pathInfos := make([]routing.PathInfo, 0, bestPaths.Len())
	for e := bestPaths.Front(); e != nil; e = e.Next() {
		pi := e.Value.(*PathWithInfo)
		pathInfos = append(pathInfos, routing.PathInfo{
			Hops:   pi.path,
			Rtt:    pi.latency,
			RawRTT: pi.latency,
		})
	}

	logger.Info("FlowOptimizationSolver completed",
		slog.String("pre", pre),
		slog.String("start", start),
		slog.Int("destCount", len(validEnds)),
		slog.Int("totalFlows", len(pathInfos)))

	return pathInfos, nil
}
