package worldgraph

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestShardCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newShardIndexCache(2)
	t.Cleanup(cache.Close)
	shards := []TileID{
		{Z: PackedShardZoom, X: 0, Y: 0},
		{Z: PackedShardZoom, X: 1, Y: 0},
		{Z: PackedShardZoom, X: 2, Y: 0},
	}
	loads := map[TileID]int{}
	get := func(shard TileID) {
		t.Helper()
		if _, err := cache.Get(context.Background(), shard, func(context.Context) (shardIndex, error) {
			loads[shard]++
			return cacheTestIndex(shard), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	get(shards[0])
	get(shards[1])
	get(shards[0])
	get(shards[2])
	get(shards[1])
	if loads[shards[0]] != 1 || loads[shards[1]] != 2 || loads[shards[2]] != 1 {
		t.Fatalf("loads = %v", loads)
	}
}

func TestShardCacheSharesConcurrentLoad(t *testing.T) {
	cache := newShardIndexCache(2)
	t.Cleanup(cache.Close)
	shard := TileID{Z: PackedShardZoom, X: 1, Y: 2}
	started := make(chan struct{})
	release := make(chan struct{})
	var loads atomic.Int32
	load := func(context.Context) (shardIndex, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		<-release
		return cacheTestIndex(shard), nil
	}

	const callers = 16
	results := make(chan shardIndex, callers)
	errorsCh := make(chan error, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			index, err := cache.Get(context.Background(), shard, load)
			results <- index
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
	for index := range results {
		if index.Shard != shard || len(index.Entries) != 1 {
			t.Fatalf("index = %+v", index)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("load count = %d, want 1", loads.Load())
	}
}

func TestShardCacheCancelsLoadAfterAllWaitersLeave(t *testing.T) {
	cache := newShardIndexCache(1)
	t.Cleanup(cache.Close)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	stopped := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := cache.Get(ctx, TileID{Z: PackedShardZoom}, func(loadCtx context.Context) (shardIndex, error) {
			close(started)
			<-loadCtx.Done()
			close(stopped)
			return shardIndex{}, loadCtx.Err()
		})
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Get() error = %v, want context.Canceled", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("load continued after all waiters canceled")
	}
}

func TestShardCacheStartsFreshLoadAfterCanceledFlight(t *testing.T) {
	cache := newShardIndexCache(1)
	t.Cleanup(cache.Close)
	shard := TileID{Z: PackedShardZoom, X: 1, Y: 2}
	ctx, cancel := context.WithCancel(context.Background())
	firstStarted := make(chan struct{})
	firstStopped := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		_, err := cache.Get(ctx, shard, func(context.Context) (shardIndex, error) {
			close(firstStarted)
			<-releaseFirst
			close(firstStopped)
			return cacheTestIndex(shard), nil
		})
		firstResult <- err
	}()
	<-firstStarted
	cancel()
	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("first Get() error = %v, want context.Canceled", err)
	}

	secondResult := make(chan error, 1)
	go func() {
		_, err := cache.Get(context.Background(), shard, func(context.Context) (shardIndex, error) {
			return cacheTestIndex(shard), nil
		})
		secondResult <- err
	}()
	select {
	case err := <-secondResult:
		if err != nil {
			t.Fatal(err)
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

func TestShardCachePropagatesCorruptIndex(t *testing.T) {
	cache := newShardIndexCache(1)
	t.Cleanup(cache.Close)
	shard := TileID{Z: PackedShardZoom}
	if _, err := cache.Get(context.Background(), shard, func(context.Context) (shardIndex, error) {
		return shardIndex{}, ErrCorruptIndex
	}); !errors.Is(err, ErrCorruptIndex) {
		t.Fatalf("Get() error = %v, want ErrCorruptIndex", err)
	}
}

func TestShardCacheReturnsCopies(t *testing.T) {
	cache := newShardIndexCache(1)
	t.Cleanup(cache.Close)
	shard := TileID{Z: PackedShardZoom}
	load := func(context.Context) (shardIndex, error) { return cacheTestIndex(shard), nil }
	first, err := cache.Get(context.Background(), shard, load)
	if err != nil {
		t.Fatal(err)
	}
	first.Entries[0].Slot = 99
	second, err := cache.Get(context.Background(), shard, load)
	if err != nil {
		t.Fatal(err)
	}
	if second.Entries[0].Slot != 1 {
		t.Fatalf("cached slot = %d, want 1", second.Entries[0].Slot)
	}
}

func TestShardCacheCloseCancelsLoads(t *testing.T) {
	cache := newShardIndexCache(1)
	shard := TileID{Z: PackedShardZoom}
	started := make(chan struct{})
	stopped := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := cache.Get(context.Background(), shard, func(ctx context.Context) (shardIndex, error) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return shardIndex{}, ctx.Err()
		})
		result <- err
	}()
	<-started
	cache.Close()
	cache.Close()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Close() did not cancel load")
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("active Get() error = %v, want context.Canceled", err)
	}
	if _, err := cache.Get(context.Background(), shard, func(context.Context) (shardIndex, error) {
		return cacheTestIndex(shard), nil
	}); !errors.Is(err, ErrRouterClosed) {
		t.Fatalf("closed Get() error = %v, want ErrRouterClosed", err)
	}
}

func cacheTestIndex(shard TileID) shardIndex {
	return shardIndex{Shard: shard, Entries: []shardIndexEntry{{Slot: 1, Length: 1}}}
}
