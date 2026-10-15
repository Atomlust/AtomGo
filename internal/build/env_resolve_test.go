package build

import (
	"context"
	"reflect"
	"testing"

	"github.com/atomlust/atomgo/internal/pipeline"
)

// TestResolveEnvVars 验证 dag 部署节点的环境变量解析:明文直取、secret 经保险库解密、
// 缺失凭据降级为 unresolved(仅 key)、环境未定义/空入参返回空不报错。
func TestResolveEnvVars(t *testing.T) {
	settings := &pipeline.Settings{Environments: []pipeline.Environment{{
		Name: "prod",
		EnvVars: []pipeline.BuildVar{
			{Key: "ASPNETCORE_ENVIRONMENT", Value: "Production"},
			{Key: "Auth__Jwt__SigningKey", Secret: true, CredentialID: "cred-jwt"},
			{Key: "Auth__Bootstrap__Password", Secret: true, CredentialID: "cred-missing"}, // 凭据不存在 → unresolved
			{Key: "Auth__Bootstrap__TenantSlug", Value: "default"},
		},
	}}}
	b := &Builder{
		settings: fakeSettings{settings: settings},
		vault:    fakeVault{secrets: map[string]string{"cred-jwt": "s3cret-signing-key"}},
	}

	vars, unresolved, err := b.resolveEnvVars(context.Background(), "proj-1", "prod")
	if err != nil {
		t.Fatalf("resolveEnvVars: %v", err)
	}
	want := map[string]string{
		"ASPNETCORE_ENVIRONMENT":      "Production",
		"Auth__Jwt__SigningKey":       "s3cret-signing-key",
		"Auth__Bootstrap__TenantSlug": "default",
	}
	if !reflect.DeepEqual(vars, want) {
		t.Fatalf("vars = %+v, want %+v", vars, want)
	}
	if !reflect.DeepEqual(unresolved, []string{"Auth__Bootstrap__Password"}) {
		t.Fatalf("unresolved = %v, want [Auth__Bootstrap__Password]", unresolved)
	}
}

// TestResolveEnvVarsNoVault 保险库未注入:secret 全部进 unresolved,明文仍解析(降级不崩)。
func TestResolveEnvVarsNoVault(t *testing.T) {
	settings := &pipeline.Settings{Environments: []pipeline.Environment{{
		Name:    "prod",
		EnvVars: []pipeline.BuildVar{{Key: "PLAIN", Value: "v"}, {Key: "SECRET", Secret: true, CredentialID: "c"}},
	}}}
	b := &Builder{settings: fakeSettings{settings: settings}}

	vars, unresolved, err := b.resolveEnvVars(context.Background(), "p", "prod")
	if err != nil {
		t.Fatalf("resolveEnvVars: %v", err)
	}
	if !reflect.DeepEqual(vars, map[string]string{"PLAIN": "v"}) {
		t.Fatalf("vars = %+v, want {PLAIN:v}", vars)
	}
	if !reflect.DeepEqual(unresolved, []string{"SECRET"}) {
		t.Fatalf("unresolved = %v, want [SECRET]", unresolved)
	}
}

// TestResolveEnvVarsSkipsUnknown 环境未定义 / 空环境名 / 空项目 / settings 未注入 → 空且不报错。
func TestResolveEnvVarsSkipsUnknown(t *testing.T) {
	settings := &pipeline.Settings{Environments: []pipeline.Environment{{Name: "prod"}}}
	b := &Builder{settings: fakeSettings{settings: settings}}
	for _, tc := range []struct{ name, proj, env string }{
		{"env undefined", "p", "staging"},
		{"empty env", "p", ""},
		{"empty project", "", "prod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vars, unresolved, err := b.resolveEnvVars(context.Background(), tc.proj, tc.env)
			if err != nil {
				t.Fatalf("resolveEnvVars: %v", err)
			}
			if len(vars) != 0 || len(unresolved) != 0 {
				t.Fatalf("want empty, got vars=%v unresolved=%v", vars, unresolved)
			}
		})
	}
	// settings 未注入(裸 Builder,如旧测试构造)→ 同样空且不报错。
	bare := &Builder{}
	if vars, _, err := bare.resolveEnvVars(context.Background(), "p", "prod"); err != nil || len(vars) != 0 {
		t.Fatalf("bare builder: vars=%v err=%v", vars, err)
	}
}

// TestResolveEnvVarsSettingsError settings 读失败 → 错误上抛(调用方诚实失败,不静默丢变量)。
func TestResolveEnvVarsSettingsError(t *testing.T) {
	b := &Builder{settings: fakeSettings{err: context.DeadlineExceeded}}
	if _, _, err := b.resolveEnvVars(context.Background(), "p", "prod"); err == nil {
		t.Fatal("want settings error surfaced")
	}
}
