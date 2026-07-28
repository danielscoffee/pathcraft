package worldgraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	return chunk, nil
}

func (s *Store) PublishGeneration(manifest Manifest, chunks map[TileID]Chunk) error {
	return s.publishGeneration(manifest, chunks, os.Rename)
}

func (s *Store) publishGeneration(manifest Manifest, chunks map[TileID]Chunk, renameManifest func(string, string) error) error {
	if renameManifest == nil {
		return fmt.Errorf("manifest rename function is nil")
	}
	prepared, err := prepareManifest(manifest)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for tile, chunk := range chunks {
		if !manifestHasTile(prepared, tile) {
			return fmt.Errorf("chunk %+v is not listed in manifest", tile)
		}
		if chunk.Tile != tile {
			return fmt.Errorf("chunk key %+v does not match payload tile %+v", tile, chunk.Tile)
		}
	}

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
		if chunk, changed := chunks[tile]; changed {
			if err := writeChunkFile(destination, chunk); err != nil {
				return err
			}
			continue
		}
		if s.manifest == nil || !manifestHasTile(*s.manifest, tile) {
			return fmt.Errorf("chunk %+v has no new or previous payload", tile)
		}
		source := filepath.Join(s.root, "generations", s.manifest.Generation, tilePath(tile))
		if err := os.Link(source, destination); err != nil {
			if err := copyChunkFile(source, destination); err != nil {
				return fmt.Errorf("reuse chunk %+v: %w", tile, err)
			}
		}
	}

	manifestTemp, err := writeManifestTemp(s.root, prepared)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(manifestTemp) }()

	if err := os.Rename(stage, target); err != nil {
		return err
	}
	if err := renameManifest(manifestTemp, filepath.Join(s.root, manifestFilename)); err != nil {
		if cleanupErr := os.RemoveAll(target); cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
		return err
	}
	s.manifest = &prepared
	return nil
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
