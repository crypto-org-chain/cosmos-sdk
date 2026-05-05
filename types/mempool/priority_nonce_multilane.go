package mempool

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/huandu/skiplist"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ ExtMempool = (*MultiLanePriorityNonceMempool[int64])(nil)
	_ Iterator   = (*MultiLanePriorityNonceIterator[int64])(nil)
)

type (
	MultiLanePriorityNonceMempool[C comparable] struct {
		mtx            sync.Mutex
		priorityIndex  *skiplist.SkipList
		priorityCounts map[C]int
		senderIndices  map[string]*skiplist.SkipList
		scores         map[txMeta[C]]txMeta[C]
		laneOwners     map[multiLaneKey]multiLaneKey
		txLanes        map[multiLaneKey][]multiLaneKey
		cfg            PriorityNonceMempoolConfig[C]
	}

	MultiLanePriorityNonceIterator[C comparable] struct {
		mempool       *MultiLanePriorityNonceMempool[C]
		priorityNode  *skiplist.Element
		senderCursors map[string]*skiplist.Element
		selected      Tx
		deferred      map[multiLaneKey]struct{}
		deferredIndex *skiplist.SkipList
	}

	multiLaneKey struct {
		sender string
		nonce  uint64
	}
)

func NewMultiLanePriorityMempool[C comparable](cfg PriorityNonceMempoolConfig[C]) *MultiLanePriorityNonceMempool[C] {
	if cfg.SignerExtractor == nil {
		cfg.SignerExtractor = NewDefaultSignerExtractionAdapter()
	}

	return &MultiLanePriorityNonceMempool[C]{
		priorityIndex:  skiplist.New(skiplistComparable(cfg.TxPriority)),
		priorityCounts: make(map[C]int),
		senderIndices:  make(map[string]*skiplist.SkipList),
		scores:         make(map[txMeta[C]]txMeta[C]),
		laneOwners:     make(map[multiLaneKey]multiLaneKey),
		txLanes:        make(map[multiLaneKey][]multiLaneKey),
		cfg:            cfg,
	}
}

func DefaultMultiLanePriorityMempool() *MultiLanePriorityNonceMempool[int64] {
	return NewMultiLanePriorityMempool(DefaultPriorityNonceMempoolConfig())
}

func (mp *MultiLanePriorityNonceMempool[C]) NextSenderTx(sender string) sdk.Tx {
	mp.mtx.Lock()
	defer mp.mtx.Unlock()

	senderIndex, ok := mp.senderIndices[sender]
	if !ok {
		return nil
	}

	cursor := senderIndex.Front()
	if cursor == nil {
		return nil
	}

	return cursor.Value.(Tx).Tx
}

func (mp *MultiLanePriorityNonceMempool[C]) InsertWithGasWanted(ctx context.Context, tx sdk.Tx, gasWanted uint64) error {
	mp.mtx.Lock()
	defer mp.mtx.Unlock()

	if mp.cfg.MaxTx < 0 {
		return nil
	}

	memTx := NewMempoolTx(tx, gasWanted)

	sigs, err := mp.cfg.SignerExtractor.GetSigners(tx)
	if err != nil {
		return err
	}
	if len(sigs) == 0 {
		return fmt.Errorf("tx must have at least one signer")
	}

	lanes, err := multiLaneSigners(sigs, tx)
	if err != nil {
		return err
	}

	anchor := lanes[0]
	priority := mp.cfg.TxPriority.GetTxPriority(ctx, tx)
	conflicts := make(map[multiLaneKey]struct{})
	for _, lane := range lanes {
		conflictAnchor, ok := mp.laneOwners[lane]
		if !ok {
			continue
		}

		oldScore, ok := mp.scores[multiLaneToMeta[C](conflictAnchor)]
		if !ok {
			return fmt.Errorf("tx score not found for lane %+v", lane)
		}

		if mp.cfg.TxReplacement != nil {
			oldTx, err := mp.getLaneTx(lane, oldScore.priority)
			if err != nil {
				return err
			}

			if !mp.cfg.TxReplacement(oldScore.priority, priority, oldTx, tx) {
				return fmt.Errorf(
					"tx doesn't fit the replacement rule on lane %+v, oldPriority: %v, newPriority: %v, oldTx: %v, newTx: %v",
					lane,
					oldScore.priority,
					priority,
					oldTx,
					tx,
				)
			}
		}

		conflicts[conflictAnchor] = struct{}{}
	}

	// Replacement conflicts can free up existing entries and should count
	// toward max-txs admission.
	if mp.cfg.MaxTx > 0 && (mp.priorityIndex.Len()-len(conflicts)+1) > mp.cfg.MaxTx {
		return ErrMempoolTxMaxCapacity
	}

	for conflict := range conflicts {
		if err := mp.removeByAnchor(conflict); err != nil {
			return err
		}
	}

	anchorElement := mp.insertTxLanes(lanes, anchor, priority, memTx)

	mp.priorityCounts[priority]++

	anchorScoreKey := multiLaneToMeta[C](anchor)
	anchorPriorityKey := txMeta[C]{
		nonce:         anchor.nonce,
		priority:      priority,
		sender:        anchor.sender,
		senderElement: anchorElement,
	}
	mp.scores[anchorScoreKey] = txMeta[C]{priority: priority}
	mp.txLanes[anchor] = lanes
	mp.priorityIndex.Set(anchorPriorityKey, tx)

	return nil
}

func (mp *MultiLanePriorityNonceMempool[C]) Insert(ctx context.Context, tx sdk.Tx) error {
	var gasLimit uint64
	if gasTx, ok := tx.(GasTx); ok {
		gasLimit = gasTx.GetGas()
	}

	return mp.InsertWithGasWanted(ctx, tx, gasLimit)
}

func (i *MultiLanePriorityNonceIterator[C]) Next() Iterator {
	if i.mempool == nil {
		return nil
	}

	if i.selectFromDeferred() {
		return i
	}

	for {
		node, sender, _, ok := nextPriorityCursor(i.priorityNode, i.mempool.priorityIndex, i.mempool.cfg.TxPriority.MinValue)
		if !ok {
			return nil
		}
		i.priorityNode = node

		anchor := multiLaneKey{sender: sender, nonce: node.Key().(txMeta[C]).nonce}

		if i.trySelectAnchor(anchor) {
			return i
		}

		i.deferAnchor(anchor)
	}
}

func (i *MultiLanePriorityNonceIterator[C]) Tx() Tx {
	return i.selected
}

func (mp *MultiLanePriorityNonceMempool[C]) Select(ctx context.Context, txs [][]byte) Iterator {
	mp.mtx.Lock()
	defer mp.mtx.Unlock()
	return mp.doSelect(ctx, txs)
}

func (mp *MultiLanePriorityNonceMempool[C]) doSelect(_ context.Context, _ [][]byte) Iterator {
	if mp.priorityIndex.Len() == 0 {
		return nil
	}

	mp.reorderPriorityTies()

	iterator := &MultiLanePriorityNonceIterator[C]{
		mempool:       mp,
		senderCursors: make(map[string]*skiplist.Element),
		deferred:      make(map[multiLaneKey]struct{}),
		deferredIndex: skiplist.New(skiplistComparable(mp.cfg.TxPriority)),
	}

	return iterator.Next()
}

func (mp *MultiLanePriorityNonceMempool[C]) SelectBy(ctx context.Context, txs [][]byte, callback func(Tx) bool) {
	mp.mtx.Lock()
	defer mp.mtx.Unlock()

	iter := mp.doSelect(ctx, txs)
	for iter != nil && callback(iter.Tx()) {
		iter = iter.Next()
	}
}

func (mp *MultiLanePriorityNonceMempool[C]) reorderPriorityTies() {
	node := mp.priorityIndex.Front()

	var reordering []reorderKey[C]
	for node != nil {
		key := node.Key().(txMeta[C])
		if mp.priorityCounts[key.priority] > 1 {
			newKey := key
			newKey.weight = senderWeight(mp.cfg.TxPriority, key.senderElement)
			reordering = append(reordering, reorderKey[C]{deleteKey: key, insertKey: newKey, tx: node.Value.(sdk.Tx)})
		}
		node = node.Next()
	}

	for _, k := range reordering {
		mp.priorityIndex.Remove(k.deleteKey)
		delete(mp.scores, txMeta[C]{nonce: k.deleteKey.nonce, sender: k.deleteKey.sender})
		mp.priorityIndex.Set(k.insertKey, k.tx)
		mp.scores[txMeta[C]{nonce: k.insertKey.nonce, sender: k.insertKey.sender}] = k.insertKey
	}
}

func (mp *MultiLanePriorityNonceMempool[C]) CountTx() int {
	mp.mtx.Lock()
	defer mp.mtx.Unlock()
	return mp.priorityIndex.Len()
}

func (mp *MultiLanePriorityNonceMempool[C]) Remove(tx sdk.Tx) error {
	sigs, err := mp.cfg.SignerExtractor.GetSigners(tx)
	if err != nil {
		return err
	}
	if len(sigs) == 0 {
		return fmt.Errorf("attempted to remove a tx with no signatures")
	}

	lanes, err := multiLaneSigners(sigs, tx)
	if err != nil {
		return err
	}

	mp.mtx.Lock()
	defer mp.mtx.Unlock()

	for _, lane := range lanes {
		anchor, ok := mp.laneOwners[lane]
		if !ok {
			continue
		}
		return mp.removeByAnchor(anchor)
	}

	return ErrTxNotFound
}

func multiLaneSigners(sigs []SignerData, tx sdk.Tx) ([]multiLaneKey, error) {
	lanes := make([]multiLaneKey, 0, len(sigs))
	seen := make(map[multiLaneKey]struct{}, len(sigs))
	for _, sig := range sigs {
		nonce, err := ChooseNonce(sig.Sequence, tx)
		if err != nil {
			return nil, err
		}

		lane := multiLaneKey{sender: sig.Signer.String(), nonce: nonce}
		if _, exists := seen[lane]; exists {
			return nil, fmt.Errorf("tx contains duplicate lane (%s, %d)", lane.sender, lane.nonce)
		}
		seen[lane] = struct{}{}
		lanes = append(lanes, lane)
	}
	return lanes, nil
}

func multiLaneToMeta[C comparable](lane multiLaneKey) txMeta[C] {
	return txMeta[C]{nonce: lane.nonce, sender: lane.sender}
}

func (mp *MultiLanePriorityNonceMempool[C]) insertTxLanes(lanes []multiLaneKey, anchor multiLaneKey, priority C, memTx Tx) *skiplist.Element {
	var anchorElement *skiplist.Element
	for _, lane := range lanes {
		senderIndex := mp.senderIndices[lane.sender]
		if senderIndex == nil {
			senderIndex = newMultiLaneSenderIndex[C]()
			mp.senderIndices[lane.sender] = senderIndex
		}

		key := txMeta[C]{nonce: lane.nonce, priority: priority, sender: lane.sender}
		el := senderIndex.Set(key, memTx)
		if lane == anchor {
			anchorElement = el
		}

		mp.laneOwners[lane] = anchor
	}

	return anchorElement
}

func (mp *MultiLanePriorityNonceMempool[C]) getLaneTx(lane multiLaneKey, priority C) (sdk.Tx, error) {
	senderIndex := mp.senderIndices[lane.sender]
	if senderIndex == nil {
		return nil, fmt.Errorf("sender %s not found", lane.sender)
	}

	key := txMeta[C]{nonce: lane.nonce, priority: priority, sender: lane.sender}
	el := senderIndex.Get(key)
	if el == nil {
		return nil, fmt.Errorf("tx not found on lane (%s, %d)", lane.sender, lane.nonce)
	}

	return el.Value.(Tx).Tx, nil
}

func (mp *MultiLanePriorityNonceMempool[C]) removeByAnchor(anchor multiLaneKey) error {
	scoreKey := multiLaneToMeta[C](anchor)
	score, ok := mp.scores[scoreKey]
	if !ok {
		return ErrTxNotFound
	}

	lanes := mp.txLanes[anchor]
	if len(lanes) == 0 {
		lanes = []multiLaneKey{anchor}
	}

	for _, lane := range lanes {
		if err := mp.removeLane(lane, score.priority); err != nil {
			return err
		}

		delete(mp.laneOwners, lane)
	}

	priorityKey := txMeta[C]{
		nonce:    anchor.nonce,
		priority: score.priority,
		sender:   anchor.sender,
		weight:   score.weight,
	}
	mp.priorityIndex.Remove(priorityKey)
	delete(mp.scores, scoreKey)
	delete(mp.txLanes, anchor)

	mp.priorityCounts[score.priority]--
	if mp.priorityCounts[score.priority] == 0 {
		delete(mp.priorityCounts, score.priority)
	}

	return nil
}

func (mp *MultiLanePriorityNonceMempool[C]) removeLane(lane multiLaneKey, priority C) error {
	senderTxs := mp.senderIndices[lane.sender]
	if senderTxs == nil {
		return fmt.Errorf("sender %s not found", lane.sender)
	}

	key := txMeta[C]{nonce: lane.nonce, priority: priority, sender: lane.sender}
	if senderTxs.Remove(key) == nil {
		return ErrTxNotFound
	}
	if senderTxs.Len() == 0 {
		delete(mp.senderIndices, lane.sender)
	}

	return nil
}

func newMultiLaneSenderIndex[C comparable]() *skiplist.SkipList {
	return skiplist.New(skiplist.LessThanFunc(func(a, b any) int {
		return skiplist.Uint64.Compare(b.(txMeta[C]).nonce, a.(txMeta[C]).nonce)
	}))
}

func (i *MultiLanePriorityNonceIterator[C]) selectFromDeferred() bool {
	for node := i.deferredIndex.Front(); node != nil; {
		next := node.Next()
		anchor := node.Value.(multiLaneKey)

		if i.trySelectAnchor(anchor) {
			i.deferredIndex.Remove(node.Key())
			delete(i.deferred, anchor)
			return true
		}
		node = next
	}
	return false
}

func (i *MultiLanePriorityNonceIterator[C]) trySelectAnchor(anchor multiLaneKey) bool {
	lanes := i.mempool.txLanes[anchor]
	if len(lanes) == 0 {
		lanes = []multiLaneKey{anchor}
	}

	lanesBySender := make(map[string][]uint64, len(lanes))
	for _, lane := range lanes {
		lanesBySender[lane.sender] = append(lanesBySender[lane.sender], lane.nonce)
	}

	nextCursors := make(map[string]*skiplist.Element, len(lanesBySender))
	for sender, nonces := range lanesBySender {
		slices.Sort(nonces)
		cursor := i.senderCursors[sender]
		senderIndex := i.mempool.senderIndices[sender]
		if senderIndex == nil {
			return false
		}

		for _, nonce := range nonces {
			if cursor == nil {
				cursor = senderIndex.Front()
			} else {
				cursor = cursor.Next()
			}
			if cursor == nil {
				return false
			}

			cursorKey := cursor.Key().(txMeta[C])
			if cursorKey.sender != sender || cursorKey.nonce != nonce {
				return false
			}

			owner, ok := i.mempool.laneOwners[multiLaneKey{sender: sender, nonce: nonce}]
			if !ok || owner != anchor {
				return false
			}
		}
		nextCursors[sender] = cursor
	}

	for sender, cursor := range nextCursors {
		i.senderCursors[sender] = cursor
	}
	i.selected = nextCursors[anchor.sender].Value.(Tx)
	return true
}

func (i *MultiLanePriorityNonceIterator[C]) deferAnchor(anchor multiLaneKey) {
	if _, ok := i.deferred[anchor]; ok {
		return
	}

	key, ok := i.mempool.anchorPriorityKey(anchor)
	if !ok {
		return
	}
	i.deferredIndex.Set(key, anchor)
	i.deferred[anchor] = struct{}{}
}

func (mp *MultiLanePriorityNonceMempool[C]) anchorPriorityKey(anchor multiLaneKey) (txMeta[C], bool) {
	score, ok := mp.scores[multiLaneToMeta[C](anchor)]
	if !ok {
		var zero txMeta[C]
		return zero, false
	}
	return txMeta[C]{
		nonce:    anchor.nonce,
		priority: score.priority,
		sender:   anchor.sender,
		weight:   score.weight,
	}, true
}

func IsMultiLaneEmpty[C comparable](mempool Mempool) error {
	mp := mempool.(*MultiLanePriorityNonceMempool[C])
	if mp.priorityIndex.Len() != 0 {
		return fmt.Errorf("priorityIndex not empty")
	}

	countKeys := make([]C, 0, len(mp.priorityCounts))
	for k := range mp.priorityCounts {
		countKeys = append(countKeys, k)
	}
	for _, k := range countKeys {
		if mp.priorityCounts[k] != 0 {
			return fmt.Errorf("priorityCounts not zero at %v, got %v", k, mp.priorityCounts[k])
		}
	}

	senderKeys := make([]string, 0, len(mp.senderIndices))
	for k := range mp.senderIndices {
		senderKeys = append(senderKeys, k)
	}
	for _, k := range senderKeys {
		if mp.senderIndices[k].Len() != 0 {
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
