# Interface contract: job timeouts in package `jobs`

Language: Go. Module `example.com/jobs`, package `jobs`. HTTP API under
`/v1/jobs`.

## Requirements

- R1: `POST /v1/jobs` accepts a timeout as `timeout_ms` (1 to 3600000) or
  as `timeout_seconds` (1 to 3600), which is converted to milliseconds; the
  job is created with that timeout and the response is HTTP 201.
- R2: A timeout outside its range returns HTTP 400 with error code
  `invalid_timeout` and creates no job; a malformed body returns HTTP 400
  with `malformed_body`.
- R3: `GET /v1/jobs/{id}` and the `POST` response return
  `{"id", "state", "timeout_ms"}`.

## Types and operations

```go
type JobRequest struct {
	Command   string `json:"command"`
	TimeoutMS int    `json:"timeout_ms"`
}

type JobStatus struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	TimeoutMS int    `json:"timeout_ms"`
}
```

Error codes: `malformed_body`, `invalid_timeout`.

## Invariants

- A stored job timeout is always in milliseconds.

## Out of scope

- Removing `timeout_seconds` from requests.

## Open questions

None.
