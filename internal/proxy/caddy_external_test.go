package proxy

import (
	"context"
	"strings"
	"testing"

	"github.com/atomlust/atomgo/internal/target"
)

// TestProxyMode 校验运行模式读取(默认 managed;ATOMGO_PROXY_MODE=external → external)。
func TestProxyMode(t *testing.T) {
	t.Setenv(proxyModeEnv, "")
	if got := proxyMode(); got != modeManaged {
		t.Errorf("默认应为 managed, got %q", got)
	}
	t.Setenv(proxyModeEnv, "external")
	if got := proxyMode(); got != modeExternal {
		t.Errorf("设为 external 应返回 external, got %q", got)
	}
	t.Setenv(proxyModeEnv, "garbage")
	if got := proxyMode(); got != modeManaged {
		t.Errorf("非法值应回退 managed, got %q", got)
	}
}

// TestExternalPaths 校验外接模式路径 getter 的默认与覆盖。
func TestExternalPaths(t *testing.T) {
	t.Setenv(externalConfigPathEnv, "")
	t.Setenv(externalFragmentDirEnv, "")
	if got := externalConfigPath(); got != defaultExternalConfigPath {
		t.Errorf("默认主配置路径应为 %q, got %q", defaultExternalConfigPath, got)
	}
	if got := externalFragmentDir(); got != defaultExternalFragmentDir {
		t.Errorf("默认片段目录应为 %q, got %q", defaultExternalFragmentDir, got)
	}
	t.Setenv(externalConfigPathEnv, "/opt/caddy/Caddyfile")
	t.Setenv(externalFragmentDirEnv, "/opt/atomgo-proxy")
	if got := externalConfigPath(); got != "/opt/caddy/Caddyfile" {
		t.Errorf("覆盖主配置路径失败, got %q", got)
	}
	if got := (CaddySettings{FragmentDir: externalFragmentDir()}).FragmentPath(); got != "/opt/atomgo-proxy/routes.caddy" {
		t.Errorf("片段文件路径拼接失败, got %q", got)
	}
}

// TestApplyExternalWritesFragmentAndReloads 校验外接模式 apply:写片段文件 + validate + reload。
func TestApplyExternalWritesFragmentAndReloads(t *testing.T) {
	t.Setenv(proxyModeEnv, "external")
	t.Setenv(externalConfigPathEnv, "/etc/caddy/Caddyfile")
	t.Setenv(externalFragmentDirEnv, "/etc/atomgo-proxy")
	t.Setenv(externalReloadCmdEnv, "")

	ft := &fakeTarget{}
	ctx := context.Background()
	content := "app.example.com {\n    reverse_proxy web:8080\n}\n"
	if err := applyCaddyfile(ctx, ft, "srv-1", content, globalCaddySettings()); err != nil {
		t.Fatalf("外接模式 apply 应成功, got %v", err)
	}
	// 片段文件应被 Upload 到主机片段路径。
	foundUpload := false
	for _, p := range ft.uploads {
		if p == "/etc/atomgo-proxy/routes.caddy" {
			foundUpload = true
		}
	}
	if !foundUpload {
		t.Fatalf("未将反代片段写到主机片段路径, uploads=%v", ft.uploads)
	}
	// 应执行 `caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile`。
	foundReload := false
	for _, cmd := range ft.execCalls {
		if len(cmd) >= 3 && cmd[0] == "caddy" && cmd[1] == "reload" {
			foundReload = true
		}
	}
	if !foundReload {
		t.Fatalf("未触发 caddy reload, execCalls=%v", ft.execCalls)
	}
}

// TestEnsureExternalChecksImport 校验外接模式 ensureCaddy:建目录 + 写空片段 + 校验主 Caddyfile 含 import。
func TestEnsureExternalChecksImport(t *testing.T) {
	t.Setenv(proxyModeEnv, "external")
	t.Setenv(externalConfigPathEnv, "/etc/caddy/Caddyfile")
	t.Setenv(externalFragmentDirEnv, "/etc/atomgo-proxy")

	// 主 Caddyfile 存在且已 import 片段 → ensureCaddy 成功。
	ft := &fakeTarget{
		resultFor: func(cmd []string) *target.ExecResult {
			if len(cmd) >= 1 && cmd[0] == "cat" {
				return &target.ExecResult{ExitCode: 0, Stdout: "import /etc/atomgo-proxy/routes.caddy\n"}
			}
			if len(cmd) >= 1 && cmd[0] == "grep" {
				return &target.ExecResult{ExitCode: 0} // import 存在
			}
			return nil
		},
	}
	if err := ensureCaddy(context.Background(), ft, "srv-1", nil, globalCaddySettings()); err != nil {
		t.Fatalf("主 Caddyfile 含 import 时 ensureCaddy 应成功, got %v", err)
	}
	// 应执行 mkdir -p 片段目录。
	foundMkdir := false
	for _, cmd := range ft.execCalls {
		if len(cmd) == 3 && cmd[0] == "mkdir" && cmd[1] == "-p" && cmd[2] == "/etc/atomgo-proxy" {
			foundMkdir = true
		}
	}
	if !foundMkdir {
		t.Fatalf("未建片段目录, execCalls=%v", ft.execCalls)
	}
}

// TestEnsureExternalMissingImport 校验外接模式主 Caddyfile 未含 import → 明确报错。
func TestEnsureExternalMissingImport(t *testing.T) {
	t.Setenv(proxyModeEnv, "external")
	t.Setenv(externalConfigPathEnv, "/etc/caddy/Caddyfile")
	t.Setenv(externalFragmentDirEnv, "/etc/atomgo-proxy")

	ft := &fakeTarget{
		resultFor: func(cmd []string) *target.ExecResult {
			if len(cmd) >= 1 && cmd[0] == "cat" {
				return &target.ExecResult{ExitCode: 0, Stdout: "app.example.com {\n    reverse_proxy web:8080\n}\n"}
			}
			if len(cmd) >= 1 && cmd[0] == "grep" {
				return &target.ExecResult{ExitCode: 1} // import 缺失
			}
			return nil
		},
	}
	err := ensureCaddy(context.Background(), ft, "srv-1", nil, globalCaddySettings())
	if err == nil || !strings.Contains(err.Error(), "import") {
		t.Fatalf("主 Caddyfile 缺 import 应报明确错误, got %v", err)
	}
}

// TestGetProxyModeInfo 校验对外只读快照。
func TestGetProxyModeInfo(t *testing.T) {
	t.Setenv(proxyModeEnv, "external")
	t.Setenv(externalConfigPathEnv, "/opt/caddy/Caddyfile")
	t.Setenv(externalFragmentDirEnv, "/opt/atomgo-proxy")
	info := GetProxyModeInfo()
	if info.Mode != modeExternal {
		t.Errorf("mode 应为 external, got %q", info.Mode)
	}
	if info.ConfigPath != "/opt/caddy/Caddyfile" {
		t.Errorf("ConfigPath 不符, got %q", info.ConfigPath)
	}
	if info.FragmentDir != "/opt/atomgo-proxy" {
		t.Errorf("FragmentDir 不符, got %q", info.FragmentDir)
	}
	if !strings.Contains(info.ReloadCmd, "caddy reload") {
		t.Errorf("ReloadCmd 应含 caddy reload, got %q", info.ReloadCmd)
	}
}

// TestExternalContainerAwareCommands 校验外接模式在「容器化 Caddy」(设了 ExternalCaddyContainer)
// 时:verifyExternalImport 经 docker exec 读主 Caddyfile 与 grep import;applyExternalCaddyfile 经
// docker cp 写片段(而非宿主 Upload)+ docker exec caddy reload。这样无需把 Caddyfile / 片段目录 bind-mount 到宿主。
func TestExternalContainerAwareCommands(t *testing.T) {
	t.Setenv(proxyModeEnv, "external")
	t.Setenv(externalConfigPathEnv, "/etc/caddy/Caddyfile")
	t.Setenv(externalFragmentDirEnv, "/etc/atomgo-proxy")
	t.Setenv(externalCaddyContainerEnv, "my-caddy")

	// 1) verifyExternalImport:cat / grep 应经 docker exec my-caddy 包装。
	ft := &fakeTarget{
		resultFor: func(cmd []string) *target.ExecResult {
			if len(cmd) >= 4 && cmd[0] == "docker" && cmd[1] == "exec" && cmd[2] == "my-caddy" {
				switch cmd[3] {
				case "cat":
					return &target.ExecResult{ExitCode: 0, Stdout: "import /etc/atomgo-proxy/routes.caddy\n"}
				case "grep":
					return &target.ExecResult{ExitCode: 0}
				}
			}
			return nil
		},
	}
	if err := verifyExternalImport(context.Background(), ft, "srv-1", globalCaddySettings()); err != nil {
		t.Fatalf("容器模式 verifyExternalImport 应成功, got %v", err)
	}
	for _, cmd := range ft.execCalls {
		if (cmd[0] == "cat" || cmd[0] == "grep") && len(cmd) < 3 {
			t.Fatalf("cat/grep 应经 docker exec 包装, 但见裸命令: %v", cmd)
		}
	}

	// 2) applyExternalCaddyfile:应 docker cp 到容器 + docker exec caddy reload(默认 reload)。
	ft2 := &fakeTarget{}
	content := "app.example.com {\n    reverse_proxy web:8080\n}\n"
	if err := applyExternalCaddyfile(context.Background(), ft2, "srv-1", content, globalCaddySettings()); err != nil {
		t.Fatalf("容器模式 applyExternalCaddyfile 应成功, got %v", err)
	}
	foundCp, foundReload := false, false
	for _, cmd := range ft2.execCalls {
		if len(cmd) >= 4 && cmd[0] == "docker" && cmd[1] == "cp" && cmd[3] == "my-caddy:"+"/etc/atomgo-proxy/routes.caddy" {
			foundCp = true
		}
		if len(cmd) >= 4 && cmd[0] == "docker" && cmd[1] == "exec" && cmd[2] == "my-caddy" && cmd[3] == "caddy" && cmd[4] == "reload" {
			foundReload = true
		}
	}
	if !foundCp {
		t.Fatalf("容器模式未执行 docker cp 片段进容器, execCalls=%v", ft2.execCalls)
	}
	if !foundReload {
		t.Fatalf("容器模式未执行 docker exec caddy reload, execCalls=%v", ft2.execCalls)
	}
	// 不应再把片段直接 Upload 到宿主片段路径(应经 docker cp)。
	for _, p := range ft2.uploads {
		if p == "/etc/atomgo-proxy/routes.caddy" {
			t.Fatalf("容器模式不应把片段直接 Upload 到宿主片段路径, uploads=%v", ft2.uploads)
		}
	}
}

// TestApplyExternalSkipsValidateWhenHostCaddyMissing 校验外接模式(容器化 Caddy)场景:宿主无
// caddy 二进制,`caddy validate` 返回 exit 127 / "command not found";此时应 best-effort 跳过宿主
// 校验,改由 reload 命令(经 docker exec 进容器)完成校验并重载,apply 成功。
func TestApplyExternalSkipsValidateWhenHostCaddyMissing(t *testing.T) {
	t.Setenv(proxyModeEnv, "external")
	t.Setenv(externalConfigPathEnv, "/etc/caddy/Caddyfile")
	t.Setenv(externalFragmentDirEnv, "/etc/atomgo-proxy")
	t.Setenv(externalReloadCmdEnv, "docker exec my-caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile")

	ft := &fakeTarget{
		resultFor: func(cmd []string) *target.ExecResult {
			// 宿主无 caddy:validate 返回 127(command not found);其余(docker exec reload / chmod)默认 exit0。
			if len(cmd) >= 2 && cmd[0] == "caddy" && cmd[1] == "validate" {
				return &target.ExecResult{ExitCode: 127, Stderr: "caddy: command not found"}
			}
			return nil
		},
	}
	ctx := context.Background()
	content := "app.example.com {\n    reverse_proxy web:8080\n}\n"
	if err := applyCaddyfile(ctx, ft, "srv-1", content, globalCaddySettings()); err != nil {
		t.Fatalf("宿主无 caddy 时 external apply 应跳过 validate 并成功, got %v", err)
	}
	// 应执行 docker exec <caddy> caddy reload(而非宿主 caddy reload)。
	foundReload := false
	for _, cmd := range ft.execCalls {
		if len(cmd) >= 4 && cmd[0] == "docker" && cmd[1] == "exec" && cmd[3] == "caddy" {
			foundReload = true
		}
	}
	if !foundReload {
		t.Fatalf("未触发 docker exec caddy reload, execCalls=%v", ft.execCalls)
	}
}
