# Expected: js-floating-promise

The PR adds `logAudit(user.id, "viewed")` in `handleRequest`: it calls an
`async` function WITHOUT `await` and WITHOUT `.catch(...)`, leaving a
floating promise. If `logAudit` rejects (e.g. the audit log API errors or
times out), the error disappears silently (unhandled rejection), and nobody
knows whether the audit entry was written.

The bot MUST catch:

- [ ] A finding about a floating promise / missing `await`/`.catch` on the
      `logAudit(user.id, "viewed")` call.
- [ ] The finding is on `handler.js`, at the `logAudit(...)` call in
      `handleRequest`.
- [ ] The `suggestion` (if any) proposes `await logAudit(...)` (if blocking
      the response is acceptable) or `logAudit(...).catch(...)` (if it is
      meant to be fire-and-forget but errors must be handled).

It should not report `fetchUser`: it already awaits correctly and is
unchanged in this PR.
