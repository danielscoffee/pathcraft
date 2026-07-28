package cli

import (
	"flag"
	"fmt"
	"net"

	"github.com/danielscoffee/pathcraft/internal/grpcapi"
)

type grpcOptions struct {
	file    string
	gtfsDir string
	addr    string
}

func parseGRPCArgs(args []string) (grpcOptions, error) {
	fs := flag.NewFlagSet("grpc", flag.ContinueOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	gtfsDir := fs.String("gtfs", "", "Directory containing GTFS files for multimodal journeys")
	addr := fs.String("addr", "127.0.0.1:9090", "gRPC server address")
	if err := fs.Parse(args); err != nil {
		return grpcOptions{}, err
	}
	if fs.NArg() != 0 {
		return grpcOptions{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *file == "" {
		return grpcOptions{}, fmt.Errorf("--file is required")
	}
	return grpcOptions{file: *file, gtfsDir: *gtfsDir, addr: *addr}, nil
}

func CmdGRPC(args []string) error {
	options, err := parseGRPCArgs(args)
	if err != nil {
		return err
	}

	router, err := loadEngine(options.file)
	if err != nil {
		return err
	}
	if options.gtfsDir != "" {
		if err := router.LoadGTFSDir(options.gtfsDir); err != nil {
			return err
		}
	}

	listener, err := net.Listen("tcp", options.addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", options.addr, err)
	}
	defer listener.Close()

	fmt.Printf("Starting plaintext gRPC server on %s...\n", listener.Addr())
	if err := grpcapi.NewServer(router).Serve(listener); err != nil {
		return fmt.Errorf("serving gRPC: %w", err)
	}
	return nil
}
