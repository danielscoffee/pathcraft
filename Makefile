OSM_FILE ?= examples/recife.osm
GTFS_DIR ?= examples/recife_gtfs
ADDR     ?= :8080

test:
	@go test ./... -v -cover

# Build the React frontend (web/app) into web/app/dist for go:embed.
web:
	@cd web/app && npm install --silent && npm run build

build:
	@go build -o ./bin/pathcraft ./cmd/pathcraft

# Full binary: frontend bundle + Go server in one artifact.
release: web build

clean:
	@rm -f ./bin/pathcraft

# Run the interactive routing demo (React app at http://localhost$(ADDR)/).
# Starts without transit when GTFS_DIR is missing; mini_gtfs ships in-repo.
demo: web build
	@echo "Open http://localhost$(ADDR)/"
	@if [ -d "$(GTFS_DIR)" ]; then \
		./bin/pathcraft serve --file $(OSM_FILE) --gtfs $(GTFS_DIR) --addr $(ADDR); \
	else \
		echo "GTFS dir $(GTFS_DIR) not found; starting without transit (try GTFS_DIR=examples/mini_gtfs)"; \
		./bin/pathcraft serve --file $(OSM_FILE) --addr $(ADDR); \
	fi

# Frontend dev server with hot reload, proxying API calls to ADDR.
dev-web:
	@cd web/app && npm install --silent && npm run dev

# Fetch a real OSM extract via Overpass for a richer demo.
# Override BBOX with: make fetch-osm BBOX="west,south,east,north" OUT=examples/city.osm
BBOX ?= -34.94,-8.13,-34.84,-8.02
OUT  ?= examples/recife.osm
fetch-osm:
	@echo "Fetching OSM bbox=$(BBOX) -> $(OUT)"
	@curl -s -o $(OUT) "https://overpass-api.de/api/map?bbox=$(BBOX)"
	@echo "Saved $(OUT) ($$(wc -c < $(OUT)) bytes)"

.PHONY: test web build release clean demo dev-web fetch-osm
