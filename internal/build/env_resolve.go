package build

import (
	"context"
	"strings"

	"github.com/atomlust/atomgo/internal/vault"
)

// resolveEnvVars 解析某项目某环境的全部作用域变量为可注入的明文 K=V(secret 经保险库即时解密)。
// 语义与晋级部署(internal/promotion Coordinator.ResolveEnvVars)逐行对齐:单一真相源是
// pipeline_settings.Environments(「环境与凭据」卡片);secret 变量只存 credentialId 引用,
// 在此经 vault.Reveal 即取即用。
//
// 降级路径(绝不因此崩部署,也不谎报注入):
//   - settings 未注入 / 项目或环境名为空 / 环境未定义 → 返回空(与「无变量」一致,不报错);
//   - secret 凭据缺失 / 保险库未配 / 解密失败 → 跳过该 key,仅把 key 记入 unresolved
//     (供调用方记日志告警;绝不明文进日志 / 错误)。
func (b *Builder) resolveEnvVars(ctx context.Context, projectID, env string) (vars map[string]string, unresolved []string, err error) {
	if b.settings == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(env) == "" {
		return nil, nil, nil
	}
	st, err := b.settings.Get(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range st.Environments {
		if e.Name != env {
			continue
		}
		vars = make(map[string]string, len(e.EnvVars))
		for _, v := range e.EnvVars {
			key := strings.TrimSpace(v.Key)
			if key == "" {
				continue
			}
			if !v.Secret {
				vars[key] = v.Value
				continue
			}
			plain, rerr := b.revealEnvSecret(v.CredentialID)
			if rerr != nil {
				unresolved = append(unresolved, key)
				continue
			}
			vars[key] = plain
		}
		return vars, unresolved, nil
	}
	// 环境不存在:返回空(与「无变量」一致,不报错)。
	return nil, nil, nil
}

// revealEnvSecret 解密环境 secret 变量的凭据引用。保险库未配 / 引用为空 / 解密失败 /
// 解密结果为空 → 返回错误(调用方记 unresolved 并跳过,绝不明文化)。
func (b *Builder) revealEnvSecret(credID string) (string, error) {
	if b.vault == nil {
		return "", vault.ErrVaultUnconfigured
	}
	credID = strings.TrimSpace(credID)
	if credID == "" {
		return "", vault.ErrVaultUnconfigured
	}
	plain, err := b.vault.Reveal(credID)
	if err != nil {
		return "", err
	}
	if plain == "" {
		return "", vault.ErrVaultUnconfigured
	}
	return plain, nil
}
