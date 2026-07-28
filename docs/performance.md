# Performance and Memory Profiling

PathCraft keeps performance checks in standard Go benchmarks. Numbers below prove Phase 0.4 mechanics on deterministic fixtures; they are not city-wide latency guarantees.

## Reproduce

```bash
go test ./internal/routing/astar ./pkg/pathcraft/engine \
  -run '^$' \
  -bench='(Contraction|Parallel|Preprocess)' \
  -benchmem -benchtime=100ms -count=5
```

Capture one preprocessing allocation and retained-memory profile:

```bash
go test ./internal/routing/astar \
  -run '^$' \
  -bench=BenchmarkBuildDegreeTwoContraction_LongChain \
  -benchmem -benchtime=1x \
  -memprofile=/tmp/pathcraft-phase04.mem

go tool pprof -top -sample_index=alloc_space /tmp/pathcraft-phase04.mem
go tool pprof -top -sample_index=inuse_space /tmp/pathcraft-phase04.mem
```

Use `go tool pprof -http=:0 /tmp/pathcraft-phase04.mem` for call graphs. Profiles stay outside repository.

## Reference run

Captured 2026-07-27 on Linux/amd64, Intel i5-1135G7, Go 1.26.5. Values are medians from five 100 ms runs unless noted.

| Benchmark | Time | Bytes/op | Allocs/op | Notes |
|---|---:|---:|---:|---|
| A* 10,000-node chain, base | 3.827 ms | 3,428,585 | 10,258 | zero heuristic |
| A* 10,000-node chain, contracted | 43.922 µs | 357,848 | 24 | exact 10,000-node result unpacked |
| Build contraction, 10,000-node chain | 22.046 ms | 17,871,763 | 80,466 | 9,998 nodes, 2 directed chains |
| Engine preprocess, toy OSM | 71.683 µs | 65,864 | 542 | parse + SHA-256 + graph + contraction |
| Engine parallel route, toy OSM | 267.8 ns/op | 480 | 12 | aggregate `RunParallel` throughput metric |

Synthetic long-chain query improved about 87×, with 9.6× fewer allocated bytes and 427× fewer allocations. This workload is intentionally favorable to degree-two contraction. Intersection-dense graphs will gain less.

One-shot memory profile reported about 17.9 MB allocated while building 10,000-node contraction and about 2.64 MB retained by index. Main temporary allocation sites were `BuildDegreeTwoContraction` and `contractionTopology`. Go profiler sampling and runtime allocations make retained numbers approximate.

## Reading results

- Compare base and contracted query on same graph and endpoint pair.
- Keep graph loading outside query timing.
- Run with `-race` separately; race instrumentation invalidates latency numbers.
- Treat parallel `ns/op` as aggregate throughput, not individual request latency.
- Profile representative city extracts before setting memory or latency budgets.
- Full contraction hierarchies become justified only if measured degree-two contraction misses product targets.
