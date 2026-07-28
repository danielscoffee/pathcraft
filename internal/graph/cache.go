package graph

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

const (
	PreprocessVersion = 1

	cacheMagic                   = "PTHCRFT\x00"
	cacheGraphVersionOffset      = int64(len(cacheMagic))
	cachePreprocessVersionOffset = cacheGraphVersionOffset + 4
	cacheHeaderSize              = cachePreprocessVersionOffset + 4 + sha256.Size
)

type CacheMetadata struct {
	GraphVersion      int
	PreprocessVersion int
	SourceSHA256      [sha256.Size]byte
}

func FingerprintFile(path string) ([sha256.Size]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return [sha256.Size]byte{}, err
	}
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], hash.Sum(nil))
	return fingerprint, nil
}

// Save serializes graph into an atomically replaced versioned cache.
func (g *Graph) Save(path string) error {
	return g.SaveCache(path, CacheMetadata{})
}

func (g *Graph) SaveCache(path string, metadata CacheMetadata) error {
	return g.saveCache(path, metadata, os.Rename)
}

func (g *Graph) saveCache(path string, metadata CacheMetadata, rename func(string, string) error) error {
	metadata.GraphVersion = CacheVersion
	metadata.PreprocessVersion = PreprocessVersion
	g.CacheVersion = CacheVersion

	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := writeCacheHeader(temporary, metadata); err != nil {
		return err
	}
	if err := gob.NewEncoder(temporary).Encode(g); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return rename(temporaryPath, path)
}

func ReadCacheMetadata(path string) (CacheMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return CacheMetadata{}, err
	}
	defer f.Close()
	return readCacheHeader(f)
}

// LoadGraph deserializes a trusted local graph cache. Gob decoding happens
// before structural validation, so callers must not pass uploaded cache files.
func LoadGraph(path string) (*Graph, error) {
	g, _, err := LoadGraphCache(path)
	return g, err
}

func LoadGraphCache(path string) (*Graph, CacheMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, CacheMetadata{}, err
	}
	defer f.Close()

	metadata, err := readCacheHeader(f)
	if err != nil {
		return nil, CacheMetadata{}, err
	}
	var g Graph
	if err := gob.NewDecoder(f).Decode(&g); err != nil {
		return nil, CacheMetadata{}, err
	}
	if g.CacheVersion != CacheVersion {
		return nil, CacheMetadata{}, fmt.Errorf("unsupported graph cache version %d, want %d", g.CacheVersion, CacheVersion)
	}
	if err := g.validateCacheGraph(); err != nil {
		return nil, CacheMetadata{}, err
	}
	g.ensureNearestNodeIndex()
	return &g, metadata, nil
}

func (g *Graph) validateCacheGraph() error {
	if g.Nodes == nil || g.Edges == nil {
		return fmt.Errorf("graph cache has nil node or edge map")
	}
	for id, node := range g.Nodes {
		if node.ID != id || math.IsNaN(node.Lat) || math.IsInf(node.Lat, 0) || math.IsNaN(node.Lon) || math.IsInf(node.Lon, 0) {
			return fmt.Errorf("graph cache node %d is invalid", id)
		}
	}
	for from, edges := range g.Edges {
		if !g.HasNode(from) {
			return fmt.Errorf("graph cache edge source %d is missing", from)
		}
		for _, edge := range edges {
			if !g.HasNode(edge.To) || invalidDistance(edge.DistanceM) {
				return fmt.Errorf("graph cache edge %d -> %d is invalid", from, edge.To)
			}
		}
	}
	if g.Contraction != nil {
		if err := g.Contraction.validate(g); err != nil {
			return fmt.Errorf("invalid graph contraction: %w", err)
		}
	}
	return nil
}

func writeCacheHeader(w io.Writer, metadata CacheMetadata) error {
	if _, err := io.WriteString(w, cacheMagic); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint32(metadata.GraphVersion)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint32(metadata.PreprocessVersion)); err != nil {
		return err
	}
	_, err := w.Write(metadata.SourceSHA256[:])
	return err
}

func readCacheHeader(r io.Reader) (CacheMetadata, error) {
	var magic [len(cacheMagic)]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return CacheMetadata{}, err
	}
	if string(magic[:]) != cacheMagic {
		return CacheMetadata{}, fmt.Errorf("invalid graph cache header")
	}

	var graphVersion, preprocessVersion uint32
	if err := binary.Read(r, binary.BigEndian, &graphVersion); err != nil {
		return CacheMetadata{}, err
	}
	if err := binary.Read(r, binary.BigEndian, &preprocessVersion); err != nil {
		return CacheMetadata{}, err
	}
	metadata := CacheMetadata{
		GraphVersion:      int(graphVersion),
		PreprocessVersion: int(preprocessVersion),
	}
	if _, err := io.ReadFull(r, metadata.SourceSHA256[:]); err != nil {
		return CacheMetadata{}, err
	}
	if metadata.GraphVersion != CacheVersion {
		return CacheMetadata{}, fmt.Errorf("unsupported graph cache version %d, want %d", metadata.GraphVersion, CacheVersion)
	}
	if metadata.PreprocessVersion != PreprocessVersion {
		return CacheMetadata{}, fmt.Errorf("unsupported preprocessing version %d, want %d", metadata.PreprocessVersion, PreprocessVersion)
	}
	return metadata, nil
}
