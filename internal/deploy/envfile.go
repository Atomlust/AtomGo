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
// (或 build 层部署节点经 WithEnvFile)经 context 透传至逐机执行点(deployOne / stageImageOne),
// 避免改动策略编排层十余个函数签名;每请求独立 ctx,并发部署互不干扰。明文绝不经此落日志。
type ctxKeyEnvFile struct{}

// WithEnvFile 把待注入的环境变量挂到 ctx(供 dag 部署节点等在 DeployInput 之外注入)。
// 空 map → 原样返回 ctx(零开销)。明文值绝不进日志/错误。
func WithEnvFile(ctx context.Context, env map[string]string) context.Context {
	return withEnvFile(ctx, env)
}

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
	return s.writeEnvFileAt(ctx, serverID, path.Join(dir, ".env"), env)
}

// writeEnvFileAt 在目标机 <file> 写入 .env 内容(base64 经 array 命令落地,绝不拼接 shell)。
func (s *service) writeEnvFileAt(ctx context.Context, serverID, file string, env map[string]string) (string, bool) {
	content := buildEnvFileContent(env)
	if content == "" {
		return "", true
	}
	payload := base64.StdEncoding.EncodeToString([]byte(content))
	cmds := [][]string{
		{"mkdir", "-p", path.Dir(file)},
		{"sh", "-c", `printf '%s' "$1" | base64 -d > "$0"`, file, payload},
	}
	return s.runStep(ctx, serverID, cmds)
}

// imageEnvDir 是镜像部署 env 文件在目标机的固定目录。按容器名分文件(<容器名>.env),
// 多应用 / 多项目部署到同一台机互不覆盖;容器经 docker run --env-file 读取(docker CLI
// 在目标机本机读取该文件,不经网络)。文件每次部署重写(600 权限),系统重启清理后可随时
// 由下次部署重建——故放 /tmp 而非需要 root 的 /etc。
const imageEnvDir = "/tmp/atomgo/env"

// imageEnvFilePath 返回某容器名对应的 env 文件路径(容器名已 sanitize,可安全入路径)。
func imageEnvFilePath(name string) string {
	return imageEnvDir + "/" + name + ".env"
}

// writeImageEnvFile 把环境变量写到目标机 /tmp/atomgo/env/<容器名>.env(600,仅属主可读),
// 供该容器的 docker run --env-file 读取。与文件型产物 writeEnvFile 同机制:
// base64 经 array 命令落地,绝不拼接 shell(AC-SEC-02);明文绝不经命令回显/日志外泄。
func (s *service) writeImageEnvFile(ctx context.Context, serverID, name string, env map[string]string) (string, bool) {
	content := buildEnvFileContent(env)
	if content == "" {
		return "", true
	}
	payload := base64.StdEncoding.EncodeToString([]byte(content))
	file := imageEnvFilePath(name)
	cmds := [][]string{
		{"mkdir", "-p", imageEnvDir},
		{"sh", "-c", `printf '%s' "$1" | base64 -d > "$0" && chmod 600 "$0"`, file, payload},
	}
	return s.runStep(ctx, serverID, cmds)
}
