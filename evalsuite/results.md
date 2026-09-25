# Eval suite results over time

One row per fixture per `go run ./cmd/evalrun` run, recorded RIGHT AFTER
comparing the `Response` with the fixture's `expected.md`, so results can be
compared across prompt/model changes (see [README.md](./README.md)).

`Caught?`: ✅ as in expected.md · ⚠️ caught but missing details (wrong line,
different category...) · ❌ not caught / false report.

| Date | Commit/change | Fixture | Caught? | Notes |
|------|---------------|---------|---------|-------|
| 2026-09-15 | baseline (eval suite added, issue #10) | sql-injection-go | ✅ | category=security, right line, suggestion uses placeholders like the rest of the file |
| 2026-09-15 | baseline (eval suite added, issue #10) | python-mutable-default | ✅ | Main bug caught + 2 reasonable extra findings (mutates the caller's list, wrong docstring) |
| 2026-09-15 | baseline (eval suite added, issue #10) | go-goroutine-leak | ✅ | Main bug caught (leak in Notify) + a few reasonable extras (debug println, unbuffered channel) |
| 2026-09-15 | baseline (eval suite added, issue #10) | js-floating-promise | ✅ | Floating promise caught, right line, suggestion uses .catch(...) as expected |
| 2026-09-23 | secret rule + regex pre-scan (issue #64) | hardcoded-secret-go | ✅ | category=security, severity=high (Claude recognized the AWS EXAMPLE key and lowered it from critical), right lines 21-22, suggestion uses os.Getenv; 1 reasonable extra finding (validate the env var pair) |
| 2026-09-25 | bundle budget 12k (default at the time, issue #73) | cross-package-nil-go | ✅ | 2 bundles, $0.46. Bundle 1 (with api/) read store/user.go itself thanks to the primer → caught the nil deref at api/handler.go:31; bundle 2 reported the same bug from the store side (duplicate finding). Also: the "Postgres driver returns nil,nil" comment is wrong (correct), safeCell misses \t/\r (correct), 2 low findings in billing |
| 2026-09-25 | bundle budget 100k (issue #73) | cross-package-nil-go | ✅ | 1 bundle, $0.30 (35% cheaper). Caught the nil deref at api/handler.go:30 (critical) + the breaking change in store, no duplicate. Extra findings in billing/report similar to the 12k run |
| 2026-09-25 | bundle budget 12k, run 2 | cross-package-nil-go | ✅ | 2 bundles, $0.36. Nil deref at api/handler.go:30 (high), reported again from the store side (critical): duplicate. Also: Get still returns Disabled users (correct) |
| 2026-09-25 | bundle budget 12k, run 3 | cross-package-nil-go | ✅ | 2 bundles, $0.39. Caught at api/handler.go:31 (critical), duplicated at store/user.go:50 (high) |
| 2026-09-25 | bundle budget 100k, run 2 | cross-package-nil-go | ✅ | 1 bundle, $0.26. api/handler.go:30 (critical), no duplicate |
| 2026-09-25 | bundle budget 100k, run 3 | cross-package-nil-go | ✅ | 1 bundle, $0.24. api/handler.go:30 (critical), no duplicate. Also: Mailer.from is unused (correct, a real bug in the fixture) |
| 2026-09-25 | CLI flags #100 (--tools Read,Grep,Glob, --restricted, --no-session-persistence, --max-budget-usd) | cross-package-nil-go | ✅ | $0.22 (was ~$0.27). api/handler.go:30 high, no duplicate; also Mailer.from unused (correct) |
| 2026-09-25 | CLI flags #100 | go-goroutine-leak | ✅ | $0.09 (was $0.19: fewer tools → shorter system prompt, cache write 19.5k → 8.2k tokens). Leak in Notify, right line |
| 2026-09-25 | CLI flags #100 | hardcoded-secret-go | ✅ | $0.09. security/high, right lines 21-22, recognized the AWS EXAMPLE key, suggestion os.Getenv |
| 2026-09-25 | CLI flags #100 | js-floating-promise | ✅ | $0.08. Floating promise at the right line (20), suggestion .catch(...) |
| 2026-09-25 | CLI flags #100 | python-mutable-default | ✅ | $0.09. Mutable default caught + mutating the caller's list |
| 2026-09-25 | CLI flags #100 | sql-injection-go | ✅ | $0.08. security/critical at the fmt.Sprintf line |
