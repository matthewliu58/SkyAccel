package core_domain

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"control-plane/routing/graph"
)

// parseBrainTopology reads the brain topology file and returns edges
func parseBrainTopology(filePath string) ([]*graph.Edge, map[string]struct{}) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Failed to open file: %v\n", err)
		return nil, nil
	}
	defer file.Close()

	var edges []*graph.Edge
	nodes := make(map[string]struct{})
	var currentSection string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Track current section
		if strings.HasPrefix(line, "NODES") {
			currentSection = "NODES"
			continue
		}
		if strings.HasPrefix(line, "LINKS") {
			currentSection = "LINKS"
			continue
		}
		if strings.HasPrefix(line, ")") && currentSection != "" {
			currentSection = ""
			continue
		}

		// Parse NODES section
		if currentSection == "NODES" && line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "NODES") && strings.Contains(line, "(") {
			// Node format: NODE_ID ( longitude latitude )
			start := strings.Index(line, "(")
			if start != -1 {
				nodeID := strings.TrimSpace(line[:start])
				if nodeID != "" {
					nodes[nodeID] = struct{}{}
				}
			}
		}

		// Parse links section
		if currentSection == "LINKS" && line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "LINKS") {
			// Link format: LINK_ID ( SOURCE TARGET ) ...
			// Example: HU_HU5 ( HU HU5 ) 0.00 0.00 0.00 0.00 ( 1000000000.00 136.42 10000000000.00 1044.17 )

			// Find the parentheses with source and target
			start := strings.Index(line, "(")
			end := strings.Index(line, ")")
			if start != -1 && end != -1 && end > start {
				nodesStr := line[start+1 : end]
				nodes := strings.Fields(nodesStr)
				if len(nodes) >= 2 {
					source := nodes[0]
					target := nodes[1]

					// Extract RTT from comment if present
					var rtt float64 = 1.0
					if idx := strings.Index(line, "# RTT:"); idx != -1 {
						rttStr := strings.TrimSpace(line[idx+5:])
						rttStr = strings.TrimSuffix(rttStr, "ms")
						fmt.Sscanf(rttStr, "%f", &rtt)
					}

					edge := &graph.Edge{
						SourceIp:      source,
						DestinationIp: target,
						EdgeWeight:    rtt,
					}
					edges = append(edges, edge)
				}
			}
		}
	}

	return edges, nodes
}

func TestBrainKShortestPaths(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Path to brain topology file
	brainFile := "evaluation/brain"

	// Parse topology
	edges, nodes := parseBrainTopology(brainFile)
	if edges == nil || len(edges) == 0 {
		t.Fatal("Failed to parse brain topology")
	}

	fmt.Printf("\n=== Brain Topology Stats ===\n")
	fmt.Printf("Total edges loaded: %d\n", len(edges))
	fmt.Printf("Total unique nodes: %d\n\n", len(nodes))

	// Create K-Shortest solver with k=5
	solver := NewKShortestSolver(edges, 5)

	// Test cases: various source-destination pairs
	testCases := []struct {
		start string
		end   string
	}{
		// Short distance pairs (same cluster)
		{"HU", "HU5"},
		{"HU", "HU10"},
		{"ZIB", "ZIB70"},
		{"SPK", "SPK20"},
		{"CVK", "CVK10"},

		// Longer distance pairs (cross-cluster)
		{"HU", "SPK"},
		{"ZIB", "TU"},
		{"ADH", "CVK"},
		{"HTW", "UP"},
		{"WIAS", "HU"},
	}

	fmt.Println("=== K-Shortest Path Test Results ===")
	fmt.Println("Finding K=5 paths, then selecting top 2")

	for _, tc := range testCases {
		fmt.Printf("========================================\n")
		fmt.Printf("Test: %s -> %s\n", tc.start, tc.end)
		fmt.Printf("========================================\n")

		// Check if nodes exist
		if _, ok := nodes[tc.start]; !ok {
			fmt.Printf("Start node '%s' not found in topology\n\n", tc.start)
			continue
		}
		if _, ok := nodes[tc.end]; !ok {
			fmt.Printf("End node '%s' not found in topology\n\n", tc.end)
			continue
		}

		// Find K shortest paths
		paths, err := solver.Computing(tc.start, tc.end, "TEST", logger)
		if err != nil {
			fmt.Printf("Error finding paths: %v\n\n", err)
			continue
		}

		// Print all K paths found
		fmt.Printf("\n[K=5 Paths Found]:\n")
		for i, p := range paths {
			hopStr := strings.Join(p.Hops, " -> ")
			fmt.Printf("  [%d] RTT=%.2fms, hops=%d: %s\n",
				i+1, p.Rtt, len(p.Hops), hopStr)
		}

		// Show selection logic
		fmt.Printf("\n[Select Top 2]:\n")
		shortCount := 0
		longCount := 0
		for _, p := range paths {
			if len(p.Hops) <= 4 {
				shortCount++
			} else {
				longCount++
			}
		}
		fmt.Printf("  Short paths (<=4 hops): %d\n", shortCount)
		fmt.Printf("  Long paths (>4 hops): %d\n", longCount)
		fmt.Printf("  -> Select %d short paths first, then %d long paths if needed\n",
			min(shortCount, 2), max(0, 2-shortCount))

		// Print final selected paths
		fmt.Printf("\n[Final 2 Paths Selected]:\n")
		for i, p := range paths {
			hopStr := strings.Join(p.Hops, " -> ")
			fmt.Printf("  [%d] RTT=%.2fms, hops=%d: %s\n",
				i+1, p.Rtt, len(p.Hops), hopStr)
		}
		fmt.Println()
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestBrainKShortestPathsAllPairs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	brainFile := "evaluation/brain"
	edges, nodes := parseBrainTopology(brainFile)
	if edges == nil || len(edges) == 0 {
		t.Fatal("Failed to parse brain topology")
	}

	solver := NewKShortestSolver(edges, 5)

	// Get unique nodes
	nodeList := make([]string, 0, len(nodes))
	for n := range nodes {
		nodeList = append(nodeList, n)
	}

	// Test paths between major hub nodes
	majorNodes := []string{"HU", "ZIB", "SPK", "CVK", "TU", "HTW", "ADH", "UP", "WIAS"}

	fmt.Println("=== Major Hub-to-Hub Paths ===")

	for i := 0; i < len(majorNodes); i++ {
		for j := i + 1; j < len(majorNodes); j++ {
			start := majorNodes[i]
			end := majorNodes[j]

			paths, err := solver.Computing(start, end, "MAJOR", logger)
			if err != nil {
				continue
			}

			fmt.Printf("\n%s -> %s: %d paths found\n", start, end, len(paths))
			for idx, p := range paths {
				fmt.Printf("  [%d] %v (RTT=%.2fms, hops=%d)\n",
					idx+1, p.Hops, p.Rtt, len(p.Hops))
			}
		}
	}
}

// TestBrainListAllNodes lists all nodes in the brain topology
func TestBrainListAllNodes(t *testing.T) {
	brainFile := "evaluation/brain"
	_, nodes := parseBrainTopology(brainFile)
	if nodes == nil {
		t.Fatal("Failed to parse brain topology")
	}

	fmt.Println("\n=== All Brain Topology Nodes ===")
	fmt.Printf("Total nodes: %d\n\n", len(nodes))

	// Group nodes by prefix
	prefixes := make(map[string][]string)
	for node := range nodes {
		// Extract prefix (e.g., "HU" from "HU5", "HU10", etc.)
		prefix := node
		for len(prefix) > 0 && prefix[len(prefix)-1] >= '0' && prefix[len(prefix)-1] <= '9' {
			prefix = prefix[:len(prefix)-1]
		}
		if prefix == "" {
			prefix = node
		}
		prefixes[prefix] = append(prefixes[prefix], node)
	}

	// Print nodes grouped by prefix
	fmt.Println("Nodes grouped by cluster:")
	for prefix, nodeList := range prefixes {
		fmt.Printf("\n%s (%d nodes):\n", prefix, len(nodeList))
		// Sort nodes
		for i := 0; i < len(nodeList)-1; i++ {
			for j := i + 1; j < len(nodeList); j++ {
				if nodeList[i] > nodeList[j] {
					nodeList[i], nodeList[j] = nodeList[j], nodeList[i]
				}
			}
		}
		for _, n := range nodeList {
			fmt.Printf("  - %s\n", n)
		}
	}
}
