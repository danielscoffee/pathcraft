package builder

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var ErrPBFSourceChanged = errors.New("PBF source changed during build")

type pbfSource struct {
	mu sync.RWMutex

	file        *os.File
	path        string
	size        int64
	info        os.FileInfo
	sha256      [sha256.Size]byte
	maxWayNodes int
	closed      bool
}

func openPBFSource(ctx context.Context, path string, maxWayNodes int) (*pbfSource, error) {
	if ctx == nil {
		return nil, fmt.Errorf("PBF source context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxWayNodes < 1 {
		return nil, fmt.Errorf("maximum way node count must be positive")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, errors.Join(fmt.Errorf("PBF input is not a regular file"), file.Close())
	}
	hash := sha256.New()
	reader := io.TeeReader(io.NewSectionReader(file, 0, info.Size()), hash)
	if err := validatePBFReader(ctx, reader, maxWayNodes); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], hash.Sum(nil))
	return &pbfSource{
		file: file, path: absolute, size: info.Size(), info: info,
		sha256: fingerprint, maxWayNodes: maxWayNodes,
	}, nil
}

func (source *pbfSource) Reader() *io.SectionReader {
	source.mu.RLock()
	defer source.mu.RUnlock()
	return io.NewSectionReader(source.file, 0, source.size)
}

func (source *pbfSource) Verify(ctx context.Context) error {
	if source == nil {
		return os.ErrClosed
	}
	if ctx == nil {
		return fmt.Errorf("PBF verification context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	source.mu.RLock()
	defer source.mu.RUnlock()
	if source.closed {
		return os.ErrClosed
	}
	before, err := source.file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(source.info, before) || before.Size() != source.size {
		return ErrPBFSourceChanged
	}
	hash := sha256.New()
	reader := io.NewSectionReader(source.file, 0, source.size)
	buffer := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			if _, err := hash.Write(buffer[:count]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	after, err := source.file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(source.info, after) || after.Size() != source.size {
		return ErrPBFSourceChanged
	}
	var actual [sha256.Size]byte
	copy(actual[:], hash.Sum(nil))
	if actual != source.sha256 {
		return ErrPBFSourceChanged
	}
	return nil
}

func (source *pbfSource) Close() error {
	if source == nil {
		return nil
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.closed {
		return nil
	}
	source.closed = true
	return source.file.Close()
}

func (source *pbfSource) Size() int64 {
	if source == nil {
		return 0
	}
	source.mu.RLock()
	defer source.mu.RUnlock()
	return source.size
}

func (source *pbfSource) Fingerprint() string {
	if source == nil {
		return ""
	}
	source.mu.RLock()
	defer source.mu.RUnlock()
	return fmt.Sprintf("%x", source.sha256)
}

func (source *pbfSource) Path() string {
	if source == nil {
		return ""
	}
	source.mu.RLock()
	defer source.mu.RUnlock()
	return source.path
}
