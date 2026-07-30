package worldgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"time"

	bbolt "go.etcd.io/bbolt"
)

const packedMetadataFilename = "metadata.json"

type packedStageMetadata struct {
	Manifest       Manifest `json:"manifest"`
	ExpectedParent string   `json:"expected_parent,omitempty"`
}

type packedPublishOps struct {
	renameGeneration func(string, string) error
	renameManifest   func(string, string) error
	syncFile         func(string) error
	syncDirectory    func(string) error
}

type PackedGenerationStage struct {
	mu sync.Mutex

	store          *Store
	manifest       Manifest
	expectedParent string
	stagePath      string
	targetPath     string
	committed      bool
	finalized      bool
}

func (store *Store) BeginPackedGeneration(manifest Manifest) (*PackedGenerationStage, error) {
	builtAtDefaulted := manifest.BuiltAt.IsZero()
	prepared, err := prepareManifest(manifest)
	if err != nil {
		return nil, err
	}
	if prepared.Layout != PackedLayout {
		return nil, fmt.Errorf("packed generation requires layout %q", PackedLayout)
	}

	store.mu.RLock()
	if store.closed {
		store.mu.RUnlock()
		return nil, ErrRouterClosed
	}
	expectedParent := ""
	if store.manifest != nil {
		expectedParent = store.manifest.Generation
	}
	store.mu.RUnlock()

	if err := os.MkdirAll(store.root, 0o700); err != nil {
		return nil, err
	}
	generations := filepath.Join(store.root, "generations")
	if err := os.MkdirAll(generations, 0o700); err != nil {
		return nil, err
	}
	target := filepath.Join(generations, prepared.Generation)
	if _, err := os.Lstat(target); err == nil {
		return nil, fmt.Errorf("generation %q already exists", prepared.Generation)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	stagePath := filepath.Join(generations, "."+prepared.Generation+".build")
	created := false
	if err := os.Mkdir(stagePath, 0o700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, statErr := os.Lstat(stagePath)
		if statErr != nil {
			return nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("packed stage %q is not a directory", stagePath)
		}
	} else {
		created = true
	}

	metadataPath := filepath.Join(stagePath, packedMetadataFilename)
	if created {
		if err := writePackedMetadata(stagePath, metadataPath, prepared, expectedParent); err != nil {
			_ = os.RemoveAll(stagePath)
			return nil, err
		}
		if err := syncDirectory(generations); err != nil {
			_ = os.RemoveAll(stagePath)
			return nil, err
		}
	} else {
		info, err := os.Lstat(metadataPath)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("packed stage metadata is not a regular file")
		}
		existing, err := readPackedMetadata(metadataPath)
		if err != nil {
			return nil, err
		}
		if builtAtDefaulted {
			prepared.BuiltAt = existing.Manifest.BuiltAt
		}
		if !reflect.DeepEqual(existing.Manifest, prepared) {
			return nil, fmt.Errorf("packed stage metadata does not match requested generation")
		}
		if existing.ExpectedParent != expectedParent {
			return nil, fmt.Errorf("%w: packed stage expects parent %q, store has %q", ErrPublishConflict, existing.ExpectedParent, expectedParent)
		}
	}
	return &PackedGenerationStage{
		store:          store,
		manifest:       prepared,
		expectedParent: expectedParent,
		stagePath:      stagePath,
		targetPath:     target,
	}, nil
}

func (stage *PackedGenerationStage) Path() string {
	if stage == nil {
		return ""
	}
	return stage.stagePath
}

func (stage *PackedGenerationStage) Commit(ctx context.Context) error {
	return stage.commitWithOps(ctx, defaultPackedPublishOps())
}

func (stage *PackedGenerationStage) commitWithOps(ctx context.Context, ops packedPublishOps) (err error) {
	if stage == nil || stage.store == nil {
		return fmt.Errorf("packed generation stage is nil")
	}
	if ctx == nil {
		return fmt.Errorf("packed publication context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ops.renameGeneration == nil || ops.renameManifest == nil || ops.syncFile == nil || ops.syncDirectory == nil {
		return fmt.Errorf("packed publication operation is nil")
	}

	stage.mu.Lock()
	defer stage.mu.Unlock()
	if stage.committed {
		return nil
	}
	if stage.finalized {
		return fmt.Errorf("packed generation stage is already finalized")
	}
	if err := validatePackedGenerationStage(ctx, stage.stagePath, stage.manifest, stage.expectedParent); err != nil {
		return err
	}
	if err := syncPackedGenerationStage(ctx, stage.stagePath, ops); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	lock, err := bbolt.Open(filepath.Join(stage.store.root, ".publish.lock"), 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		if errors.Is(err, bbolt.ErrTimeout) {
			return fmt.Errorf("%w: another writer holds publication lock", ErrPublishConflict)
		}
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()

	store := stage.store
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return ErrRouterClosed
	}
	localParent := ""
	if store.manifest != nil {
		localParent = store.manifest.Generation
	}
	if localParent != stage.expectedParent {
		return fmt.Errorf("%w: expected local generation %q, found %q", ErrPublishConflict, stage.expectedParent, localParent)
	}
	if err := verifyManifestGeneration(store.root, stage.expectedParent); err != nil {
		return err
	}
	if _, err := os.Lstat(stage.targetPath); err == nil {
		return fmt.Errorf("generation %q already exists", stage.manifest.Generation)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	manifestTemp, err := writeManifestTemp(store.root, stage.manifest)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(manifestTemp) }()
	rollbackTemp := ""
	if store.manifest != nil {
		rollbackTemp, err = writeManifestTemp(store.root, *store.manifest)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(rollbackTemp) }()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ops.renameGeneration(stage.stagePath, stage.targetPath); err != nil {
		return err
	}
	generations := filepath.Dir(stage.targetPath)
	restoreStage := func(cause error) error {
		restoreErr := restorePackedStage(stage.targetPath, stage.stagePath, generations, ops.syncDirectory)
		if restoreErr != nil {
			stage.finalized = true
		}
		return errors.Join(cause, restoreErr)
	}
	if err := ctx.Err(); err != nil {
		return restoreStage(err)
	}
	if err := ops.syncDirectory(generations); err != nil {
		return restoreStage(err)
	}
	if err := ctx.Err(); err != nil {
		return restoreStage(err)
	}
	manifestPath := filepath.Join(store.root, manifestFilename)
	if err := ops.renameManifest(manifestTemp, manifestPath); err != nil {
		return restoreStage(err)
	}
	if err := ops.syncDirectory(store.root); err != nil {
		restored, rollbackErr := rollbackPublication(manifestPath, rollbackTemp, store.root, publishFileOps{
			renameManifest: ops.renameManifest,
			syncDirectory:  ops.syncDirectory,
		})
		stage.finalized = true
		if !restored {
			store.manifest = &stage.manifest
			stage.committed = true
		}
		return errors.Join(err, rollbackErr)
	}
	store.manifest = &stage.manifest
	stage.committed = true
	return nil
}

func (stage *PackedGenerationStage) Abort() error {
	if stage == nil {
		return nil
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if stage.committed || stage.finalized {
		return nil
	}
	if err := os.RemoveAll(stage.stagePath); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(stage.stagePath))
}

func defaultPackedPublishOps() packedPublishOps {
	return packedPublishOps{
		renameGeneration: os.Rename,
		renameManifest:   os.Rename,
		syncFile:         syncRegularFile,
		syncDirectory:    syncDirectory,
	}
}

func writePackedMetadata(stagePath, metadataPath string, manifest Manifest, expectedParent string) error {
	file, err := os.CreateTemp(stagePath, ".metadata.tmp-*")
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
	if err := encoder.Encode(packedStageMetadata{Manifest: manifest, ExpectedParent: expectedParent}); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	if err := os.Rename(temporary, metadataPath); err != nil {
		return err
	}
	return syncDirectory(stagePath)
}

func readPackedMetadata(path string) (packedStageMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return packedStageMetadata{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var metadata packedStageMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return packedStageMetadata{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return packedStageMetadata{}, fmt.Errorf("packed metadata contains trailing JSON")
		}
		return packedStageMetadata{}, err
	}
	manifest, err := normalizeManifest(metadata.Manifest)
	if err != nil {
		return packedStageMetadata{}, err
	}
	metadata.Manifest = manifest
	if metadata.ExpectedParent != "" && !safeGeneration(metadata.ExpectedParent) {
		return packedStageMetadata{}, fmt.Errorf("packed metadata has invalid parent %q", metadata.ExpectedParent)
	}
	return metadata, nil
}

type packedSegmentRange struct {
	offset uint64
	end    uint64
}

func validatePackedGenerationStage(ctx context.Context, root string, manifest Manifest, expectedParent string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("packed stage is not a directory")
	}
	metadataPath := filepath.Join(root, packedMetadataFilename)
	metadata, err := readPackedMetadata(metadataPath)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(metadata.Manifest, manifest) || metadata.ExpectedParent != expectedParent {
		return fmt.Errorf("packed stage metadata changed")
	}

	allowedFiles := map[string]struct{}{packedMetadataFilename: {}}
	allowedDirectories := map[string]struct{}{".": {}}
	segmentRanges := make(map[string][]packedSegmentRange)
	for _, shard := range manifest.Shards {
		if err := ctx.Err(); err != nil {
			return err
		}
		prefix, err := shardPackPrefix(root, shard)
		if err != nil {
			return err
		}
		indexPath := shardIndexPath(prefix)
		if err := addPackedAllowedPath(root, indexPath, allowedFiles, allowedDirectories); err != nil {
			return err
		}
		index, err := loadShardIndex(ctx, indexPath)
		if err != nil {
			return err
		}
		if index.Shard != shard || len(index.Entries) == 0 {
			return fmt.Errorf("%w: shard %+v has invalid occupancy", ErrCorruptIndex, shard)
		}
		for _, entry := range index.Entries {
			tile, err := packedShardTile(shard, entry.Slot)
			if err != nil {
				return err
			}
			packPath := packSegmentPath(prefix, entry.Segment)
			if err := addPackedAllowedPath(root, packPath, allowedFiles, allowedDirectories); err != nil {
				return err
			}
			chunk, err := readPackedChunk(ctx, prefix, entry)
			if err != nil {
				return err
			}
			if chunk.Tile != tile {
				return fmt.Errorf("%w: shard %+v slot %d contains tile %+v", ErrCorruptChunk, shard, entry.Slot, chunk.Tile)
			}
			if err := validateChunkProvenance(*chunk, manifest); err != nil {
				return fmt.Errorf("%w: %w", ErrCorruptChunk, err)
			}
			segmentRanges[packPath] = append(segmentRanges[packPath], packedSegmentRange{
				offset: entry.Offset,
				end:    entry.Offset + uint64(entry.Length),
			})
		}
	}
	for path, ranges := range segmentRanges {
		sort.Slice(ranges, func(i, j int) bool { return ranges[i].offset < ranges[j].offset })
		var end uint64
		for _, item := range ranges {
			if item.offset != end {
				return fmt.Errorf("%w: pack %q has overlapping or sparse ranges", ErrCorruptIndex, path)
			}
			end = item.end
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || uint64(info.Size()) != end {
			return fmt.Errorf("%w: pack %q size does not match index", ErrCorruptChunk, path)
		}
	}
	return walkPackedStage(root, allowedFiles, allowedDirectories)
}

func addPackedAllowedPath(root, path string, files, directories map[string]struct{}) error {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return fmt.Errorf("invalid packed stage path %q", path)
	}
	files[relative] = struct{}{}
	for directory := filepath.Dir(relative); directory != "."; directory = filepath.Dir(directory) {
		directories[directory] = struct{}{}
	}
	return nil
}

func walkPackedStage(root string, allowedFiles, allowedDirectories map[string]struct{}) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("packed stage contains symlink %q", relative)
		}
		if entry.IsDir() {
			if _, allowed := allowedDirectories[relative]; !allowed {
				return fmt.Errorf("packed stage contains unexpected directory %q", relative)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("packed stage contains non-regular file %q", relative)
		}
		if _, allowed := allowedFiles[relative]; !allowed {
			return fmt.Errorf("packed stage contains unexpected file %q", relative)
		}
		return nil
	})
}

func syncPackedGenerationStage(ctx context.Context, root string, ops packedPublishOps) error {
	var files []string
	var directories []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("packed stage contains symlink %q", path)
		}
		if entry.IsDir() {
			directories = append(directories, path)
			return nil
		}
		files = append(files, path)
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(files)
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ops.syncFile(path); err != nil {
			return err
		}
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, path := range directories {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ops.syncDirectory(path); err != nil {
			return err
		}
	}
	return nil
}

func syncRegularFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func restorePackedStage(target, stage, generations string, syncDirectory func(string) error) error {
	if err := os.Rename(target, stage); err != nil {
		return err
	}
	return syncDirectory(generations)
}

func verifyManifestGeneration(root, expected string) error {
	current, err := readManifestFile(filepath.Join(root, manifestFilename))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && expected == "" {
			return nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: expected generation %q, found no manifest", ErrPublishConflict, expected)
		}
		return err
	}
	if expected == "" || current.Generation != expected {
		return fmt.Errorf("%w: expected generation %q, found %q", ErrPublishConflict, expected, current.Generation)
	}
	return nil
}
