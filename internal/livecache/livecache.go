// Package livecache 提供「统一实时采集缓存」:把原本每次 HTTP 请求都现采 SSH/docker 的
// 服务器指标与容器清单,改为后台按可配置间隔周期性采集并写入进程内缓存;前端接口优先读缓存,
// 命中即秒回,缺失/过期再懒采集回填。这样把「页面打开 = 服务器数 × SSH 往返」的阻塞,变成
// 后台平滑压力,界面体验从「卡」变「即时」。
//
// 设计要点:
//   - 缓存为泛型(any),不由本包依赖 httpapi 的 DTO 类型(避免 import 环);采集器(adapter)
//     在 httpapi 包定义,本包只认 func(ctx, svc, id) (any, error) 契约。
//   - 间隔经 intervalFn 动态读取(来自 system_settings),ticker 每次 Reset 以应用最新间隔,
//     无需重启。
//   - 启动即采一轮(非阻塞),避免重启后首屏空窗;ctx 取消即平滑退出。
//   - 有界并发(信号量)防 N 台服务器同时 SSH 打爆,复用既有 metrics 采集纪律。
package livecache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomlust/atomgo/internal/target"
)

// Collector 采集单台某类数据。失败返回 (nil, err) 由调用方决定(后台采集时忽略该台;
// handler 懒填充时据 err 决定回退到实时采集)。never panic。
type Collector func(ctx context.Context, svc target.Service, serverID string) (any, error)

type entry struct {
	data any
	at   time.Time
}

// Service 是实时采集缓存服务(进程内,单例)。
type Service struct {
	mu         sync.RWMutex
	cache      map[string]entry
	collectors map[string]Collector
	svc        target.Service
	intervalFn func() time.Duration

	// intervalNsec 缓存当前间隔(纳秒)。轻量化的关键:间隔只在一个 tick 边界从
	// settings 读一次库并缓存在此,handler 每次请求调 StaleAfter() 都是纯内存读,
	// **绝不在请求热路径上查库**(否则每次请求多一次 SQL,反而放大开销)。
	intervalNsec atomic.Int64

	collectOnce sync.Once // 保证首轮采只在 Start 触发一次(不重复阻塞启动)
}

// New 构造空缓存服务(未注册采集器/未设 target 前不工作)。
func New() *Service {
	return &Service{
		cache:      make(map[string]entry),
		collectors: make(map[string]Collector),
	}
}

// Register 注册一类采集器(kind 如 "metrics" / "containers")。
func (s *Service) Register(kind string, c Collector) {
	if c == nil {
		return
	}
	s.collectors[kind] = c
}

// SetTarget 注入目标服务器服务(采集时经其 SSH 执行)。
func (s *Service) SetTarget(svc target.Service) { s.svc = svc }

// SetIntervalFn 注入间隔读取函数(返回秒级 duration;<=0 视为用默认)。每次 tick 都会调用,
// 故用户在设置页改间隔后下一个 tick 即生效。
func (s *Service) SetIntervalFn(fn func() time.Duration) { s.intervalFn = fn }

func (s *Service) key(kind, id string) string { return kind + ":" + id }

// defaultInterval 是未配置/未刷新时的兜底间隔。
const defaultInterval = 30 * time.Second

// tickInterval 返回缓存的间隔(纯内存读,不查库)。
func (s *Service) tickInterval() time.Duration {
	if v := s.intervalNsec.Load(); v > 0 {
		return time.Duration(v)
	}
	return defaultInterval
}

// refreshInterval 从 intervalFn 读取最新间隔并缓存到内存。只在 tick 边界调用
// (每个采集周期一次),从而把"读设置"的库查询从请求热路径移到后台低频路径。
func (s *Service) refreshInterval() time.Duration {
	d := defaultInterval
	if s.intervalFn != nil {
		if v := s.intervalFn(); v > 0 {
			d = v
		}
	}
	s.intervalNsec.Store(int64(d))
	return d
}

// StaleAfter 返回缓存被视为「过期需重采」的阈值:间隔的 4 倍(至少 2 分钟)。
// handler 用它在缓存命中但偏旧时回退实时采集,避免采集器异常后长期展示陈旧数据。
func (s *Service) StaleAfter() time.Duration {
	d := s.tickInterval()
	if d <= 0 {
		return 2 * time.Minute
	}
	return d * 4
}

// Get 读缓存(不判过期);缺失返回 (nil, false)。
func (s *Service) Get(kind, id string) (any, bool) {
	s.mu.RLock()
	e, ok := s.cache[s.key(kind, id)]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return e.data, true
}

// GetFresh 读缓存并判过期;缺失或超过 maxAge 返回 (nil, false)。maxAge<=0 表示不判过期。
func (s *Service) GetFresh(kind, id string, maxAge time.Duration) (any, bool) {
	s.mu.RLock()
	e, ok := s.cache[s.key(kind, id)]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if maxAge > 0 && time.Since(e.at) > maxAge {
		return nil, false
	}
	return e.data, true
}

// Put 写缓存(覆盖)。
func (s *Service) Put(kind, id string, data any) {
	if data == nil {
		return
	}
	s.mu.Lock()
	s.cache[s.key(kind, id)] = entry{data: data, at: time.Now()}
	s.mu.Unlock()
}

// CollectServer 同步采集单台单类并写入缓存(handler 懒填充 / refresh 用)。
// 返回采集到的数据;采集器不存在/未设 target/采集失败返回 (nil, false)。
func (s *Service) CollectServer(ctx context.Context, kind, id string) (any, bool) {
	c, ok := s.collectors[kind]
	if !ok || s.svc == nil {
		return nil, false
	}
	data, err := c(ctx, s.svc, id)
	if err != nil || data == nil {
		return nil, false
	}
	s.Put(kind, id, data)
	return data, true
}

// CollectAll 同步采集所有服务器所有类(供「立即刷新」用);返回即完成,不懒。
func (s *Service) CollectAll(ctx context.Context) {
	s.runOnce(ctx)
}

// Start 后台周期采集:启动即采一轮(非阻塞),之后按 intervalFn 节奏 ticker(每次 Reset 以应用新间隔)。
// ctx 取消即退出。采集器/ target 未就绪时静默空转(不报错)。
func (s *Service) Start(ctx context.Context) {
	go func() {
		s.collectOnce.Do(func() {
			// 首轮立即采(后台),让缓存尽快热起来;失败不影响启动。
			go s.runOnce(ctx)
		})
		// 首个周期前先读一次设置并缓存(此后请求路径不再查库)。
		ticker := time.NewTicker(s.refreshInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runOnce(ctx)
				// 间隔可能已被用户改 → 重读设置(库查询,仅每周期一次)并重置定时器。
				if iv := s.refreshInterval(); iv > 0 {
					ticker.Reset(iv)
				}
			}
		}
	}()
}

// runOnce 对所有服务器 × 所有类并发采集(有界并发 6),逐台独立:某台失败仅该台不更新。
func (s *Service) runOnce(ctx context.Context) {
	if s.svc == nil || len(s.collectors) == 0 {
		return
	}
	servers, err := s.svc.List(ctx)
	if err != nil {
		return
	}
	kinds := make([]string, 0, len(s.collectors))
	for k := range s.collectors {
		kinds = append(kinds, k)
	}
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, srv := range servers {
		for _, kind := range kinds {
			wg.Add(1)
			go func(id, kind string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				data, err := s.collectors[kind](ctx, s.svc, id)
				if err == nil && data != nil {
					s.Put(kind, id, data)
				}
			}(srv.ID, kind)
		}
	}
	wg.Wait()
}
