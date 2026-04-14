// Package golang 从 schema 生成 Go 源码。
package golang

import (
	"strings"

	"xlsgen/internal/model"
)

func GenerateBundle(pkg, rootName string, customOrder []string, custom map[string][]model.Field, orderedTypeNames []string, schemas map[string][]model.Field) (string, error) {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")

	for _, typeName := range customOrder {
		fields := custom[typeName]
		b.WriteString("type ")
		b.WriteString(typeName)
		b.WriteString(" struct {\n")
		for _, f := range fields {
			b.WriteString("\t")
			b.WriteString(f.Name)
			b.WriteString(" ")
			b.WriteString(f.GoType)
			b.WriteString(" `json:\"")
			b.WriteString(f.RawName)
			b.WriteString("\"`\n")
		}
		b.WriteString("}\n\n")
	}

	b.WriteString("type ")
	b.WriteString(rootName)
	b.WriteString(" struct {\n")
	for _, typeName := range orderedTypeNames {
		fieldName := model.PluralizeTypeName(typeName)
		jsonKey := model.LowerFirst(fieldName)
		b.WriteString("\t")
		b.WriteString(fieldName)
		b.WriteString(" []")
		b.WriteString(typeName)
		b.WriteString(" `json:\"")
		b.WriteString(jsonKey)
		b.WriteString("\"`\n")
	}
	b.WriteString("}\n\n")

	for _, typeName := range orderedTypeNames {
		fields := schemas[typeName]
		b.WriteString("type ")
		b.WriteString(typeName)
		b.WriteString(" struct {\n")
		for _, f := range fields {
			b.WriteString("\t")
			b.WriteString(f.Name)
			b.WriteString(" ")
			b.WriteString(f.GoType)
			b.WriteString(" `json:\"")
			b.WriteString(f.RawName)
			b.WriteString("\"`\n")
		}
		b.WriteString("}\n\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n", nil
}
