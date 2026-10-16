package deploy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/atomlust/atomgo/internal/pipeline"
	"github.com/atomlust/atomgo/internal/run"
	"github.com/atomlust/atomgo/internal/target"
	"github.com/atomlust/atomgo/internal/vault"
	"github.com/google/uuid"
)

// ---- 纯函数测试 -------------------------------------------------------------

func TestParseRegistryHost(t *testing.T) {
	cases := map[string]string{
		"docker.atomlust.com/atomlust/atomupdate:tag": "docker.atomlust.com",
		"localhost:5000/myimg:tag":                    "localhost:5000",
		"reg.io:5000/a/b:c":                           "reg.io:5000",
		"nginx:latest":                                "docker.io",
		"myorg/app":                                   "docker.io",
		"https://docker.atomlust.com/x/y:z":           "docker.atomlust.com", // 协议不应残留
	}
	for ref, want := range cases {
		if got := parseRegistryHost(ref); got != want {
			t.Fatalf("parseRegistryHost(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestHostOfRegistry(t *testing.T) {
	cases := map[string]string{
		"https://docker.atomlust.com": "docker.atomlust.com",
		"http://reg.io:5000/":         "reg.io:5000",
		"docker.atomlust.com":         "docker.atomlust.com",
		"  https://x.io/  ":           "x.io",
	}
	for in, want := range cases {
		if got := hostOfRegistry(in); got != want {
			t.Fatalf("hostOfRegistry(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitRegistrySecret(t *testing.T) {
	u, p := splitRegistrySecret("alice:s3cr3t")
	if u != "alice" || p != "s3cr3t" {
		t.Fatalf("got (%q,%q), want (alice,s3cr3t)", u, p)
	}
	u, p = splitRegistrySecret("token-only")
	if u != "" || p != "token-only" {
		t.Fatalf("got (%q,%q), want ('',token-only)", u, p)
	}
	u, p = splitRegistrySecret("user:pass:with:colons")
	if u != "user" || p != "pass:with:colons" {
		t.Fatalf("got (%q,%q), want (user,pass:with:colons)", u, p)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("alice"); got != "'alice'" {
		t.Fatalf("shellQuote plain = %q", got)
	}
	if got := shellQuote("a'b"); got != "'a'\\''b'" {
		t.Fatalf("shellQuote embed = %q", got)
	}
}

// ---- 解析器测试(真实 settings + vault) --------------------------------------

func TestSettingsRegistryResolverResolve(t *testing.T) {
	db := testDB(t)
	var key [32]byte
	v := vault.New(db, &key)
	setsvc := pipeline.NewSettingsService(db, v)
	projID := uuid.NewString()

	// settings.Save 校验项目存在性(且 projects.credential_id 外键指向 credentials)。
	projCred, err := v.Create(vault.CreateInput{Name: "projcred", Type: vault.TypeGitToken, Secret: "x"})
	if err != nil {
		t.Fatalf("create proj credential: %v", err)
	}
	credID := projCred.ID
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO projects (id, name, repo_url, default_branch, credential_id, created_at, updated_at)
		 VALUES (?, 'acme', 'https://example.com/p.git', 'main', ?, ?, ?)`, projID, credID, now, now); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	cred, err := v.Create(vault.CreateInput{Name: "reg", Type: vault.TypeRegistry, Secret: "alice:s3cr3t"})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	_, err = setsvc.Save(context.Background(), projID, pipeline.SettingsInput{
		Environments: []pipeline.Environment{
			{
				ID:            uuid.NewString(),
				Name:          "prod",
				ImageRegistry: pipeline.ImageRegistry{Type: "custom", URL: "https://docker.atomlust.com", CredentialID: cred.ID},
			},
		},
	})
	if err != nil {
		t.Fatalf("save settings: %v", err)
	}

	r := NewSettingsRegistryResolver(setsvc, v)
	ctx := context.Background()

	// 1) 按环境名精确命中。
	creds, err := r.Resolve(ctx, projID, "prod", "docker.atomlust.com")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if creds == nil {
		t.Fatalf("expected creds, got nil")
	}
	if creds.Host != "docker.atomlust.com" || creds.Username != "alice" || creds.Password != "s3cr3t" {
		t.Fatalf("creds = %+v", creds)
	}

	// 2) 环境名已解析但不匹配 → **不**借用其它环境的凭据。
	//    刻意的安全加固:环境名写错/环境未绑定时,宁可跳过自动登录(沿用目标机既有登录),
	//    也不悄悄拿别的环境凭据去登,避免登错仓库(详见 registry_login.go 的 Resolve)。
	creds, err = r.Resolve(ctx, projID, "nonexistent", "docker.atomlust.com")
	if err != nil || creds != nil {
		t.Fatalf("环境名未匹配时不应借用其它环境凭据: creds=%v err=%v", creds, err)
	}

	// 2b) 环境名未解析(手动触发等,envName 为空)且主机匹配 → 回退命中。
	creds, err = r.Resolve(ctx, projID, "", "docker.atomlust.com")
	if err != nil || creds == nil {
		t.Fatalf("envName 为空时应回退命中: creds=%v err=%v", creds, err)
	}
	if creds.Host != "docker.atomlust.com" || creds.Username != "alice" || creds.Password != "s3cr3t" {
		t.Fatalf("fallback creds = %+v", creds)
	}

	// 3) 无凭据绑定的项目 → nil(跳过)。
	other := uuid.NewString()
	if _, err := db.Exec(
		`INSERT INTO projects (id, name, repo_url, default_branch, credential_id, created_at, updated_at)
		 VALUES (?, 'other', 'https://example.com/o.git', 'main', ?, ?, ?)`, other, credID, now, now); err != nil {
		t.Fatalf("seed other project: %v", err)
	}
	if _, err := setsvc.Save(context.Background(), other, pipeline.SettingsInput{
		Environments: []pipeline.Environment{{ID: uuid.NewString(), Name: "empty"}},
	}); err != nil {
		t.Fatalf("save empty settings: %v", err)
	}
	creds, err = r.Resolve(ctx, other, "empty", "docker.atomlust.com")
	if err != nil || creds != nil {
		t.Fatalf("expected nil creds for unbound env, got creds=%v err=%v", creds, err)
	}
}

// ---- 自动登录编排测试(假 resolver + stubTarget) -----------------------------

type fakeResolver struct{ creds *registryCreds }

func (f *fakeResolver) Resolve(_ context.Context, _, _, _ string) (*registryCreds, error) {
	return f.creds, nil
}

func TestLoginRegistryForImagePerformsSafeLogin(t *testing.T) {
	db := testDB(t)
	rsvc := run.New(db)
	runID, _ := seedSuccessRunWithArtifact(t, db, rsvc, run.ArtifactImage, "docker.atomlust.com/atomlust/atomupdate:abc123")
	tgt := &stubTarget{servers: map[string]*target.Server{}}
	srv := seedServer(t, tgt, "target1")

	svc := New(tgt, rsvc, WithRegistryResolver(&fakeResolver{
		creds: &registryCreds{Host: "docker.atomlust.com", Username: "alice", Password: "s3cr3t"},
	})).(*service)

	a := run.Artifact{RunID: runID, Type: run.ArtifactImage, Reference: "docker.atomlust.com/atomlust/atomupdate:abc123"}
	if msg, ok := svc.loginRegistryForImage(context.Background(), srv, a, a.Reference); !ok || msg != "" {
		t.Fatalf("loginRegistryForImage ok=%v msg=%q", ok, msg)
	}

	// 口令应经 Upload 写到 /dev/shm 临时文件,且内容正确。
	var uploadedPW string
	for path, body := range tgt.uploads {
		if strings.Contains(path, ".atomgo-docker-login-") {
			uploadedPW = string(body)
		}
	}
	if uploadedPW != "s3cr3t" {
		t.Fatalf("uploaded password = %q, want s3cr3t", uploadedPW)
	}

	// 任意执行命令都不应包含口令明文。
	for _, cmd := range tgt.calls {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "s3cr3t") {
			t.Fatalf("password leaked into command: %v", cmd)
		}
	}

	// 应出现 docker login --password-stdin 与 rm -f 清理。
	sawLogin, sawRm := false, false
	for _, cmd := range tgt.calls {
		if len(cmd) == 3 && cmd[0] == "sh" && cmd[1] == "-c" && strings.Contains(cmd[2], "--password-stdin") && strings.Contains(cmd[2], "/dev/shm/.atomgo-docker-login-") {
			sawLogin = true
		}
		if len(cmd) == 3 && cmd[0] == "rm" && cmd[1] == "-f" && strings.Contains(cmd[2], ".atomgo-docker-login-") {
			sawRm = true
		}
	}
	if !sawLogin {
		t.Fatalf("expected docker login --password-stdin command, got %v", tgt.calls)
	}
	if !sawRm {
		t.Fatalf("expected rm -f of temp file, got %v", tgt.calls)
	}
}

func TestLoginRegistryForImageSkipsOnHostMismatch(t *testing.T) {
	db := testDB(t)
	rsvc := run.New(db)
	runID, _ := seedSuccessRunWithArtifact(t, db, rsvc, run.ArtifactImage, "other.reg.io/img:tag")
	tgt := &stubTarget{servers: map[string]*target.Server{}}
	srv := seedServer(t, tgt, "target1")
	// resolver 返回的是另一 registry 的凭据(主机不一致)。
	svc := New(tgt, rsvc, WithRegistryResolver(&fakeResolver{
		creds: &registryCreds{Host: "docker.atomlust.com", Username: "alice", Password: "s3cr3t"},
	})).(*service)
	a := run.Artifact{RunID: runID, Type: run.ArtifactImage, Reference: "other.reg.io/img:tag"}
	if msg, ok := svc.loginRegistryForImage(context.Background(), srv, a, a.Reference); !ok || msg != "" {
		t.Fatalf("ok=%v msg=%q", ok, msg)
	}
	if len(tgt.uploads) != 0 || len(tgt.calls) != 0 {
		t.Fatalf("should skip login on host mismatch, got uploads=%v calls=%v", tgt.uploads, tgt.calls)
	}
}

func TestLoginRegistryForImageSkipsWhenNoResolverOrNoCreds(t *testing.T) {
	db := testDB(t)
	rsvc := run.New(db)
	runID, _ := seedSuccessRunWithArtifact(t, db, rsvc, run.ArtifactImage, "docker.atomlust.com/img:tag")
	tgt := &stubTarget{servers: map[string]*target.Server{}}
	srv := seedServer(t, tgt, "target1")
	a := run.Artifact{RunID: runID, Type: run.ArtifactImage, Reference: "docker.atomlust.com/img:tag"}

	// 1) 无 resolver → 跳过。
	svc := New(tgt, rsvc).(*service)
	if msg, ok := svc.loginRegistryForImage(context.Background(), srv, a, a.Reference); !ok || msg != "" {
		t.Fatalf("nil resolver ok=%v msg=%q", ok, msg)
	}
	if len(tgt.calls) != 0 {
		t.Fatalf("nil resolver should not exec, got %v", tgt.calls)
	}

	// 2) resolver 返回 nil 凭据 → 跳过。
	svc2 := New(tgt, rsvc, WithRegistryResolver(&fakeResolver{creds: nil})).(*service)
	if msg, ok := svc2.loginRegistryForImage(context.Background(), srv, a, a.Reference); !ok || msg != "" {
		t.Fatalf("nil creds ok=%v msg=%q", ok, msg)
	}
	if len(tgt.calls) != 0 {
		t.Fatalf("nil creds should not exec, got %v", tgt.calls)
	}
}
