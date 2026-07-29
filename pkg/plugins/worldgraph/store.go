package worldgraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
)

type Store struct {
	root string

	mu       sync.RWMutex
	manifest *Manifest
}

func OpenStore(root string) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("worldgraph store path is empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	store := &Store{root: absolute}
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.manifest == nil {
		return Manifest{}, os.ErrNotExist
	}
	return cloneManifest(*s.manifest), nil
}

func (s *Store) LoadChunk(tile TileID) (*Chunk, error) {
	s.mu.RLock()
	if s.manifest == nil {
		s.mu.RUnlock()
		return nil, fmt.Errorf("%w: %+v", ErrUncoveredTile, tile)
	}
	manifest := cloneManifest(*s.manifest)
	s.mu.RUnlock()

	n, err := tileCount(tile.Z)
	if err != nil {
		return nil, err
	}
	if err := validateTile(tile, n); err != nil {
		return nil, err
	}
	if !manifestHasTile(manifest, tile) {
		return nil, fmt.Errorf("%w: %+v", ErrUncoveredTile, tile)
	}
	path := filepath.Join(s.root, "generations", manifest.Generation, tilePath(tile))
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %+v", ErrMissingChunk, tile)
		}
		return nil, err
	}
	defer file.Close()

	chunk, err := DecodeChunk(file)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorruptChunk, err)
	}
	if chunk.Tile != tile {
		return nil, fmt.Errorf("%w: file for %+v contains %+v", ErrCorruptChunk, tile, chunk.Tile)
	}
	if err := validateChunkProvenance(*chunk, manifest); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorruptChunk, err)
	}
	return chunk, nil
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
	return s.publishGenerationFromWithOps(manifest, provide, publishFileOps{
		renameManifest: os.Rename,
		syncDirectory:  syncDirectory,
	})
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
		destination := filepath.Join(stage, tilePath(tile))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		chunk, changed, err := provide(tile)
		if err != nil {
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

	if err := syncTreeDirectories(stage, ops.syncDirectory); err != nil {
		return err
	}
	manifestTemp, err := writeManifestTemp(s.root, prepared)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(manifestTemp) }()

	if err := os.Rename(stage, target); err != nil {
		return err
	}
	if err := ops.syncDirectory(generations); err != nil {
		return errors.Join(err, removePublishedGeneration(target, generations, ops.syncDirectory))
	}
	if err := ops.renameManifest(manifestTemp, filepath.Join(s.root, manifestFilename)); err != nil {
		return errors.Join(err, removePublishedGeneration(target, generations, ops.syncDirectory))
	}
	s.manifest = &prepared
	return ops.syncDirectory(s.root)
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
		if containsTile(region.Tiles, chunk.Tile) {
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
	cloned.Regions = append([]RegionManifest(nil), manifest.Regions...)
	for i := range cloned.Regions {
		cloned.Regions[i].Tiles = append([]TileID(nil), manifest.Regions[i].Tiles...)
	}
	return cloned
}

func tilePath(tile TileID) string {
	return filepath.Join(strconv.Itoa(tile.Z), strconv.Itoa(tile.X), strconv.Itoa(tile.Y)+".pcg")
}
