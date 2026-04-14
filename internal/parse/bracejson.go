package parse

import (
	"fmt"
	"regexp"
	"strings"
)

// vec2DRowSep 匹配「一行 struct[] 结束」与「下一行开始」之间的分隔：}},{{（中间可有空格）。
var vec2DRowSep = regexp.MustCompile(`\}\}\s*,\s*\{\{`)

// bracesToStandardJSON 将表内「大括号层级」写法转为标准 JSON：数组层用 { }，JSON 对象仍用 { "key": ... }。
// 规则：在字符串外，若 { 后（跳过空白）为 " 则视为 JSON 对象起始；否则视为数组（输出为 [）。
// 对应闭合符 } 分别输出为 } 或 ]。
// 大括号须成对闭合；勿在单元格末尾多写未与 `{` 配对的 `}`。
//
// struct[][]（如 Vector[][]）有两种常见写法：
// 1) 最外层一对大括号包整块：{{{1,2,3},{4,5,6}},{{7,8,9}}} — 直接由 parseBraceArrayJSON + bracesToStandardJSON 解析；
// 2) 省略最外层、行间用 `}},{{`（中间可有空格）串联：{{1,2,3},{4,5,6}},{{7,8,9}} — 由 parseBraceStructMatrix2D 拆行后再分别解析。
// 写出的 all.json 仍为标准 JSON（encoding/json）。
func bracesToStandardJSON(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "null", nil
	}
	if s == "{}" {
		return "[]", nil
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	var stack []byte // 'o' object, 'a' array(from brace)
	inStr := false
	esc := false
	for i := 0; i < len(s); {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
				i++
				continue
			}
			if c == '\\' {
				esc = true
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		switch c {
		case '"':
			inStr = true
			b.WriteByte(c)
			i++
		case '{':
			j := i + 1
			for j < len(s) && isJSONWhitespace(s[j]) {
				j++
			}
			if j < len(s) && s[j] == '"' {
				b.WriteByte('{')
				stack = append(stack, 'o')
			} else if j < len(s) && s[j] == '}' {
				// 空组 {} → 空数组 []
				b.WriteString("[]")
				i = j + 1
			} else {
				b.WriteByte('[')
				stack = append(stack, 'a')
			}
			i++
		case '}':
			if len(stack) == 0 {
				return "", fmt.Errorf("多余的 '}'（位置 %d）", i)
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top == 'a' {
				b.WriteByte(']')
			} else {
				b.WriteByte('}')
			}
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	if len(stack) != 0 {
		return "", fmt.Errorf("未闭合的大括号/方括号结构")
	}
	return b.String(), nil
}

func isJSONWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// parseBraceStructMatrix2D 解析 struct[][] 单元格：多行用 `}},{{` 分隔；每行再按大括号规则解析为一维 struct[]。
func parseBraceStructMatrix2D(s string) ([][]any, error) {
	rest := strings.TrimSpace(s)
	var rows [][]any
	for {
		loc := vec2DRowSep.FindStringIndex(rest)
		if loc == nil {
			var last []any
			if err := parseBraceArrayJSON(rest, &last); err != nil {
				return nil, err
			}
			rows = append(rows, last)
			break
		}
		piece := strings.TrimSpace(rest[:loc[0]+2])
		var items []any
		if err := parseBraceArrayJSON(piece, &items); err != nil {
			return nil, fmt.Errorf("struct[][] 行片段: %w", err)
		}
		rows = append(rows, items)
		rest = "{{" + strings.TrimSpace(rest[loc[1]:])
	}
	return rows, nil
}
