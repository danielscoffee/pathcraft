package cli

import "fmt"

func PrintUsage() {
	fmt.Println(`
	PathCraft - Walking and transit routing engine
	Usage:
	pathcraft <command> [options]

	Commands:
	parse      Parse OSM file and show statistics
	preprocess Build versioned routing cache from OSM
	route      Find route between two points (walking, node IDs or coordinates)
	transit  Find transit route using RAPTOR algorithm
	journey  Find a walk + transit journey from coordinates
	grpc     Start protobuf/gRPC routing server
	serve    Start HTTP server with routing endpoints
	server   Alias for serve
	plugins  List registered plugins (algorithms, loaders, exporters)
	pipeline Run loader → algorithm → exporter via plugin registry
	help     Show this help message

	Examples:
	pathcraft parse --file examples/recife.osm
	pathcraft preprocess --file examples/recife.osm
	pathcraft route --file examples/recife.osm --mode walk --from 1 --to 100
	pathcraft route --file examples/recife.osm --mode car --from-lat -8.06266 --from-lon -34.87800 --to-lat -8.12903 --to-lon -34.90058 --coords
	pathcraft transit --gtfs examples/recife_gtfs --from 452 --to 5931 --time 05:00
	pathcraft journey --file examples/recife.osm --gtfs examples/recife_gtfs --from-lat -8.12903 --from-lon -34.90058 --to-lat -8.06266 --to-lon -34.87798 --time 05:00
	pathcraft grpc --file examples/recife.osm --gtfs examples/recife_gtfs
	pathcraft serve --file examples/recife.osm --gtfs examples/recife_gtfs --addr :8080
	pathcraft serve --file testdata/example.osm --cors-origin https://app.example
	`)
}
