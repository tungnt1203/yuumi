# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

yuumi is a Go GitHub App that reviews Pull Requests: it receives webhooks, clones the PR head, runs the `claude` CLI on it, and posts a summary comment, inline findings, and a `yuumi review` check run. Code comments, commit messages, PR bodies, and README are written in Vietnamese. Keep that convention.

## Commands

```bash
go build ./... && go vet ./... && go test ./...   # exactly what CI runs (.github/workflows/ci.yml)
go test ./internal/review/ -run TestJobRun_CheckRun -v   # a single package / test
go test -race ./internal/healthcheck/             # use -race when touching goroutines
docker build -t yuumi:dev .                        # CI also builds the image (build only, no push)
go run ./cmd/evalrun [fixture]                     # eval suite: needs an authenticated `claude`, not part of CI
go run ./cmd/reviewstats                           # token/cost summary from review logs
```

- Unit tests need no token, network, or `claude` binary: external commands are faked with shell scripts put first on `PATH` (see `withFakeClaude`, `withFakeGit`).
- `internal/config` tests fail if the shell exports `GITHUB_APP_PRIVATE_KEY_PATH` / `GITHUB_APP_PRIVATE_KEY` (e.g. after `source .env`). Run them with `env -u GITHUB_APP_PRIVATE_KEY_PATH -u GITHUB_APP_PRIVATE_KEY go test ./...`.

## Running the bot locally

The bot runs as the Docker container `yuumi` (image `yuumi:dev`). It reviews this repo's own PRs, so **rebuild and recreate the container before pushing**; otherwise the review of the push runs old code. `docker restart` alone is not enough, because the code is baked into the image.

```bash
docker build -t yuumi:dev . && docker rm -f yuumi && docker run -d --name yuumi -p 8080:8080 \
  --env-file .env \
  -e GITHUB_APP_PRIVATE_KEY_PATH=/run/secrets/app.pem \
  -v "$PWD/logs:/app/logs" \
  -e SANDBOX=docker -e SANDBOX_IMAGE=yuumi:dev -e WORK_DIR="$PWD/work" \
  -v "$PWD/work:$PWD/work" \
  -v /var/run/docker.sock:/var/run/docker.sock --group-add 0 \
  -v <path to App private key .pem>:/run/secrets/app.pem:ro \
  yuumi:dev
curl -s localhost:8080/health   # 200 when both claude_cli and github_app are ok
```

The first `/health` check takes ~20s after start. The health monitor re-checks every 5 minutes, so a transient startup failure (503) recovers by itself. Without Docker, run `set -a && source .env && set +a && go run ./cmd/server`. To send a fake webhook with curl, or test with real GitHub via ngrok, see CONTRIBUTING.md.

## Architecture

Request flow (`cmd/server/main.go`): `POST /webhook` verifies the HMAC signature (`internal/webhook`), then routes on the `X-GitHub-Event` header:
- `issue_comment` (a `@yuumi ...` mention): checks `ALLOWED_USERS`, dedupes on the comment ID.
- `pull_request` opened/synchronize (auto-review): checks that the PR author is in `ALLOWED_USERS`.

Both paths skip a head SHA that was already reviewed (`review.AlreadyReviewedSHA`). They then get an installation token (`internal/githubapp.Provider`: signs an App JWT and caches a token per installation), post a "Đang review..." placeholder, and submit a `review.Job` to the `Dispatcher`, which caps concurrent jobs.

`review.Job.Run` (`internal/review/job.go`) is the core. It depends only on small interfaces (`GitHubClient`, `Cloner`, `Reviewer`, `ReviewLogger`, `ReviewStateStore`, `BundleCache`), so tests drive it with fakes. It uses only primitive parameter types, so `review` never imports `githubapi`. Steps:
1. Get head + base SHA, create the check run (`checkrun.go`), clone at head (`internal/gitrepo`), start the job's sandbox (`internal/sandbox`: `Local`, or one Docker container per job when `SANDBOX=docker`). Every command on PR code (`gofmt`/`go vet`, `claude`) runs through `sandbox.Env.Command`. Reading config files from the clone stays on the server.
2. Static checks (`gofmt`/`go vet`), `.yuumi.yml` (`exclude`, `instructions`) and `.gitignore` from the clone.
3. Get the diff: the full PR diff, or only the changes since the last reviewed SHA (`reviewstate`, "incremental"). Filter junk files, split into bundles by directory under `MAX_DIFF_BUNDLE_CHARS`.
4. One `claude -p` call per bundle (`internal/claudecli`, retries), findings come back as CLI structured output (`--json-schema review.FindingsSchema`, parsed per finding in `finding.go`), cache per-bundle results for resume.
5. Edit the placeholder with header + notes + body, post inline comments through the Reviews API (validated against the full base...head diff; one bad line rejects the whole review), save the reviewed SHA only if no bundle errored.
6. Complete the check run. It starts as `neutral` in a `defer`, so every error or panic path closes it. It becomes `success`, or `failure` when a finding matches `block_severity`.

`cmd/server` builds each `review.Job` through `newJob`. The GitHub client and the clone closure get the installation token from `githubapp.Provider` on every use (`installationToken`), never a token captured at webhook time: tokens last 1 hour and a queued job can outlive that (#99). `Job` never sees the token.

## Security invariants (PR code is untrusted input)

Keep these when changing anything that touches the clone directory or subprocesses (README "Chạy an toàn trên code PR không tin cậy", issue #78):
- `claude` runs with `--tools Read,Grep,Glob` (an allowlist, never go back to `--disallowedTools`: with a blocklist CLI 2.1.281 still exposes Agent, Workflow, Skill, ToolSearch...), `--restricted`, `--setting-sources user`, `--strict-mcp-config`, `--no-session-persistence`, and `--max-budget-usd`. It must not gain write, shell, network, or agent tools.
- Subprocesses that run on PR code (`claude`, `gofmt`/`go vet`) get their env from `internal/procenv`, which strips server secrets.
- The Docker sandbox runs only on the `--internal` network `yuumi-sandbox`. Its only way out is container `yuumi-egress` (`internal/egress`): a `CONNECT :443` proxy limited to `egress.DefaultAllowedHosts`, and a credential proxy to the Anthropic API (`ANTHROPIC_BASE_URL`, paths under `/v1/` only). The real Claude credential never enters the sandbox: the sandbox gets a same-kind placeholder, and the credential proxy swaps in the real one. Never add `CLAUDE_CODE_OAUTH_TOKEN`/`ANTHROPIC_API_KEY` to `forwardEnvKeys`. A new outbound dependency of a review step must be added to that allowlist, or it fails with a `DENY` line in `docker logs yuumi-egress`. Do not forward the server's `HTTP(S)_PROXY` into the sandbox (it would bypass the egress proxy).
- Never exec directly on the clone dir: go through `sandbox.Env.Command`. The Docker sandbox forwards only `forwardEnvKeys` (an allowlist), passed as `-e KEY` so values stay out of args. If the sandbox cannot start, the review fails; it must not fall back to running on the server.
- `gofmt`/`go vet` run with a timeout, `GOTOOLCHAIN=local`, `CGO_ENABLED=0`, and a restricted `GOPROXY`.
- The installation token is passed to `git fetch` only, through `GIT_CONFIG_*` env (`http.extraheader`). Never put it in the remote URL (it would land in `.git/config`, which Claude reads) or in command args.
- Installation tokens are requested with a minimal permission set (`tokenPermissions` in `internal/githubapp/installation.go`). A new GitHub endpoint may need a new permission there, or the API returns 403.
- `.yuumi.yml` at the PR head is author-controlled. Anything that must not be bypassable by the PR (like `block_severity`) is read from the **base** commit through the GitHub API.
- User-visible failure comments are generic (`reviewSetupFailureComment`). Error details go only to the server log.

## Conventions

- Branches: `feat/<topic>-<issue>` or `fix/...`. Commits and PR titles: `feat: <mô tả> (#<issue>)`. A follow-up commit for review feedback is titled `fix: xử lý góp ý review PR #<n>`.
- The bot reviews every push to a PR. Use the `yuumi-review-loop` skill (`.claude/skills/`) to wait for its review, fix the findings, and reply.
