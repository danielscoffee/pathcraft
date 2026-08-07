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
	"strings"
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
	shards         map[TileID]struct{}
	expectedParent string
	stagePath      string
	targetPath     string
	committed      bool
	finalized      bool
}

func (store *Store) BeginPackedGeneration(manifest Manifest) (_ *PackedGenerationStage, err error) {
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
	lock, err := bbolt.Open(filepath.Join(store.root, ".publish.lock"), 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		if errors.Is(err, bbolt.ErrTimeout) {
			return nil, fmt.Errorf("%w: another writer holds publication lock", ErrPublishConflict)
		}
		return nil, err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	if err := verifyManifestGeneration(store.root, expectedParent); err != nil {
		return nil, err
	}

	target := filepath.Join(generations, prepared.Generation)
	stagePath := filepath.Join(generations, "."+prepared.Generation+".build")
	targetInfo, targetErr := os.Lstat(target)
	stageInfo, stageErr := os.Lstat(stagePath)
	if targetErr == nil {
		if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.IsDir() {
			return nil, fmt.Errorf("generation %q is not a directory", prepared.Generation)
		}
		if stageErr == nil {
			return nil, fmt.Errorf("generation %q has both staged and published artifacts", prepared.Generation)
		}
		if !errors.Is(stageErr, os.ErrNotExist) {
			return nil, stageErr
		}
		existing, err := readPackedStageMetadata(target)
		if err != nil {
			return nil, fmt.Errorf("generation %q already exists: %w", prepared.Generation, err)
		}
		if builtAtDefaulted {
			prepared.BuiltAt = existing.Manifest.BuiltAt
		}
		if !reflect.DeepEqual(existing.Manifest, prepared) || existing.ExpectedParent != expectedParent {
			return nil, fmt.Errorf("generation %q already exists", prepared.Generation)
		}
		if err := os.Rename(target, stagePath); err != nil {
			return nil, err
		}
		if err := syncDirectory(generations); err != nil {
			return nil, err
		}
		stageInfo, stageErr = os.Lstat(stagePath)
	} else if !errors.Is(targetErr, os.ErrNotExist) {
		return nil, targetErr
	}

	if stageErr == nil {
		if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() {
			return nil, fmt.Errorf("packed stage %q is not a directory", stagePath)
		}
		existing, metadataErr := readPackedStageMetadata(stagePath)
		if metadataErr != nil {
			if !errors.Is(metadataErr, os.ErrNotExist) {
				return nil, metadataErr
			}
			recoverable, err := recoverablePackedStageInitialization(stagePath)
			if err != nil {
				return nil, err
			}
			if !recoverable {
				return nil, metadataErr
			}
			if err := os.RemoveAll(stagePath); err != nil {
				return nil, err
			}
			if err := syncDirectory(generations); err != nil {
				return nil, err
			}
			stageErr = os.ErrNotExist
		} else {
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
	} else if !errors.Is(stageErr, os.ErrNotExist) {
		return nil, stageErr
	}

	if errors.Is(stageErr, os.ErrNotExist) {
		temporary, err := os.MkdirTemp(generations, "."+prepared.Generation+".init-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(temporary)
		if err := os.Chmod(temporary, 0o700); err != nil {
			return nil, err
		}
		if err := writePackedMetadata(temporary, filepath.Join(temporary, packedMetadataFilename), prepared, expectedParent); err != nil {
			return nil, err
		}
		if err := os.Rename(temporary, stagePath); err != nil {
			return nil, err
		}
		if err := syncDirectory(generations); err != nil {
			return nil, err
		}
	}
	shards := make(map[TileID]struct{}, len(prepared.Shards))
	for _, shard := range prepared.Shards {
		shards[shard] = struct{}{}
	}
	return &PackedGenerationStage{
		store:          store,
		manifest:       prepared,
		shards:         shards,
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

func (stage *PackedGenerationStage) ValidateShard(ctx context.Context, shard TileID) (chunks int, edges int64, err error) {
	if stage == nil {
		return 0, 0, fmt.Errorf("packed generation stage is nil")
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if err := stage.validateMutableShard(shard); err != nil {
		return 0, 0, err
	}
	inspection, err := inspectPackedShard(ctx, stage.stagePath, stage.manifest, shard)
	if err != nil {
		return 0, 0, err
	}
	return inspection.chunks, inspection.edges, nil
}

func (stage *PackedGenerationStage) ResetShard(shard TileID) error {
	if stage == nil {
		return fmt.Errorf("packed generation stage is nil")
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if err := stage.validateMutableShard(shard); err != nil {
		return err
	}
	prefix, err := shardPackPrefix(stage.stagePath, shard)
	if err != nil {
		return err
	}
	directory := filepath.Dir(prefix)
	exists, err := validatePackedDirectoryChain(stage.stagePath, directory, true)
	if err != nil || !exists {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	base := filepath.Base(prefix)
	for _, entry := range entries {
		if !packedShardArtifactName(base, entry.Name()) {
			continue
		}
		if entry.IsDir() {
			return fmt.Errorf("packed shard artifact %q is a directory", filepath.Join(directory, entry.Name()))
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(directory)
}

func (stage *PackedGenerationStage) SyncShard(ctx context.Context, shard TileID) (chunks int, edges int64, err error) {
	if stage == nil {
		return 0, 0, fmt.Errorf("packed generation stage is nil")
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if err := stage.validateMutableShard(shard); err != nil {
		return 0, 0, err
	}
	inspection, err := inspectPackedShard(ctx, stage.stagePath, stage.manifest, shard)
	if err != nil {
		return 0, 0, err
	}
	for _, path := range inspection.files {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		if err := syncRegularFile(path); err != nil {
			return 0, 0, err
		}
	}
	prefix, err := shardPackPrefix(stage.stagePath, shard)
	if err != nil {
		return 0, 0, err
	}
	for directory := filepath.Dir(prefix); ; directory = filepath.Dir(directory) {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		if err := syncDirectory(directory); err != nil {
			return 0, 0, err
		}
		if directory == stage.stagePath {
			break
		}
		if parent := filepath.Dir(directory); parent == directory {
			return 0, 0, fmt.Errorf("packed shard directory escaped stage")
		}
	}
	return inspection.chunks, inspection.edges, nil
}

func (stage *PackedGenerationStage) validateMutableShard(shard TileID) error {
	if stage.committed || stage.finalized {
		return fmt.Errorf("packed generation stage is already finalized")
	}
	if _, occupied := stage.shards[shard]; occupied {
		return nil
	}
	return fmt.Errorf("packed generation does not contain shard %+v", shard)
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

func readPackedStageMetadata(stagePath string) (packedStageMetadata, error) {
	path := filepath.Join(stagePath, packedMetadataFilename)
	info, err := os.Lstat(path)
	if err != nil {
		return packedStageMetadata{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return packedStageMetadata{}, fmt.Errorf("packed stage metadata is not a regular file")
	}
	return readPackedMetadata(path)
}

func recoverablePackedStageInitialization(stagePath string) (bool, error) {
	entries, err := os.ReadDir(stagePath)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".metadata.tmp-") || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return false, nil
		}
	}
	return true, nil
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

type packedShardInspection struct {
	files  []string
	chunks int
	edges  int64
}

func inspectPackedShard(ctx context.Context, root string, manifest Manifest, shard TileID) (packedShardInspection, error) {
	if ctx == nil {
		return packedShardInspection{}, fmt.Errorf("packed shard context is nil")
	}
	if err := ctx.Err(); err != nil {
		return packedShardInspection{}, err
	}
	prefix, err := shardPackPrefix(root, shard)
	if err != nil {
		return packedShardInspection{}, err
	}
	if _, err := validatePackedDirectoryChain(root, filepath.Dir(prefix), false); err != nil {
		return packedShardInspection{}, err
	}
	indexPath := shardIndexPath(prefix)
	info, err := os.Lstat(indexPath)
	if err != nil {
		return packedShardInspection{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return packedShardInspection{}, fmt.Errorf("%w: shard index is not a regular file", ErrCorruptIndex)
	}
	index, err := loadShardIndex(ctx, indexPath)
	if err != nil {
		return packedShardInspection{}, err
	}
	if index.Shard != shard || len(index.Entries) == 0 {
		return packedShardInspection{}, fmt.Errorf("%w: shard %+v has invalid occupancy", ErrCorruptIndex, shard)
	}
	expectedFiles := map[string]struct{}{indexPath: {}}
	segmentRanges := make(map[string][]packedSegmentRange)
	inspection := packedShardInspection{chunks: len(index.Entries)}
	for _, entry := range index.Entries {
		if err := ctx.Err(); err != nil {
			return packedShardInspection{}, err
		}
		tile, err := packedShardTile(shard, entry.Slot)
		if err != nil {
			return packedShardInspection{}, err
		}
		packPath := packSegmentPath(prefix, entry.Segment)
		if _, seen := expectedFiles[packPath]; !seen {
			info, err := os.Lstat(packPath)
			if err != nil {
				return packedShardInspection{}, err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return packedShardInspection{}, fmt.Errorf("%w: pack %q is not a regular file", ErrCorruptChunk, packPath)
			}
			expectedFiles[packPath] = struct{}{}
		}
		chunk, err := readPackedChunk(ctx, prefix, entry)
		if err != nil {
			return packedShardInspection{}, err
		}
		if chunk.Tile != tile {
			return packedShardInspection{}, fmt.Errorf("%w: shard %+v slot %d contains tile %+v", ErrCorruptChunk, shard, entry.Slot, chunk.Tile)
		}
		if err := validateChunkProvenance(*chunk, manifest); err != nil {
			return packedShardInspection{}, fmt.Errorf("%w: %w", ErrCorruptChunk, err)
		}
		for _, edge := range chunk.Edges {
			if edge.Owner == tile {
				inspection.edges++
			}
		}
		segmentRanges[packPath] = append(segmentRanges[packPath], packedSegmentRange{
			offset: entry.Offset,
			end:    entry.Offset + uint64(entry.Length),
		})
	}
	for path, ranges := range segmentRanges {
		sort.Slice(ranges, func(i, j int) bool { return ranges[i].offset < ranges[j].offset })
		var end uint64
		for _, item := range ranges {
			if item.offset != end {
				return packedShardInspection{}, fmt.Errorf("%w: pack %q has overlapping or sparse ranges", ErrCorruptIndex, path)
			}
			end = item.end
		}
		info, err := os.Lstat(path)
		if err != nil {
			return packedShardInspection{}, err
		}
		if uint64(info.Size()) != end {
			return packedShardInspection{}, fmt.Errorf("%w: pack %q size does not match index", ErrCorruptChunk, path)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(prefix))
	if err != nil {
		return packedShardInspection{}, err
	}
	base := filepath.Base(prefix)
	for _, entry := range entries {
		if !packedShardArtifactName(base, entry.Name()) {
			continue
		}
		path := filepath.Join(filepath.Dir(prefix), entry.Name())
		if _, expected := expectedFiles[path]; !expected {
			return packedShardInspection{}, fmt.Errorf("%w: shard %+v contains unexpected artifact %q", ErrCorruptIndex, shard, path)
		}
	}
	inspection.files = make([]string, 0, len(expectedFiles))
	for path := range expectedFiles {
		inspection.files = append(inspection.files, path)
	}
	sort.Strings(inspection.files)
	return inspection, nil
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
	metadata, err := readPackedStageMetadata(root)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(metadata.Manifest, manifest) || metadata.ExpectedParent != expectedParent {
		return fmt.Errorf("packed stage metadata changed")
	}

	allowedFiles := map[string]struct{}{packedMetadataFilename: {}}
	allowedDirectories := map[string]struct{}{".": {}}
	for _, shard := range manifest.Shards {
		inspection, err := inspectPackedShard(ctx, root, manifest, shard)
		if err != nil {
			return err
		}
		for _, path := range inspection.files {
			if err := addPackedAllowedPath(root, path, allowedFiles, allowedDirectories); err != nil {
				return err
			}
		}
	}
	return walkPackedStage(root, allowedFiles, allowedDirectories)
}

func packedShardArtifactName(base, name string) bool {
	if name == base+".idx" {
		return true
	}
	segment := strings.TrimSuffix(strings.TrimPrefix(name, base+"-"), ".pack")
	if segment == "" || name != base+"-"+segment+".pack" {
		return false
	}
	for _, digit := range segment {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func validatePackedDirectoryChain(root, directory string, missingOK bool) (bool, error) {
	relative, err := filepath.Rel(root, directory)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("invalid packed directory %q", directory)
	}
	current := root
	parts := []string(nil)
	if relative != "." {
		parts = strings.Split(relative, string(filepath.Separator))
	}
	for index := 0; index <= len(parts); index++ {
		if index > 0 {
			current = filepath.Join(current, parts[index-1])
		}
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && missingOK && index > 0 {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false, fmt.Errorf("packed path component %q is not a directory", current)
		}
	}
	return true, nil
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
