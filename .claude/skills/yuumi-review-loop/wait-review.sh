#!/usr/bin/env bash
# Wait for the yuumi bot to finish reviewing a PR's current head commit, then
# print the result: the "yuumi review" check run, the bot's inline findings on
# that exact commit, and the bot's latest summary comment.
#
# Usage: wait-review.sh <PR number> [timeout seconds, default 1200]
# Exit: 0 when the review is done, 1 on timeout (bot down or stuck).
set -euo pipefail

pr="${1:?usage: wait-review.sh <PR number> [timeout seconds]}"
timeout="${2:-1200}"
bot="yuumi-review[bot]"

repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
head="$(gh pr view "$pr" --json headRefOid --jq .headRefOid)"
echo "PR #$pr ($repo), head ${head:0:7}: waiting for check run \"yuumi review\"..."

# A commit can have several "yuumi review" check runs (e.g. a failed review,
# then a new mention on the same SHA): always take the latest (highest id).
checks_url="repos/$repo/commits/$head/check-runs?check_name=yuumi%20review"
latest_run='.check_runs | max_by(.id)'

deadline=$(( $(date +%s) + timeout ))
status=""
while :; do
  # `|| true`: one failed API call (flaky network) must not kill the wait loop.
  status="$(gh api "$checks_url" --jq "$latest_run | .status // \"\"" 2>/dev/null || true)"
  [ "$status" = "completed" ] && break
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "TIMED OUT after ${timeout}s, check run status='${status:-not created}'."
    echo "Check the bot: docker ps --filter name=yuumi; docker logs --tail 50 yuumi; curl -s localhost:8080/health"
    exit 1
  fi
  sleep 20
done

echo
echo "=== Check run"
gh api "$checks_url" --jq "$latest_run | \"\\(.conclusion): \\(.output.title)\""

echo
echo "=== Bot inline findings on ${head:0:7}"
# original_commit_id, not commit_id: GitHub moves an old comment's commit_id
# forward to the newest commit if its line is still in the diff, so filtering
# on commit_id would mix in findings from earlier reviews.
gh api --paginate "repos/$repo/pulls/$pr/comments" \
  --jq ".[] | select(.user.login==\"$bot\" and .original_commit_id==\"$head\" and .in_reply_to_id==null)
        | \"--- id=\(.id) \(.path):\(.line // .original_line)\n\(.body)\n\""

echo "=== Bot's latest summary comment"
# --paginate applies --jq to each page separately (and --slurp cannot be
# combined with --jq), so `last` in jq does not work: collect the ids of all
# matching comments across pages, take the last one, then read its body.
summary_id="$(gh api --paginate "repos/$repo/issues/$pr/comments" \
  --jq ".[] | select(.user.login==\"$bot\") | .id" | tail -1)"
if [ -n "$summary_id" ]; then
  gh api "repos/$repo/issues/comments/$summary_id" --jq .body
else
  echo "(none)"
fi
