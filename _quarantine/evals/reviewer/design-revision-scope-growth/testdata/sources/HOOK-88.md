# HOOK-88: Retry failed webhook deliveries

Type: Story
Reporter: Ravi Menon (Integrations)
Updated: 2026-10-20

## Description

A webhook that fails once is lost today: the delivery is marked failed and
never sent again. Customers ask us to retry transient failures without
hammering an endpoint that is already struggling, and without losing
retries when the service is redeployed.

## Acceptance criteria

1. A delivery that fails with a network error or a 5xx response is retried
   up to 5 times, waiting 1 s, 2 s, 4 s, 8 s and 16 s before attempts 2 to 6.
2. A 4xx response other than 429 is not retried; the delivery is marked
   failed at once.
3. A delivery waiting for a retry is still retried on schedule after the
   service restarts or is redeployed.
4. After the sixth failed attempt the delivery is marked failed.
