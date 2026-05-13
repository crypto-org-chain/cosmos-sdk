package mempool

import "github.com/huandu/skiplist"

func nextPriorityCursor[C comparable](current *skiplist.Element, index *skiplist.SkipList) (*skiplist.Element, string, bool) {
	if current == nil {
		current = index.Front()
	} else {
		current = current.Next()
	}

	if current == nil {
		return nil, "", false
	}

	sender := current.Key().(txMeta[C]).sender
	return current, sender, true
}
