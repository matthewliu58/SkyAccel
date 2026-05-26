package collector

import (
	model "data-plane/report-info"
	"math"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
)

const (
	sampleInterval = 1 * time.Second
)

var (
	physicalCores int
	logicalCores  int

	peakMu    sync.Mutex
	peakUsage float64
	maxDelta  float64
	prevUsage float64
	load1Min  float64

	startOnce sync.Once
	stopCh    chan struct{}
)

// StartCPUSampler starts background CPU sampling (once).
func StartCPUSampler() {
	startOnce.Do(func() {
		stopCh = make(chan struct{})
		go runSampler()
	})
}

// StopCPUSampler stops the background CPU sampler.
func StopCPUSampler() {
	if stopCh != nil {
		close(stopCh)
	}
}

func runSampler() {
	// cache core counts (static)
	physical, _ := cpu.Counts(false)
	logical, _ := cpu.Counts(true)
	physicalCores = physical
	logicalCores = logical

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	// seed initial prevUsage
	percent, _ := cpu.Percent(0, true)
	var initTotal float64
	for _, p := range percent {
		initTotal += p
	}
	prevUsage = initTotal

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			sample()
		}
	}
}

func sample() {
	// true = per-core, sum them up to match "top" output
	percent, err := cpu.Percent(0, true)
	if err != nil || len(percent) == 0 {
		return
	}

	total := 0.0
	for _, p := range percent {
		total += p
	}

	// update system load
	if l, err := load.Avg(); err == nil {
		load1Min = l.Load1
	}

	// track peak and delta
	peakMu.Lock()
	if total > peakUsage {
		peakUsage = total
	}

	// compute step delta
	d := math.Abs(total - prevUsage)
	if d > maxDelta {
		maxDelta = d
	}
	prevUsage = total
	peakMu.Unlock()
}

// collectCPU non-blocking; returns the current window's peak values.
func collectCPU() (model.CPUInfo, error) {
	peakMu.Lock()
	usage := peakUsage
	delta := maxDelta

	// reset window
	peakUsage = 0
	maxDelta = 0
	peakMu.Unlock()

	return model.CPUInfo{
		PhysicalCore: physicalCores,
		LogicalCore:  logicalCores,
		Usage:        usage,
		Load1Min:     load1Min,
		LoadDelta:    delta,
	}, nil
}
