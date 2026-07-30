package worldgraph

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestCacheBoundsDistinctConcurrentLoads(t *testing.T) {
	cache := newChunkCache(1)
	started := make(chan struct{}, maxConcurrentChunkLoads+2)
	release := make(chan struct{})
	errorsCh := make(chan error, maxConcurrentChunkLoads+2)
	for index := range maxConcurrentChunkLoads + 2 {
		tile := TileID{Z: 4, X: index, Y: 0}
		go func() {
			_, err := cache.GetContext(context.Background(), tile, func(ctx context.Context) (*Chunk, error) {
				started <- struct{}{}
				select {
				case <-release:
					return cacheTestChunk(tile, "bounded"), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			})
			errorsCh <- err
		}()
	}
	for range maxConcurrentChunkLoads {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for bounded loads")
		}
	}
	select {
	case <-started:
		t.Fatal("more than configured distinct chunk loads ran concurrently")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for range maxConcurrentChunkLoads + 2 {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
}

func TestCacheCancelsLoadAfterAllWaitersLeave(t *testing.T) {
	cache := newChunkCache(1)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	stopped := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := cache.GetContext(ctx, TileID{Z: 0}, func(loadCtx context.Context) (*Chunk, error) {
			close(started)
			<-loadCtx.Done()
			close(stopped)
			return nil, loadCtx.Err()
		})
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("GetContext() error = %v, want context.Canceled", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("load continued after all waiters canceled")
	}
}

func TestCacheStartsFreshLoadAfterCanceledFlightRetires(t *testing.T) {
	chunk := cacheTestChunk(TileID{Z: 2, X: 0, Y: 0}, "replacement")
	cache := newChunkCache(decodedChunkBytes(chunk))
	ctx, cancel := context.WithCancel(context.Background())
	firstStarted := make(chan struct{})
	firstStopped := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		_, err := cache.GetContext(ctx, chunk.Tile, func(context.Context) (*Chunk, error) {
			close(firstStarted)
			<-releaseFirst
			close(firstStopped)
			return chunk, nil
		})
		firstResult <- err
	}()
	<-firstStarted
	cancel()
	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("first GetContext() error = %v, want context.Canceled", err)
	}

	secondResult := make(chan error, 1)
	go func() {
		_, err := cache.GetContext(context.Background(), chunk.Tile, func(context.Context) (*Chunk, error) {
			return chunk, nil
		})
		secondResult <- err
	}()
	select {
	case err := <-secondResult:
		if err != nil {
			t.Fatalf("replacement GetContext() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement request joined canceled flight")
	}
	close(releaseFirst)
	select {
	case <-firstStopped:
	case <-time.After(time.Second):
		t.Fatal("retired loader did not stop")
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
