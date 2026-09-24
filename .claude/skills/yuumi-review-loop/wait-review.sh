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

# 1 commit có thể có nhiều check run "yuumi review" (vd review lỗi rồi
# mention review lại cùng SHA): luôn lấy lần mới nhất (id lớn nhất).
checks_url="repos/$repo/commits/$head/check-runs?check_name=yuumi%20review"
latest_run='.check_runs | max_by(.id)'

deadline=$(( $(date +%s) + timeout ))
status=""
while :; do
  # `|| true`: 1 lần gọi API lỗi (mạng chập chờn) không được làm vòng chờ chết.
  status="$(gh api "$checks_url" --jq "$latest_run | .status // \"\"" 2>/dev/null || true)"
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
gh api "$checks_url" --jq "$latest_run | \"\\(.conclusion): \\(.output.title)\""

echo
echo "=== Góp ý inline của bot trên ${head:0:7}"
# original_commit_id, không phải commit_id: GitHub dời commit_id của comment
# cũ sang commit mới nhất nếu dòng đó còn trong diff, nên lọc theo commit_id
# sẽ lẫn cả góp ý của các lần review trước.
gh api --paginate "repos/$repo/pulls/$pr/comments" \
  --jq ".[] | select(.user.login==\"$bot\" and .original_commit_id==\"$head\" and .in_reply_to_id==null)
        | \"--- id=\(.id) \(.path):\(.line // .original_line)\n\(.body)\n\""

echo "=== Comment tổng hợp mới nhất của bot"
# --paginate áp --jq riêng cho từng trang (và --slurp không đi chung với
# --jq), nên không dùng được `last` trong jq: lấy id của mọi comment khớp
# qua tất cả các trang, chọn id cuối, rồi mới đọc body.
summary_id="$(gh api --paginate "repos/$repo/issues/$pr/comments" \
  --jq ".[] | select(.user.login==\"$bot\") | .id" | tail -1)"
if [ -n "$summary_id" ]; then
  gh api "repos/$repo/issues/comments/$summary_id" --jq .body
else
  echo "(không có)"
fi
