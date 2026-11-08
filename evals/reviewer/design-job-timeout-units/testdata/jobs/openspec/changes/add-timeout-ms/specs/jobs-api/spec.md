## MODIFIED Requirements
### Requirement: Job creation timeout
The system SHALL accept a job timeout as `timeout_ms` (1 to 3600000) or as `timeout_seconds` (1 to 3600), and SHALL reject with HTTP 400 and error code `conflicting_timeout` a request whose `timeout_ms` and `timeout_seconds` disagree.

#### Scenario: Timeout in milliseconds
- **GIVEN** a request with `"timeout_ms": 250`
- **WHEN** it is posted to `/v1/jobs`
- **THEN** the job is created with a 250 ms timeout and HTTP 201

#### Scenario: Legacy timeout in seconds
- **GIVEN** a request with `"timeout_seconds": 30` and no `timeout_ms`
- **WHEN** it is posted to `/v1/jobs`
- **THEN** the job is created with a 30000 ms timeout and HTTP 201

#### Scenario: Conflicting timeouts
- **GIVEN** a request with `"timeout_ms": 1500` and `"timeout_seconds": 30`
- **WHEN** it is posted to `/v1/jobs`
- **THEN** the response is HTTP 400 with error code `conflicting_timeout` and no job is created

### Requirement: Job status timeout fields
The system SHALL return both `timeout_ms` and `timeout_seconds`, the latter rounded up to whole seconds, in every job status response until `timeout_seconds` is removed.

#### Scenario: Status of a sub-second job
- **GIVEN** a job created with `"timeout_ms": 250`
- **WHEN** its status is fetched
- **THEN** the response has `"timeout_ms": 250` and `"timeout_seconds": 1`
