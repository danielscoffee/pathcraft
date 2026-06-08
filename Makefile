OSM_FILE ?= examples/recife_demo.osm
GTFS_DIR ?= examples/mini_gtfs
ADDR     ?= :8080

test:
	@go test ./... -v -cover

build:
	@go build -o ./bin/pathcraft ./cmd/pathcraft

clean:
	@rm -f ./bin/pathcraft

# Run the interactive routing demo (Leaflet map at http://localhost$(ADDR)/graph-visual)
demo: build
	@echo "Open http://localhost$(ADDR)/graph-visual"
	@./bin/pathcraft serve --file $(OSM_FILE) --gtfs $(GTFS_DIR) --addr $(ADDR)

# Fetch a real OSM extract via Overpass for a richer demo.
# Override BBOX with: make fetch-osm BBOX="south,west,north,east" OUT=examples/city.osm
BBOX ?= -8.0660,-34.8920,-8.0420,-34.8700
OUT  ?= examples/recife_central.osm
fetch-osm:
	@echo "Fetching OSM bbox=$(BBOX) -> $(OUT)"
	@curl -s -o $(OUT) "https://overpass-api.de/api/map?bbox=$(BBOX)"
	@echo "Saved $(OUT) ($$(wc -c < $(OUT)) bytes)"

.PHONY: test build clean demo fetch-osm
