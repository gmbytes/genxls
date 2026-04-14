// Package gdscript 从 schema 生成 Godot GDScript Resource 脚本。
package gdscript

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"xlsgen/internal/model"
)

func splitLevels(raw string) (inner string, levels int) {
	raw = strings.TrimSpace(raw)
	for strings.HasSuffix(raw, "[]") {
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "[]"))
		levels++
	}
	return raw, levels
}

func mapGDType(raw string, custom map[string][]model.Field) (string, bool) {
	inner, levels := splitLevels(raw)
	low := strings.ToLower(strings.TrimSpace(inner))
	switch low {
	case "int", "int32", "int64":
		if levels == 0 {
			return "int", true
		}
		if levels == 1 {
			return "Array[int]", true
		}
		if levels == 2 {
			return "Array", true
		}
	case "float", "float32", "float64":
		if levels == 0 {
			return "float", true
		}
		if levels == 1 {
			return "Array[float]", true
		}
		if levels == 2 {
			return "Array", true
		}
	case "bool":
		if levels == 0 {
			return "bool", true
		}
		if levels == 1 {
			return "Array[bool]", true
		}
	case "string":
		if levels == 0 {
			return "String", true
		}
		if levels == 1 {
			return "Array[String]", true
		}
	}
	tn := model.ExportName(inner)
	if custom != nil {
		if _, ok := custom[tn]; ok {
			// 结构体定义表中的类型与配置表行类型一致，GDScript class_name 均为 C 前缀
			g := gdStructClassName(tn)
			if levels == 0 {
				return g, true
			}
			if levels == 1 {
				return "Array[" + g + "]", true
			}
			if levels == 2 {
				return "Array", true
			}
		}
	}
	return "", false
}

func gdDefaultValue(gdType string) string {
	switch gdType {
	case "int", "float", "bool", "String":
		return ""
	case "Array[int]", "Array[float]", "Array[bool]", "Array[String]", "Array":
		return " = []"
	default:
		if strings.HasPrefix(gdType, "Array[") {
			return " = []"
		}
		return ""
	}
}

// gdConfigClassName 配置表行 / 容器等由策划表生成的 Resource 类名（C 前缀，降低与 Godot 内置类同名概率）。
func gdConfigClassName(typeName string) string {
	return "C" + typeName
}

// gdStructClassName 结构体定义表（如 0.struct.xlsx）中的类型在 GDScript 中的类名（与配置表同为 C 前缀）。
func gdStructClassName(typeName string) string {
	return gdConfigClassName(typeName)
}

func gdPluralClassName(typeName string) string {
	return gdConfigClassName(model.PluralizeTypeName(typeName))
}

func isAllDecimalDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// gdWorkbookFileSlug 由工作簿 stem（无扩展名，如 1.character）生成 GD 文件名用片段：首段纯数字则至少两位补零，其后各段小写并以 '.' 连接，如 1.character -> 01.character。
func gdWorkbookFileSlug(stem string) string {
	stem = strings.TrimSpace(stem)
	if stem == "" {
		return "config"
	}
	parts := strings.Split(stem, ".")
	var segs []string
	for _, p := range parts {
		if p != "" {
			segs = append(segs, p)
		}
	}
	if len(segs) == 0 {
		return "config"
	}
	first := segs[0]
	rest := segs[1:]
	head := strings.ToLower(first)
	if isAllDecimalDigits(first) {
		if v, err := strconv.Atoi(first); err == nil {
			head = fmt.Sprintf("%02d", v)
		}
	}
	if len(rest) == 0 {
		return head
	}
	lowRest := make([]string, len(rest))
	for i, r := range rest {
		lowRest[i] = strings.ToLower(r)
	}
	return head + "." + strings.Join(lowRest, ".")
}

// gdWorkbookNumericPrefix 从工作簿 stem（无扩展名，如 00.struct、1.character）取出用于 GD 文件名的数字段 xx：
// 优先首段为纯十进制数字（与 stem 中「点分」第一段一致）；否则取 stem 最前面的连续数字；都没有则 "00"。
// 数值按至少两位宽度补零（与旧规则一致），超过 99 不截断（如 100 -> "100"）。
func gdWorkbookNumericPrefix(stem string) string {
	stem = strings.TrimSpace(stem)
	if stem == "" {
		return "00"
	}
	parts := strings.Split(stem, ".")
	for _, p := range parts {
		if p == "" {
			continue
		}
		if isAllDecimalDigits(p) {
			if v, err := strconv.Atoi(p); err == nil {
				if v < 0 || v >= 100 {
					return strconv.Itoa(v)
				}
				return fmt.Sprintf("%02d", v)
			}
		}
		break
	}
	var lead strings.Builder
	for _, r := range stem {
		if r >= '0' && r <= '9' {
			lead.WriteRune(r)
			continue
		}
		break
	}
	s := lead.String()
	if s == "" {
		return "00"
	}
	if v, err := strconv.Atoi(s); err == nil {
		if v >= 100 || v < 0 {
			return strconv.Itoa(v)
		}
		return fmt.Sprintf("%02d", v)
	}
	return "00"
}

var gdInvalidFileNameRunes = strings.NewReplacer(
	`<`, "_",
	`>`, "_",
	`:`, "_",
	`"`, "_",
	`/`, "_",
	`\`, "_",
	`|`, "_",
	`?`, "_",
	`*`, "_",
)

// gdSheetFileSlug 将 xlsx 中的 sheet 标签转为 GD 文件名安全的小写 snake 片段（不含扩展名）。
func gdSheetFileSlug(sheet string) string {
	s := strings.TrimSpace(sheet)
	if s == "" {
		return "sheet"
	}
	s = gdInvalidFileNameRunes.Replace(s)
	s = strings.ReplaceAll(s, " ", "_")
	return toSnakeCase(s)
}

func gdConfigRowFileName(typeName string, src model.ConfigTableSource) string {
	sheetSlug := gdSheetFileSlug(src.Sheet)
	if strings.TrimSpace(src.WorkbookStem) == "" {
		return "c_00_" + sheetSlug + ".gd"
	}
	return "c_" + gdWorkbookNumericPrefix(src.WorkbookStem) + "_" + sheetSlug + ".gd"
}

func gdStructRowFileName(typeName string) string {
	return "d_" + toSnakeCase(typeName) + ".gd"
}

func gdPluralFileName(typeName string, src model.ConfigTableSource) string {
	pluralSnake := toSnakeCase(model.PluralizeTypeName(typeName))
	if strings.TrimSpace(src.WorkbookStem) == "" {
		return "c_00_" + pluralSnake + ".gd"
	}
	return "c_" + gdWorkbookNumericPrefix(src.WorkbookStem) + "_" + pluralSnake + ".gd"
}

func generateGDRowClass(typeName string, fields []model.Field, custom map[string][]model.Field, fromStructSheet bool) (string, error) {
	var b strings.Builder
	b.WriteString("# Auto-generated by xlsgen. DO NOT EDIT.\n")
	b.WriteString("extends Resource\n")
	b.WriteString("class_name ")
	if fromStructSheet {
		b.WriteString(gdStructClassName(typeName))
	} else {
		b.WriteString(gdConfigClassName(typeName))
	}
	b.WriteString("\n\n")

	cidFirst := reorderCidFirst(fields)
	for _, f := range cidFirst {
		gdType, ok := mapGDType(f.RawType, custom)
		if !ok {
			return "", fmt.Errorf("unsupported type %q for GDScript", f.RawType)
		}
		b.WriteString("@export var ")
		b.WriteString(toSnakeCase(f.Name))
		b.WriteString(": ")
		b.WriteString(gdType)
		b.WriteString(gdDefaultValue(gdType))
		b.WriteString("\n")
	}
	return b.String(), nil
}

func generateGDContainerClass(typeName string) string {
	var b strings.Builder
	b.WriteString("# Auto-generated by xlsgen. DO NOT EDIT.\n")
	b.WriteString("extends Resource\n")
	b.WriteString("class_name ")
	b.WriteString(gdPluralClassName(typeName))
	b.WriteString("\n\n")
	b.WriteString("@export var items: Array = []\n")
	return b.String()
}

func generateGDAllConfig(orderedTypeNames []string) string {
	var b strings.Builder
	b.WriteString("# Auto-generated by xlsgen. DO NOT EDIT.\n")
	b.WriteString("extends Resource\n")
	b.WriteString("class_name CAllConfig\n\n")

	for _, typeName := range orderedTypeNames {
		snakeKey := toSnakeCase(model.PluralizeTypeName(typeName))
		b.WriteString("@export var ")
		b.WriteString(snakeKey)
		b.WriteString(": Array = []\n")
	}
	return b.String()
}

func reorderCidFirst(fields []model.Field) []model.Field {
	result := make([]model.Field, 0, len(fields))
	for _, f := range fields {
		if f.RawName == "cid" {
			result = append([]model.Field{f}, result...)
		} else {
			result = append(result, f)
		}
	}
	return result
}

func toSnakeCase(s string) string {
	var buf strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := rune(s[i-1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					buf.WriteByte('_')
				}
			}
			buf.WriteRune(unicode.ToLower(r))
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// GenerateBundle 生成结构体定义表对应的 `d_*.gd`、配置表对应的 `c_*.gd` 与 `all_config.gd`（不含 Godot `.res` 导入脚本）。
// configSources 为各配置表类型名对应的工作簿 stem 与 sheet 名；配置行脚本文件名为 c_<工作簿数字前缀>_<sheet 名 snake>.gd，表容器为 c_<前缀>_<类型复数 snake>.gd。
func GenerateBundle(orderedTypeNames []string, schemas map[string][]model.Field, customOrder []string, custom map[string][]model.Field, configSources map[string]model.ConfigTableSource) (map[string]string, error) {
	files := make(map[string]string)

	for _, typeName := range customOrder {
		fields := custom[typeName]
		rowCode, err := generateGDRowClass(typeName, fields, custom, true)
		if err != nil {
			return nil, err
		}
		files[gdStructRowFileName(typeName)] = rowCode
	}

	for _, typeName := range orderedTypeNames {
		fields := schemas[typeName]
		src := configSources[typeName]

		rowCode, err := generateGDRowClass(typeName, fields, custom, false)
		if err != nil {
			return nil, err
		}
		files[gdConfigRowFileName(typeName, src)] = rowCode
		files[gdPluralFileName(typeName, src)] = generateGDContainerClass(typeName)
	}

	files["all_config.gd"] = generateGDAllConfig(orderedTypeNames)

	return files, nil
}
