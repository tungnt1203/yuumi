# Expected: sql-injection-go

The PR adds `FindUsersBySearchTerm`, which builds the SQL query with
`fmt.Sprintf`, concatenating `term` (input from a search box) directly into
the query instead of using a `?` placeholder like the other two functions in
the same file. Classic SQL injection.

The bot MUST catch:

- [ ] A `category: "security"` finding (`"bug"` also accepted) about SQL
      injection / string concatenation into the query.
- [ ] The finding is on `query.go`, at the `fmt.Sprintf(...)` line or the
      `db.Query(query)` line.
- [ ] The `suggestion` (if any) uses a placeholder/parameterized query, not
      hand-rolled string escaping.

It should not report false positives on `FindUserByName`/`FindUsersByRole`:
both already use placeholders correctly and are unchanged in this PR.
