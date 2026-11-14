## 1. Retries
- [ ] 1.1 Schedule retries for network errors and 5xx responses with the 1, 2, 4, 8, 16 s schedule (size: M; depends on: none; check: scenarios `Second attempt after one second` and `Failed after the sixth attempt` pass)
- [ ] 1.2 Mark 4xx other than 429 failed without a retry (size: S; depends on: 1.1; check: scenario `Endpoint rejects the payload` passes)
- [ ] 1.3 Keep scheduled retries across a restart (size: M; depends on: 1.1; check: scenario `Restart between attempts` passes)
