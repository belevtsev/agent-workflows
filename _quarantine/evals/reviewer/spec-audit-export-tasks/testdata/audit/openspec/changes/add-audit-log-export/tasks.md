## 1. Export endpoint
- [ ] 1.1 Return the audit events of an inclusive UTC date range as CSV with the header `time,actor,action,target`, and document the endpoint in `docs/api.md` (size: M; depends on: none; check: `go test ./audit -run TestExportRange` passes and `docs/api.md` describes `GET /audit/export`)
- [ ] 1.2 Improve audit export performance and tidy up the audit package as needed
- [ ] 1.3 Reject `from` after `to` with HTTP 400 `from must not be after to` (size: S; depends on: 1.4; check: scenario `from after to` passes)
- [ ] 1.4 Restrict export to the `auditor` role with HTTP 403 for anyone else (size: S; depends on: 1.1; check: `go test ./audit -run TestExportForbidden`)
