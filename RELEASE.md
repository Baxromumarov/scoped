# Release Checklist

Use this checklist before creating a new release tag.

## 1. Preflight

- Ensure working tree is clean: `git status --short`
- Ensure dependencies are tidy: `go mod tidy`
- Verify docs/examples reflect current APIs (`Readme.md`, `examples/`)

## 2. Quality Gates

- Run unit tests: `go test ./...`
- Run race detector: `go test -race ./...`
- Run vet: `go vet ./...`
- Run benchmark smoke:
  `go test -run '^$' -bench '^Benchmark(OverheadPerTask|FanOut|Limited)_' -benchtime=100ms .`

## 3. Versioning

- Follow semantic versioning:
  - `MAJOR`: breaking API changes
  - `MINOR`: backwards-compatible features
  - `PATCH`: backwards-compatible bug fixes
- Update changelog/release notes with:
  - Breaking changes
  - New features
  - Fixes
  - Performance changes

## 4. Tag and Publish

- Create annotated tag:
  `git tag -a vX.Y.Z -m "vX.Y.Z"`
- Push branch and tag:
  `git push origin <branch>`
  `git push origin vX.Y.Z`

## 5. Post-release Verification

- Confirm CI is green on the tagged commit.
- Validate module resolution:
  `go list -m github.com/baxromumarov/scoped@vX.Y.Z`
