# Design discussion (architect only; never pass to the implementer)

Considered a sweeper goroutine with a ticker; rejected because it needs an
owner and a stop path the contract does not expose. Decision: lazy expiry on
each call plus an explicit `Sweep`.
