ROOT="$(mktemp -d)"

make release

./bin/pathcraft chunks build-global \
 --pbf pkg/plugins/worldgraph/builder/testdata/seam.osm.pbf \
 --store "$ROOT/store" \
 --work-dir "$ROOT/work" \
 --run-memory-mb 16 \
 --pack-mb 1 \
 --open-shards 2

./bin/pathcraft route \
 --chunks "$ROOT/store" \
 --mode walk \
 --from-position '12.5683,55.6761' \
 --to-position '12.5685,55.6762'

./bin/pathcraft serve --chunks "$ROOT/store" --addr 127.0.0.1:8080
