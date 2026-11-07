# Implementation brief: package `idem`

- Goal: implement the interface contract's R1 to R8 as `idem/idem.go`.
- Owned paths: `go.mod`, `idem/idem.go`, `artefacts/self-review.md`.
- Constraints: standard library only; the public interface exactly as listed.
- Checks: `go vet ./...`, `go build ./...`.
- Stop condition: every requirement is met, the self-review checklist is
  answered with evidence and every check ran on the final tree.
