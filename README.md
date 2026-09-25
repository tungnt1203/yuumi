<div align="center">
  <img src=".github/assets/logo.jpeg" alt="Yuumi Review logo" width="160" />

  # Yuumi Review

  **Automated code review for GitHub pull requests, powered by the Claude Code CLI**

  [![CI](https://github.com/tungnt1203/yuumi/actions/workflows/ci.yml/badge.svg)](https://github.com/tungnt1203/yuumi/actions/workflows/ci.yml)
  [![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
  [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

<p align="center"><b>English</b> · <a href="README.vi.md">Tiếng Việt</a></p>

Yuumi is a GitHub App that reviews pull requests. It runs when someone
mentions `@yuumi <command>` in a PR comment, or automatically when a PR is
opened or receives new commits. It calls the
[Claude Code CLI](https://docs.claude.com/claude-code) on a checkout of the
PR, then posts the result back as a summary comment plus inline comments on
the exact lines of code.

> The bot's own output on PRs (the summary comment, inline findings, check
> run text) is in Vietnamese. The code, docs, and contribution workflow are
> in English.

## Contents

- [Features](#features)
- [Architecture](#architecture)
- [Project layout](#project-layout)
- [Requirements](#requirements)
- [Setup and configuration](#setup-and-configuration)
- [Per-repo review config (`.yuumi.yml`)](#per-repo-review-config-yuumiyml)
- [Running locally](#running-locally)
- [Running with Docker](#running-with-docker)
- [How it works](#how-it-works)
- [Eval suite](#eval-suite)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)

## Features

- **Review on mention or automatically**: comment `@yuumi review`, or let the bot run when a PR is opened or gets new commits (allowlisted by author).
- **GitHub App authentication**: RS256 JWT plus installation access tokens (cached and refreshed automatically). The bot has its own identity, `<App name>[bot]`, not tied to a personal account.
- **Categorized findings and inline comments**: Claude returns findings with `category`, `severity`, and a suggested fix. Findings that match a line in the diff are posted inline through the Reviews API; the rest go into the summary comment.
- **Default rules per language**: Go, JavaScript/TypeScript, Python, and SQL rules are added to the prompt based on the file extensions in the diff, with no repo configuration. A rule for hardcoded secrets and credentials applies to every file.
- **Per-repo configuration** through `.yuumi.yml` (file exclusions, custom review instructions, severity gate). The repo's `.gitignore` is read as well.
- **`yuumi review` check run** on the PR's Checks tab for every review, usable in branch protection.
- **Incremental review**: later reviews of the same PR only look at changes since the last reviewed SHA, which saves tokens.
- **Noise filtering and diff bundling**: junk files are filtered out and the diff is split into bundles so large PRs are still reviewed in full.
- **Production hardening**: retries on transient errors, a cap on concurrent jobs, duplicate-comment protection, and a JSON log for every Claude CLI call.
- **Sandboxed execution**: with `SANDBOX=docker`, every command on PR code runs in a per-job container with restricted network access and no real credentials.
- **Eval suite** (`evalsuite/`) to track review quality over time when the prompt or model changes.

## Architecture

```
PR comment "@yuumi <command>"             PR opened / new commits pushed
        │  (issue_comment event)                  │  (pull_request event)
        │  (GitHub webhook: HTTP POST, HMAC-SHA256 signed, routed on the X-GitHub-Event header)
        ▼                                         ▼
  Go HTTP server (cmd/server)
        │  verify signature → check allowlist → route by event type
        ▼
  React 👀 (mention flow only) + post a placeholder comment ("Đang review...")  [internal/githubapi]
        │
        ▼
  Get the PR head SHA → git fetch --depth 1 into a work dir   [internal/githubapi, internal/gitrepo]
        │
        ▼
  Get the real PR diff (full, or only new changes if reviewed before)
  → filter junk files → split into bundles → build prompts   [internal/review]
        │
        ▼
  Run `claude -p` on the checkout, one call per bundle       [internal/claudecli, internal/sandbox]
  (retries transient errors; the work dir is removed afterwards)
        │
        ▼
  Edit the placeholder with the result, post inline findings
  through the Reviews API, complete the check run            [internal/githubapi]
```

## Project layout

```
cmd/
  server/main.go          # entry point: load config, register routes, start the server
  egressproxy/main.go     # egress + credential proxy for the Docker sandbox
  evalrun/main.go         # CLI that runs the eval suite (see Eval suite)
  reviewstats/main.go     # CLI that sums tokens/cost from review logs per repo and per day
internal/
  config/                 # read and validate environment variables
  review/                 # all review logic: Job (one review run), prompts, diff bundling,
                          # finding/hunk parsing, inline comments, language rules, .yuumi.yml,
                          # .gitignore, submodules, Dispatcher (concurrency cap), SHA dedupe
  webhook/                # payload structs, VerifySignature (HMAC), SeenComments (dedupe)
  claudecli/              # run the `claude` CLI on the checkout, retry, parse the result
  sandbox/                # where commands on PR code run: Local, or a Docker container per job
  egress/                 # CONNECT allowlist proxy and Anthropic credential proxy
  githubapi/              # GitHub REST API: reactions, comments, diff, compare, Reviews, check runs
  githubapp/              # GitHub App auth: sign the JWT, exchange and cache installation tokens
  gitrepo/                # fetch the PR head SHA into a work dir, return cleanup()
  healthcheck/            # check that the claude CLI and GitHub App auth work, cached for /health
  procenv/                # strip server secrets from subprocess environments
  reviewstate/            # last reviewed SHA per PR, and the per-bundle resume cache
  reviewlog/              # JSON log (prompt/response/error/timing/usage) for every Claude CLI call
  evalrunner/             # build a diff from before/after fixtures and review it, for cmd/evalrun
evalsuite/                # seeded-bug fixtures + results.md tracking review quality over time
.github/workflows/ci.yml  # CI: go build, go vet, go test, docker build (+ tests inside the image)
```

## Requirements

- Go 1.26+ (see `go.mod`)
- [Claude Code CLI](https://docs.claude.com/claude-code), installed and authenticated (`claude --version` works)
- `git` on the machine that runs the server (used to fetch the PR head)
- A registered [GitHub App](https://github.com/settings/apps) with these permissions: `Issues: Read and write`, `Pull requests: Read and write` (inline findings go through the Reviews API), `Contents: Read-only` (to clone private repos), and `Checks: Read and write` (the `yuumi review` check run). Subscribe it to the `Issue comments` and `Pull request` events and **install** it on the target repos.
- A webhook secret of your choice. GitHub uses it to sign requests so the server can reject forged ones. Set it in the App's webhook settings, not on each repo.

## Setup and configuration

Create a `.env` file in the repo root. It is already in `.gitignore`; **do not commit it**.

```
GITHUB_APP_ID=<App ID, from the App's settings page>
GITHUB_APP_PRIVATE_KEY_PATH=<path to the .pem downloaded when creating the App>   # for local runs
# OR, instead of _PATH (easier for Docker/cloud deploys, avoids PEM newline escaping):
# GITHUB_APP_PRIVATE_KEY=<the .pem file base64-encoded on one line, e.g. base64 < private-key.pem>
GITHUB_WEBHOOK_SECRET=<your secret, same value as in the App's webhook settings>
ALLOWED_USERS=<username1,username2,...>   # GitHub usernames allowed to trigger the bot
```

`GITHUB_APP_ID`, `GITHUB_WEBHOOK_SECRET`, `ALLOWED_USERS`, and **one of** the two private key variables are **required**; the server does not start without them. These variables are **optional** and fall back to a default:

| Variable | Default | Meaning |
|----------|---------|---------|
| `MAX_DIFF_BUNDLE_CHARS` | `100000` | Maximum diff characters per bundle (one bundle = one Claude call). Positive integer. |
| `REVIEW_MAX_BUDGET_USD` | `3` | Cost cap (USD) for one Claude CLI call (`--max-budget-usd`). A bundle over the cap fails and is not retried. Positive number. |
| `REVIEW_TIMEOUT_MINUTES` | `15` | Maximum duration of one Claude CLI call. A bundle that times out fails and is not retried. Positive integer. |
| `MAX_CONCURRENT_REVIEWS` | `3` | Maximum review jobs running at once; extra jobs wait for a free slot. Positive integer. |
| `REVIEW_LOG_DIR` | `logs/reviews` | Directory for the per-call review log (see [Review logs](#review-logs)). |
| `REVIEW_STATE_FILE` | `logs/review-state.json` | File storing the last reviewed SHA per PR (see [Incremental review](#incremental-review)). |
| `BUNDLE_CACHE_DIR` | `logs/bundle-cache` | Directory storing results of finished bundles, so an interrupted review does not redo them. |
| `SANDBOX` | _(empty)_ | `docker`: each review job runs `gofmt`/`go vet` and the Claude CLI in its own container (see [Per-job sandbox](#per-job-sandbox-sandboxdocker)). Empty: run directly on the server. Any other value fails at startup. |
| `SANDBOX_IMAGE` | _(empty)_ | Image for sandbox containers, required when `SANDBOX=docker`. Use the server's own image. |
| `WORK_DIR` | system temp dir | Directory holding PR checkouts. Required when `SANDBOX=docker`. |

## Per-repo review config (`.yuumi.yml`)

A reviewed repo can add a `.yuumi.yml` file at its root to customize how the bot reviews it, without touching the bot's code or configuration:

```yaml
exclude:
  - "testdata/"
  - "*.generated.go"
instructions: |
  Be strict about error handling.
  Always ask for unit tests on exported functions.
block_severity: [critical, high]
```

- `exclude`: extra patterns of files/directories to leave out of the review. They are **added to** the bot's defaults (lock files, `vendor/`, `node_modules/`...), not a replacement.
- `instructions`: text inserted into the prompt, so the review follows the repo's own conventions and level of strictness.
- `block_severity` (severity gate): findings at these levels (`critical`/`high`/`medium`/`low`) make the `yuumi review` check run fail. Combine it with branch protection (see [The `yuumi review` check run](#the-yuumi-review-check-run)) to block merges. When unset, nothing is blocked: the check run is always `success` and the bot only comments. Unknown values (e.g. `hight`) are ignored, with a warning on the check run.
  - **Read only from `.yuumi.yml` on the base branch** (the PR's base commit, through the GitHub API), never from the PR. Otherwise a PR author could delete this line in their own PR to get past the gate. To change the gate, merge the `.yuumi.yml` change into the base branch first. `exclude` and `instructions` are still read from the PR.
  - If the base file cannot be read (GitHub API error, invalid YAML), the gate does not apply to that review and the check run summary says so.

Without this file the bot uses its defaults. If the file is not valid YAML, the bot logs the error and reviews with the defaults.

## Running locally

```bash
set -a && source .env && set +a
go run ./cmd/server
```

The server listens on `:8080` with `GET /health` and `POST /webhook`. To send
a test webhook with curl, or test against real GitHub through ngrok, see
[CONTRIBUTING.md](./CONTRIBUTING.md).

## Running with Docker

The image contains the server and every tool a review needs: `git`, the Go toolchain (for `gofmt`/`go vet`), and the Claude CLI (pinned to `2.1.281`, auto-update disabled). It runs as the unprivileged user `yuumi`, not root.

```bash
docker build -t yuumi .

docker run -d --name yuumi -p 8080:8080 \
  -v yuumi-logs:/app/logs \
  -v /path/to/app.pem:/run/secrets/app.pem:ro \
  -e GITHUB_APP_PRIVATE_KEY_PATH=/run/secrets/app.pem \
  -e GITHUB_APP_ID=... \
  -e GITHUB_WEBHOOK_SECRET=... \
  -e ALLOWED_USERS=... \
  -e CLAUDE_CODE_OAUTH_TOKEN=... \
  yuumi
```

- **The Claude CLI in the container cannot use the host's login** (keychain). Pass `CLAUDE_CODE_OAUTH_TOKEN` (created with `claude setup-token`, uses your Claude plan) or `ANTHROPIC_API_KEY`. Never bake credentials into the image.
- **`/app/logs`** holds the review log, review state, and bundle cache. Mount a volume so they survive container recreation.
- **The GitHub App private key** can be mounted as a read-only file as above, or passed base64-encoded in `GITHUB_APP_PRIVATE_KEY`.
- `GET /health` returns `503` with a reason when the Claude CLI is not logged in or GitHub App auth fails; the Docker `HEALTHCHECK` uses it. The server re-checks every 5 minutes, so the status can lag by up to that long.
- To upgrade the Claude CLI, change `CLAUDE_VERSION` and the two checksums `CLAUDE_SHA256_AMD64`/`CLAUDE_SHA256_ARM64` in the Dockerfile (from `https://downloads.claude.ai/claude-code-releases/<version>/manifest.json`). First re-verify the security flags described in [Running safely on untrusted PR code](#running-safely-on-untrusted-pr-code) and run the eval suite. The build checks the sha256, and `claude --version` must match the pinned version. Dependabot does not bump the Claude CLI or the Docker CLI.

### Per-job sandbox (`SANDBOX=docker`)

By default, `gofmt`/`go vet` and the Claude CLI run inside the server container, on untrusted PR code. With `SANDBOX=docker`, each review job gets its own container, created from this image and removed when the job ends (issue #78):

- The PR checkout is mounted **read-only**, and so is the root filesystem. Only `/tmp` and `$HOME` are writable, as tmpfs, and disappear with the container.
- `--cap-drop ALL`, `no-new-privileges`, and limits of 2 GB RAM, 2 CPUs, and 512 processes.
- No server secrets. Only allowlisted variables (`forwardEnvKeys` in `internal/sandbox/docker.go`: Go settings) are passed to each command, as `-e KEY` so values stay out of the process arguments.
- The server still does the clone itself (`git fetch` does not run PR code), so the installation token never enters the sandbox.
- A command that times out is stopped inside the container too (wrapped in `timeout`), not only the `docker exec` client on the server.
- **Restricted network**: the sandbox is only on the Docker network `yuumi-sandbox`, created with `--internal` (no route to the Internet). The only way out is the `yuumi-egress` container (`cmd/egressproxy`), which runs two proxies:
  - `:3128`, a `CONNECT` proxy that only allows port 443 to `proxy.golang.org` (modules for `go vet`). Every other destination, including `api.anthropic.com`, plain HTTP, and direct connections without the proxy, is blocked.
  - `:3129`, a **credential proxy** to the Anthropic API: the Claude CLI in the sandbox reaches the model through `ANTHROPIC_BASE_URL=http://yuumi-egress:3129`.
- **The real Claude credential never enters the sandbox.** The sandbox only gets a placeholder of the same kind (`CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY` set to `yuumi-sandbox-placeholder`), so the Claude CLI starts and sends the right header type. The credential proxy drops the placeholder, attaches the real credential, forwards over HTTPS, and only allows paths under `/v1/`. Even if PR code tricks Claude into reading the environment, it only sees the placeholder. The real credential lives in the server and the `yuumi-egress` container.
- Both proxies log `ALLOW`/`DENY` for every connection and request: `docker logs yuumi-egress`.

```bash
WORK="$PWD/work"   # path on the host
docker run -d --name yuumi -p 8080:8080 \
  ... variables as above ... \
  -e SANDBOX=docker -e SANDBOX_IMAGE=yuumi -e WORK_DIR="$WORK" \
  -v "$WORK:$WORK" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --group-add "$(stat -c %g /var/run/docker.sock)" \
  yuumi
```

- **`WORK_DIR` must be mounted at the same path** on the host and in the server container. The Docker daemon resolves mount paths on the host, so when the server clones into `$WORK/...`, the sandbox can mount that same directory.
- **`docker.sock`**: the `yuumi` user must belong to the group that owns the socket. On Linux that is the `docker` group's gid (the `stat` command above). On Docker Desktop (macOS) the socket is `root:root`, so use `--group-add 0`. Note that anyone who takes over the server process gets root-equivalent access to the host through this socket. PR code runs in the sandbox, which has no socket.
- If a sandbox cannot be created (e.g. the Docker daemon is down), the review fails. It **never** falls back to running PR code on the server.
- At startup, the server creates the `yuumi-sandbox` network if needed and recreates the `yuumi-egress` container from the current image. If that fails, the server exits. If a `yuumi-sandbox` network exists but is not `--internal`, the server also exits with an error rather than let the sandbox reach the Internet. Remove that network (`docker network rm yuumi-sandbox`) and start again.
- Sandbox containers carry the label `yuumi.sandbox=1` and exit on their own after at most 2 hours if the server dies mid-job. To clean up by hand: `docker rm -f $(docker ps -aq --filter label=yuumi.sandbox=1)`.

## How it works

<details id="the-yuumi-review-check-run">
<summary><strong>The <code>yuumi review</code> check run</strong></summary>

Every review (mention or automatic) creates a check run named `yuumi review` on the PR's head SHA (`internal/review/checkrun.go`):

- `in_progress` as soon as the review starts, before the clone.
- Review completed: `success`, the title is the number of findings, and the summary is the per-severity table. The "Details" link points to the full review comment. If a finding is at a level listed in `block_severity` (see [`.yuumi.yml`](#per-repo-review-config-yuumiyml)), the result is `failure`; without that setting, findings still give `success`.
- Review failed (clone error, Claude CLI error on part of the diff, error posting the comment), or a submodule's content was not reviewed: `neutral`. The check run never stays `in_progress`.
- If creating the check run fails (e.g. the App lacks the `Checks` permission), the error is logged and the review still runs.

To require a finished review before merging, go to the repo's **Settings → Branches → Branch protection rule** (or Rulesets), enable "Require status checks to pass", and select `yuumi review`. GitHub counts `neutral` as passing, so a failed review does not block the merge.

</details>

<details id="noise-filtering-and-diff-bundling">
<summary><strong>Noise filtering and diff bundling</strong></summary>

The PR diff is processed before it is sent to Claude (`internal/review/diffsplit.go`):

1. **Filter out files not worth reviewing**, before measuring size: lock files (`go.sum`, `package-lock.json`, `yarn.lock`...), generated directories (`vendor/`, `node_modules/`, `dist/`, `build/`...), minified/binary/image/font files. This list is merged with `exclude` from `.yuumi.yml` and the repo's `.gitignore`. The result comment lists skipped files so readers know.
2. **Bundling**: the remaining diff is grouped by directory (implementation and tests in the same directory stay together) and split into bundles no larger than `MAX_DIFF_BUNDLE_CHARS` (default 100,000 characters, so most PRs are a single bundle; see issue #73). Each bundle is a separate `claude -p` call, run **sequentially**, and the results are merged into one comment.
3. **Truncated diff warning**: if the diff has fewer files than the `changed_files` GitHub reports for the PR (GitHub truncates the diff of very large PRs), the comment says so.
4. **Submodules** (`internal/review/submodule.go`, issue #93): the bot does not fetch submodules, so gitlink diffs (only a `Subproject commit` line) are removed from the prompt. The comment lists which submodules changed (`old → new`) and states that **their content was not reviewed**; the header does not claim "no issues" and the check run is `neutral`. A PR that changes the URL of an existing submodule in `.gitmodules` (compared with the base branch) gets a `security`/`high` finding.

</details>

<details id="reliability-retries-concurrency-dedupe">
<summary><strong>Reliability: retries, concurrency, dedupe</strong></summary>

- **Retries**: a Claude CLI call that fails to run (network error, CLI crash...) is retried up to 3 times in total, with backoff. Errors Claude reports itself (`is_error`, including going over `REVIEW_MAX_BUDGET_USD`), unparseable output, and timeouts (`REVIEW_TIMEOUT_MINUTES`, default 15 minutes) are **not** retried: the same input would give the same result and only multiply the wait.
- **Concurrency cap**: the `Dispatcher` runs each job in its own goroutine but allows at most `MAX_CONCURRENT_REVIEWS` at once (each job spawns real `git` and `claude` processes). The HTTP handler always answers the webhook immediately, never blocked by the queue.
- **Dedupe**: processed comment IDs are kept in memory (`webhook.SeenComments`), so a GitHub redelivery or a duplicate mention does not start two jobs editing the same comment. The ID is marked as soon as the webhook arrives, so two concurrent requests do not both run. If getting a token or posting the placeholder fails, the mark is removed and the handler returns `500` so GitHub retries. This state is in RAM with no TTL and is lost on restart, which is acceptable for a single internal instance; multiple instances would need external storage (Redis/DB).
- **Error handling**: a Claude CLI error on a bundle is written into the comment as `❌ Review thất bại: ...` (the error of that call). Errors getting the head SHA, cloning, and panics (recovered so the server does not crash) only replace the placeholder with a generic sentence; details go to the server log, because raw errors can contain machine paths or tokens. If editing the comment itself fails, the placeholder may stay as is, with the cause only in the server log.

</details>

<details id="static-checks-before-review-go-repos">
<summary><strong>Static checks before review (Go repos)</strong></summary>

If the reviewed repo has a `go.mod`, the bot runs `gofmt` and `go vet` on the checkout and adds the report to the prompt, so Claude focuses on logic and design instead of repeating what the tools already caught. Non-Go repos skip this step; other languages' tools are not supported yet.

</details>

<details id="running-safely-on-untrusted-pr-code">
<summary><strong>Running safely on untrusted PR code</strong></summary>

PR code can come from anyone, so every subprocess that runs on the checkout is locked down (issue #78):

- **Claude CLI** runs with `--setting-sources user` and `--strict-mcp-config`, ignoring the repo's `.claude/settings*.json` and `.mcp.json` (those files can declare hooks that run shell commands). Tools are limited to `Read`, `Grep`, `Glob` through the `--tools` allowlist, not a blocklist, so tools added in future CLI versions do not slip in; the review only reads code. `--restricted` confines file tools to the repo directory, `--no-session-persistence` keeps transcripts off disk, and `--max-budget-usd` (`REVIEW_MAX_BUDGET_USD`) stops runaway calls.
- **`gofmt`/`go vet`** run with a 2-minute timeout, `GOTOOLCHAIN=local` (no toolchain download requested by the PR's `go.mod`), `CGO_ENABLED=0`, and `GOPROXY` limited to `proxy.golang.org` (no arbitrary VCS clones).
- No subprocess receives server secrets (`GITHUB_WEBHOOK_SECRET`, the App private key...) through its environment.
- The installation token is only passed to the `git fetch` command (through `GIT_CONFIG_*`, as an `Authorization` header), never embedded in the remote URL. So it is not in the checkout's `.git/config`, which Claude can read, nor in process arguments.
- Config files the server reads from the checkout (`.yuumi.yml`, `.gitignore`, `.gitmodules`) are opened through `os.Root`, so a symlink pointing outside the checkout is rejected.
- **Do not add `additionalDirectories` or `allow` rules for Read/Grep/Glob to `~/.claude/settings.json` of the user running the server.** That would reopen a path to reading server secrets.

With `SANDBOX=docker`, these subprocesses also run in a per-job container that does not share the server's filesystem (see [Per-job sandbox](#per-job-sandbox-sandboxdocker)). The sandbox can only reach `proxy.golang.org` and the credential proxy to the Anthropic API, and the real Claude credential never enters it. Remaining gap: the Claude CLI still reads the repo's `CLAUDE.md` (it can only influence the review's content).

</details>

<details id="review-logs">
<summary><strong>Review logs</strong></summary>

Each Claude CLI call is written as one JSON file in `logs/reviews/` (change with `REVIEW_LOG_DIR`) with: time, repo, PR number, SHA, bundle index, **the verbatim prompt and response**, the error (if any), duration (`duration_ms`), attempts (`attempts`), the number of turns Claude used (`num_turns`; an unusually low value on a multi-file bundle is a sign Claude reviewed the diff blind), and `usage`: tokens (`input_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `output_tokens`) and `cost_usd` as reported by the Claude CLI, summed across retries. Most input is in the two cache fields; `input_tokens` is only the uncached part. Use the log to investigate failed or surprising reviews. Logging errors are printed to the console and never block a review. `logs/` is in `.gitignore`; since prompts and responses are stored verbatim, be careful if reviewed code contains sensitive data.

Totals of calls, tokens, and cost per repo and per day (in the local time of the machine running the command):

```bash
go run ./cmd/reviewstats                 # reads $REVIEW_LOG_DIR or logs/reviews
go run ./cmd/reviewstats -dir <dir> -json
```

Logs written before `usage` existed still count as calls, with zero tokens and cost; `reviewstats` reports how many (`no_usage` in `-json`). Attempts killed by a timeout have no output, so their tokens are not counted and the real cost can be slightly higher.

</details>

<details id="default-rules-per-file-type">
<summary><strong>Default rules per file type</strong></summary>

Besides the repo's own `instructions` in `.yuumi.yml`, the bot has built-in rules keyed on the file extensions in the diff, with no repo configuration needed:

- **Go**: race conditions, error wrapping (`%w`), context leaks, goroutine leaks.
- **JavaScript/TypeScript**: floating promises, overuse of `any`/`as`, missing null/undefined checks.
- **Python**: mutable default arguments, overly broad `except`, resources not managed with `with`.
- **SQL**: N+1 queries, missing indexes, SQL injection through string concatenation.
- **All files**: hardcoded secrets and credentials (API keys, passwords, tokens, private keys, connection strings with passwords). Before calling Claude, the bot also runs a quick regex scan over **added** lines in the diff (AWS keys `AKIA...`, PEM private key headers, GitHub/Slack tokens, `sk-...`, variables named like `password`/`passphrase`/`secret`/`api_key`/`access_token`/`auth_token` assigned a string literal of at least 6 characters with `=`, `:=`, or `:`; values that are just an env var name like `"DB_PASSWORD"` are skipped, as are reference keys: `secretName`/`secretKeyRef`, keys ending in `file`/`path` after `_`/`.`/`-` or a camelCase boundary (`DB_PASSWORD_FILE`, `secretPath`, but not `secretProfile`), and `*Header` keys whose value is also a header name (`"X-Api-Key"`); unquoted `DB_PASSWORD=...` forms (including `- KEY=...`, a trailing ` #` comment, and a `#` inside the value, which counts as part of it) in config/`.env` files; and `scheme://user:pass@host`). Matching `file:line` locations are added to the prompt for Claude to verify, with only the location and kind, never the secret value. This rule is **mandatory**: `instructions` in `.yuumi.yml` cannot turn it off (that file is read from the PR head, which the author controls). It can only add exceptions for specific file paths (e.g. a known test fixture); exceptions covering a whole directory or pattern are ignored.

When a bundle mixes file types, the rules for ALL types present are added (not just the majority type). File types without specific rules are still reviewed with the general instructions. If the repo has its own `instructions` in `.yuumi.yml`, they **take precedence** over these defaults when they conflict.

</details>

<details id="changed-symbol-hints-for-cross-package-breaking-changes">
<summary><strong>Changed-symbol hints for cross-package breaking changes</strong></summary>

Grouping files by directory (`groupByDirectory`) handles implementation and tests in the same directory, but can miss a change that affects files in **another package** (e.g. a method signature change whose callers live elsewhere; especially risky in Go because interfaces are satisfied implicitly).

To compensate without a full AST parser, the bot extracts, best-effort, the names of symbols (functions/types/methods) on changed lines, lists them in the prompt, and tells Claude to `grep`/search for those names elsewhere in the repo before concluding there is no breaking change, instead of relying only on the diff or the directory heuristic.

</details>

<details id="shared-context-across-bundles">
<summary><strong>Shared context across bundles</strong></summary>

When a large PR is split into several bundles (each a separate `claude -p` call), the bot builds shared context once before splitting:

- The list of **all** files changed in the PR (not only the current bundle's).
- Paths of the nearest README/convention docs found in the repo (at the root and in the directory of each changed file).

This context is embedded unchanged at the top of **every** bundle's prompt, instead of a generic "read related files yourself" that each bundle decides on again. That avoids each part of the same PR rediscovering context from scratch (more turns and tokens) or skipping it (reviews that lack context and disagree between parts). PRs that fit in one bundle (most of them) skip this step.

</details>

<details id="reading-the-repos-gitignore">
<summary><strong>Reading the repo's <code>.gitignore</code></strong></summary>

Besides `exclude` in `.yuumi.yml`, the bot reads the reviewed repo's actual `.gitignore` at its root and **adds** its patterns to the exclusion list (on top of the defaults and `.yuumi.yml`, not replacing them). If a repo already marks a directory or file as untracked (`coverage/`, `.turbo/`, `*.log`...), the bot does not review it by mistake.

Only the most common cases are supported, not the full `.gitignore` spec: comments, blank lines, and negated patterns (`!...`) are skipped; patterns with `/` (directories or nested paths) and fixed basenames/extensions work; simple wildcards like `*.ext` are translated; more complex wildcards (`file?.txt`, `[a-z]*`...) are skipped rather than translated wrongly. A missing or unreadable `.gitignore` never blocks a review.

</details>

<details id="incremental-review">
<summary><strong>Incremental review</strong></summary>

After each review, the bot records the reviewed SHA for that PR (`internal/reviewstate`, default `logs/review-state.json`, override with `REVIEW_STATE_FILE`). The next review of the **same PR** (e.g. the author pushes more commits) gets the diff through the GitHub compare API (`GET /compare/{old_sha}...{new_sha}`), which contains only the NEW changes, instead of the full diff against the base. On PRs with many review rounds this saves a lot of tokens and time.

- The first review of a PR (no state yet) works as before: full diff against the base.
- Errors reading the state or calling the compare API fall back to the full diff and never block a review.
- If the review fails (Claude CLI error, ...), that SHA is **not** recorded as reviewed, so the next review still starts from the last successfully reviewed SHA and nothing is skipped.
- The comment notes that only new changes were reviewed, so readers know the bot optimized rather than missed something.

</details>

<details id="resuming-an-interrupted-review">
<summary><strong>Resuming an interrupted review</strong></summary>

When a large PR is split into bundles, each finished bundle's result is saved in `logs/bundle-cache` (override with `BUNDLE_CACHE_DIR`). If the review is interrupted (a bundle errors or times out, the server restarts...), reviewing the **same SHA** again only calls Claude for the missing bundles and reuses the saved results for the rest.

- Results are only reused when the prompt is identical: a different diff, `.yuumi.yml`, or `@yuumi` command means a fresh review.
- When a review finishes without errors, that PR's cache is deleted. Cache entries older than 7 days are not reused.
- Cache read/write errors are only logged and never block a review.

</details>

<details id="categorized-findings-and-inline-comments">
<summary><strong>Categorized findings and inline comments</strong></summary>

Claude returns its result through the Claude CLI's structured output (`--json-schema`, see `review.FindingsSchema`): a list of findings (`category`, `severity`, `message`, `suggestion`, plus `file`/`line` when the finding applies to a specific line), instead of free text. The CLI turns the schema into a tool the model must call and validates each call against the schema (wrong type, missing field, or a `severity`/`category` outside the allowed list makes the model call again), so the bot never has to extract JSON from prose. The bot still decodes each finding separately: one malformed finding is dropped on its own.

- **Findings whose `file`/`line` match the actual diff** (checked by parsing the diff's hunks, not by trusting Claude) → posted as **inline comments** on that line through the GitHub Reviews API (`POST /pulls/{number}/reviews`), instead of a line of text lost in the summary.
- **Findings without `file`/`line`, or whose location does not match the diff** (Claude invented a file/line, or paraphrased instead of copying the line number) → still shown in the summary comment, grouped by severity (🔴 critical / 🟠 high / 🟡 medium / 🔵 low) with the category and suggested fix, if any. Nothing is silently dropped or posted in the wrong place.
- If the CLI cannot produce valid structured output within its allowed turns, that bundle fails (❌) and the SHA is not recorded, so the next push reviews it again. If the CLI returns prose instead of structured output, the prose is shown verbatim and the header says the review is inconclusive.
- Posting inline comments is **best-effort** and separate from the summary comment: errors at this step (rate limits, network...) are only logged and never lose the review already posted in the main comment.

</details>

<details id="automatic-review-on-new-prs-and-commits">
<summary><strong>Automatic review on new PRs and commits</strong></summary>

Besides manual mentions, the bot reviews automatically on `pull_request` webhook events with action `opened` (new PR) or `synchronize` (new commits pushed), with no one typing `@yuumi review`.

- **Webhook setup on GitHub**: besides the `Issue comments` event used by the mention flow, enable the **`Pull requests`** event in the App's webhook settings. The server tells the two events apart by the `X-GitHub-Event` header, not the `action` field in the body (both events have that field, with different meanings).
- **Allowlist**: `ALLOWED_USERS` (which already controls who may mention the bot) is **reused** for automatic reviews: only PRs whose author (`pull_request.user.login`) is on the list are reviewed automatically, so the bot does not review every PR anyone sends to an installed repo for free.
- **Shared review logic** with the mention flow: both build a `review.Job` the same way (only `RepoFullName`/`IssueNumber`/the input comment differ) and both check the last reviewed SHA (`review.AlreadyReviewedSHA`) to avoid duplicates. If a PR was reviewed automatically when opened and someone then mentions `@yuumi review` at that same SHA (or GitHub redelivers a webhook), the request is skipped instead of spending a Claude CLI call on nothing new.
- Other `pull_request` actions (`closed`, `reopened`, `edited`, `labeled`...) do nothing.

</details>

## Eval suite

`evalsuite/` contains PR fixtures with seeded bugs (`sql-injection-go`, `go-goroutine-leak`, `js-floating-promise`, `python-mutable-default`, `hardcoded-secret-go`, `cross-package-nil-go`) to answer "did changing the prompt/model make reviews better or worse?":

```bash
go run ./cmd/evalrun                          # all fixtures
go run ./cmd/evalrun sql-injection-go         # one fixture
go run ./cmd/evalrun -budget 12000 cross-package-nil-go   # different bundle budget
```

It needs an authenticated `claude` CLI. It is a manual tool, **not** part of `go test`/CI. Compare the output with each fixture's `expected.md` and add a row to `evalsuite/results.md`. Details and how to add a fixture: [evalsuite/README.md](./evalsuite/README.md).

## Roadmap

- [x] Review private repos: clone with the GitHub App's installation token (issue #48)
- [x] Docker packaging (issue #49)
- [x] Per-job sandbox with egress and credential proxies (issue #78)
- [ ] Deploy with a real public URL (instead of local testing through curl/ngrok) (issue #46)
- [ ] Deploy on AWS (issue #50)
- [ ] Review submodule content through the GitHub Compare API (issue #93, phase 2)

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for running locally, unit tests and CI, and testing real webhooks (curl + ngrok).

## License

[MIT](./LICENSE)
