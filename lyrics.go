package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// metaLine 映射网易云混在 lrc 字段里的 JSON 元信息行,例如:
//
//	{"t":0,"c":[{"tx":"作词: "},{"tx":"宋冬野"}]}
//
// 这类行不是标准 LRC([mm:ss.xx]文本),Navidrome 的解析器会丢弃,
// 导致作词/作曲/编曲等信息丢失。
type metaLine struct {
	T int `json:"t"` // 毫秒时间戳
	C []struct {
		Tx string `json:"tx"` // 文本片段,需按序拼接
	} `json:"c"`
}

// normalizeLrc 清洗网易云 lrc 文本:把混入的 JSON 元信息行转换成标准
// LRC 行([mm:ss.xx]文本),其余行原样保留。这样作词/作曲/编曲信息
// 能被 Navidrome 正常解析显示。
func normalizeLrc(raw string) string {
	if !strings.Contains(raw, `"tx"`) {
		return raw // 无 JSON 行,快速返回
	}

	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"tx"`) {
			if converted, ok := convertMetaLine(trimmed); ok {
				out = append(out, converted)
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// convertMetaLine 把单行 JSON 元信息转成标准 LRC 行,失败时返回 false。
func convertMetaLine(line string) (string, bool) {
	var m metaLine
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return "", false
	}
	var sb strings.Builder
	for _, frag := range m.C {
		sb.WriteString(frag.Tx)
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return "", false
	}
	return formatLrcTimestamp(m.T) + text, true
}

// formatLrcTimestamp 把毫秒转成 LRC 时间标签 [mm:ss.xx]。
func formatLrcTimestamp(ms int) string {
	if ms < 0 {
		ms = 0
	}
	minutes := ms / 60000
	seconds := (ms % 60000) / 1000
	centis := (ms % 1000) / 10
	return fmt.Sprintf("[%02d:%02d.%02d]", minutes, seconds, centis)
}
