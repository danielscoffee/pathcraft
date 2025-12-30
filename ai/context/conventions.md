# Project Conventions & Rules

## Philosophy
- **Correctness > Cleverness**: Prioritize readable, correct code over micro-optimizations.
- **Explicit > Implicit**: No hidden magic or global state.
- **Infrastructure First**: Pathcraft is a routing engine/library, not just an app.

## Architectural Rules
- **Dependency Direction**: Interfaces → Engine → Core ← Adapters.
- **Core Purity**: 
    - No HTTP / CLI logic in routing algorithms.
    - No JSON tags in core domain structs.
    - No external dependencies in core if possible.
- **Engine Facade**: The `engine` package is the ONLY public API. Users should not import internal packages.

## Code Style
- **Formatting**: `go fmt` is mandatory.
- **Error Handling**: No panics in library code. Return errors.
- **Comments**: Explain *why*, not *what*.
- **Naming**: Meaningful variable names.

## Testing
- **Determinism**: Tests must be deterministic.
- **Coverage**: Routing algorithms must have edge-case tests.
