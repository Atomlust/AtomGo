#!/usr/bin/env bash
#
# 安装 git pre-push 钩子:每次 git push 前自动跑 scripts/pre-push-check.sh
# (gofmt + go vet + 全量 go test,与 CI backend job 一致),失败即阻止推送。
#
# 为什么用 pre-push 而不是 pre-commit:
#   全量测试要跑几分钟,放在每次 commit 太打断节奏;而「漏跑导致 CI 红」恰恰发生在
#   推送之后,所以在 push 这道关口拦最合适。
#
# 用法:
#   bash scripts/install-hooks.sh      # 或 make hooks
# 新克隆的机器上需要再跑一次(.git/hooks 不入库)。
# 紧急跳过某次推送:git push --no-verify

set -euo pipefail

cd "$(dirname "$0")/.."

if [ ! -d .git ]; then
  echo "错误:当前目录不是 git 仓库根目录(未找到 .git)" >&2
  exit 1
fi

hook=".git/hooks/pre-push"

if [ -f "$hook" ] && ! grep -q "pre-push-check.sh" "$hook"; then
  echo "已存在其它 pre-push 钩子,未覆盖:$hook" >&2
  echo "如需本项目的自检,请手动把下行加入该钩子:" >&2
  echo "  exec bash scripts/pre-push-check.sh" >&2
  exit 1
fi

cat > "$hook" <<'EOF'
#!/bin/sh
# 推送前自检:与 CI 后端 job 同款(gofmt + go vet + 全量 go test)。
# 由 scripts/install-hooks.sh 安装;紧急跳过:git push --no-verify
exec bash scripts/pre-push-check.sh
EOF

chmod +x "$hook"
echo "已安装 pre-push 钩子:$hook"
echo "今后 git push 会自动跑后端自检;失败则阻止推送。"
