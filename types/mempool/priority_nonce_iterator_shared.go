package mempool

import "github.com/huandu/skiplist"

func nextPriorityCursor[C comparable](current *skiplist.Element, index *skiplist.SkipList, minPriority C) (*skiplist.Element, string, C, bool) {
	if current == nil {
		current = index.Front()
	} else {
		current = current.Next()
	}

	if current == nil {
		return nil, "", minPriority, false
	}

	sender := current.Key().(txMeta[C]).sender
	nextPriorityNode := current.Next()
	if nextPriorityNode != nil {
		return current, sender, nextPriorityNode.Key().(txMeta[C]).priority, true
	}

	return current, sender, minPriority, true
}
