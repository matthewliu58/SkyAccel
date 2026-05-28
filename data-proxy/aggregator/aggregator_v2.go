package aggregator

import (
	"container/heap"
	"context"
	"data-proxy/config"
	manager "data-proxy/tunnel-manager"
	packet "data-proxy/tunnel-packet"
	"data-proxy/util"
	"hash/fnv"
	"log/slog"
	"net"
	"sync"
	"time"
)

// ==================== Optimization 1: Object Pool ====================
var bufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 64*1024) // Pre-allocate 64KB buffer
	},
}

// ==================== Optimization 2: Sharded Lock Configuration ====================
const shardCount = 16

// ==================== Core Data Structures ====================
type aggregatorMsgV2 struct {
	emerge      bool
	routingKey  string
	protocol    string
	port        uint16
	routingInfo util.PathInfo
	nextHop     net.IP
	userID      uint32
	data        []byte
}

type BatchV2 struct {
	mu         sync.Mutex // Use plain mutex since shard lock is at outer level
	BuffSize   int
	RoutingKey string
	NextHop    net.IP
	pkt        *packet.Packet
	heapItem   *HeapItemV2
	createTime time.Time
}

type HeapItemV2 struct {
	batch    *BatchV2
	deadline time.Time
	index    int
}

type MinHeapV2 []*HeapItemV2

func (h MinHeapV2) Len() int           { return len(h) }
func (h MinHeapV2) Less(i, j int) bool { return h[i].deadline.Before(h[j].deadline) }
func (h MinHeapV2) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *MinHeapV2) Push(x any) {
	n := len(*h)
	item := x.(*HeapItemV2)
	item.index = n
	*h = append(*h, item)
}

func (h *MinHeapV2) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}

// ==================== Optimization 3: Worker Pool ====================
type sendTask struct {
	buf        []byte
	routingKey string
	nextHop    net.IP
}

type sendWorker struct {
	taskCh chan sendTask
	stopCh chan struct{}
}

// ==================== Sharded Worker ====================
type shardWorker struct {
	batches map[string][]*BatchV2
	heap    MinHeapV2
	mu      sync.RWMutex // Shard-level lock
}

// ==================== Optimized Aggregator ====================
type AggregatorV2 struct {
	inputChan    chan *aggregatorMsgV2
	shardMu      [shardCount]sync.RWMutex // Optimization 2: Sharded lock
	shardWorkers [shardCount]*shardWorker
	sendWorkers  []*sendWorker
	sendTaskCh   chan sendTask
	wg           sync.WaitGroup
	stopCh       chan struct{}
}

var GlobalAggRequestV2 *AggregatorV2
var GlobalAggResponseV2 *AggregatorV2

// Hash function for sharding
func hashRoutingKey(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32()) % shardCount
}

func NewAggregatorV2(pre string, l *slog.Logger) *AggregatorV2 {
	aggWorkerNum_ := config.Config_.AggWorkerNum
	if aggWorkerNum_ <= 0 {
		aggWorkerNum_ = aggWorkerNum
	}

	l.Info("NewAggregatorV2", "pre", pre, "aggWorkerNum_", aggWorkerNum_)

	agg := &AggregatorV2{
		inputChan:  make(chan *aggregatorMsgV2, inputChanSize),
		sendTaskCh: make(chan sendTask, 10000),
		stopCh:     make(chan struct{}),
	}

	// Initialize sharded workers
	for i := 0; i < shardCount; i++ {
		agg.shardWorkers[i] = &shardWorker{
			batches: make(map[string][]*BatchV2),
			heap:    make(MinHeapV2, 0),
		}
	}

	// Initialize send worker pool (Optimization 3)
	sendWorkerCount := config.Config_.AggWorkerCount
	if sendWorkerCount <= 0 {
		sendWorkerCount = aggWorkerCount
	}
	agg.sendWorkers = make([]*sendWorker, sendWorkerCount)
	for i := 0; i < sendWorkerCount; i++ {
		agg.sendWorkers[i] = &sendWorker{
			taskCh: make(chan sendTask, 100),
			stopCh: make(chan struct{}),
		}
	}

	return agg
}

func (a *AggregatorV2) Start(pre string, logger *slog.Logger) {
	logger.Info("AggregatorV2 Start", "pre", pre)

	// Start message handling goroutines
	for i := 0; i < shardCount; i++ {
		a.wg.Add(1)
		go func(shardIdx int) {
			defer a.wg.Done()
			for msg := range a.inputChan {
				shard := hashRoutingKey(msg.routingKey)
				if shard == shardIdx {
					a.handleMsg(msg, shardIdx, logger)
				}
			}
		}(i)
	}

	// Start timeout checking goroutines
	for i := 0; i < shardCount; i++ {
		a.wg.Add(1)
		go func(shardIdx int) {
			defer a.wg.Done()
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					a.checkTimeout(shardIdx, logger)
				case <-a.stopCh:
					return
				}
			}
		}(i)
	}

	// Start stale batch eviction goroutine
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.evictStaleBatches(logger)
			case <-a.stopCh:
				return
			}
		}
	}()

	// Start send worker pool (Optimization 3)
	for _, sw := range a.sendWorkers {
		a.wg.Add(1)
		go func(w *sendWorker) {
			defer a.wg.Done()
			for {
				select {
				case task := <-w.taskCh:
					a.sendPacket(task, logger)
				case <-w.stopCh:
					return
				}
			}
		}(sw)
	}

	// Start task dispatcher goroutine
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for task := range a.sendTaskCh {
			idx := hashRoutingKey(task.routingKey) % len(a.sendWorkers)
			a.sendWorkers[idx].taskCh <- task
		}
	}()
}

func (a *AggregatorV2) AddToBatch(
	emerge bool,
	routingKey string,
	protocol string,
	port uint16,
	routingInfo util.PathInfo,
	nextHop net.IP,
	userID uint32,
	data []byte,
) {
	a.inputChan <- &aggregatorMsgV2{
		emerge:      emerge,
		routingKey:  routingKey,
		protocol:    protocol,
		port:        port,
		routingInfo: routingInfo,
		nextHop:     nextHop,
		userID:      userID,
		data:        data,
	}
}

func (a *AggregatorV2) handleMsg(msg *aggregatorMsgV2, shardIdx int, logger *slog.Logger) {
	sw := a.shardWorkers[shardIdx]
	buffSize := config.Config_.Aggregator.BufferSize
	batchTimeout := time.Duration(config.Config_.Aggregator.BatchTimeoutMs) * time.Millisecond

	if len(msg.data) >= 1024 {
		msg.emerge = true
		buffSize = packet.HeaderSize + 4 + 2 + len(msg.data)
	}

	if msg.emerge {
		pkt := packet.NewPacket(buffSize)
		for i, h := range msg.routingInfo.Hops {
			pkt.SetHopIP(i, util.HopIPToNet(h))
		}
		pkt.SetPort(msg.port)
		pkt.SetProtocol(msg.protocol)
		pkt.SetHopPos(1)
		if !pkt.AppendUserPacket(msg.userID, msg.data, logger) {
			logger.Error("AppendUserPacket failed for emerge message",
				slog.Int("shardIdx", shardIdx), slog.Int("buffSize", buffSize),
				slog.Int("dataLen", len(msg.data)))
			return
		}
		pkt.SerializeHead()

		// Optimization 1: Use object pool
		buf := bufPool.Get().([]byte)[:pkt.TotalBytes()]
		copy(buf, pkt.Buf[:pkt.TotalBytes()])
		a.sendTaskCh <- sendTask{buf, msg.routingKey, msg.nextHop}
		return
	}

	var b *BatchV2 = nil

	// Optimization 2: Use sharded lock (read lock)
	sw.mu.RLock()
	bList := sw.batches[msg.routingKey]
	lList := len(bList)
	if lList > 0 {
		if lList <= 1 {
			b = bList[0]
		} else {
			idx := int(msg.userID) % lList
			b = bList[idx]
		}
	}
	sw.mu.RUnlock()

	if b == nil {
		var batches []*BatchV2
		num, ok_ := config.BatchNumMap[msg.port]
		if !ok_ || num <= 0 {
			num = 1
		}
		for i := 0; i < num; i++ {
			b_ := &BatchV2{
				BuffSize:   buffSize,
				RoutingKey: msg.routingKey,
				NextHop:    msg.nextHop,
				pkt:        packet.NewPacket(buffSize),
				createTime: time.Now(),
			}
			for j, h := range msg.routingInfo.Hops {
				b_.pkt.SetHopIP(j, util.HopIPToNet(h))
			}
			b_.pkt.SetPort(msg.port)
			b_.pkt.SetProtocol(msg.protocol)
			b_.pkt.SetHopPos(1)
			batches = append(batches, b_)
		}

		sw.mu.Lock()
		var ok bool
		bList, ok = sw.batches[msg.routingKey]
		if !ok {
			sw.batches[msg.routingKey] = batches
		}
		bList, _ = sw.batches[msg.routingKey]
		lList = len(bList)
		if lList > 0 {
			if lList <= 1 {
				b = bList[0]
			} else {
				idx := int(msg.userID % uint32(lList))
				b = bList[idx]
			}
		}
		sw.mu.Unlock()
	}

	if b != nil {
		b.mu.Lock()
		if (b.heapItem == nil && b.pkt.Wp > packet.HeaderSize) ||
			!b.pkt.AppendUserPacket(msg.userID, msg.data, logger) {

			b.mu.Unlock()

			b_ := &BatchV2{
				BuffSize:   buffSize,
				RoutingKey: msg.routingKey,
				NextHop:    msg.nextHop,
				pkt:        packet.NewPacket(buffSize),
				createTime: time.Now(),
			}
			for j, h := range msg.routingInfo.Hops {
				b_.pkt.SetHopIP(j, util.HopIPToNet(h))
			}
			b_.pkt.SetPort(msg.port)
			b_.pkt.SetProtocol(msg.protocol)
			b_.pkt.SetHopPos(1)

			b = b_
			b.mu.Lock()

			sw.mu.Lock()
			sw.batches[msg.routingKey] = append(sw.batches[msg.routingKey], b_)
			sw.mu.Unlock()
			b.pkt.AppendUserPacket(msg.userID, msg.data, logger)
		}

		if b.heapItem == nil {
			item := &HeapItemV2{batch: b, deadline: time.Now().Add(batchTimeout)}
			sw.mu.Lock()
			heap.Push(&sw.heap, item)
			sw.mu.Unlock()
			b.heapItem = item
		}
		b.mu.Unlock()
	} else {
		logger.Error("create batch failed", slog.Int("shardIdx", shardIdx), slog.Any("userID", msg.userID))
		return
	}
}

func (a *AggregatorV2) checkTimeout(shardIdx int, logger *slog.Logger) {
	sw := a.shardWorkers[shardIdx]
	now := time.Now()
	var expired []*HeapItemV2

	sw.mu.Lock()
	for sw.heap.Len() > 0 {
		b := sw.heap[0]
		if b.deadline.After(now) {
			break
		}
		popItem := heap.Pop(&sw.heap).(*HeapItemV2)
		expired = append(expired, popItem)
	}
	sw.mu.Unlock()

	for _, item := range expired {
		b := item.batch
		b.mu.Lock()
		b.pkt.SerializeHead()

		// Optimization 1: Use object pool
		buf := bufPool.Get().([]byte)[:b.pkt.TotalBytes()]
		copy(buf, b.pkt.Buf[:b.pkt.TotalBytes()])

		a.sendTaskCh <- sendTask{buf, b.RoutingKey, b.NextHop}

		b.pkt.Wp = packet.HeaderSize
		b.createTime = now
		b.heapItem = nil
		b.mu.Unlock()
	}
}

func (a *AggregatorV2) sendPacket(task sendTask, logger *slog.Logger) {
	logger.Info("send packet", slog.String("routingKey", task.routingKey), slog.Any("bufLen", len(task.buf)))
	err := manager.TunnelMgr.SendPacket(context.Background(), task.nextHop, task.buf, task.nextHop.String(), logger)

	// Optimization 1: Return to object pool
	defer bufPool.Put(task.buf[:cap(task.buf)])

	if err != nil {
		logger.Error("send packet failed", slog.Any("nextHop", task.nextHop), slog.Any("err", err))
	}
}

func (a *AggregatorV2) evictStaleBatches(logger *slog.Logger) {
	now := time.Now()

	for i := 0; i < shardCount; i++ {
		sw := a.shardWorkers[i]

		sw.mu.RLock()
		batches := make(map[string][]*BatchV2)
		for k, v := range sw.batches {
			batches[k] = v
		}
		sw.mu.RUnlock()

		for k, v := range batches {
			bDel := false
			for _, b := range v {
				stale := now.Sub(b.createTime) > batchMaxAge
				if stale {
					bDel = true
					break
				}
			}

			if bDel {
				sw.mu.Lock()
				for _, b := range v {
					if b.heapItem != nil {
						heap.Remove(&sw.heap, b.heapItem.index)
					}
				}
				delete(sw.batches, k)
				sw.mu.Unlock()
				logger.Info("evict stale batch", slog.Int("shardIdx", i), slog.String("routingKey", k))
			}
		}
	}
}

func (a *AggregatorV2) Stop() {
	close(a.stopCh)
	close(a.inputChan)
	close(a.sendTaskCh)
	for _, sw := range a.sendWorkers {
		close(sw.taskCh)
	}
	a.wg.Wait()
}
