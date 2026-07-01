package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

const (
	// 正向缓存默认 TTL(天),用户可通过 cache_ttl_days 覆盖。
	defaultCacheTTLDays = 7
	// 负向缓存(查不到的结果)固定短 TTL,避免冷门条目每次都重试上游。
	negativeCacheTTLSeconds = 7200 // 2h
)

// cacheEnvelope 统一包裹正/负缓存:NotFound 标记负向缓存,Data 承载正向结果。
type cacheEnvelope struct {
	NotFound bool            `json:"nf,omitempty"`
	Data     json.RawMessage `json:"d,omitempty"`
}

// cacheEnabled 读 enable_cache,默认开启。
func cacheEnabled() bool {
	return boolConfig("enable_cache", true)
}

// cacheTTLSeconds 读 cache_ttl_days 换算为秒,非法或 <=0 回退默认 7 天。
func cacheTTLSeconds() int64 {
	days := defaultCacheTTLDays
	if v, ok := pdk.GetConfig("cache_ttl_days"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			days = n
		}
	}
	return int64(days) * 24 * 3600
}

// normKey 归一化名称类缓存键片段(小写+去空白),让大小写/首尾空格不同的查询命中同一条。
func normKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// cached 是 client 各 API 方法的通用缓存包裹器。
//
//   - 缓存关闭时直接执行 fetch,不读写缓存。
//   - 命中正向缓存:反序列化到 out;命中负向缓存:返回 ErrNotFound。
//   - 未命中:执行 fetch。成功按 cacheTTLSeconds 存正向信封;
//     ErrNotFound 按 negativeCacheTTLSeconds 存负向信封;其他错误不缓存。
//
// out 必须是指针;fetch 负责把结果填入 out 指向的对象。
func (c *client) cached(key string, out any, fetch func() error) error {
	if !cacheEnabled() {
		return fetch()
	}

	if raw, exists, err := host.CacheGetString(key); err == nil && exists {
		var env cacheEnvelope
		if json.Unmarshal([]byte(raw), &env) == nil {
			if env.NotFound {
				return ErrNotFound
			}
			if json.Unmarshal(env.Data, out) == nil {
				return nil
			}
			// 反序列化失败(结构变更等)当作未命中,继续回源。
		}
	}

	err := fetch()
	switch {
	case err == nil:
		if data, mErr := json.Marshal(out); mErr == nil {
			cachePut(key, cacheEnvelope{Data: data}, cacheTTLSeconds())
		}
	case err == ErrNotFound:
		cachePut(key, cacheEnvelope{NotFound: true}, negativeCacheTTLSeconds)
	}
	return err
}

// cachePut 序列化信封并写入宿主缓存,失败仅记日志(缓存写入不应影响主流程)。
func cachePut(key string, env cacheEnvelope, ttl int64) {
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	if err := host.CacheSetString(key, string(data), ttl); err != nil {
		pdk.Log(pdk.LogWarn, "netease: 缓存写入失败 key="+key+" err="+err.Error())
	}
}
