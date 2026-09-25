# Contributing

This guide is for running and changing the code locally, testing real
webhooks, and opening pull requests.

## Conventions

- **Language**: code, comments, docs, commit messages, and PR descriptions are in English. Some older code comments are still in Vietnamese and are being translated; write new comments in English. The bot's output on PRs (comments, findings, check run text) is intentionally in Vietnamese.
- **Branches**: `feat/<topic>-<issue>`, `fix/<topic>-<issue>`, `docs/...`, `chore/...`.
- **Commits and PR titles**: `feat: <summary> (#<issue>)`, `fix: ...`, `docs: ...`, `chore: ...`. A follow-up commit that addresses review feedback is titled `fix: address review feedback on PR #<n>`.
- The yuumi bot reviews every PR in this repo (when the author is in its `ALLOWED_USERS`). Address or answer each finding before merging.

## Running locally

```bash
set -a && source .env && set +a
go run ./cmd/server
```

The server listens on `:8080` with two routes:

- `GET /health`: the real status of dependencies (checked at startup and every 5 minutes, then cached, so no CLI/API call per request). `200` with JSON like `{"claude_cli":{"ok":true,...},"github_app":{"ok":true,...},"checked_at":"..."}` when everything works, `503` when a dependency fails.
- `POST /webhook`: receives GitHub webhooks (`issue_comment` for mentions, `pull_request` for automatic reviews, told apart by the `X-GitHub-Event` header; see the [README](./README.md#automatic-review-on-new-prs-and-commits)).

**Note:** `issue.number` in the payload must be the number of a real **pull request** (not a plain issue), because getting the head SHA calls `/pulls/{number}`, which returns 404 for plain issues.

To run the bot in Docker with the per-job sandbox, see [Running with Docker](./README.md#running-with-docker) in the README.

## Unit tests and CI

```bash
go build ./... && go vet ./... && go test ./...
```

CI (`.github/workflows/ci.yml`) runs these on every PR and every push to `main`, builds the Docker image, and runs `go vet`/`go test` again inside that image (so a Go version bump in the Dockerfile is actually tested). Unit tests need no token, network, or `claude` binary: external commands are faked with shell scripts put first on `PATH`.

`internal/config` tests fail if your shell exports `GITHUB_APP_PRIVATE_KEY_PATH` or `GITHUB_APP_PRIVATE_KEY` (e.g. after `source .env`). Run them with:

```bash
env -u GITHUB_APP_PRIVATE_KEY_PATH -u GITHUB_APP_PRIVATE_KEY go test ./...
```

## Manual testing (simulated GitHub webhooks)

**Note (issue #47):** `<installation id>` must be the **real** ID of an App installation on a repo. The server calls GitHub to exchange it for an installation token before doing anything else, so this cannot run fully offline. Find the ID in the App settings → **Advanced** → any delivery → the `installation.id` field, or in the installation's settings URL (`.../installations/<id>`).

```bash
BODY='{"action":"created","comment":{"id":1,"body":"@yuumi review","user":{"login":"<username>"}},"repository":{"full_name":"<owner>/<repo>"},"issue":{"number":<PR number>},"installation":{"id":<real installation id>}}'
SIG=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" | sed 's/^.* //')
curl -i -X POST localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: issue_comment" \
  -H "X-Hub-Signature-256: sha256=$SIG" \
  -d "$BODY"
```

(`comment.id` is fake, so the 👀 reaction always fails with 404. That is expected and does not block the rest.)

Simulating an automatic review (`pull_request` event; `<username>` must be in `ALLOWED_USERS`):

```bash
BODY='{"action":"opened","repository":{"full_name":"<owner>/<repo>"},"pull_request":{"number":<PR number>,"head":{"sha":"<head sha>"},"user":{"login":"<username>"}},"installation":{"id":<real installation id>}}'
SIG=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" | sed 's/^.* //')
curl -i -X POST localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: pull_request" \
  -H "X-Hub-Signature-256: sha256=$SIG" \
  -d "$BODY"
```

## Testing against real GitHub with ngrok

To try real GitHub webhooks before deploying, expose your local server through [ngrok](https://ngrok.com):

1. Start the server: `set -a && source .env && set +a && go run ./cmd/server`, and check that `curl -i localhost:8080/health` returns `200`.
2. In another terminal, open a tunnel: `ngrok http 8080` (or `ngrok http --url=<fixed-domain> 8080` if you have a fixed ngrok domain, so the webhook URL does not change between runs). Each request GitHub sends is visible at `http://127.0.0.1:4040`.
3. A GitHub App has **one** webhook configuration, on the App itself (not per repo). In the App settings (**Settings → Developer settings → GitHub Apps → \<App name\>**):
   - **Webhook URL**: `https://<ngrok-domain>/webhook` (no spaces, must end with `/webhook`). Update it whenever the ngrok domain changes.
   - **Webhook secret**: exactly the `GITHUB_WEBHOOK_SECRET` from `.env` (a wrong secret makes the server return `401`).
   - **Permissions & events**: `Issues: Read and write`, `Pull requests: Read and write`, `Contents: Read-only`, `Checks: Read and write`; subscribe to **Issue comment** and **Pull request**.
   - If the App is not installed on the target repo yet: **Install App** (left menu) → choose the repo.
4. In the App's **Advanced** tab, check **Recent Deliveries**: the first `ping` event should have a green tick. The server logs `Ignored: unsupported X-GitHub-Event ping`, which is expected; it only handles `issue_comment` and `pull_request`.
5. Comment `@yuumi review` on a real **pull request** from an account in `ALLOWED_USERS`, or open a PR / push a commit to try automatic review (the PR author must be in `ALLOWED_USERS`). The bot reacts 👀, posts "Đang review...", then edits that comment into the review result, as **`<App name>[bot]`** rather than a personal account.

## Eval suite

See [evalsuite/README.md](./evalsuite/README.md) for running the eval suite and adding fixtures that measure review quality. Run it when you change the prompt, the Claude CLI flags, or the Claude CLI version.

## Adding fixtures or rules

- Default per-file-type rules live in `internal/review`. Check the [existing rules in the README](./README.md#default-rules-per-file-type) before adding one, to avoid duplicates.
- Always run `go build ./... && go vet ./... && go test ./...` before opening a PR.
- Changes that touch the checkout directory or subprocesses must keep the security invariants listed in [CLAUDE.md](./CLAUDE.md#security-invariants-pr-code-is-untrusted-input) and the README section [Running safely on untrusted PR code](./README.md#running-safely-on-untrusted-pr-code).
