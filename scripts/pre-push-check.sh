#!/usr/bin/env bash
#
# 推送前的本地自检:跑与 CI(ci.yml 的 backend job)**完全相同**的三项后端检查。
#
# CI 的 backend job 跑的是:
#   gofmt -l cmd internal embed.go        # 有输出即失败
#   go vet   . ./cmd/... ./internal/...
#   go test  . ./cmd/... ./internal/...   # 全量,不是部分包
#
# 为什么需要它(2026-10-17 事故):
#   本地只跑了 trigger/pipeline/promotion/httpapi 就提交推送,漏掉了 internal/deploy
#   里一个早已失败的用例,导致 CI 从那次提交起每次 push 都红,而人只在发版时才看 CI,
#   于是误以为「每次更新版本 CI 才失败」。教训:**检查范围必须和 CI 一致,且要自动跑**。
#
# 用法:
#   bash scripts/pre-push-check.sh      # 或 make check
#   make hooks                          # 装成 git pre-push 钩子,推送时自动跑
# 紧急情况跳过:git push --no-verify(不推荐;跳过的问题仍会在 CI 上暴露)

set -euo pipefail

cd "$(dirname "$0")/.."

GO_FMT_DIRS="cmd internal embed.go"
GO_PKGS=". ./cmd/... ./internal/..."

echo "==> [1/3] gofmt check ($GO_FMT_DIRS)"
out="$(gofmt -l $GO_FMT_DIRS)"
if [ -n "$out" ]; then
  echo "需要 gofmt 的文件:"
  echo "$out"
  echo "修复方式:make fmt"
  exit 1
fi
echo "    ok"

echo "==> [2/3] go vet ($GO_PKGS)"
go vet $GO_PKGS
echo "    ok"

echo "==> [3/3] go test 全量($GO_PKGS) — 首次较慢,后续走缓存"
# 已知 flaky:internal/ai 的 TestOpsProxyServiceCompatibility 偶发失败
# (单独跑通常通过,重复执行/并发跑时更容易挂)。为避免这种抖动挡住正常推送,
# 首次失败后重跑一次:两次都失败才算真实失败,不会掩盖真正的问题。
# TODO:修掉该 flaky 后,可去掉下面这段重试。
if ! go test $GO_PKGS; then
  echo "    !! 首次有失败,重跑一次以排除 flaky(已知 internal/ai 偶发不稳定)"
  sleep 2
  go test -count=1 $GO_PKGS
fi
echo "    ok"

echo ""
echo "后端检查全部通过。"
echo "注:改过 web/(前端)的话,CI 还会跑 npm run build —— 本地请自行确认 npm run build 通过。"
