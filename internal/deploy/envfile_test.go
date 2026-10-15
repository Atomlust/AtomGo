package deploy

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/atomlust/atomgo/internal/run"
	"github.com/atomlust/atomgo/internal/target"
)

// TestBuildEnvFileContent 验证 .env 渲染:按 key 升序、空值与特殊字符双引号转义。
func TestBuildEnvFileContent(t *testing.T) {
	got := buildEnvFileContent(map[string]string{
		"EMPTY":       "",
		"PLAIN":       "hello",
		"SPACED":      "a b c",
		"HASH":        "x#y",
		"QUOTED":      `he"llo`,
		"BACKSL":      `a\b`,
		"WITH_DOLLAR": "$VAR",
	})
	want := "BACKSL=\"a\\\\b\"\n" +
		"EMPTY=\"\"\n" +
		"HASH=\"x#y\"\n" +
		"PLAIN=hello\n" +
		"QUOTED=\"he\\\"llo\"\n" +
		"SPACED=\"a b c\"\n" +
		"WITH_DOLLAR=\"$VAR\"\n"
	if got != want {
		t.Fatalf("buildEnvFileContent =\n%q\nwant\n%q", got, want)
	}
}

// TestDeployWritesEnvFile 验证文件型部署在注入 EnvFile 时,会把 .env 写入目标机部署根目录;
// 且不注入时(EnvFile 空)不出现任何 .env 写命令。
func TestDeployWritesEnvFile(t *testing.T) {
	db := testDB(t)
	rsvc := run.New(db)
	st := &stubTarget{servers: map[string]*target.Server{}}
	srv := seedServer(t, st, "web-1")
	svc := New(st, rsvc)

	runID, artID := seedSuccessRunWithArtifact(t, db, rsvc, run.ArtifactDist, "dist/app")

	// 注入两条变量:一条含空格(需引号)、一条普通。
	_, err := svc.Deploy(context.Background(), DeployInput{
		RunID:      runID,
		ArtifactID: artID,
		ServerIDs:  []string{srv.ID},
		EnvFile:    map[string]string{"APP_ENV": "prod", "GREETING": "hello world"},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	// 找到 .env 写入命令,解码 base64 校验内容。
	var envContent string
	found := false
	for _, cmd := range st.calls {
		if len(cmd) >= 5 && cmd[0] == "sh" && strings.HasSuffix(cmd[3], ".env") {
			found = true
			decoded, derr := base64.StdEncoding.DecodeString(cmd[4])
			if derr != nil {
				t.Fatalf("decode .env payload: %v", derr)
			}
			envContent = string(decoded)
			break
		}
	}
	if !found {
		t.Fatalf("no .env write command in calls: %v", st.calls)
	}
	if !strings.Contains(envContent, "APP_ENV=prod\n") {
		t.Fatalf(".env missing APP_ENV: %q", envContent)
	}
	if !strings.Contains(envContent, "GREETING=\"hello world\"\n") {
		t.Fatalf(".env missing quoted GREETING: %q", envContent)
	}
}

// TestDeploySkipsEnvFileWhenEmpty 验证 EnvFile 为空时不产生 .env 写命令(向后兼容普通部署)。
func TestDeploySkipsEnvFileWhenEmpty(t *testing.T) {
	db := testDB(t)
	rsvc := run.New(db)
	st := &stubTarget{servers: map[string]*target.Server{}}
	srv := seedServer(t, st, "web-1")
	svc := New(st, rsvc)

	runID, artID := seedSuccessRunWithArtifact(t, db, rsvc, run.ArtifactDist, "dist/app")
	if _, err := svc.Deploy(context.Background(), DeployInput{
		RunID:      runID,
		ArtifactID: artID,
		ServerIDs:  []string{srv.ID},
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	for _, cmd := range st.calls {
		if len(cmd) >= 4 && strings.HasSuffix(cmd[3], ".env") {
			t.Fatalf("unexpected .env write when EnvFile empty: %v", cmd)
		}
	}
}
