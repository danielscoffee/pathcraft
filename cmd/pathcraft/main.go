package main

import (
	"fmt"
	"os"

	"github.com/danielscoffee/pathcraft/internal/cli"
)

func main() {
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
	case "server":
		return cli.CmdServer(os.Args[2:])
	case "help":
		cli.PrintUsage()
		return nil
	default:
		cli.PrintUsage()
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}
