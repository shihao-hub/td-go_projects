//go:build !windows

package pool

import (
	"time"
)

func (r *Reaper) isProcessCPUIdle(pid int, sampleDuration time.Duration) bool {
	return false
}

func (r *Reaper) findCandidates() []HarnessInfo {
	return nil
}

func (r *Reaper) collect() (map[int][]HarnessInfo, map[int]int) {
	return nil, nil
}

func treeCPU(pid int, parents map[int]int) time.Duration {
	return 0
}
