package core

import "context"

// GraphLoader builds a Graph from a source string (file path, directory,
// URL, etc.). The interpretation of source is loader-specific.
type GraphLoader interface {
	Name() string
	Load(ctx context.Context, source string) (Graph, error)
}
