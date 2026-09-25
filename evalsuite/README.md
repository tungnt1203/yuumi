# Eval suite: tracking review quality over time (issue #10)

A set of fixtures to answer "did changing the prompt/model make reviews
better or worse?". Before this, the bot was only tested by hand on a few
real PRs, with nothing to compare against after changing
`internal/review/prompt.go` or the `claude` CLI flags.

## How it works

Each fixture in `testdata/<name>/` simulates a PR with a known, deliberately
seeded bug:

```
testdata/<name>/
  before/        # code state BEFORE the PR (base)
  after/         # code state AFTER the PR (head); the bug is seeded here
  expected.md    # checklist of what the bot MUST catch
```

`go run ./cmd/evalrun` builds a temporary git repo to get the real diff
between `before/` and `after/` (the same unified diff shape GitHub returns),
then calls the review pipeline and `claudecli.Reviewer` directly. It skips
the webhook and GitHub API: the goal is to evaluate the QUALITY of the
prompt/model, not to re-test webhook handling (covered by the
`internal/review` unit tests).

```bash
go run ./cmd/evalrun                          # all fixtures
go run ./cmd/evalrun sql-injection-go         # one fixture
go run ./cmd/evalrun -budget 12000 cross-package-nil-go   # different bundle budget
```

The diff goes through the production bundling path
(`review.BuildBundlePlan`): a fixture larger than the budget is split into
bundles, with the shared primer and "part i/n" notes, like a real PR.
`-budget 0` (the default) uses `review.Job`'s default budget.

It needs an installed and authenticated `claude` CLI (same as running the
server; see the main README). It is NOT part of `go test`/CI; it is a manual
tool for evaluating a change.

## Evaluation process (manual, as issue #10 allows for now)

1. Run `go run ./cmd/evalrun` before the change (baseline) and after
   changing the prompt/model/CLI flags.
2. For each fixture, read the printed `Response` and compare it with the
   `expected.md` checklist: did the bot name the right problem, file, and
   line?
3. Record the result in [`results.md`](./results.md) (one row per fixture
   per run) so it can be compared over time.

Automating the comparison (asserting the expected `category`/keywords in the
JSON response) is a LATER step, once there are enough fixtures to make it
worthwhile.

## Adding a fixture

1. Create `testdata/<short-name>/before/` and `after/`. One or two small
   files are enough. Seed exactly ONE clear bug so the result is easy to
   check (do not mix unrelated bugs in one fixture).
2. Write `expected.md`: describe the bug and the checklist the bot must meet
   (category, file/line, and whether it should avoid false positives in
   unchanged code).
3. Run `go run ./cmd/evalrun <fixture-name>` to make sure the diff builds and
   the review runs before treating the fixture as valid.

Fixture code is input to the model: once results are recorded, do not edit
`before/`/`after/`, or later runs are no longer comparable. Add a new
fixture instead.

Prefer covering the bot's default rules (see the main README, "Default rules
per file type"). Five fixtures (`sql-injection-go`, `go-goroutine-leak`,
`js-floating-promise`, `python-mutable-default`, `hardcoded-secret-go`) each
map to one rule, to show whether that rule actually works or just sits in
the prompt.

`cross-package-nil-go` (issue #73) is different: the bug is only visible
when looking at two packages together, and its diff (~17k characters) is
split into 2 bundles at a 12k budget. Use it to compare quality and cost
across bundle budgets.
