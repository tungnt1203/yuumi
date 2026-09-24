---
name: yuumi-review-loop
description: Wait for the yuumi bot to finish reviewing a PR in this repo, then triage and fix its findings, push, and reply on each comment. Use after pushing to a PR branch, or when the user says "xem và fix comment", "bot review xong chưa", "fix góp ý review PR #N".
---

# yuumi review loop

The yuumi bot (running locally as the Docker container `yuumi`) reviews every push to a PR in this repo. When it finishes, it completes a check run named `yuumi review` on the PR head commit. Waiting on that check run is the reliable "review done" signal. Do not grep the container logs: old log lines give false matches.

## 1. Before pushing: make sure the bot runs the current code

Rebuild the image and recreate the container (see CLAUDE.md, "Running the bot locally"). Then wait for `curl -s localhost:8080/health` to return 200. If the push changes bot code but the container is stale, the review of that push runs old code.

## 2. Wait for the review

Run the helper script **in the background** so the conversation stays responsive. You are notified when it exits:

```bash
.claude/skills/yuumi-review-loop/wait-review.sh <PR number> [timeout seconds, default 1200]
```

The script polls the check run every 20s until it reaches `completed`, then prints:
- the check run conclusion and title;
- the bot's inline findings on **this** head commit, filtered by `original_commit_id`. `commit_id` is not reliable: GitHub moves old comments forward to newer commits;
- the bot's latest summary comment.

Exit code 1 means the timeout was reached: the bot is down or stuck. Check `docker ps --filter name=yuumi`, `docker logs --tail 50 yuumi`, and `/health` before retrying. A 503 from `/health` right after start is often a transient network error, and the monitor re-checks every 5 minutes.

## 3. Triage each finding. Do not apply suggestions blindly

For every finding, read the real code it points at and decide:
- **Valid**: fix it. If the bot's ```suggestion``` block is wrong or too narrow, fix it your own way. Example: `neturl.PathEscape` on a whole path breaks `/`, so escape each segment instead.
- **Not valid**: do not change the code. Explain why in the reply, with evidence (language semantics, a test that proves it).
- **Out of scope**: say so in the reply and leave it for a separate issue.

Add or adjust a test for each fix when one can reproduce the problem. Check that the test fails without the fix.

## 4. Verify, push, reply

```bash
go vet ./... && env -u GITHUB_APP_PRIVATE_KEY_PATH -u GITHUB_APP_PRIVATE_KEY go test ./...
```

1. Rebuild and recreate the container (step 1), unless only comments or docs changed.
2. Commit as `fix: xử lý góp ý review PR #<n>`, with one bullet per finding addressed, then push.
3. Reply in each finding's thread, naming the commit and what changed (or why nothing did):

```bash
gh api -X POST repos/<owner>/<repo>/pulls/<n>/comments/<comment id>/replies -f body="Đã sửa ở <short sha>: ..."
```

For findings that appear only in the summary comment (not inline), reply with `gh pr comment <n> --body "..."`.

The push triggers a new incremental review. Go back to step 2 until the check run reports no new findings, then tell the user the PR is ready to merge.
