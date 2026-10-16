# Contributing to GoCacheShield

Thank you for your interest in contributing to **GoCacheShield**!

We welcome pull requests for bug fixes, performance optimizations, and documentation improvements.

## Guiding Principles

1. **Zero External Dependencies in Core:** The core package must rely exclusively on the Go standard library.
2. **Concurrency Correctness:** Every PR modifying concurrency or synchronization logic must pass `go test -race ./...` with zero data races.
3. **Reproducible Benchmarks:** Any PR claiming performance improvements must include reproducible benchmarks before and after changes.
4. **Context Safety:** Always respect `context.Context` cancellation and deadlines. Never block indefinitely.

## Development Workflow

### Prerequisites
- Go 1.22 or higher.

### Running Tests
Run all unit and integration tests:
```bash
go test -v -count=1 ./...
```

Run race detection (requires CGO/GCC):
```bash
go test -race -v ./...
```

### Running Benchmarks
```bash
go test -bench=. -benchmem -run=none ./...
```

## Pull Request Checklist
- [ ] Code is formatted using `gofmt` or `goimports`.
- [ ] New features include comprehensive unit tests.
- [ ] All tests pass without warnings or race conditions.
- [ ] Public API methods and types include descriptive doc comments.
