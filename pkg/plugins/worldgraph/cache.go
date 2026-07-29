package worldgraph

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"unsafe"
)

type cacheEntry struct {
	tile  TileID
	chunk *Chunk
	bytes int64
}

type cacheFlight struct {
	done  chan struct{}
	chunk *Chunk
	err   error
}

type chunkCache struct {
	mu       sync.Mutex
	maxBytes int64
	bytes    int64
	entries  map[TileID]*list.Element
	lru      *list.List
	flights  map[TileID]*cacheFlight
	closed   bool
}

func newChunkCache(maxBytes int64) *chunkCache {
	if maxBytes < 1 {
		maxBytes = 1
	}
	return &chunkCache{
		maxBytes: maxBytes,
		entries:  make(map[TileID]*list.Element),
		lru:      list.New(),
		flights:  make(map[TileID]*cacheFlight),
	}
}

func (c *chunkCache) Get(ctx context.Context, tile TileID, load func() (*Chunk, error)) (*Chunk, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrRouterClosed
	}
	if element := c.entries[tile]; element != nil {
		c.lru.MoveToFront(element)
		chunk := element.Value.(*cacheEntry).chunk
		c.mu.Unlock()
		return chunk, nil
	}
	flight := c.flights[tile]
	if flight == nil {
		flight = &cacheFlight{done: make(chan struct{})}
		c.flights[tile] = flight
		go c.load(tile, flight, load)
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		return flight.chunk, flight.err
	}
}

func (c *chunkCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.entries = make(map[TileID]*list.Element)
	c.lru.Init()
	c.bytes = 0
}

func (c *chunkCache) load(tile TileID, flight *cacheFlight, load func() (*Chunk, error)) {
	chunk, err := load()
	if err == nil && chunk == nil {
		err = errors.New("worldgraph chunk loader returned nil")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && !c.closed {
		bytes := decodedChunkBytes(chunk)
		if bytes <= c.maxBytes {
			entry := &cacheEntry{tile: tile, chunk: chunk, bytes: bytes}
			c.entries[tile] = c.lru.PushFront(entry)
			c.bytes += bytes
			for c.bytes > c.maxBytes {
				oldest := c.lru.Back()
				if oldest == nil {
					break
				}
				removed := oldest.Value.(*cacheEntry)
				delete(c.entries, removed.tile)
				c.lru.Remove(oldest)
				c.bytes -= removed.bytes
			}
		}
	}
	flight.chunk = chunk
	flight.err = err
	delete(c.flights, tile)
	close(flight.done)
}

func decodedChunkBytes(chunk *Chunk) int64 {
	if chunk == nil {
		return 0
	}
	bytes := int64(unsafe.Sizeof(*chunk))
	bytes += int64(len(chunk.Nodes)) * int64(unsafe.Sizeof(Node{}))
	bytes += int64(len(chunk.Edges)) * int64(unsafe.Sizeof(Edge{}))
	for _, edge := range chunk.Edges {
		bytes += int64(len(edge.Highway) + len(edge.Name))
		bytes += int64(len(edge.Sources)) * int64(unsafe.Sizeof(""))
		for _, source := range edge.Sources {
			bytes += int64(len(source))
		}
	}
	return bytes
}
