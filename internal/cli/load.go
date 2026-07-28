package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func loadEngine(file string) (*engine.Engine, error) {
	e := engine.New()
	cacheFile := file + ".cache"

	metadata, metadataErr := graph.ReadCacheMetadata(cacheFile)
	if metadataErr == nil {
		fingerprint, fingerprintErr := graph.FingerprintFile(file)
		if fingerprintErr == nil && metadata.SourceSHA256 == fingerprint {
			fmt.Printf("Loading from cache %s...\n", cacheFile)
			if err := e.LoadGraph(cacheFile); err == nil {
				return e, nil
			}
			fmt.Printf("Cache load failed, falling back to OSM parsing...\n")
		} else {
			fmt.Printf("Cache is stale, rebuilding from %s...\n", file)
		}
	} else if !errors.Is(metadataErr, os.ErrNotExist) {
		fmt.Printf("Cache metadata invalid, rebuilding from %s...\n", file)
	}

	fmt.Printf("Parsing OSM %s...\n", file)
	if err := e.LoadOSM(file); err != nil {
		return nil, err
	}

	fmt.Printf("Saving cache to %s...\n", cacheFile)
	if err := e.SaveGraph(cacheFile); err != nil {
		fmt.Printf("Warning: failed to save cache: %v\n", err)
	}

	return e, nil
}
