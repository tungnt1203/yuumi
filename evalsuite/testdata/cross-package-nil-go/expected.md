# Expected: cross-package-nil-go

Fixture for issue #73: the bug is only visible when looking at two packages
together. The diff is ~17k characters; with a 12k budget, `api/` and
`store/` land in different bundles (git orders files by name: `api` first,
`store` last). With the current 100k default, it is a single bundle.

- `store/user.go`: `Get(id) (User, error)` becomes
  `Get(ctx, id) (*User, error)`, and **"not found" returns `(nil, nil)`**
  (stated in the doc comment; `ErrNotFound` is removed). This change on its
  own is valid.
- `api/handler.go`: `GetUser` calls the new `Get`, drops the old 404 branch,
  and uses `u.ID`/`u.Name`/`u.Email` without checking `u == nil` → **nil
  pointer dereference (panic) for an unknown id**, and the 404 response is
  lost. The `api/` diff alone does not show the bug.

The bot MUST catch:

- [ ] A `category: "bug"` finding saying `GetUser` dereferences `u` when
      `Get` returns `(nil, nil)` (user not found) → panic / no 404.
- [ ] The finding is on `api/handler.go`, at the `writeJSON(w, userResponse{...})`
      line in `GetUser` (or the `h.users.Get` call). A finding only on
      `store/user.go` saying "returning nil, nil is dangerous for callers"
      without pointing at `GetUser` counts as ⚠️.

It should not report `bug`/`security` at medium or above in `billing/`,
`notify/`, or `report/`: these three new packages have no seeded bug. Low
findings about input validation or integer overflow are acceptable, and
`safeCell` missing `\t`/`\r` per the OWASP recommendation is a correct
finding. The `store.Get` doc comment claiming "the Postgres driver returns
(nil, nil)" is genuinely wrong (database/sql and pgx both return
ErrNoRows); pointing that out is a bonus.

When comparing budgets (`go run ./cmd/evalrun -budget N cross-package-nil-go`),
record the number of bundles and the total cost in results.md.
