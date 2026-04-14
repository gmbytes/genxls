// Package parse 负责输入路径解析、表头/字段解析与行数据读取。
package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"xlsgen/internal/model"
)

func ResolveInputPaths(in string) ([]string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return nil, errors.New("empty --in")
	}
	if st, err := os.Stat(in); err == nil {
		if st.IsDir() {
			return listExcelFiles(in)
		}
		return []string{in}, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	candidate := filepath.Join(wd, "xls", filepath.Base(in))
	if st, err := os.Stat(candidate); err == nil {
		if st.IsDir() {
			return listExcelFiles(candidate)
		}
		return []string{candidate}, nil
	}
	return nil, fmt.Errorf("input file not found: %s (also tried %s)", in, candidate)
}

func listExcelFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "~$") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".xlsx" && ext != ".xls" {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no .xls/.xlsx files in %s", dir)
	}
	return out, nil
}

func ReadTSVRows(path string) ([][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	var rows [][]string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s: empty file", path)
	}
	return rows, nil
}

func DetectHeaderSpec(rows [][]string) (model.HeaderSpec, error) {
	if len(rows) >= 3 && rowHasFieldDefs(rows[2]) {
		ori := model.OrientationHorizontal
		a1 := ""
		if len(rows[0]) > 0 {
			a1 = strings.TrimSpace(rows[0][0])
		}
		if a1 == "2" {
			ori = model.OrientationVertical
		}
		return model.HeaderSpec{HeaderRows: 3, Orientation: ori, DefineRow: 3}, nil
	}
	if len(rows) >= 2 && rowHasFieldDefs(rows[1]) {
		return model.HeaderSpec{HeaderRows: 2, Orientation: model.OrientationHorizontal, DefineRow: 2}, nil
	}
	if len(rows) >= 1 && rowHasFieldDefs(rows[0]) {
		return model.HeaderSpec{HeaderRows: 1, Orientation: model.OrientationHorizontal, DefineRow: 1}, nil
	}
	return model.HeaderSpec{}, errors.New("cannot detect header")
}

func rowHasFieldDefs(row []string) bool {
	for _, c := range row {
		if strings.Contains(c, "#") {
			return true
		}
	}
	return false
}

var fieldRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*#\s*([^,\s]+)\s*(?:,\s*([sc]))?\s*$`)

func ParseFieldsFromDefineRow(rows [][]string, defineRow int, exportFlag string) ([]model.Field, error) {
	return ParseFieldsFromDefineRowWithCustom(rows, defineRow, exportFlag, nil)
}

// ParseFieldsFromDefineRowWithCustom 与 ParseFieldsFromDefineRow 相同，但允许 rawType 使用 known 中已定义的结构体名。
func ParseFieldsFromDefineRowWithCustom(rows [][]string, defineRow int, exportFlag string, known map[string][]model.Field) ([]model.Field, error) {
	if defineRow <= 0 || defineRow > len(rows) {
		return nil, fmt.Errorf("define row %d out of range", defineRow)
	}
	row := rows[defineRow-1]
	var fields []model.Field
	for colIdx, cell := range row {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			continue
		}
		lower := strings.ToLower(cell)
		if strings.Contains(lower, "#comment") || strings.Contains(lower, "#common") {
			continue
		}
		m := fieldRe.FindStringSubmatch(cell)
		if m == nil {
			return nil, fmt.Errorf("invalid field def %q at row %d", cell, defineRow)
		}
		rawName := m[1]
		rawType := m[2]
		if strings.ToLower(rawType) == "comment" || strings.ToLower(rawType) == "common" {
			continue
		}
		flagCh := m[3]

		ff := model.FieldFlagAll
		switch flagCh {
		case "":
			ff = model.FieldFlagAll
		case "s":
			ff = model.FieldFlagServer
		case "c":
			ff = model.FieldFlagClient
		default:
			ff = model.FieldFlagAll
		}

		if exportFlag != "" {
			switch exportFlag {
			case "server":
				if ff == model.FieldFlagClient {
					continue
				}
			case "client":
				if ff == model.FieldFlagServer {
					continue
				}
			default:
				return nil, fmt.Errorf("invalid --flag %q (expect server|client)", exportFlag)
			}
		}

		goType, ok := mapGoTypeExtended(rawType, known, "")
		if !ok {
			return nil, fmt.Errorf("unsupported type %q", rawType)
		}
		fields = append(fields, model.Field{
			RawName:  rawName,
			Name:     model.ExportName(rawName),
			RawType:  rawType,
			GoType:   goType,
			Col:      colIdx,
			Flag:     ff,
			Exported: true,
		})
	}
	if len(fields) == 0 {
		return nil, errors.New("no exported fields found")
	}
	return fields, nil
}

func splitArrayLevels(raw string) (inner string, levels int) {
	raw = strings.TrimSpace(raw)
	for strings.HasSuffix(raw, "[]") {
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "[]"))
		levels++
	}
	return raw, levels
}

func primitiveGoType(inner string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(inner)) {
	case "int", "int32":
		return "int", true
	case "int64":
		return "int64", true
	case "int[]", "int[][]":
		return "", false
	case "float", "float32", "float64":
		return "float64", true
	case "bool":
		return "bool", true
	case "string":
		return "string", true
	default:
		return "", false
	}
}

func wrapGoSlice(base string, levels int) string {
	s := base
	for i := 0; i < levels; i++ {
		s = "[]" + s
	}
	return s
}

// mapGoTypeExtended 解析表头/结构体字段类型。known 为已定义的结构体（PascalCase）；definingStruct 非空时禁止自引用。
func mapGoTypeExtended(rawType string, known map[string][]model.Field, definingStruct string) (string, bool) {
	inner, levels := splitArrayLevels(rawType)
	if inner == "" && levels > 0 {
		return "", false
	}
	if g, ok := primitiveGoType(inner); ok {
		return wrapGoSlice(g, levels), true
	}
	tn := model.ExportName(inner)
	if tn == "" {
		return "", false
	}
	if definingStruct != "" && tn == definingStruct {
		return "", false
	}
	if known == nil {
		return "", false
	}
	if _, ok := known[tn]; !ok {
		return "", false
	}
	return wrapGoSlice(tn, levels), true
}

func ReadHorizontalItems(rows [][]string, dataStartRow int, fields []model.Field, custom map[string][]model.Field) ([]map[string]any, error) {
	if dataStartRow <= 0 {
		dataStartRow = 1
	}
	var items []map[string]any
	for r := dataStartRow - 1; r < len(rows); r++ {
		row := rows[r]
		if isEmptyRow(row) {
			continue
		}
		obj := make(map[string]any, len(fields))
		for _, field := range fields {
			cell := ""
			if field.Col >= 0 && field.Col < len(row) {
				cell = strings.TrimSpace(row[field.Col])
			}
			v, err := parseCellValue(field.RawType, cell, custom)
			if err != nil {
				return nil, fmt.Errorf("row %d col %d (%s): %w", r+1, field.Col+1, field.RawName, err)
			}
			obj[field.RawName] = v
		}
		items = append(items, obj)
	}
	return items, nil
}

func isEmptyRow(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// SheetHasNoContent 表示整张表无非空单元格（用于跳过 Excel/WPS 默认的空 Sheet2 等）。
func SheetHasNoContent(rows [][]string) bool {
	if len(rows) == 0 {
		return true
	}
	for _, row := range rows {
		if !isEmptyRow(row) {
			return false
		}
	}
	return true
}

// isEmptyCompositeCellMarker 表内「空复合类型」约定：统一写 `{}`（兼容旧表 `[]` 仅表示空数组）。
func isEmptyCompositeCellMarker(s string) bool {
	switch strings.TrimSpace(s) {
	case "{}", "[]":
		return true
	default:
		return false
	}
}

// rawTypeSupportsEmptyBraceMarker 为 true 时，`{}` / `[]` 与空单元格等价，走 zeroForRawType（数组、二维数组、自定义结构体及其切片）。
func rawTypeSupportsEmptyBraceMarker(rawType string, custom map[string][]model.Field) bool {
	inner, levels := splitArrayLevels(rawType)
	if _, ok := primitiveGoType(inner); ok {
		return levels >= 1 && levels <= 2
	}
	tn := model.ExportName(inner)
	if custom == nil {
		return false
	}
	if _, ok := custom[tn]; !ok {
		return false
	}
	return levels <= 2
}

func parseCellValue(rawType string, s string, custom map[string][]model.Field) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return zeroForRawType(rawType, custom)
	}
	if isEmptyCompositeCellMarker(s) && rawTypeSupportsEmptyBraceMarker(rawType, custom) {
		return zeroForRawType(rawType, custom)
	}
	inner, levels := splitArrayLevels(rawType)
	if _, ok := primitiveGoType(inner); ok {
		return parsePrimitiveFromString(inner, levels, s)
	}
	tn := model.ExportName(inner)
	if custom == nil {
		return nil, fmt.Errorf("unsupported type %q", rawType)
	}
	if _, ok := custom[tn]; !ok {
		return nil, fmt.Errorf("unsupported type %q", rawType)
	}
	if levels == 0 {
		return parseCustomStructFromString(tn, s, custom)
	}
	return parseCustomSliceFromString(tn, levels, s, custom)
}

func zeroForRawType(rawType string, custom map[string][]model.Field) (any, error) {
	inner, levels := splitArrayLevels(rawType)
	if g, ok := primitiveGoType(inner); ok {
		switch levels {
		case 0:
			switch g {
			case "int":
				return 0, nil
			case "int64":
				return int64(0), nil
			case "float64":
				return float64(0), nil
			case "bool":
				return false, nil
			case "string":
				return "", nil
			}
		case 1:
			if g == "int" {
				return []int{}, nil
			}
			if g == "int64" {
				return []int64{}, nil
			}
			if g == "float64" {
				return []float64{}, nil
			}
		case 2:
			if g == "int" {
				return [][]int{}, nil
			}
			if g == "int64" {
				return [][]int64{}, nil
			}
			if g == "float64" {
				return [][]float64{}, nil
			}
		}
		return nil, fmt.Errorf("unsupported primitive shape %q", rawType)
	}
	tn := model.ExportName(inner)
	if custom == nil {
		return nil, fmt.Errorf("unsupported type %q", rawType)
	}
	if _, ok := custom[tn]; !ok {
		return nil, fmt.Errorf("unsupported type %q", rawType)
	}
	switch levels {
	case 0:
		return zeroStructMap(tn, custom)
	case 1:
		return []map[string]any{}, nil
	case 2:
		return [][]map[string]any{}, nil
	}
	return nil, fmt.Errorf("unsupported custom shape %q", rawType)
}

func zeroStructMap(tn string, custom map[string][]model.Field) (map[string]any, error) {
	sch := custom[tn]
	out := make(map[string]any, len(sch))
	for _, f := range sch {
		z, err := zeroForRawType(f.RawType, custom)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", tn, f.RawName, err)
		}
		out[f.RawName] = z
	}
	return out, nil
}

func parsePrimitiveFromString(inner string, levels int, s string) (any, error) {
	inner = strings.TrimSpace(inner)
	low := strings.ToLower(inner)
	if levels == 0 {
		switch low {
		case "int", "int32":
			v, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return nil, err
			}
			return v, nil
		case "int64":
			v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return nil, err
			}
			return v, nil
		case "float", "float32", "float64":
			v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil, err
			}
			return v, nil
		case "bool":
			ls := strings.ToLower(strings.TrimSpace(s))
			if ls == "1" {
				return true, nil
			}
			if ls == "0" {
				return false, nil
			}
			return strconv.ParseBool(ls)
		case "string":
			return s, nil
		}
		return nil, fmt.Errorf("unsupported primitive %q", inner)
	}
	if low == "int" || low == "int32" {
		if levels == 1 {
			var v []int
			if err := parseBraceArrayJSON(s, &v); err != nil {
				return nil, err
			}
			return v, nil
		}
		var v [][]int
		if err := parseBraceArrayJSON(s, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	if low == "int64" {
		if levels == 1 {
			var v []int64
			if err := parseJSONOrBraceNumberArray(s, &v); err != nil {
				return nil, err
			}
			return v, nil
		}
		var v [][]int64
		if err := parseJSONOrBrace2DNumberArray(s, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	if low == "float" || low == "float32" || low == "float64" {
		if levels == 1 {
			var v []float64
			if err := parseJSONOrBraceFloatArray(s, &v); err != nil {
				return nil, err
			}
			return v, nil
		}
		var v [][]float64
		if err := parseJSONOrBrace2DFloatArray(s, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	return nil, fmt.Errorf("unsupported primitive %q levels %d", inner, levels)
}

func parseJSONOrBraceNumberArray(s string, out *[]int64) error {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	if err := json.Unmarshal([]byte(s), out); err == nil {
		return nil
	}
	var tmp []int
	if err := parseBraceArrayJSON(s, &tmp); err != nil {
		return err
	}
	*out = make([]int64, len(tmp))
	for i, x := range tmp {
		(*out)[i] = int64(x)
	}
	return nil
}

func parseJSONOrBrace2DNumberArray(s string, out *[][]int64) error {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	if err := json.Unmarshal([]byte(s), out); err == nil {
		return nil
	}
	var tmp [][]int
	if err := parseBraceArrayJSON(s, &tmp); err != nil {
		return err
	}
	*out = make([][]int64, len(tmp))
	for i, row := range tmp {
		(*out)[i] = make([]int64, len(row))
		for j, x := range row {
			(*out)[i][j] = int64(x)
		}
	}
	return nil
}

func parseJSONOrBraceFloatArray(s string, out *[]float64) error {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	if err := json.Unmarshal([]byte(s), out); err == nil {
		return nil
	}
	return parseBraceArrayJSON(s, out)
}

func parseJSONOrBrace2DFloatArray(s string, out *[][]float64) error {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	if err := json.Unmarshal([]byte(s), out); err == nil {
		return nil
	}
	return parseBraceArrayJSON(s, out)
}

func parseCustomStructFromString(tn string, s string, custom map[string][]model.Field) (map[string]any, error) {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	var raw map[string]any
	jsonErr := json.Unmarshal([]byte(s), &raw)
	if jsonErr == nil {
		return fillStructFromMap(tn, raw, custom)
	}
	// 花括号元组：{1,2,3}，按结构体定义表中的字段顺序对应（与 Vector[] 的 parseBraceArrayJSON 一致）。
	var elems []any
	if err := parseBraceArrayJSON(s, &elems); err == nil {
		m, err2 := fillStructFromTuple(tn, elems, custom)
		if err2 == nil {
			return m, nil
		}
		return nil, fmt.Errorf("struct %s tuple: %w", tn, err2)
	}
	return nil, fmt.Errorf("struct %s json: %w", tn, jsonErr)
}

// fillStructFromTuple 将一列标量按 custom[tn] 中的字段顺序写入结构体（用于 Excel 中的 {x,y,z} 写法）。
func fillStructFromTuple(tn string, elems []any, custom map[string][]model.Field) (map[string]any, error) {
	sch := custom[tn]
	if len(elems) != len(sch) {
		return nil, fmt.Errorf("tuple length %d != struct %s field count %d", len(elems), tn, len(sch))
	}
	out := make(map[string]any, len(sch))
	for i, f := range sch {
		nv, err := coerceJSONValue(f.RawType, elems[i], custom)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", tn, f.RawName, err)
		}
		out[f.RawName] = nv
	}
	return out, nil
}

func fillStructFromMap(tn string, raw map[string]any, custom map[string][]model.Field) (map[string]any, error) {
	sch := custom[tn]
	if elems, ok := mapAsNumericKeyTuple(sch, raw); ok {
		return fillStructFromTuple(tn, elems, custom)
	}
	out := make(map[string]any, len(sch))
	for _, f := range sch {
		v, ok := raw[f.RawName]
		if !ok {
			z, err := zeroForRawType(f.RawType, custom)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", tn, f.RawName, err)
			}
			out[f.RawName] = z
			continue
		}
		nv, err := coerceJSONValue(f.RawType, v, custom)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", tn, f.RawName, err)
		}
		out[f.RawName] = nv
	}
	return out, nil
}

// mapAsNumericKeyTuple：当 JSON 对象键均为 "0".."n-1" 且与结构体字段数一致、且未出现任何正式字段名时，
// 按数字顺序把值当作元组对齐到结构体定义顺序（用于无字段名的弱对象表示）。
func mapAsNumericKeyTuple(sch []model.Field, raw map[string]any) ([]any, bool) {
	if len(raw) == 0 || len(raw) != len(sch) {
		return nil, false
	}
	schName := make(map[string]struct{}, len(sch))
	for _, f := range sch {
		schName[f.RawName] = struct{}{}
	}
	for k := range raw {
		if _, hit := schName[k]; hit {
			return nil, false
		}
	}
	type kv struct {
		idx int
		v   any
	}
	kvs := make([]kv, 0, len(raw))
	for k, v := range raw {
		i, err := strconv.Atoi(k)
		if err != nil {
			return nil, false
		}
		kvs = append(kvs, kv{i, v})
	}
	sort.Slice(kvs, func(a, b int) bool { return kvs[a].idx < kvs[b].idx })
	for i := range kvs {
		if kvs[i].idx != i {
			return nil, false
		}
	}
	out := make([]any, len(kvs))
	for i := range kvs {
		out[i] = kvs[i].v
	}
	return out, true
}

func coerceJSONValue(rawType string, v any, custom map[string][]model.Field) (any, error) {
	inner, levels := splitArrayLevels(rawType)
	if _, ok := primitiveGoType(inner); ok {
		return coercePrimitiveJSON(inner, levels, v)
	}
	tn := model.ExportName(inner)
	if _, ok := custom[tn]; !ok {
		return nil, fmt.Errorf("unknown type %q", rawType)
	}
	if levels == 0 {
		m, err := asStringKeyedMap(v)
		if err != nil {
			return nil, err
		}
		return fillStructFromMap(tn, m, custom)
	}
	if levels == 1 {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected array for %q", rawType)
		}
		out := make([]map[string]any, 0, len(arr))
		for _, el := range arr {
			sm, err := coerceSingleStructElement(tn, el, custom)
			if err != nil {
				return nil, err
			}
			out = append(out, sm)
		}
		return out, nil
	}
	if levels == 2 {
		top, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected 2d array for %q", rawType)
		}
		out := make([][]map[string]any, 0, len(top))
		for _, row := range top {
			sub, ok := row.([]any)
			if !ok {
				return nil, fmt.Errorf("expected 2d array for %q", rawType)
			}
			innerRow := make([]map[string]any, 0, len(sub))
			for _, el := range sub {
				sm, err := coerceSingleStructElement(tn, el, custom)
				if err != nil {
					return nil, err
				}
				innerRow = append(innerRow, sm)
			}
			out = append(out, innerRow)
		}
		return out, nil
	}
	return nil, fmt.Errorf("levels %d for %q", levels, rawType)
}

const maxStructCoerceDepth = 16

func coerceSingleStructElement(tn string, it any, custom map[string][]model.Field) (map[string]any, error) {
	return coerceSingleStructElementDepth(tn, it, custom, 0)
}

func coerceSingleStructElementDepth(tn string, it any, custom map[string][]model.Field, depth int) (map[string]any, error) {
	if depth > maxStructCoerceDepth {
		return nil, fmt.Errorf("%s: 结构体元素展开层数超过上限", tn)
	}
	if it == nil {
		return zeroStructMap(tn, custom)
	}
	if arr, ok := it.([]any); ok {
		if looksLikeScalarTuple(arr) {
			return fillStructFromTuple(tn, arr, custom)
		}
		// 大括号转 JSON 时可能多包一层数组（如 [[{...}]]），逐层剥掉单元素数组再解析
		if len(arr) == 1 {
			return coerceSingleStructElementDepth(tn, arr[0], custom, depth+1)
		}
		return nil, fmt.Errorf("expected object map or 标量元组 for %s, got %T", tn, it)
	}
	m, err := asStringKeyedMap(it)
	if err != nil {
		return nil, err
	}
	return fillStructFromMap(tn, m, custom)
}

// looksLikeScalarTuple 判断是否为 {a,b,c} 解析出的一维标量列表（而非 JSON 对象数组）。
func looksLikeScalarTuple(arr []any) bool {
	if len(arr) == 0 {
		return false
	}
	for _, el := range arr {
		switch el.(type) {
		case map[string]any:
			return false
		case []any:
			return false
		}
	}
	return true
}

func asStringKeyedMap(v any) (map[string]any, error) {
	switch t := v.(type) {
	case map[string]any:
		return t, nil
	default:
		return nil, fmt.Errorf("expected object map, got %T", v)
	}
}

func coercePrimitiveJSON(inner string, levels int, v any) (any, error) {
	low := strings.ToLower(strings.TrimSpace(inner))
	if levels == 0 {
		switch low {
		case "int", "int32":
			return jsonToInt(v)
		case "int64":
			return jsonToInt64(v)
		case "float", "float32", "float64":
			return jsonToFloat64(v)
		case "bool":
			return jsonToBool(v)
		case "string":
			switch t := v.(type) {
			case string:
				return t, nil
			case fmt.Stringer:
				return t.String(), nil
			default:
				return fmt.Sprint(v), nil
			}
		}
		return nil, fmt.Errorf("unsupported primitive %q", inner)
	}
	if levels == 1 {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected array")
		}
		switch low {
		case "int", "int32":
			out := make([]int, 0, len(arr))
			for _, el := range arr {
				x, err := jsonToInt(el)
				if err != nil {
					return nil, err
				}
				out = append(out, x)
			}
			return out, nil
		case "int64":
			out := make([]int64, 0, len(arr))
			for _, el := range arr {
				x, err := jsonToInt64(el)
				if err != nil {
					return nil, err
				}
				out = append(out, x)
			}
			return out, nil
		case "float", "float32", "float64":
			out := make([]float64, 0, len(arr))
			for _, el := range arr {
				x, err := jsonToFloat64(el)
				if err != nil {
					return nil, err
				}
				out = append(out, x)
			}
			return out, nil
		case "bool":
			out := make([]bool, 0, len(arr))
			for _, el := range arr {
				x, err := jsonToBool(el)
				if err != nil {
					return nil, err
				}
				out = append(out, x)
			}
			return out, nil
		case "string":
			out := make([]string, 0, len(arr))
			for _, el := range arr {
				s, ok := el.(string)
				if !ok {
					s = fmt.Sprint(el)
				}
				out = append(out, s)
			}
			return out, nil
		}
	}
	if levels == 2 && (low == "int" || low == "int32") {
		top, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected 2d array")
		}
		out := make([][]int, 0, len(top))
		for _, row := range top {
			sub, ok := row.([]any)
			if !ok {
				return nil, fmt.Errorf("expected 2d array")
			}
			line := make([]int, 0, len(sub))
			for _, el := range sub {
				x, err := jsonToInt(el)
				if err != nil {
					return nil, err
				}
				line = append(line, x)
			}
			out = append(out, line)
		}
		return out, nil
	}
	if levels == 2 && low == "int64" {
		top, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected 2d array")
		}
		out := make([][]int64, 0, len(top))
		for _, row := range top {
			sub, ok := row.([]any)
			if !ok {
				return nil, fmt.Errorf("expected 2d array")
			}
			line := make([]int64, 0, len(sub))
			for _, el := range sub {
				x, err := jsonToInt64(el)
				if err != nil {
					return nil, err
				}
				line = append(line, x)
			}
			out = append(out, line)
		}
		return out, nil
	}
	if levels == 2 && (low == "float" || low == "float32" || low == "float64") {
		top, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("expected 2d array")
		}
		out := make([][]float64, 0, len(top))
		for _, row := range top {
			sub, ok := row.([]any)
			if !ok {
				return nil, fmt.Errorf("expected 2d array")
			}
			line := make([]float64, 0, len(sub))
			for _, el := range sub {
				x, err := jsonToFloat64(el)
				if err != nil {
					return nil, err
				}
				line = append(line, x)
			}
			out = append(out, line)
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported primitive %q levels %d", inner, levels)
}

func jsonToInt(v any) (int, error) {
	switch t := v.(type) {
	case int:
		return t, nil
	case int32:
		return int(t), nil
	case int64:
		return int(t), nil
	case uint:
		return int(t), nil
	case uint32:
		return int(t), nil
	case uint64:
		return int(t), nil
	case float64:
		return int(t), nil
	case json.Number:
		i, err := t.Int64()
		return int(i), err
	case string:
		return strconv.Atoi(t)
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

func jsonToInt64(v any) (int64, error) {
	switch t := v.(type) {
	case int:
		return int64(t), nil
	case int32:
		return int64(t), nil
	case int64:
		return t, nil
	case uint:
		return int64(t), nil
	case uint32:
		return int64(t), nil
	case uint64:
		return int64(t), nil
	case float64:
		return int64(t), nil
	case json.Number:
		return t.Int64()
	case string:
		return strconv.ParseInt(t, 10, 64)
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

func jsonToFloat64(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case json.Number:
		return t.Float64()
	case string:
		return strconv.ParseFloat(t, 64)
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

func jsonToBool(v any) (bool, error) {
	switch t := v.(type) {
	case bool:
		return t, nil
	case float64:
		return t != 0, nil
	case string:
		ls := strings.ToLower(strings.TrimSpace(t))
		if ls == "1" {
			return true, nil
		}
		if ls == "0" {
			return false, nil
		}
		return strconv.ParseBool(ls)
	default:
		return false, fmt.Errorf("expected bool, got %T", v)
	}
}

func parseCustomSliceFromString(tn string, levels int, s string, custom map[string][]model.Field) (any, error) {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	if levels == 1 {
		var items []any
		if err := json.Unmarshal([]byte(s), &items); err != nil {
			if err2 := parseBraceArrayJSON(s, &items); err2 != nil {
				return nil, fmt.Errorf("struct %s[]: %w", tn, err)
			}
		}
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			sm, err := coerceSingleStructElement(tn, it, custom)
			if err != nil {
				return nil, err
			}
			out = append(out, sm)
		}
		return out, nil
	}
	if levels == 2 {
		var rows [][]any
		if err := json.Unmarshal([]byte(s), &rows); err != nil {
			// 先整体解析：策划表常用「最外层再包一对大括号」的写法，如
			// Vector[][] = {{{1,2,3},{4,5,6}},{{7,8,9}}}，其中含子串 `}},{{`；
			// 若优先按 `}},{{` 拆行会得到错误片段，故必须在矩阵拆行之前尝试 parseBraceArrayJSON。
			var err2 error
			if err2 = parseBraceArrayJSON(s, &rows); err2 != nil && vec2DRowSep.MatchString(s) {
				rows, err2 = parseBraceStructMatrix2D(s)
			}
			if err2 != nil {
				return nil, fmt.Errorf("struct %s[][]: %w", tn, err2)
			}
		}
		out := make([][]map[string]any, 0, len(rows))
		for _, row := range rows {
			line := make([]map[string]any, 0, len(row))
			for _, it := range row {
				sm, err := coerceSingleStructElement(tn, it, custom)
				if err != nil {
					return nil, err
				}
				line = append(line, sm)
			}
			out = append(out, line)
		}
		return out, nil
	}
	return nil, fmt.Errorf("struct %s: levels %d", tn, levels)
}

func parseBraceArrayJSON(s string, out any) error {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	if s == "" || s == "{}" {
		s = "[]"
	}
	// 已是合法 JSON（如 [1,2]、[[1]]）则直接解析
	if err := json.Unmarshal([]byte(s), out); err == nil {
		return nil
	}
	js, err := bracesToStandardJSON(s)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(js), out)
}
