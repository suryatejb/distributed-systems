// Config options used by tests to control network conditions.

package staff

import (
	"sync/atomic"
)

var artiLatencyZero int32 = 0
var artiLatencyMsPtr *int32 = &artiLatencyZero

// For testing use: sets the artificial latency used for new DialWithLatency
// calls.
func SetArtiLatencyMs(latencyMs int) {
	atomic.StoreInt32(artiLatencyMsPtr, int32(latencyMs))
}

// In ms.
func artiLatencyMs() int {
	return int(atomic.LoadInt32(artiLatencyMsPtr))
}
