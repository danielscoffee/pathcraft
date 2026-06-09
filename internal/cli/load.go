package cli

import (
	"fmt"
	"os"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func loadEngine(file string) (*engine.Engine, error) {
	e := engine.New()
	cacheFile := file + ".cache"

	if _, err := os.Stat(cacheFile); err == nil {
		fmt.Printf("Loading from cache %s...\n", cacheFile)
		if err := e.LoadGraph(cacheFile); err == nil {
			return e, nil
		}
		fmt.Printf("Cache load failed, falling back to OSM parsing...\n")
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
