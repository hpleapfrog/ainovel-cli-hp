package imp

import (
	"fmt"
	"regexp"
	"strings"
)

// envelopeTagRe 匹配 === TAG === 行（前后可有空白），不区分大小写。
var envelopeTagRe = regexp.MustCompile(`(?m)^\s*===\s*([A-Z_]+)\s*===\s*$`)

// parseTaggedEnvelope 把 `=== TAG ===\nbody...` 形式的多段输出解析成 map。
// key 为大写标签名，value 为对应段落（已 trim 首尾空白）。
// 出现重复标签时，后者覆盖前者。
func parseTaggedEnvelope(text string) map[string]string {
	matches := envelopeTagRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make(map[string]string, len(matches))
	for i, m := range matches {
		tag := strings.ToUpper(text[m[2]:m[3]])
		bodyStart := m[1]
		bodyEnd := len(text)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		out[tag] = strings.TrimSpace(text[bodyStart:bodyEnd])
	}
	return out
}

// requireTags 校验 envelope 必含给定标签且非空。
func requireTags(env map[string]string, tags ...string) error {
	var missing []string
	for _, t := range tags {
		if strings.TrimSpace(env[t]) == "" {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tags: %s", strings.Join(missing, ", "))
	}
	return nil
}

// extractJSONObject 从模型输出中截取首个平衡的 JSON 对象（容忍围栏/前后缀文本）。
// 与 arbiter.extractJSON 同款路径：主流程已统一到「结构化输出 + 机械校验」范式，
// === TAG === 信封仅作降级。
func extractJSONObject(raw string) string {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			switch {
			case escape:
				escape = false
			case c == '\\':
				escape = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}
