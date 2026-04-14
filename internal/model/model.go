// Package model 承载 xlsgen 各阶段共用的类型与命名规则。
package model

import "strings"

// ConfigTableSource 记录配置表所在工作簿（无扩展名 stem）与 sheet 标签，用于 GDScript 文件名等。
type ConfigTableSource struct {
	WorkbookStem string
	Sheet        string
}

type Orientation int

const (
	OrientationHorizontal Orientation = iota
	OrientationVertical
)

type FieldFlag int

const (
	FieldFlagAll FieldFlag = iota
	FieldFlagServer
	FieldFlagClient
	FieldFlagNone
)

type Field struct {
	RawName   string
	Name      string
	RawType   string
	GoType    string
	Col       int
	Flag      FieldFlag
	Exported  bool
	IsComment bool
}

type HeaderSpec struct {
	HeaderRows  int
	Orientation Orientation
	DefineRow   int // 1-based row number in sheet
}

func LowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func PluralizeTypeName(typeName string) string {
	if typeName == "" {
		return typeName
	}
	if strings.HasSuffix(typeName, "s") || strings.HasSuffix(typeName, "x") || strings.HasSuffix(typeName, "z") ||
		strings.HasSuffix(typeName, "ch") || strings.HasSuffix(typeName, "sh") {
		return typeName + "es"
	}
	return typeName + "s"
}

func ExportName(name string) string {
	if name == "" {
		return name
	}
	if !strings.ContainsAny(name, "_-") {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, "")
}
