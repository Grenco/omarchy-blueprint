# Contributing

Create a focused branch from `master`, keep commits small and coherent, and
include tests for behavior changes. Before opening a pull request, run:

```sh
go test ./...
go vet ./...
go build ./cmd/omarchy-state
```

Do not exercise restore operations against a developer machine in automated
tests. Use the injected command runner and temporary filesystem roots.
