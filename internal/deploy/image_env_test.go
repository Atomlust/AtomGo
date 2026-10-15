package deploy

import (
	"context"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/atomlust/atomgo/internal/run"
)

// image_env_test.go 覆盖「环境作用域变量注入镜像部署」:
//   - splitRunArgs 引号感知切分(含空格的值不再被 strings.Fields 拆断);
//   - DeployForStage 带 env 时:目标机落 /tmp/atomgo/env/<容器名>.env(600),
//     docker run 追加 --env-file(位于镜像 ref 之前);
//   - 无 env 时:不写 env 文件、docker run 不含 --env-file(回归保护)。
// 全部经 stubTarget 捕获命令断言,不触真实 SSH / docker。

// TestSplitRunArgs 引号感知切分:空白分隔、双/单引号包裹含空白值、引号内转义、引号外转义。
func TestSplitRunArgs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"plain", "-e KEY=value --restart always", []string{"-e", "KEY=value", "--restart", "always"}},
		{"double-quoted spaced value", `-e ConnectionStrings__Sqlite="Data Source=/data/atomupdate.db"`,
			[]string{"-e", "ConnectionStrings__Sqlite=Data Source=/data/atomupdate.db"}},
		{"single-quoted spaced value", `-e Auth__Jwt__SigningKey='at least 32 chars'`,
			[]string{"-e", "Auth__Jwt__SigningKey=at least 32 chars"}},
		{"volumes and env mixed", `-v atomupdate-sqlite:/data -v atomupdate-uploads:/data/atomupdate/uploads -e X=1`,
			[]string{"-v", "atomupdate-sqlite:/data", "-v", "atomupdate-uploads:/data/atomupdate/uploads", "-e", "X=1"}},
		{"escaped quote inside dquotes", `-e X="a\"b"`, []string{"-e", `X=a"b`}},
		{"escaped space outside quotes", `-e X=a\ b`, []string{"-e", "X=a b"}},
		{"backslash literal inside squotes", `-e X='a\b'`, []string{"-e", `X=a\b`}},
		{"adjacent quoted segments", `--label a="x y"b`, []string{"--label", "a=x yb"}},
		{"empty quoted token", `-e X="" -e Y=v`, []string{"-e", "X=", "-e", "Y=v"}},
		{"unterminated quote lenient", `-e X="abc`, []string{"-e", "X=abc"}},
		{"extra whitespace collapsed", "  -e   X=1  ", []string{"-e", "X=1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitRunArgs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("splitRunArgs(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestStageImageInjectsEnvFile 镜像部署带 env:落 /tmp/atomgo/env/<容器名>.env(600)+
// docker run 追加 --env-file(在镜像 ref 之前);回滚用的 docker run 同样携带(参数在 imageState)。
func TestStageImageInjectsEnvFile(t *testing.T) {
	db := testDB(t)
	rsvc := run.New(db)
	tgt := &stubTarget{}
	srv := seedServer(t, tgt, "web-1")
	runID, _ := seedSuccessRunWithArtifact(t, db, rsvc, run.ArtifactImage, "registry/shop:1.0")

	svc := New(tgt, rsvc)
	ctx := WithEnvFile(context.Background(), map[string]string{
		"ASPNETCORE_ENVIRONMENT":    "Production",
		"ConnectionStrings__Sqlite": "Data Source=/data/atomupdate.db", // 含空格:env 文件双引号包裹
	})
	cfg := map[string]string{
		"containerName": "atomupdate-web",
		"ports":         "8001:8080",
		"runArgs":       `--restart unless-stopped -e Database__Provider=sqlite`,
	}
	res, err := svc.DeployForStage(ctx, runID, []string{srv.ID}, cfg, "")
	if err != nil {
		t.Fatalf("DeployForStage: %v", err)
	}
	if len(res) != 1 || res[0].Status != run.TargetSuccess {
		t.Fatalf("want 1 success, got %+v", res)
	}

	// 1) env 文件写入:base64 落 /tmp/atomgo/env/atomupdate-web.env 且 chmod 600。
	wantPath := "/tmp/atomgo/env/atomupdate-web.env"
	var envContent string
	found := false
	for _, cmd := range tgt.calls {
		if len(cmd) >= 5 && cmd[0] == "sh" && cmd[3] == wantPath {
			found = true
			if !strings.Contains(cmd[2], "chmod 600") {
				t.Fatalf("env 文件应置 600 权限: %v", cmd)
			}
			decoded, derr := base64.StdEncoding.DecodeString(cmd[4])
			if derr != nil {
				t.Fatalf("decode env payload: %v", derr)
			}
			envContent = string(decoded)
			break
		}
	}
	if !found {
		t.Fatalf("未写入 env 文件 %s: %v", wantPath, tgt.calls)
	}
	if !strings.Contains(envContent, "ASPNETCORE_ENVIRONMENT=Production\n") {
		t.Fatalf("env 文件缺 ASPNETCORE_ENVIRONMENT: %q", envContent)
	}
	if !strings.Contains(envContent, `ConnectionStrings__Sqlite="Data Source=/data/atomupdate.db"`+"\n") {
		t.Fatalf("含空格值未双引号包裹: %q", envContent)
	}

	// 2) docker run 追加 --env-file <path>,位于镜像 ref 之前。
	rc := runCmd(tgt.calls)
	if rc == nil {
		t.Fatalf("无 docker run: %v", tgt.calls)
	}
	envIdx, refIdx := -1, -1
	for i, a := range rc {
		if a == "--env-file" {
			envIdx = i
		}
		if a == "registry/shop:1.0" {
			refIdx = i
		}
	}
	if envIdx < 0 || envIdx+1 >= len(rc) || rc[envIdx+1] != wantPath {
		t.Fatalf("docker run 应带 --env-file %s: %v", wantPath, rc)
	}
	if refIdx < 0 || envIdx > refIdx {
		t.Fatalf("--env-file 应位于镜像 ref 之前: %v", rc)
	}
	// runArgs 的含空格值(引号包裹)也应完整成单个参数。
	if !hasCmd(tgt.calls, "docker", "run", "-d", "--name", "atomupdate-web") {
		t.Fatalf("容器名/端口参数不符: %v", rc)
	}
}

// TestStageImageNoEnvFileWithoutEnv 无 env:不写 /tmp env 文件、docker run 不含 --env-file。
func TestStageImageNoEnvFileWithoutEnv(t *testing.T) {
	db := testDB(t)
	rsvc := run.New(db)
	tgt := &stubTarget{}
	srv := seedServer(t, tgt, "web-1")
	runID, _ := seedSuccessRunWithArtifact(t, db, rsvc, run.ArtifactImage, "registry/shop:1.0")

	svc := New(tgt, rsvc)
	res, err := svc.DeployForStage(context.Background(), runID, []string{srv.ID},
		map[string]string{"containerName": "shop"}, "")
	if err != nil {
		t.Fatalf("DeployForStage: %v", err)
	}
	if res[0].Status != run.TargetSuccess {
		t.Fatalf("want success, got %+v", res)
	}
	for _, cmd := range tgt.calls {
		for _, a := range cmd {
			if strings.Contains(a, imageEnvDir) || a == "--env-file" {
				t.Fatalf("无 env 时不应出现 env 文件/参数: %v", cmd)
			}
		}
	}
}

// TestStageImageEnvFileWriteFailureAborts env 文件写入失败 → 预备阶段失败(尚未触碰旧容器),
// 不谎报成功;不应出现 docker run。
func TestStageImageEnvFileWriteFailureAborts(t *testing.T) {
	db := testDB(t)
	rsvc := run.New(db)
	rec := &touchRecorder{
		failOn: func(_ string, cmd []string) bool {
			return len(cmd) >= 5 && cmd[0] == "sh" && strings.Contains(cmd[3], imageEnvDir)
		},
	}
	tgt := &stubTarget{execFn: rec.exec}
	srv := seedServer(t, tgt, "web-1")
	runID, _ := seedSuccessRunWithArtifact(t, db, rsvc, run.ArtifactImage, "registry/shop:1.0")

	svc := New(tgt, rsvc)
	ctx := WithEnvFile(context.Background(), map[string]string{"K": "v"})
	res, err := svc.DeployForStage(ctx, runID, []string{srv.ID}, map[string]string{"containerName": "shop"}, "")
	if err != nil {
		t.Fatalf("DeployForStage: %v", err)
	}
	if len(res) != 1 || res[0].Status != run.TargetFailed {
		t.Fatalf("env 写入失败应 failed, got %+v", res)
	}
	if runCmd(tgt.calls) != nil {
		t.Fatalf("env 写入失败不应起容器: %v", tgt.calls)
	}
}
