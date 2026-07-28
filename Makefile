OSM_FILE ?= testdata/example.osm
GTFS_DIR ?= testdata/mini_gtfs
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
	@set --; [ ! -d "$(GTFS_DIR)" ] || set -- --gtfs "$(GTFS_DIR)"; exec ./bin/pathcraft serve --file "$(OSM_FILE)" "$$@" --addr "$(ADDR)"

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

# Official Grande Recife GTFS feed (~35 MB zip, 3.1M stop_times rows).
GTFS_URL ?= https://www.granderecife.pe.gov.br/gtfs/gtfs.zip
fetch-gtfs:
	@echo "Fetching GTFS $(GTFS_URL) -> $(GTFS_DIR)"
	@mkdir -p $(GTFS_DIR)
	@curl -sL -o /tmp/pathcraft_gtfs.zip "$(GTFS_URL)"
	@unzip -o -q /tmp/pathcraft_gtfs.zip -d $(GTFS_DIR)
	@echo "Extracted $$(ls $(GTFS_DIR) | wc -l) files"

.PHONY: test web build release clean demo dev-web fetch-osm fetch-gtfs
