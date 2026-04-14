package parse

import (
	"fmt"
	"strings"

	"xlsgen/internal/model"
)

// IsStructDefinitionSheet 判断是否为「结构体定义」表：首行首列为 struct_def。
func IsStructDefinitionSheet(rows [][]string) bool {
	if len(rows) == 0 {
		return false
	}
	r0 := rows[0]
	if len(r0) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r0[0]), "struct_def")
}

// ParseStructDefinitions 解析结构体定义表（多块：struct_def 标题行 + 下一行为具体 struct 与字段）。
// known 为已注册的结构体名 -> 字段；解析过程中按出现顺序写入，供后续行引用已定义类型。
func ParseStructDefinitions(origin string, rows [][]string, known map[string][]model.Field, order *[]string) error {
	for i := 0; i < len(rows); i++ {
		if isEmptyRow(rows[i]) {
			continue
		}
		first := strings.TrimSpace(rows[i][0])
		if !strings.EqualFold(first, "struct_def") {
			return fmt.Errorf("%s: struct sheet row %d: expected first cell struct_def, got %q", origin, i+1, first)
		}
		if i+1 >= len(rows) {
			return fmt.Errorf("%s: struct sheet row %d: missing data row after struct_def", origin, i+1)
		}
		dataRow := rows[i+1]
		if isEmptyRow(dataRow) {
			return fmt.Errorf("%s: struct sheet row %d: empty data row after struct_def", origin, i+2)
		}
		rawStruct := strings.TrimSpace(dataRow[0])
		if rawStruct == "" {
			return fmt.Errorf("%s: struct sheet row %d: empty struct name", origin, i+2)
		}
		if strings.EqualFold(rawStruct, "struct_def") {
			return fmt.Errorf("%s: struct sheet row %d: invalid struct name %q", origin, i+2, rawStruct)
		}
		typeName := model.ExportName(rawStruct)
		if typeName == "" {
			return fmt.Errorf("%s: struct sheet row %d: bad struct name %q", origin, i+2, rawStruct)
		}
		if _, dup := known[typeName]; dup {
			return fmt.Errorf("%s: duplicate struct %q", origin, typeName)
		}
		fields, err := parseStructFieldRow(dataRow, typeName, known)
		if err != nil {
			return fmt.Errorf("%s row %d: %w", origin, i+2, err)
		}
		if len(fields) == 0 {
			return fmt.Errorf("%s: struct %q has no fields", origin, typeName)
		}
		known[typeName] = fields
		*order = append(*order, typeName)
		i++
	}
	return nil
}

func parseStructFieldRow(dataRow []string, structTypeName string, known map[string][]model.Field) ([]model.Field, error) {
	var fields []model.Field
	for colIdx := 1; colIdx < len(dataRow); colIdx++ {
		cell := strings.TrimSpace(dataRow[colIdx])
		if cell == "" {
			continue
		}
		lower := strings.ToLower(cell)
		if strings.Contains(lower, "#comment") || strings.Contains(lower, "#common") {
			continue
		}
		m := fieldRe.FindStringSubmatch(cell)
		if m == nil {
			return nil, fmt.Errorf("invalid field def %q at col %d", cell, colIdx+1)
		}
		rawName := m[1]
		rawType := m[2]
		if strings.ToLower(rawType) == "comment" || strings.ToLower(rawType) == "common" {
			continue
		}
		goType, ok := mapGoTypeExtended(rawType, known, structTypeName)
		if !ok {
			return nil, fmt.Errorf("unsupported type %q for field %q", rawType, rawName)
		}
		fields = append(fields, model.Field{
			RawName:  rawName,
			Name:     model.ExportName(rawName),
			RawType:  rawType,
			GoType:   goType,
			Col:      colIdx,
			Flag:     model.FieldFlagAll,
			Exported: true,
		})
	}
	return fields, nil
}
