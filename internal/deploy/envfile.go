package deploy

import (
	"context"
	"encoding/base64"
	"path"
	"sort"
	"strings"

	"github.com/atomlust/atomgo/internal/run"
)

// ctxKeyEnvFile 携带本次部署要注入目标机的环境变量(已解密明文 K=V)。仅由 DeployInput.EnvFile
// 经 context 透传至逐机执行点(deployOne),避免改动策略编排层十余个函数签名;每请求独立 ctx,
// 并发部署互不干扰。明文绝不经此落日志。
type ctxKeyEnvFile struct{}

func withEnvFile(ctx context.Context, env map[string]string) context.Context {
	if len(env) == 0 {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyEnvFile{}, env)
}

func envFileFromCtx(ctx context.Context) map[string]string {
	if env, ok := ctx.Value(ctxKeyEnvFile{}).(map[string]string); ok {
		return env
	}
	return nil
}

// buildEnvFileContent 把环境变量渲染为 .env 文本(KEY=VALUE 逐行,按 key 升序稳定)。
// 值含需转义字符(空白/引号/反斜杠/$、`#`、分号等)或为空 → 双引号包裹并转义内部 " 与 \,
// 兼容主流 dotenv 解析器且杜绝意外注释 / 分词。明文绝不在此写入日志。
func buildEnvFileContent(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := env[k]
		b.WriteString(k)
		b.WriteByte('=')
		if needsEnvQuote(v) {
			b.WriteByte('"')
			b.WriteString(strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`))
			b.WriteByte('"')
		} else {
			b.WriteString(v)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// needsEnvQuote 报告值是否需要双引号包裹(空值或含 dotenv 敏感字符)。
func needsEnvQuote(v string) bool {
	if v == "" {
		return true
	}
	return strings.ContainsAny(v, " \t\"\\$#`;")
}

// deployEnvDir 返回文件型产物的「部署根目录」(release 模式用 releaseBase,其余用 targetDir),
// .env 写入此目录,部署的应用版本可就近读取(../.env 或同目录)。
func deployEnvDir(a run.Artifact, cfg map[string]string) string {
	if releaseModeArtifact(a) {
		return releaseBase(a, cfg)
	}
	return targetDir(a, cfg)
}

// writeEnvFile 在目标机 <dir> 写入 .env(已解析的明文变量)。成功 → ok=true;失败 → (失败文案, false)。
// 写入用 base64 经 array 命令落地(同 buildFileDeploy),绝不拼接 shell(AC-SEC-02)。
func (s *service) writeEnvFile(ctx context.Context, serverID, dir string, env map[string]string) (string, bool) {
	content := buildEnvFileContent(env)
	if content == "" {
		return "", true
	}
	payload := base64.StdEncoding.EncodeToString([]byte(content))
	cmds := [][]string{
		{"mkdir", "-p", dir},
		{"sh", "-c", `printf '%s' "$1" | base64 -d > "$0"`, path.Join(dir, ".env"), payload},
	}
	return s.runStep(ctx, serverID, cmds)
}
