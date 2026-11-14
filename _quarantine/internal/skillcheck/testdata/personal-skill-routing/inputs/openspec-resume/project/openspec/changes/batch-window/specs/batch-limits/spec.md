## MODIFIED Requirements

### Requirement: Default batch limit

A tenant without an explicit batch limit uses 50.

#### Scenario: Explicit limit

- WHEN a tenant has a configured limit of 80
- THEN the request is admitted using 80.
