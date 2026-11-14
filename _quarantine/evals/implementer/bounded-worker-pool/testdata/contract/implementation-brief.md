# Implementation brief: package `pool`

- Goal: implement the interface contract's R1 to R7 as `pool/pool.go`.
- Owned paths: `go.mod`, `pool/pool.go`, `artefacts/self-review.md`.
- Constraints: standard library only; keep the public interface exactly as
  the contract lists it; no package-level state.
- Checks: `go vet ./...`, `go build ./...`.
- Stop condition: every requirement is met, the self-review checklist is
  answered with evidence and every check ran on the final tree.
