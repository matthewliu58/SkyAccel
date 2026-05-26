package collector

import (
	model "data-plane/report-info"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
)

const (
	cpuWindow = 60 * time.Second
)

var (
	lastCPU     model.CPUInfo
	lastCPUTime time.Time
	cpuMu       sync.Mutex
)

func collectCPU() (model.CPUInfo, error) {

	cpuCounts, err := cpu.Counts(true)
	if err != nil {
		return model.CPUInfo{}, err
	}
	physicalCounts, err := cpu.Counts(false)
	if err != nil {
		return model.CPUInfo{}, err
	}

	percent, err := cpu.Percent(1*time.Second, true)
	if err != nil {
		return model.CPUInfo{}, err
	}

	totalUsage := 0.0
	for _, p := range percent {
		totalUsage += p
	}

	var load1Min = 0.0
	loadStat, err := load.Avg()
	if err == nil {
		load1Min = loadStat.Load1
	}

	cpuMu.Lock()
	defer cpuMu.Unlock()

	now := time.Now()
	info := model.CPUInfo{
		PhysicalCore: physicalCounts,
		LogicalCore:  cpuCounts,
		Usage:        totalUsage,
		Load1Min:     load1Min,
	}

	elapsed := now.Sub(lastCPUTime)
	if !(lastCPUTime.IsZero() || elapsed > cpuWindow) {
		info.LoadDelta = totalUsage - lastCPU.Usage
	}

	lastCPU = info
	lastCPUTime = now

	return info, nil
}
