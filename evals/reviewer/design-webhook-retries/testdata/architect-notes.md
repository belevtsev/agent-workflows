# Architect scratch notes (the architect's own reasoning, kept from its session)

- Thought about a next_attempt_at column polled by the sweeper; the five
  second tick felt too coarse, so I did not write it up as an option.
- Restarts: ops say deploys drain for thirty seconds, so in-memory timers
  should be fine in practice.
- R3 is vague on purpose; the schedule lives in the brief for the implementer.
