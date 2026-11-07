# Implementation brief: package `limits`

- Goal: implement the interface contract's R1 to R6 as `limits/limits.go`.
- Owned paths: `go.mod`, `limits/limits.go`, `artefacts/self-review.md`.
- Constraints: standard library only; the public interface exactly as listed.
- Checks: `go vet ./...`, `go build ./...`.
- Stop condition: every requirement is met, the self-review checklist is
  answered with evidence and every check ran on the final tree.
