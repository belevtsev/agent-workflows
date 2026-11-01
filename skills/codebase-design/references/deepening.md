# Deepening

Use this to merge a cluster of shallow modules into a deeper one without losing test coverage.

## Dependency categories

Classify each dependency first, because the category decides how the deepened module is tested.

1. **In-process.** Pure computation and in-memory state. Always deepenable: merge the modules and test through the new interface. No adapter is needed.
2. **Local substitutable.** Dependencies with a real local stand-in: `httptest` for HTTP, `bufconn` for gRPC, `fstest.MapFS` for read-only files, a temporary directory, or Postgres in a container. Test with the stand-in. The seam stays internal, so no port appears at the module's interface. Prefer real Postgres over an in-memory SQL engine, because transaction and constraint behavior is usually what you're testing.
3. **Remote but owned.** Your own services across a network. Define a port at the seam, with a gRPC or HTTP adapter in production and an in-memory adapter in tests, so the logic stays in one deep module even though it's deployed across a network.
4. **True external.** Third-party services you don't control. Inject them as a port and use a fake in tests. Cover the real adapter separately with a contract test, because a fake only encodes what you believe the service does.

## Seam discipline

- Add a port only when at least two adapters are justified, typically production and test. A single-adapter seam is just indirection.
- A deep module can have internal seams, private to its implementation and used by its own tests. Don't expose them through the interface because a test wants them. In Go, an `export_test.go` file can reach them without widening the public API.

## Replace tests, don't layer them

- Write new tests at the deepened module's interface, asserting observable outcomes.
- Delete the old unit tests on the merged shallow modules once the new ones cover the same behavior. Keeping them locks in the structure you just removed.
- Before deleting a test, confirm a new test fails for the defect it guarded. Otherwise you're losing coverage rather than moving it.
- A test that must change when the implementation changes, with no behavior change, is testing past the interface.
