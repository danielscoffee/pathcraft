package worldgraph

import (
	"container/list"
	"context"
	"fmt"
	"sync"
)

const (
	defaultShardIndexCacheEntries = 1_024
	maxConcurrentShardIndexLoads  = 6
)

type shardIndexCacheKey struct {
	generation string
	shard      TileID
}

type shardIndexCacheEntry struct {
	key   shardIndexCacheKey
	index shardIndex
}

type shardIndexFlight struct {
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	waiters int
	index   shardIndex
	err     error
}

type shardIndexCache struct {
	mu         sync.Mutex
	maxEntries int
	entries    map[shardIndexCacheKey]*list.Element
	lru        *list.List
	flights    map[shardIndexCacheKey]*shardIndexFlight
	loadSlots  chan struct{}
	closed     bool
}

func newShardIndexCache(maxEntries int) *shardIndexCache {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &shardIndexCache{
		maxEntries: maxEntries,
		entries:    make(map[shardIndexCacheKey]*list.Element),
		lru:        list.New(),
		flights:    make(map[shardIndexCacheKey]*shardIndexFlight),
		loadSlots:  make(chan struct{}, maxConcurrentShardIndexLoads),
	}
}

func (cache *shardIndexCache) Get(ctx context.Context, shard TileID, load func(context.Context) (shardIndex, error)) (shardIndex, error) {
	return cache.get(ctx, shardIndexCacheKey{shard: shard}, load)
}

func (cache *shardIndexCache) get(ctx context.Context, key shardIndexCacheKey, load func(context.Context) (shardIndex, error)) (shardIndex, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if load == nil {
		return shardIndex{}, fmt.Errorf("shard index loader is nil")
	}
	cache.mu.Lock()
	if cache.closed {
		cache.mu.Unlock()
		return shardIndex{}, ErrRouterClosed
	}
	if element := cache.entries[key]; element != nil {
		cache.lru.MoveToFront(element)
		index := cloneShardIndex(element.Value.(*shardIndexCacheEntry).index)
		cache.mu.Unlock()
		return index, nil
	}
	flight := cache.flights[key]
	if flight == nil {
		flightCtx, cancel := context.WithCancel(context.Background())
		flight = &shardIndexFlight{done: make(chan struct{}), ctx: flightCtx, cancel: cancel}
		cache.flights[key] = flight
		go cache.load(key, flight, load)
	}
	flight.waiters++
	cache.mu.Unlock()

	select {
	case <-ctx.Done():
		cache.releaseWaiter(key, flight)
		return shardIndex{}, ctx.Err()
	case <-flight.done:
		cache.releaseWaiter(key, flight)
		return cloneShardIndex(flight.index), flight.err
	}
}

func (cache *shardIndexCache) Close() {
	if cache == nil {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.closed {
		return
	}
	cache.closed = true
	for _, flight := range cache.flights {
		flight.cancel()
	}
	cache.entries = make(map[shardIndexCacheKey]*list.Element)
	cache.lru.Init()
}

func (cache *shardIndexCache) releaseWaiter(key shardIndexCacheKey, flight *shardIndexFlight) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if flight.waiters > 0 {
		flight.waiters--
	}
	if flight.waiters == 0 {
		if cache.flights[key] == flight {
			delete(cache.flights, key)
		}
		flight.cancel()
	}
}

func (cache *shardIndexCache) load(key shardIndexCacheKey, flight *shardIndexFlight, load func(context.Context) (shardIndex, error)) {
	var index shardIndex
	var err error
	select {
	case cache.loadSlots <- struct{}{}:
		index, err = load(flight.ctx)
		<-cache.loadSlots
	case <-flight.ctx.Done():
		err = flight.ctx.Err()
	}
	if err == nil {
		if index.Shard != key.shard {
			err = fmt.Errorf("%w: loaded shard %+v, want %+v", ErrCorruptIndex, index.Shard, key.shard)
		} else if validationErr := validateShardIndex(index); validationErr != nil {
			err = validationErr
		}
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	current := cache.flights[key] == flight
	if err == nil && !cache.closed && current {
		entry := &shardIndexCacheEntry{key: key, index: cloneShardIndex(index)}
		cache.entries[key] = cache.lru.PushFront(entry)
		for cache.lru.Len() > cache.maxEntries {
			oldest := cache.lru.Back()
			if oldest == nil {
				break
			}
			removed := oldest.Value.(*shardIndexCacheEntry)
			delete(cache.entries, removed.key)
			cache.lru.Remove(oldest)
		}
	}
	flight.index = cloneShardIndex(index)
	flight.err = err
	if current {
		delete(cache.flights, key)
	}
	close(flight.done)
}
