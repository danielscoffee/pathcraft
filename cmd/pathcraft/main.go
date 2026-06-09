package main

import (
	"fmt"
	"os"

	"github.com/danielscoffee/pathcraft/internal/cli"
	"github.com/danielscoffee/pathcraft/internal/logging"

	// Register built-in plugins so they are visible to the registry-backed
	// pipeline command and `pathcraft plugins list`.
	_ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/astar"
	_ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/geojson"
	_ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/gtfs"
	_ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/osm"
	_ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/raptor"
	_ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/zaplogger"
)

func main() {
	if err := logging.InitDevelopment(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logging.Sync()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		cli.PrintUsage()
		return nil
	}

	switch os.Args[1] {
	case "parse":
		return cli.CmdParse(os.Args[2:])
	case "route":
		return cli.CmdRoute(os.Args[2:])
	case "transit":
		return cli.CmdTransit(os.Args[2:])
	case "journey":
		return cli.CmdJourney(os.Args[2:])
	case "serve":
		return cli.CmdServer(os.Args[2:])
	case "server":
		return cli.CmdServer(os.Args[2:])
	case "plugins":
		return cli.CmdPlugins(os.Args[2:])
	case "pipeline":
		return cli.CmdPipeline(os.Args[2:])
	case "help":
		cli.PrintUsage()
		return nil
	default:
		cli.PrintUsage()
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}
