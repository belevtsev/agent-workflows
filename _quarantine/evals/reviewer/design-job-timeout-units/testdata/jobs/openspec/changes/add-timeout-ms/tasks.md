## 1. Timeout in milliseconds
- [ ] 1.1 Accept `timeout_ms` or `timeout_seconds` on job creation and reject conflicting values (size: M; depends on: none; check: scenarios `Timeout in milliseconds`, `Legacy timeout in seconds` and `Conflicting timeouts` pass)
- [ ] 1.2 Return both timeout fields in job status responses (size: S; depends on: 1.1; check: scenario `Status of a sub-second job` passes)
