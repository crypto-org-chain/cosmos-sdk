package mempool

import "fmt"

// IsMultiLaneEmpty is a test helper that verifies all internal data structures
// of a MultiLanePriorityNonceMempool are fully drained (no leaks after remove).
func IsMultiLaneEmpty[C comparable](m Mempool) error {
	mp := m.(*MultiLanePriorityNonceMempool[C])
	if mp.priorityIndex.Len() != 0 {
		return fmt.Errorf("priorityIndex not empty")
	}
	for k, v := range mp.priorityCounts {
		if v != 0 {
			return fmt.Errorf("priorityCounts not zero at %v, got %v", k, v)
		}
	}
	for k, idx := range mp.senderIndices {
		if idx.Len() != 0 {
			return fmt.Errorf("senderIndex not empty for sender %v", k)
		}
	}
	if len(mp.laneOwners) != 0 {
		return fmt.Errorf("laneOwners not empty")
	}
	if len(mp.txLanes) != 0 {
		return fmt.Errorf("txLanes not empty")
	}
	return nil
}
