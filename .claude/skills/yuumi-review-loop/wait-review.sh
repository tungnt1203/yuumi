#!/usr/bin/env bash
# Chờ bot yuumi review xong commit head hiện tại của 1 PR, rồi in kết quả:
# check run "yuumi review", góp ý inline của bot trên đúng commit đó, và
# comment tổng hợp mới nhất của bot.
#
# Dùng: wait-review.sh <số PR> [timeout giây, mặc định 1200]
# Exit: 0 khi review xong, 1 khi hết thời gian chờ (bot không chạy/treo).
set -euo pipefail

pr="${1:?usage: wait-review.sh <PR number> [timeout seconds]}"
timeout="${2:-1200}"
bot="yuumi-review[bot]"

repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
head="$(gh pr view "$pr" --json headRefOid --jq .headRefOid)"
echo "PR #$pr ($repo), head ${head:0:7}: chờ check run \"yuumi review\"..."

deadline=$(( $(date +%s) + timeout ))
status=""
while :; do
  # `|| true`: 1 lần gọi API lỗi (mạng chập chờn) không được làm vòng chờ chết.
  status="$(gh api "repos/$repo/commits/$head/check-runs" \
    --jq '.check_runs[] | select(.name=="yuumi review") | .status' 2>/dev/null | head -1 || true)"
  [ "$status" = "completed" ] && break
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "HẾT THỜI GIAN CHỜ sau ${timeout}s, check run status='${status:-chưa tạo}'."
    echo "Kiểm tra bot: docker ps --filter name=yuumi; docker logs --tail 50 yuumi; curl -s localhost:8080/health"
    exit 1
  fi
  sleep 20
done

echo
echo "=== Check run"
gh api "repos/$repo/commits/$head/check-runs" \
  --jq '.check_runs[] | select(.name=="yuumi review") | "\(.conclusion): \(.output.title)"'

echo
echo "=== Góp ý inline của bot trên ${head:0:7}"
# original_commit_id, không phải commit_id: GitHub dời commit_id của comment
# cũ sang commit mới nhất nếu dòng đó còn trong diff, nên lọc theo commit_id
# sẽ lẫn cả góp ý của các lần review trước.
gh api --paginate "repos/$repo/pulls/$pr/comments" \
  --jq ".[] | select(.user.login==\"$bot\" and .original_commit_id==\"$head\" and .in_reply_to_id==null)
        | \"--- id=\(.id) \(.path):\(.line // .original_line)\n\(.body)\n\""

echo "=== Comment tổng hợp mới nhất của bot"
gh api --paginate "repos/$repo/issues/$pr/comments" \
  --jq "[.[] | select(.user.login==\"$bot\")] | last | .body // \"(không có)\""
