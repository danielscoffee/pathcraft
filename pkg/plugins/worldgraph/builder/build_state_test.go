package builder

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestBuildStateWritesAtomicallyAndResumesExactMatch(t *testing.T) {
	work := t.TempDir()
	expected := globalBuildState{
		Version:          globalBuildStateVersion,
		SourcePath:       "/data/planet.osm.pbf",
		SourceSize:       123,
		SourceSHA256:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RoutingZoom:      12,
		ShardZoom:        8,
		RunMemoryBytes:   512 << 20,
		PackSegmentBytes: 1 << 30,
		MaxOpenShards:    64,
		Generation:       "global-a",
		BuiltAt:          time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}
	state, err := loadOrCreateGlobalBuildState(work, expected, true)
	if err != nil {
		t.Fatal(err)
	}
	state.Completed = []string{"validate-source", "spool-ways-and-refs"}
	state.Counts.Ways = 12
	state.PackedShards = []string{"8/1/2"}
	if err := writeGlobalBuildState(filepath.Join(work, globalBuildStateFilename), state); err != nil {
		t.Fatal(err)
	}
	resumed, err := loadOrCreateGlobalBuildState(work, expected, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resumed.Completed, state.Completed) || resumed.Counts != state.Counts || !reflect.DeepEqual(resumed.PackedShards, state.PackedShards) {
		t.Fatalf("resumed state = %+v, want %+v", resumed, state)
	}
	info, err := os.Stat(filepath.Join(work, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(work, ".build-state-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("state temporary files remain: %v", matches)
	}
}

func TestBuildStateDefersVersionOneUpgradeUntilArtifactValidation(t *testing.T) {
	work := t.TempDir()
	expected := globalBuildState{
		Version: globalBuildStateVersion, SourcePath: "/data/planet.osm.pbf", SourceSize: 123,
		SourceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RoutingZoom:  12, ShardZoom: 8, RunMemoryBytes: 1024, PackSegmentBytes: 2048,
		MaxOpenShards: 2, Generation: "global-a", BuiltAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}
	legacy := expected
	legacy.Version = 1
	legacy.Completed = []string{"validate-source", "spool-ways-and-refs", "sort-refs", "select-nodes"}
	legacy.Counts.Ways = 12
	legacy.Counts.References = 24
	if err := writeGlobalBuildState(filepath.Join(work, globalBuildStateFilename), legacy); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadOrCreateGlobalBuildState(work, expected, true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != 1 || !reflect.DeepEqual(loaded.Completed, legacy.Completed) || loaded.Counts != legacy.Counts {
		t.Fatalf("loaded state = %+v, want unchanged version-one progress", loaded)
	}
	stored, err := readGlobalBuildState(filepath.Join(work, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != 1 {
		t.Fatalf("stored version = %d, want deferred version 1", stored.Version)
	}
}

func TestBuildStateRejectsMismatchAndDisabledResume(t *testing.T) {
	expected := globalBuildState{
		Version: globalBuildStateVersion, SourcePath: "/data/planet.osm.pbf", SourceSize: 123,
		SourceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RoutingZoom:  12, ShardZoom: 8, RunMemoryBytes: 1024, PackSegmentBytes: 2048,
		MaxOpenShards: 2, Generation: "global-a", BuiltAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name   string
		mutate func(*globalBuildState)
	}{
		{name: "source path", mutate: func(state *globalBuildState) { state.SourcePath += ".other" }},
		{name: "source size", mutate: func(state *globalBuildState) { state.SourceSize++ }},
		{name: "source hash", mutate: func(state *globalBuildState) {
			state.SourceSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
		{name: "memory", mutate: func(state *globalBuildState) { state.RunMemoryBytes++ }},
		{name: "pack", mutate: func(state *globalBuildState) { state.PackSegmentBytes++ }},
		{name: "open shards", mutate: func(state *globalBuildState) { state.MaxOpenShards++ }},
		{name: "generation", mutate: func(state *globalBuildState) { state.Generation = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			work := t.TempDir()
			stored := expected
			test.mutate(&stored)
			if err := writeGlobalBuildState(filepath.Join(work, globalBuildStateFilename), stored); err != nil {
				t.Fatal(err)
			}
			if _, err := loadOrCreateGlobalBuildState(work, expected, true); !errors.Is(err, ErrGlobalBuildStateMismatch) {
				t.Fatalf("loadOrCreateGlobalBuildState() error = %v, want ErrGlobalBuildStateMismatch", err)
			}
		})
	}

	work := t.TempDir()
	if err := writeGlobalBuildState(filepath.Join(work, globalBuildStateFilename), expected); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateGlobalBuildState(work, expected, false); !errors.Is(err, ErrGlobalBuildStateMismatch) {
		t.Fatalf("disabled resume error = %v, want ErrGlobalBuildStateMismatch", err)
	}
}

func TestBuildStateValidatesFragmentSpoolSummaries(t *testing.T) {
	shard := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 1, Y: 2}
	valid := globalBuildState{
		Version: globalBuildStateVersion, SourcePath: "/data/planet.osm.pbf", SourceSize: 123,
		SourceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RoutingZoom:  12, ShardZoom: 8, RunMemoryBytes: 1024, PackSegmentBytes: 2048,
		MaxOpenShards: 2, Generation: "global-a", BuiltAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		Completed: []string{"partition-fragments-v2"}, OccupiedShards: []worldgraph.TileID{shard},
		FragmentSpools: []fragmentSpoolSummary{{
			Shard: shard, Records: 2, Bytes: 10,
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		Counts: GlobalCounts{Shards: 1, Fragments: 2, FragmentBytes: 10},
	}
	if err := validateGlobalBuildState(valid); err != nil {
		t.Fatalf("valid fragment state: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*globalBuildState)
	}{
		{name: "missing summary", mutate: func(state *globalBuildState) { state.FragmentSpools = nil }},
		{name: "record total", mutate: func(state *globalBuildState) { state.FragmentSpools[0].Records++ }},
		{name: "byte total", mutate: func(state *globalBuildState) { state.FragmentSpools[0].Bytes++ }},
		{name: "digest", mutate: func(state *globalBuildState) { state.FragmentSpools[0].SHA256 = "bad" }},
		{name: "shard", mutate: func(state *globalBuildState) { state.FragmentSpools[0].Shard.X++ }},
		{name: "stage", mutate: func(state *globalBuildState) { state.Completed = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := valid
			state.Completed = append([]string(nil), valid.Completed...)
			state.OccupiedShards = append([]worldgraph.TileID(nil), valid.OccupiedShards...)
			state.FragmentSpools = append([]fragmentSpoolSummary(nil), valid.FragmentSpools...)
			test.mutate(&state)
			if err := validateGlobalBuildState(state); err == nil {
				t.Fatal("validateGlobalBuildState() error = nil")
			}
		})
	}

	versionTwo := valid
	versionTwo.Version = 2
	if err := validateGlobalBuildState(versionTwo); err == nil {
		t.Fatal("version-two state validation error = nil")
	}
}

func TestBuildStateRequiresEmptyWorkDirectoryWithoutState(t *testing.T) {
	expected := globalBuildState{
		Version: globalBuildStateVersion, SourcePath: "/data/planet.osm.pbf", SourceSize: 123,
		SourceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RoutingZoom:  12, ShardZoom: 8, RunMemoryBytes: 1024, PackSegmentBytes: 2048,
		MaxOpenShards: 2, Generation: "global-a", BuiltAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "leftover"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateGlobalBuildState(work, expected, true); !errors.Is(err, ErrGlobalBuildStateMismatch) {
		t.Fatalf("non-empty work error = %v, want ErrGlobalBuildStateMismatch", err)
	}

	empty := filepath.Join(t.TempDir(), "new-work")
	state, err := loadOrCreateGlobalBuildState(empty, expected, true)
	if err != nil {
		t.Fatal(err)
	}
	if state.identity() != expected.identity() {
		t.Fatalf("new state = %+v, want %+v", state.identity(), expected.identity())
	}
}
