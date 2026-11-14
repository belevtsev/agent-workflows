## Purpose
Failed webhook deliveries are retried on a fixed schedule that survives
restarts.

## ADDED Requirements
### Requirement: Retry schedule
The system SHALL retry a delivery that fails with a network error or a 5xx response up to 5 times, waiting 1 s, 2 s, 4 s, 8 s and 16 s before attempts 2 to 6, and SHALL mark it failed after the sixth failed attempt.

#### Scenario: Second attempt after one second
- **GIVEN** delivery `D-1` whose first attempt at 2026-05-01T10:00:00Z gets HTTP 503
- **WHEN** the time reaches 2026-05-01T10:00:01Z
- **THEN** the second attempt of `D-1` is sent, and not before

#### Scenario: Failed after the sixth attempt
- **GIVEN** delivery `D-2` whose first five attempts got HTTP 500
- **WHEN** the sixth attempt also gets HTTP 500
- **THEN** `D-2` is marked `failed` and no seventh attempt is sent

### Requirement: Client errors are not retried
The system SHALL mark a delivery failed without a retry when its endpoint answers with a 4xx status other than 429.

#### Scenario: Endpoint rejects the payload
- **GIVEN** delivery `D-3` whose first attempt gets HTTP 400
- **WHEN** the attempt completes
- **THEN** `D-3` is marked `failed` and no second attempt is sent

### Requirement: Retries survive a restart
The system SHALL send a retry that was scheduled before a restart at its scheduled time after the restart.

#### Scenario: Restart between attempts
- **GIVEN** delivery `D-4` whose first attempt at 2026-05-01T10:00:00Z gets HTTP 502
- **AND** the service restarts at 2026-05-01T10:00:00.5Z
- **WHEN** the time reaches 2026-05-01T10:00:01Z
- **THEN** the second attempt of `D-4` is sent
