# Contributing to trigrep

Small, focused pull requests are easiest to review.

1. Fork and branch from `main`.
2. Run `go vet ./... && gofmt -l . && go test -race ./...` before pushing. CI runs the same.
3. Add or update tests with the change. Search-planning changes need a case in `TestIndexedEqualsScan`.
4. Keep the commit message in the imperative ("add X", "fix Y").

By contributing you agree your work is released under the MIT license.
