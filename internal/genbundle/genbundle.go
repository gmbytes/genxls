// Package genbundle 将多份 Excel/TSV 输入聚合为与 xlsgen 主程序一致的解析结果，供主程序与测试复用。
package genbundle

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"

	"xlsgen/internal/model"
	"xlsgen/internal/parse"
)

// Result 与 xlsgen 主程序解析阶段一致：JSON 载荷与 Go/GD 生成所需模式信息。
type Result struct {
	JSONPayload        map[string]any
	CustomStructs      map[string][]model.Field
	CustomOrder        []string
	Schemas            map[string][]model.Field
	OrderedTypeNames   []string
	ConfigTableSources map[string]model.ConfigTableSource // 配置表类型名 -> 工作簿 stem 与 sheet 名（GD 文件名用）
}

// Build 扫描 inPaths 中所有文件，解析结构体表与数据表。
func Build(inPaths []string, exportFlag string) (*Result, error) {
	customStructs := make(map[string][]model.Field)
	var customOrder []string
	schemas := make(map[string][]model.Field)
	jsonPayload := make(map[string]any)
	seenKeys := make(map[string]string)
	var orderedTypeNames []string
	configTableSources := make(map[string]model.ConfigTableSource)

	// 与 main 一致：仅当文件可作为 Excel 打开时才扫描结构体表（TSV 等跳过）。
	for _, p := range inPaths {
		f, err := excelize.OpenFile(p)
		if err != nil {
			continue
		}
		err = func() error {
			defer func() { _ = f.Close() }()
			for _, sheet := range f.GetSheetList() {
				rows, err := f.GetRows(sheet)
				if err != nil {
					return fmt.Errorf("%s[%s]: %w", p, sheet, err)
				}
				if !parse.IsStructDefinitionSheet(rows) {
					continue
				}
				origin := fmt.Sprintf("%s[%s]", p, sheet)
				if err := parse.ParseStructDefinitions(origin, rows, customStructs, &customOrder); err != nil {
					return err
				}
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}

	addSheet := func(workbookPath, sheetName string, rows [][]string) error {
		origin := fmt.Sprintf("%s[%s]", workbookPath, sheetName)
		if parse.IsStructDefinitionSheet(rows) {
			return nil
		}
		if parse.SheetHasNoContent(rows) {
			return nil
		}
		spec, err := parse.DetectHeaderSpec(rows)
		if err != nil {
			return fmt.Errorf("%s: %w", origin, err)
		}
		if spec.Orientation == model.OrientationVertical {
			return fmt.Errorf("%s: vertical orientation (A1=2) is not supported yet", origin)
		}
		fields, err := parse.ParseFieldsFromDefineRowWithCustom(rows, spec.DefineRow, exportFlag, customStructs)
		if err != nil {
			return fmt.Errorf("%s: %w", origin, err)
		}
		hasCid := false
		for _, f := range fields {
			if f.RawName == "cid" {
				hasCid = true
				break
			}
		}
		if !hasCid {
			return fmt.Errorf("%s: sheet missing required field 'cid#int' (unique key)", origin)
		}
		items, err := parse.ReadHorizontalItems(rows, spec.DefineRow+1, fields, customStructs)
		if err != nil {
			return fmt.Errorf("%s: %w", origin, err)
		}

		typeName := model.ExportName(sheetName)
		if typeName == "" {
			return fmt.Errorf("%s: empty sheet name", origin)
		}
		if _, dup := customStructs[typeName]; dup {
			return fmt.Errorf("%s: sheet type %q conflicts with struct definition of the same name", origin, typeName)
		}
		fieldName := model.PluralizeTypeName(typeName)
		jsonKey := model.LowerFirst(fieldName)
		if prev, ok := seenKeys[jsonKey]; ok {
			return fmt.Errorf("duplicate sheet key %q from %s (already used by %s)", jsonKey, origin, prev)
		}
		seenKeys[jsonKey] = origin
		schemas[typeName] = fields
		jsonPayload[jsonKey] = items
		orderedTypeNames = append(orderedTypeNames, typeName)
		stem := strings.TrimSuffix(filepath.Base(workbookPath), filepath.Ext(workbookPath))
		configTableSources[typeName] = model.ConfigTableSource{WorkbookStem: stem, Sheet: sheetName}
		return nil
	}

	for _, p := range inPaths {
		f, err := excelize.OpenFile(p)
		if err == nil {
			err = func() error {
				defer func() { _ = f.Close() }()
				sheets := f.GetSheetList()
				if len(sheets) == 0 {
					return fmt.Errorf("%s: xlsx has no sheets", p)
				}
				for _, sheet := range sheets {
					rows, err := f.GetRows(sheet)
					if err != nil {
						return fmt.Errorf("%s[%s]: %w", p, sheet, err)
					}
					if err := addSheet(p, sheet, rows); err != nil {
						return err
					}
				}
				return nil
			}()
			if err != nil {
				return nil, err
			}
			continue
		}

		rows, err2 := parse.ReadTSVRows(p)
		if err2 != nil {
			return nil, fmt.Errorf("%s: %w", p, err2)
		}
		sheet := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if err := addSheet(p, sheet, rows); err != nil {
			return nil, err
		}
	}

	return &Result{
		JSONPayload:        jsonPayload,
		CustomStructs:      customStructs,
		CustomOrder:        customOrder,
		Schemas:            schemas,
		OrderedTypeNames:   orderedTypeNames,
		ConfigTableSources: configTableSources,
	}, nil
}
