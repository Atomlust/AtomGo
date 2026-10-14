package build

import (
	"context"
	"testing"

	"github.com/atomlust/atomgo/internal/pipeline"
)

// pulledRef 报告 fake commander 是否执行过 `docker pull <ref>`。
func pulledRef(cmdr *fakeCommander, ref string) bool {
	for _, e := range cmdr.execs {
		if len(e.args) >= 2 && e.args[0] == "pull" && e.args[1] == ref {
			return true
		}
	}
	return false
}

func mirrorSettings(envs ...pipeline.Environment) fakeSettings {
	return fakeSettings{settings: &pipeline.Settings{Environments: envs}}
}

// TestMirrorDifferentHost 异仓库:完整镜像(login→pull→tag→push),deployRef 指向目标仓库、mirrored=true。
func TestMirrorDifferentHost(t *testing.T) {
	cmdr := newFakeCommander()
	cmdr.script("login", fakeCmd{exitCode: 0})
	cmdr.script("pull", fakeCmd{exitCode: 0})
	cmdr.script("tag", fakeCmd{exitCode: 0})
	cmdr.script("push", fakeCmd{exitCode: 0})
	drv := &shellDriver{bin: "docker", cmdr: cmdr}
	vlt := fakeVault{secrets: map[string]string{"cred-reg": "robot:pw"}}
	settings := mirrorSettings(pipeline.Environment{
		Name:          "prod",
		ImageRegistry: pipeline.ImageRegistry{Type: pipeline.RegistryHarbor, URL: "https://harbor-prod.acme.com", CredentialID: "cred-reg"},
	})

	deployRef, mirrored, err := MirrorImageForPromotion(context.Background(), drv, vlt, settings, "p1", "prod",
		"harbor-dev.acme.com/acme/shop-api:abc1234")
	if err != nil {
		t.Fatalf("mirror err: %v", err)
	}
	if !mirrored {
		t.Fatalf("expected mirrored=true")
	}
	if deployRef != "harbor-prod.acme.com/acme/shop-api:abc1234" {
		t.Fatalf("deployRef = %q, want target-host ref (name/tag unchanged)", deployRef)
	}
	if !pulledRef(cmdr, "harbor-dev.acme.com/acme/shop-api:abc1234") {
		t.Fatalf("expected pull of source; execs=%v", cmdr.execs)
	}
	if !pushedRef(cmdr, "harbor-prod.acme.com/acme/shop-api:abc1234") {
		t.Fatalf("expected push to target; execs=%v", cmdr.execs)
	}
}

// TestMirrorSameHostChannelTag 同仓库:不搬镜像本体(mirrored=false、deployRef 不变),
// 但补打目标环境的通道 tag(先 pull 到本地再 tag+push 别名)。
func TestMirrorSameHostChannelTag(t *testing.T) {
	cmdr := newFakeCommander()
	cmdr.script("login", fakeCmd{exitCode: 0})
	cmdr.script("pull", fakeCmd{exitCode: 0})
	cmdr.script("tag", fakeCmd{exitCode: 0})
	cmdr.script("push", fakeCmd{exitCode: 0})
	drv := &shellDriver{bin: "docker", cmdr: cmdr}
	vlt := fakeVault{secrets: map[string]string{"cred": "u:p"}}
	settings := mirrorSettings(pipeline.Environment{
		Name:          "prod",
		ImageRegistry: pipeline.ImageRegistry{Type: pipeline.RegistryHarbor, URL: "reg.acme.com", CredentialID: "cred"},
		ChannelTag:    "prod",
	})

	deployRef, mirrored, err := MirrorImageForPromotion(context.Background(), drv, vlt, settings, "p1", "prod",
		"reg.acme.com/app:abc1234")
	if err != nil {
		t.Fatalf("mirror err: %v", err)
	}
	if mirrored {
		t.Fatalf("same host must not mirror the image body")
	}
	if deployRef != "reg.acme.com/app:abc1234" {
		t.Fatalf("deployRef changed on same host: %q", deployRef)
	}
	if pushedRef(cmdr, "reg.acme.com/app:abc1234") {
		t.Fatalf("must not push the body on same host; execs=%v", cmdr.execs)
	}
	if !pushedRef(cmdr, "reg.acme.com/app:prod") {
		t.Fatalf("expected channel-tag push reg.acme.com/app:prod; execs=%v", cmdr.execs)
	}
}

// TestMirrorNoRegistryNoOp 目标环境未绑定仓库:不镜像、不发任何 docker 命令,deployRef=源引用(降级)。
func TestMirrorNoRegistryNoOp(t *testing.T) {
	cmdr := newFakeCommander()
	drv := &shellDriver{bin: "docker", cmdr: cmdr}
	settings := mirrorSettings(pipeline.Environment{Name: "prod"}) // 无 registry

	deployRef, mirrored, err := MirrorImageForPromotion(context.Background(), drv, fakeVault{}, settings, "p1", "prod",
		"harbor-dev.acme.com/app:abc1234")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if mirrored {
		t.Fatalf("should not mirror without a bound registry")
	}
	if deployRef != "harbor-dev.acme.com/app:abc1234" {
		t.Fatalf("deployRef = %q, want source ref", deployRef)
	}
	if len(cmdr.execs) != 0 {
		t.Fatalf("expected no docker calls, got %v", cmdr.execs)
	}
}

// TestMirrorPushFailure 异仓库推送失败 → 返回错误(调用方据此跳过部署、不回滚晋级)。
func TestMirrorPushFailure(t *testing.T) {
	cmdr := newFakeCommander()
	cmdr.script("login", fakeCmd{exitCode: 0})
	cmdr.script("pull", fakeCmd{exitCode: 0})
	cmdr.script("tag", fakeCmd{exitCode: 0})
	cmdr.script("push", fakeCmd{stderrLines: []string{"denied: requested access"}, exitCode: 1})
	drv := &shellDriver{bin: "docker", cmdr: cmdr}
	vlt := fakeVault{secrets: map[string]string{"cred": "u:p"}}
	settings := mirrorSettings(pipeline.Environment{
		Name:          "prod",
		ImageRegistry: pipeline.ImageRegistry{Type: pipeline.RegistryHarbor, URL: "https://harbor-prod.acme.com", CredentialID: "cred"},
	})

	if _, _, err := MirrorImageForPromotion(context.Background(), drv, vlt, settings, "p1", "prod",
		"harbor-dev.acme.com/app:abc1234"); err == nil {
		t.Fatalf("expected error when target push fails")
	}
}
