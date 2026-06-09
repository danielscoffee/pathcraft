package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	pcengine "github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"
)

// CmdPlugins implements: pathcraft plugins list [--json]
func CmdPlugins(args []string) error {
	if len(args) == 0 || args[0] == "help" {
		fmt.Println("Usage: pathcraft plugins list [--json]")
		return nil
	}
	switch args[0] {
	case "list":
		return cmdPluginsList(args[1:])
	default:
		return fmt.Errorf("unknown plugins subcommand: %s", args[0])
	}
}

func cmdPluginsList(args []string) error {
	fs := flag.NewFlagSet("plugins list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "Emit JSON instead of human-readable text")
	if err := fs.Parse(args); err != nil {
		return err
	}

	reg := registry.Default
	data := map[string][]string{
		"algorithms": reg.Algorithms(),
		"loaders":    reg.Loaders(),
		"exporters":  reg.Exporters(),
		"cost":       reg.CostModels(),
		"loggers":    reg.Loggers(),
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(data)
	}
	for _, kind := range []string{"algorithms", "loaders", "exporters", "cost", "loggers"} {
		fmt.Printf("%s:\n", strings.ToUpper(kind))
		if len(data[kind]) == 0 {
			fmt.Println("  (none)")
		}
		for _, name := range data[kind] {
			fmt.Printf("  - %s\n", name)
		}
		fmt.Println()
	}
	return nil
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// CmdPipeline implements: pathcraft pipeline --loader X --graph PATH --algorithm Y --from A --to B [--export Z]
func CmdPipeline(args []string) error {
	fs := flag.NewFlagSet("pipeline", flag.ExitOnError)
	loader := fs.String("loader", "", "Graph loader plugin name (see `pathcraft plugins list`)")
	source := fs.String("graph", "", "Graph source path / directory")
	algo := fs.String("algorithm", "", "Algorithm plugin name")
	from := fs.String("from", "", "From node id (loader-specific encoding)")
	to := fs.String("to", "", "To node id")
	exporter := fs.String("export", "", "Optional exporter plugin name (e.g. geojson)")
	output := fs.String("o", "", "Write exporter output to file (default: stdout)")
	var opts multiFlag
	fs.Var(&opts, "opt", "Algorithm option k=v (repeatable, e.g. --opt departure_time=05:00:00)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	options := map[string]any{}
	for _, kv := range opts {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("--opt expects key=value, got %q", kv)
		}
		options[k] = v
	}

	if *loader == "" || *source == "" || *algo == "" || *from == "" || *to == "" {
		return fmt.Errorf("--loader, --graph, --algorithm, --from, --to are required")
	}

	res, err := pcengine.Run(context.Background(), pcengine.PipelineRequest{
		LoaderName:    *loader,
		Source:        *source,
		AlgorithmName: *algo,
		ExporterName:  *exporter,
		Route: core.RouteRequest{
			From:      core.NodeID(*from),
			To:        core.NodeID(*to),
			Algorithm: *algo,
			Options:   options,
		},
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "route ok: nodes=%d cost=%.2f duration=%dms visited=%d\n",
		len(res.Result.Path), res.Result.Cost, res.Result.DurationMS, res.Result.VisitedNodes)

	if *exporter == "" {
		return nil
	}
	if *output != "" {
		return os.WriteFile(*output, res.Output, 0644)
	}
	_, err = os.Stdout.Write(res.Output)
	return err
}
