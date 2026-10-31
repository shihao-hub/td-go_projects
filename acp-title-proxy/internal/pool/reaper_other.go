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
