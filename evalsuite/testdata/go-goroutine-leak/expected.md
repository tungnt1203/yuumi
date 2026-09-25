# Expected: go-goroutine-leak

The PR adds `Notify`. Every call spawns a new goroutine
`for { msg := <-p.events; ... }` that runs FOREVER with no way to stop it
(no `context`, no "done" channel, no `return` after handling). Calling
`Notify` many times leaks one more goroutine each time, all waiting on the
same channel that is never closed.

The bot MUST catch:

- [ ] A finding about a goroutine leak / a goroutine with no way to stop
      (context/channel cancellation) in `Notify`.
- [ ] The finding is on `worker.go`, around the `go func() { for { ... } }()`
      lines in `Notify`.

It should not report `Start`: that goroutine already stops on `ctx.Done()`
and is unchanged in this PR.
