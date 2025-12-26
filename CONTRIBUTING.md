# Contributing to Pathcraft

Thanks for your interest in contributing to Pathcraft!

This project values **clarity, correctness, and simplicity**.

---

## 1. Philosophy

- Prefer correctness over cleverness
- Readability > micro-optimizations
- Explicit is better than implicit
- No hidden magic

---

## 2. Project Rules

### Core Rules

- No HTTP / CLI logic inside routing algorithms
- No JSON in core domain
- No global state
- No panics in library code

---

## 3. Code Style

- Go fmt required
- Meaningful variable names
- No premature abstractions
- Comments explain **why**, not **what**

---

## 4. Tests

- Routing algorithms must have tests
- Edge cases are mandatory
- Deterministic results only

---

## 5. Commits

- Small, focused commits
- Clear commit messages
- One concern per PR

Example:
- routing: add A* heuristic bounds check

---

## 6. What to Contribute

Good first contributions:
- Tests
- Benchmarks
- Documentation
- GeoJSON improvements

Advanced contributions:
- New routing modes
- Performance improvements
- Transit algorithms

---

## 7. Reporting Issues

When reporting bugs, include:
- OSM snippet (minimal)
- Expected vs actual behavior
- Steps to reproduce

---

## 8. License

By contributing, you agree that your code will be licensed
under the same license as the project.
