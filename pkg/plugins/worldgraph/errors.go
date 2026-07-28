package worldgraph

import "errors"

var (
	ErrChecksumMismatch   = errors.New("worldgraph chunk checksum mismatch")
	ErrUnsupportedVersion = errors.New("worldgraph format version unsupported")
	ErrInvalidChunk       = errors.New("worldgraph chunk invalid")
	ErrUncoveredTile      = errors.New("worldgraph tile is not covered")
	ErrMissingChunk       = errors.New("worldgraph expected chunk is missing")
	ErrCorruptChunk       = errors.New("worldgraph chunk is corrupt")
)
