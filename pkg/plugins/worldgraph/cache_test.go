package worldgraph

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCacheEvictsLeastRecentlyUsedByByteBudget(t *testing.T) {
	first := cacheTestChunk(TileID{Z: 2, X: 0, Y: 0}, "first")
	second := cacheTestChunk(TileID{Z: 2, X: 1, Y: 0}, "second")
	cache := newChunkCache(decodedChunkBytes(first) + decodedChunkBytes(second) - 1)
	loads := map[TileID]int{}
	load := func(chunk *Chunk) func() (*Chunk, error) {
		return func() (*Chunk, error) {
			loads[chunk.Tile]++
			return chunk, nil
		}
	}
	if _, err := cache.Get(context.Background(), first.Tile, load(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Get(context.Background(), second.Tile, load(second)); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Get(context.Background(), first.Tile, load(first)); err != nil {
		t.Fatal(err)
	}
	if loads[first.Tile] != 2 || loads[second.Tile] != 1 {
		t.Fatalf("loads = %v, want first evicted", loads)
	}
}

func TestCacheEvictionKeepsActiveReferenceSafe(t *testing.T) {
	first := cacheTestChunk(TileID{Z: 2, X: 0, Y: 0}, "first")
	second := cacheTestChunk(TileID{Z: 2, X: 1, Y: 0}, "second")
	cache := newChunkCache(decodedChunkBytes(first))
	active, err := cache.Get(context.Background(), first.Tile, func() (*Chunk, error) { return first, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Get(context.Background(), second.Tile, func() (*Chunk, error) { return second, nil }); err != nil {
		t.Fatal(err)
	}
	if active.Edges[0].Name != "first" {
		t.Fatalf("active chunk changed after eviction: %+v", active)
	}
}

func TestCacheSharesConcurrentLoad(t *testing.T) {
	chunk := cacheTestChunk(TileID{Z: 2, X: 0, Y: 0}, "shared")
	cache := newChunkCache(decodedChunkBytes(chunk) * 2)
	started := make(chan struct{})
	release := make(chan struct{})
	var loads atomic.Int32
	load := func() (*Chunk, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		<-release
		return chunk, nil
	}

	const callers = 16
	results := make(chan *Chunk, callers)
	errorsCh := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			result, err := cache.Get(context.Background(), chunk.Tile, load)
			results <- result
			errorsCh <- err
		}()
	}
	<-started
	close(release)
	wait.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result != chunk {
			t.Fatalf("result pointer = %p, want %p", result, chunk)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("load count = %d, want 1", loads.Load())
	}
}

func TestCacheRetriesFailure(t *testing.T) {
	chunk := cacheTestChunk(TileID{Z: 2, X: 0, Y: 0}, "retry")
	cache := newChunkCache(decodedChunkBytes(chunk))
	attempts := 0
	load := func() (*Chunk, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("temporary")
		}
		return chunk, nil
	}
	if _, err := cache.Get(context.Background(), chunk.Tile, load); err == nil {
		t.Fatal("first Get() error = nil")
	}
	if _, err := cache.Get(context.Background(), chunk.Tile, load); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestCachePropagatesCorruption(t *testing.T) {
	cache := newChunkCache(1)
	_, err := cache.Get(context.Background(), TileID{Z: 0}, func() (*Chunk, error) {
		return nil, ErrCorruptChunk
	})
	if !errors.Is(err, ErrCorruptChunk) {
		t.Fatalf("Get() error = %v, want ErrCorruptChunk", err)
	}
}

func cacheTestChunk(tile TileID, name string) *Chunk {
	return &Chunk{
		Tile:  tile,
		Nodes: []Node{{ID: 1, Owner: tile}, {ID: 2, Owner: tile}},
		Edges: []Edge{{
			ID:      EdgeID{WayID: 1, From: 1, To: 2},
			Name:    name,
			Owner:   tile,
			Sources: []string{"test"},
		}},
	}
}
