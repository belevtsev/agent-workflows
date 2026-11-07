# Design discussion (architect only; never pass to the implementer)

Options considered: a buffered channel of tasks with `size` consumers versus
a semaphore channel of `size` tokens with one goroutine per task. Decision:
either meets the contract; the implementer chooses. Consequence noted: the
buffered-channel variant must not buffer, or R2 would accept more than
`size` tasks without running them.
