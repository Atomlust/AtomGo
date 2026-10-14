package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/atomlust/atomgo/internal/livecache"
	"github.com/atomlust/atomgo/internal/settings"
	"github.com/atomlust/atomgo/internal/target"
)

// 实时采集缓存的 system_settings 键与约束。
const (
	// livePollIntervalKey 是后台采集间隔(秒)的键值;缺省用 defaultLivePollInterval。
	livePollIntervalKey = "live_poll_interval"
	// defaultLivePollInterval 是默认采集间隔(秒)。
	defaultLivePollInterval = 30
	// minLivePollInterval / maxLivePollInterval 是用户可设区间(秒)。
	minLivePollInterval = 10
	maxLivePollInterval = 3600
)

// NewLiveCacheCollectors 返回 livecache 的采集器集合:复用既有 6-1 指标采集与容器清单采集
// (collectServerMetrics / collectServerContainers,均为同包 unexported,永不返回致命 error)。
// 这样 livecache 包不反向依赖 httpapi 的 DTO 类型,避免 import 环;缓存存 any,由 handler
// 读取时按 kind 做类型断言还原。
func NewLiveCacheCollectors(svc target.Service) map[string]livecache.Collector {
	return map[string]livecache.Collector{
		"metrics": func(ctx context.Context, s target.Service, id string) (any, error) {
			// 批量端点语义:定位类错误对某台亦只表现为 reachable:false,故忽略 locErr。
			dto, _ := collectServerMetrics(ctx, s, id)
			return dto, nil
		},
		"containers": func(ctx context.Context, s target.Service, id string) (any, error) {
			dto, _ := collectServerContainers(ctx, s, id)
			return dto, nil
		},
	}
}

// LivePollInterval 从 system_settings 读取后台采集间隔(秒)为 time.Duration;
// 缺省/非法/越界均回落默认。供 main.go 注入 livecache 的 intervalFn。
func LivePollInterval(svc *settings.Service) time.Duration {
	if svc == nil {
		return defaultLivePollInterval * time.Second
	}
	v, err := svc.Get(context.Background(), livePollIntervalKey)
	if err != nil || strings.TrimSpace(v) == "" {
		return defaultLivePollInterval * time.Second
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return defaultLivePollInterval * time.Second
	}
	if n < minLivePollInterval {
		n = minLivePollInterval
	}
	if n > maxLivePollInterval {
		n = maxLivePollInterval
	}
	return time.Duration(n) * time.Second
}

// makeGetLivePollHandler 返回 GET /api/system/live-poll → { intervalSeconds }。
func makeGetLivePollHandler(svc *settings.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "internal", "系统设置服务未初始化")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"intervalSeconds": int(LivePollInterval(svc).Seconds())})
	}
}

// makeSetLivePollHandler 返回 PUT /api/system/live-poll(auth + CSRF + PermSettingsManage)。
// body { intervalSeconds };钳到 [10, 3600];持久化到 system_settings;回读返回。
func makeSetLivePollHandler(svc *settings.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "internal", "系统设置服务未初始化")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
		var in struct {
			IntervalSeconds int `json:"intervalSeconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "请求体格式错误")
			return
		}
		n := in.IntervalSeconds
		if n < minLivePollInterval {
			n = minLivePollInterval
		}
		if n > maxLivePollInterval {
			n = maxLivePollInterval
		}
		if err := svc.Set(r.Context(), livePollIntervalKey, strconv.Itoa(n)); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "保存采集间隔失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"intervalSeconds": n})
	}
}

// makeRefreshLiveCacheHandler 返回 POST /api/system/live-poll/refresh(auth + CSRF + PermSettingsManage)。
// 立即同步重采所有服务器所有类(后台采集器下次 tick 也会自然覆盖);用于用户改间隔后想立刻见效。
func makeRefreshLiveCacheHandler(cache *livecache.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cache == nil {
			writeError(w, http.StatusServiceUnavailable, "internal", "实时采集缓存未初始化")
			return
		}
		cache.CollectAll(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
