package worldgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	bbolt "go.etcd.io/bbolt"
)

type Store struct {
	root string

	mu         sync.RWMutex
	manifest   *Manifest
	indexCache *shardIndexCache
	closed     bool
}

func OpenStore(root string) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("worldgraph store path is empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	store := &Store{root: absolute, indexCache: newShardIndexCache(defaultShardIndexCacheEntries)}
	info, err := os.Stat(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("worldgraph store %q is not a directory", absolute)
	}
	manifest, err := readManifestFile(filepath.Join(absolute, manifestFilename))
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	store.manifest = &manifest
	return store, nil
}

func (s *Store) Manifest() (Manifest, error) {
	if s == nil {
		return Manifest{}, ErrRouterClosed
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return Manifest{}, ErrRouterClosed
	}
	if s.manifest == nil {
		return Manifest{}, os.ErrNotExist
	}
	return cloneManifest(*s.manifest), nil
}

func (s *Store) Covers(ctx context.Context, tile TileID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	manifest, cache, err := s.snapshot()
	if err != nil {
		return false, err
	}
	if manifest == nil {
		return false, nil
	}
	if err := validateStoreTile(tile); err != nil {
		return false, err
	}
	if manifest.Layout == "" {
		return manifestHasTile(*manifest, tile), nil
	}
	_, _, covered, err := s.packedEntry(ctx, manifest, cache, tile)
	return covered, err
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cache := s.indexCache
	s.mu.Unlock()
	cache.Close()
	return nil
}

func (s *Store) LoadChunk(tile TileID) (*Chunk, error) {
	return s.LoadChunkContext(context.Background(), tile)
}

func (s *Store) LoadChunkContext(ctx context.Context, tile TileID) (*Chunk, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manifest, cache, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, fmt.Errorf("%w: %+v", ErrUncoveredTile, tile)
	}
	if err := validateStoreTile(tile); err != nil {
		return nil, err
	}

	var chunk *Chunk
	if manifest.Layout == "" {
		if !manifestHasTile(*manifest, tile) {
			return nil, fmt.Errorf("%w: %+v", ErrUncoveredTile, tile)
		}
		path := filepath.Join(s.root, "generations", manifest.Generation, tilePath(tile))
		file, openErr := os.Open(path)
		if openErr != nil {
			if os.IsNotExist(openErr) {
				return nil, fmt.Errorf("%w: %+v", ErrMissingChunk, tile)
			}
			return nil, openErr
		}
		defer file.Close()
		chunk, err = DecodeChunk(readerWithContext{ctx: ctx, reader: file})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCorruptChunk, err)
		}
	} else {
		entry, prefix, covered, entryErr := s.packedEntry(ctx, manifest, cache, tile)
		if entryErr != nil {
			return nil, entryErr
		}
		if !covered {
			return nil, fmt.Errorf("%w: %+v", ErrUncoveredTile, tile)
		}
		chunk, err = readPackedChunk(ctx, prefix, entry)
		if err != nil {
			return nil, err
		}
	}
	if chunk.Tile != tile {
		return nil, fmt.Errorf("%w: file for %+v contains %+v", ErrCorruptChunk, tile, chunk.Tile)
	}
	if err := validateChunkProvenance(*chunk, *manifest); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorruptChunk, err)
	}
	return chunk, nil
}

func (s *Store) snapshot() (*Manifest, *shardIndexCache, error) {
	if s == nil {
		return nil, nil, ErrRouterClosed
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, nil, ErrRouterClosed
	}
	return s.manifest, s.indexCache, nil
}

func (s *Store) packedEntry(ctx context.Context, manifest *Manifest, cache *shardIndexCache, tile TileID) (shardIndexEntry, string, bool, error) {
	address, err := packedShardAddress(tile)
	if err != nil {
		return shardIndexEntry{}, "", false, err
	}
	if !containsTile(manifest.Shards, address.Shard) {
		return shardIndexEntry{}, "", false, nil
	}
	generationRoot := filepath.Join(s.root, "generations", manifest.Generation)
	prefix, err := shardPackPrefix(generationRoot, address.Shard)
	if err != nil {
		return shardIndexEntry{}, "", false, err
	}
	index, err := cache.get(ctx, shardIndexCacheKey{generation: manifest.Generation, shard: address.Shard}, func(loadCtx context.Context) (shardIndex, error) {
		return loadShardIndex(loadCtx, shardIndexPath(prefix))
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrRouterClosed) || errors.Is(err, ErrMissingChunk) {
			return shardIndexEntry{}, "", false, err
		}
		if errors.Is(err, ErrCorruptIndex) {
			return shardIndexEntry{}, "", false, fmt.Errorf("%w: %w", ErrCorruptChunk, err)
		}
		return shardIndexEntry{}, "", false, err
	}
	entry, covered := index.entry(address.Slot)
	return entry, prefix, covered, nil
}

func loadShardIndex(ctx context.Context, path string) (shardIndex, error) {
	if err := ctx.Err(); err != nil {
		return shardIndex{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return shardIndex{}, fmt.Errorf("%w: shard index %q", ErrMissingChunk, path)
		}
		return shardIndex{}, err
	}
	defer file.Close()
	index, err := decodeShardIndex(readerWithContext{ctx: ctx, reader: file})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return shardIndex{}, ctxErr
	}
	return index, err
}

func validateStoreTile(tile TileID) error {
	n, err := tileCount(tile.Z)
	if err != nil {
		return err
	}
	return validateTile(tile, n)
}

type readerWithContext struct {
	ctx    context.Context
	reader io.Reader
}

func (r readerWithContext) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

type publishFileOps struct {
	renameManifest func(string, string) error
	syncDirectory  func(string) error
}

func (s *Store) PublishGeneration(manifest Manifest, chunks map[TileID]Chunk) error {
	if err := validateChunkMap(manifest, chunks); err != nil {
		return err
	}
	return s.PublishGenerationFrom(manifest, chunkMapProvider(chunks))
}

func (s *Store) PublishGenerationFrom(manifest Manifest, provide func(TileID) (Chunk, bool, error)) error {
	return s.PublishGenerationFromContext(context.Background(), manifest, provide)
}

func (s *Store) PublishGenerationFromContext(ctx context.Context, manifest Manifest, provide func(TileID) (Chunk, bool, error)) (err error) {
	if ctx == nil {
		return fmt.Errorf("publish context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	lock, err := bbolt.Open(filepath.Join(s.root, ".publish.lock"), 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		if errors.Is(err, bbolt.ErrTimeout) {
			return fmt.Errorf("%w: another writer holds publication lock", ErrPublishConflict)
		}
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.verifyManifestCurrent(); err != nil {
		return err
	}
	return s.publishGenerationFromContextWithOps(ctx, manifest, provide, publishFileOps{
		renameManifest: os.Rename,
		syncDirectory:  syncDirectory,
	})
}

func (s *Store) verifyManifestCurrent() error {
	s.mu.RLock()
	expected := ""
	if s.manifest != nil {
		expected = s.manifest.Generation
	}
	s.mu.RUnlock()

	current, err := readManifestFile(filepath.Join(s.root, manifestFilename))
	if err != nil {
		if os.IsNotExist(err) && expected == "" {
			return nil
		}
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: expected generation %q, found no manifest", ErrPublishConflict, expected)
		}
		return err
	}
	if expected == "" || current.Generation != expected {
		return fmt.Errorf("%w: expected generation %q, found %q", ErrPublishConflict, expected, current.Generation)
	}
	return nil
}

func (s *Store) publishGeneration(manifest Manifest, chunks map[TileID]Chunk, renameManifest func(string, string) error) error {
	if err := validateChunkMap(manifest, chunks); err != nil {
		return err
	}
	return s.publishGenerationFromWithOps(manifest, chunkMapProvider(chunks), publishFileOps{
		renameManifest: renameManifest,
		syncDirectory:  syncDirectory,
	})
}

func (s *Store) publishGenerationWithOps(manifest Manifest, chunks map[TileID]Chunk, ops publishFileOps) error {
	if err := validateChunkMap(manifest, chunks); err != nil {
		return err
	}
	return s.publishGenerationFromWithOps(manifest, chunkMapProvider(chunks), ops)
}

func (s *Store) publishGenerationFromWithOps(manifest Manifest, provide func(TileID) (Chunk, bool, error), ops publishFileOps) error {
	return s.publishGenerationFromContextWithOps(context.Background(), manifest, provide, ops)
}

func (s *Store) publishGenerationFromContextWithOps(ctx context.Context, manifest Manifest, provide func(TileID) (Chunk, bool, error), ops publishFileOps) error {
	if ctx == nil {
		return fmt.Errorf("publish context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if provide == nil || ops.renameManifest == nil || ops.syncDirectory == nil {
		return fmt.Errorf("publish operation is nil")
	}
	prepared, err := prepareManifest(manifest)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	generations := filepath.Join(s.root, "generations")
	if err := os.MkdirAll(generations, 0o700); err != nil {
		return err
	}
	target := filepath.Join(generations, prepared.Generation)
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("generation %q already exists", prepared.Generation)
	} else if !os.IsNotExist(err) {
		return err
	}

	stage, err := os.MkdirTemp(generations, "."+prepared.Generation+".tmp-*")
	if err != nil {
		return err
	}
	if err := os.Chmod(stage, 0o700); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()

	for _, tile := range prepared.Tiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		destination := filepath.Join(stage, tilePath(tile))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		chunk, changed, err := provide(tile)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if changed {
			if chunk.Tile != tile {
				return fmt.Errorf("chunk key %+v does not match payload tile %+v", tile, chunk.Tile)
			}
			if err := validateChunkProvenance(chunk, prepared); err != nil {
				return err
			}
			if err := writeChunkFile(destination, chunk); err != nil {
				return err
			}
			continue
		}
		if s.manifest == nil || !manifestHasTile(*s.manifest, tile) {
			return fmt.Errorf("chunk %+v has no new or previous payload", tile)
		}
		if !sameTileProvenance(*s.manifest, prepared, tile) {
			return fmt.Errorf("chunk %+v provenance changed without a new payload", tile)
		}
		source := filepath.Join(s.root, "generations", s.manifest.Generation, tilePath(tile))
		if err := os.Link(source, destination); err != nil {
			if err := copyChunkFile(source, destination); err != nil {
				return fmt.Errorf("reuse chunk %+v: %w", tile, err)
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := syncTreeDirectories(stage, ops.syncDirectory); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manifestTemp, err := writeManifestTemp(s.root, prepared)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(manifestTemp) }()
	rollbackTemp := ""
	if s.manifest != nil {
		rollbackTemp, err = writeManifestTemp(s.root, *s.manifest)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(rollbackTemp) }()
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, removePublishedGeneration(target, generations, ops.syncDirectory))
	}
	if err := ops.syncDirectory(generations); err != nil {
		return errors.Join(err, removePublishedGeneration(target, generations, ops.syncDirectory))
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, removePublishedGeneration(target, generations, ops.syncDirectory))
	}
	manifestPath := filepath.Join(s.root, manifestFilename)
	if err := ops.renameManifest(manifestTemp, manifestPath); err != nil {
		return errors.Join(err, removePublishedGeneration(target, generations, ops.syncDirectory))
	}
	if err := ops.syncDirectory(s.root); err != nil {
		restored, rollbackErr := rollbackPublication(manifestPath, rollbackTemp, s.root, ops)
		if !restored {
			s.manifest = &prepared
		}
		return errors.Join(err, rollbackErr)
	}
	s.manifest = &prepared
	return nil
}

func syncTreeDirectories(root string, sync func(string) error) error {
	var directories []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			directories = append(directories, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := sync(directory); err != nil {
			return err
		}
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}

func removePublishedGeneration(target, generations string, sync func(string) error) error {
	return errors.Join(os.RemoveAll(target), sync(generations))
}

func rollbackPublication(manifestPath, rollbackManifest, root string, ops publishFileOps) (bool, error) {
	var restoreErr error
	if rollbackManifest == "" {
		restoreErr = os.Remove(manifestPath)
	} else {
		restoreErr = ops.renameManifest(rollbackManifest, manifestPath)
	}
	if restoreErr != nil {
		return false, restoreErr
	}
	// Keep target: readers may have pinned it while its manifest was visible.
	return true, ops.syncDirectory(root)
}

func writeChunkFile(path string, chunk Chunk) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := EncodeChunk(file, chunk); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

func copyChunkFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source chunk is not a regular file")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
	}()
	if err := output.Chmod(0o600); err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

func writeManifestTemp(root string, manifest Manifest) (string, error) {
	file, err := os.CreateTemp(root, ".manifest.tmp-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	closed = true
	return path, nil
}

func readManifestFile(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Manifest{}, fmt.Errorf("manifest contains trailing JSON")
		}
		return Manifest{}, err
	}
	return normalizeManifest(manifest)
}

func validateChunkMap(manifest Manifest, chunks map[TileID]Chunk) error {
	listed := make(map[TileID]struct{}, len(manifest.Tiles))
	for _, tile := range manifest.Tiles {
		listed[tile] = struct{}{}
	}
	for tile, chunk := range chunks {
		if _, exists := listed[tile]; !exists {
			return fmt.Errorf("chunk %+v is not listed in manifest", tile)
		}
		if chunk.Tile != tile {
			return fmt.Errorf("chunk key %+v does not match payload tile %+v", tile, chunk.Tile)
		}
	}
	return nil
}

func chunkMapProvider(chunks map[TileID]Chunk) func(TileID) (Chunk, bool, error) {
	return func(tile TileID) (Chunk, bool, error) {
		chunk, ok := chunks[tile]
		return chunk, ok, nil
	}
}

func validateChunkProvenance(chunk Chunk, manifest Manifest) error {
	regions := make(map[string]struct{})
	for _, region := range manifest.Regions {
		if manifest.Layout == PackedLayout || containsTile(region.Tiles, chunk.Tile) {
			regions[region.Name] = struct{}{}
		}
	}
	for _, edge := range chunk.Edges {
		for _, source := range edge.Sources {
			if _, exists := regions[source]; !exists {
				return fmt.Errorf("%w: edge %+v source %q does not cover tile %+v", ErrInvalidChunk, edge.ID, source, chunk.Tile)
			}
		}
	}
	return nil
}

func sameTileProvenance(previous, next Manifest, tile TileID) bool {
	previousRegions := tileProvenance(previous, tile)
	nextRegions := tileProvenance(next, tile)
	if len(previousRegions) != len(nextRegions) {
		return false
	}
	for i := range previousRegions {
		if previousRegions[i] != nextRegions[i] {
			return false
		}
	}
	return true
}

func tileProvenance(manifest Manifest, tile TileID) []string {
	regions := make([]string, 0, len(manifest.Regions))
	for _, region := range manifest.Regions {
		if containsTile(region.Tiles, tile) {
			regions = append(regions, region.Name+"\x00"+region.SourceSHA256)
		}
	}
	return regions
}

func cloneManifest(manifest Manifest) Manifest {
	cloned := manifest
	cloned.Tiles = append([]TileID(nil), manifest.Tiles...)
	cloned.Shards = append([]TileID(nil), manifest.Shards...)
	cloned.Regions = append([]RegionManifest(nil), manifest.Regions...)
	for i := range cloned.Regions {
		cloned.Regions[i].Tiles = append([]TileID(nil), manifest.Regions[i].Tiles...)
	}
	return cloned
}

func tilePath(tile TileID) string {
	return filepath.Join(strconv.Itoa(tile.Z), strconv.Itoa(tile.X), strconv.Itoa(tile.Y)+".pcg")
}
