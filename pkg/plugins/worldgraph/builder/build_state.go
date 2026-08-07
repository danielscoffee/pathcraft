package builder

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const (
	globalBuildStateVersion  = 2
	globalBuildStateFilename = "build-state.json"
)

var ErrGlobalBuildStateMismatch = errors.New("global build state does not match requested build")

type GlobalCounts struct {
	Ways          int64 `json:"ways"`
	References    int64 `json:"references"`
	Nodes         int64 `json:"nodes"`
	Segments      int64 `json:"segments"`
	Fragments     int64 `json:"fragments"`
	Edges         int64 `json:"edges"`
	Contributions int64 `json:"contributions"`
	Shards        int64 `json:"shards"`
	Chunks        int64 `json:"chunks"`
	WorkBytes     int64 `json:"work_bytes"`
	StoreBytes    int64 `json:"store_bytes"`
}

type globalBuildState struct {
	Version          int       `json:"version"`
	SourcePath       string    `json:"source_path"`
	SourceSize       int64     `json:"source_size"`
	SourceSHA256     string    `json:"source_sha256"`
	RoutingZoom      int       `json:"routing_zoom"`
	ShardZoom        int       `json:"shard_zoom"`
	RunMemoryBytes   int64     `json:"run_memory_bytes"`
	PackSegmentBytes int64     `json:"pack_segment_bytes"`
	MaxOpenShards    int       `json:"max_open_shards"`
	Generation       string    `json:"generation"`
	BuiltAt          time.Time `json:"built_at"`

	Completed      []string            `json:"completed,omitempty"`
	OccupiedShards []worldgraph.TileID `json:"occupied_shards,omitempty"`
	PackedShards   []string            `json:"packed_shards,omitempty"`
	StagePath      string              `json:"stage_path,omitempty"`
	Counts         GlobalCounts        `json:"counts"`
}

type globalBuildIdentity struct {
	Version          int
	SourcePath       string
	SourceSize       int64
	SourceSHA256     string
	RoutingZoom      int
	ShardZoom        int
	RunMemoryBytes   int64
	PackSegmentBytes int64
	MaxOpenShards    int
	Generation       string
	BuiltAt          time.Time
}

func (state globalBuildState) identity() globalBuildIdentity {
	return globalBuildIdentity{
		Version: state.Version, SourcePath: state.SourcePath, SourceSize: state.SourceSize,
		SourceSHA256: state.SourceSHA256, RoutingZoom: state.RoutingZoom, ShardZoom: state.ShardZoom,
		RunMemoryBytes: state.RunMemoryBytes, PackSegmentBytes: state.PackSegmentBytes,
		MaxOpenShards: state.MaxOpenShards, Generation: state.Generation, BuiltAt: state.BuiltAt,
	}
}

func loadOrCreateGlobalBuildState(workDir string, expected globalBuildState, resume bool) (globalBuildState, error) {
	if err := validateGlobalBuildState(expected); err != nil {
		return globalBuildState{}, err
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return globalBuildState{}, err
	}
	path := filepath.Join(workDir, globalBuildStateFilename)
	state, err := readGlobalBuildState(path)
	if err == nil {
		if !resume {
			return globalBuildState{}, ErrGlobalBuildStateMismatch
		}
		if state.Version == 1 && expected.Version == globalBuildStateVersion {
			state.Version = globalBuildStateVersion
			if state.identity() != expected.identity() {
				return globalBuildState{}, ErrGlobalBuildStateMismatch
			}
			if err := writeGlobalBuildState(path, state); err != nil {
				return globalBuildState{}, err
			}
			return state, nil
		}
		if state.identity() != expected.identity() {
			return globalBuildState{}, ErrGlobalBuildStateMismatch
		}
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return globalBuildState{}, err
	}
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return globalBuildState{}, err
	}
	if len(entries) != 0 {
		return globalBuildState{}, fmt.Errorf("%w: work directory is non-empty without state", ErrGlobalBuildStateMismatch)
	}
	if err := writeGlobalBuildState(path, expected); err != nil {
		return globalBuildState{}, err
	}
	return expected, nil
}

func writeGlobalBuildState(path string, state globalBuildState) error {
	if err := validateGlobalBuildState(state); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".build-state-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		_ = os.Remove(temporary)
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return syncBuilderDirectory(filepath.Dir(path))
}

func readGlobalBuildState(path string) (globalBuildState, error) {
	file, err := os.Open(path)
	if err != nil {
		return globalBuildState{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var state globalBuildState
	if err := decoder.Decode(&state); err != nil {
		return globalBuildState{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return globalBuildState{}, fmt.Errorf("global build state contains trailing JSON")
		}
		return globalBuildState{}, err
	}
	if err := validateGlobalBuildState(state); err != nil {
		return globalBuildState{}, err
	}
	return state, nil
}

func validateGlobalBuildState(state globalBuildState) error {
	validVersion := state.Version == 1 || state.Version == globalBuildStateVersion
	if !validVersion || !filepath.IsAbs(state.SourcePath) || state.SourceSize < 1 ||
		state.RoutingZoom != 12 || state.ShardZoom != 8 || state.RunMemoryBytes < 8 ||
		state.PackSegmentBytes < 1 || state.MaxOpenShards < 1 || state.Generation == "" || state.BuiltAt.IsZero() {
		return fmt.Errorf("invalid global build state identity")
	}
	hash, err := hex.DecodeString(state.SourceSHA256)
	if err != nil || len(hash) != 32 {
		return fmt.Errorf("invalid global build source SHA-256")
	}
	completed := make(map[string]struct{}, len(state.Completed))
	for _, stage := range state.Completed {
		if stage == "" {
			return fmt.Errorf("global build state contains empty stage")
		}
		if _, exists := completed[stage]; exists {
			return fmt.Errorf("global build state contains duplicate stage %q", stage)
		}
		completed[stage] = struct{}{}
	}
	for index, shard := range state.OccupiedShards {
		if shard.Z != worldgraph.PackedShardZoom || shard.X < 0 || shard.X >= 1<<worldgraph.PackedShardZoom || shard.Y < 0 || shard.Y >= 1<<worldgraph.PackedShardZoom {
			return fmt.Errorf("global build state contains invalid occupied shard %+v", shard)
		}
		if index > 0 {
			previous := state.OccupiedShards[index-1]
			if previous.X > shard.X || previous.X == shard.X && previous.Y >= shard.Y {
				return fmt.Errorf("global build occupied shards are not strictly sorted")
			}
		}
	}
	packed := make(map[string]struct{}, len(state.PackedShards))
	for _, shard := range state.PackedShards {
		if shard == "" {
			return fmt.Errorf("global build state contains empty packed shard")
		}
		if _, exists := packed[shard]; exists {
			return fmt.Errorf("global build state contains duplicate packed shard %q", shard)
		}
		packed[shard] = struct{}{}
	}
	if !sort.StringsAreSorted(state.PackedShards) {
		return fmt.Errorf("global build packed shards are not sorted")
	}
	for _, count := range []int64{
		state.Counts.Ways, state.Counts.References, state.Counts.Nodes, state.Counts.Segments, state.Counts.Fragments,
		state.Counts.Edges, state.Counts.Contributions, state.Counts.Shards, state.Counts.Chunks,
		state.Counts.WorkBytes, state.Counts.StoreBytes,
	} {
		if count < 0 {
			return fmt.Errorf("global build state contains negative count")
		}
	}
	return nil
}

func (state globalBuildState) hasStage(stage string) bool {
	for _, completed := range state.Completed {
		if completed == stage {
			return true
		}
	}
	return false
}

func (state *globalBuildState) completeStage(stage string) {
	if !state.hasStage(stage) {
		state.Completed = append(state.Completed, stage)
	}
}

func (state *globalBuildState) uncompleteStage(stage string) {
	for index, completed := range state.Completed {
		if completed == stage {
			state.Completed = append(state.Completed[:index], state.Completed[index+1:]...)
			return
		}
	}
}
