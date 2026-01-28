package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

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

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func pluralizeTypeName(typeName string) string {
	if typeName == "" {
		return typeName
	}
	// Minimal pluralization for config names: Item->Items, Quest->Quests
	if strings.HasSuffix(typeName, "s") || strings.HasSuffix(typeName, "x") || strings.HasSuffix(typeName, "z") || strings.HasSuffix(typeName, "ch") || strings.HasSuffix(typeName, "sh") {
		return typeName + "es"
	}
	return typeName + "s"
}

func resolveInputPaths(in string) ([]string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return nil, errors.New("empty --in")
	}
	// If it's already an existing path, keep it.
	if st, err := os.Stat(in); err == nil {
		if st.IsDir() {
			return listExcelFiles(in)
		}
		return []string{in}, nil
	}

	// If user passed just a filename (or a relative path that doesn't exist), try ./xls/<name>.
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

func readRowsAuto(path string) ([][]string, error) {
	f, err := excelize.OpenFile(path)
	if err == nil {
		defer func() { _ = f.Close() }()
		list := f.GetSheetList()
		if len(list) == 0 {
			return nil, fmt.Errorf("%s: xlsx has no sheets", path)
		}
		rows, err := f.GetRows(list[0])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return rows, nil
	}
	rows, err2 := readTSVRows(path)
	if err2 != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return rows, nil
}

func readTSVRows(path string) ([][]string, error) {
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

type HeaderSpec struct {
	HeaderRows  int
	Orientation Orientation
	DefineRow   int // 1-based row number in sheet
}

type Options struct {
	InPath  string
	OutDir  string
	Flag    string
	Lang    string
	Pkg     string
	JSON    bool
	Verbose bool
}

func main() {
	var opts Options
	flag.StringVar(&opts.InPath, "in", "", "input xlsx file or directory (default: ./xls)")
	flag.StringVar(&opts.OutDir, "out", ".", "output directory")
	flag.StringVar(&opts.Flag, "flag", "", "export flag: server|client (optional)")
	flag.StringVar(&opts.Lang, "lang", "all", "target lang: go|cs|ts|all (or comma-separated)")
	flag.StringVar(&opts.Pkg, "pkg", "config", "go package name")
	flag.BoolVar(&opts.JSON, "json", true, "export json data")
	flag.BoolVar(&opts.Verbose, "v", false, "verbose")
	flag.Parse()

	if opts.InPath == "" {
		opts.InPath = "xls"
	}
	inPaths, err := resolveInputPaths(opts.InPath)
	if err != nil {
		exitErr(err)
	}
	langs, err := parseLangs(opts.Lang)
	if err != nil {
		exitErr(err)
	}
	if len(inPaths) == 0 {
		exitErr(errors.New("no input files"))
	}

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		exitErr(err)
	}

	rootName := "AllConfig"

	// Aggregated output:
	// - generate one go.gen.go/cs.gen.cs/ts.gen.ts
	// - generate one all.json with keys based on sheet name (pluralized)
	schemas := make(map[string][]Field)      // typeName -> fields
	jsonPayload := make(map[string]any)      // jsonKey -> []object
	seenKeys := make(map[string]string)      // jsonKey -> origin (file/sheet)
	orderedTypeNames := make([]string, 0, 8) // stable output order

	addSheet := func(origin string, sheetName string, rows [][]string) {
		spec, err := detectHeaderSpec(rows)
		if err != nil {
			exitErr(fmt.Errorf("%s: %w", origin, err))
		}
		if spec.Orientation == OrientationVertical {
			exitErr(fmt.Errorf("%s: vertical orientation (A1=2) is not supported yet", origin))
		}
		fields, err := parseFieldsFromDefineRow(rows, spec.DefineRow, opts.Flag)
		if err != nil {
			exitErr(fmt.Errorf("%s: %w", origin, err))
		}
		items, err := readHorizontalItems(rows, spec.DefineRow+1, fields)
		if err != nil {
			exitErr(fmt.Errorf("%s: %w", origin, err))
		}

		typeName := exportName(sheetName)
		if typeName == "" {
			exitErr(fmt.Errorf("%s: empty sheet name", origin))
		}
		fieldName := pluralizeTypeName(typeName)
		jsonKey := lowerFirst(fieldName)
		if prev, ok := seenKeys[jsonKey]; ok {
			exitErr(fmt.Errorf("duplicate sheet key %q from %s (already used by %s)", jsonKey, origin, prev))
		}
		seenKeys[jsonKey] = origin
		schemas[typeName] = fields
		jsonPayload[jsonKey] = items
		orderedTypeNames = append(orderedTypeNames, typeName)
	}

	for _, p := range inPaths {
		if f, err := excelize.OpenFile(p); err == nil {
			func() {
				defer func() { _ = f.Close() }()
				sheets := f.GetSheetList()
				if len(sheets) == 0 {
					exitErr(fmt.Errorf("%s: xlsx has no sheets", p))
				}
				for _, sheet := range sheets {
					rows, err := f.GetRows(sheet)
					if err != nil {
						exitErr(fmt.Errorf("%s[%s]: %w", p, sheet, err))
					}
					addSheet(fmt.Sprintf("%s[%s]", p, sheet), sheet, rows)
				}
			}()
			continue
		}

		rows, err := readTSVRows(p)
		if err != nil {
			exitErr(err)
		}
		sheet := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		addSheet(p, sheet, rows)
	}

	// Generate aggregated code
	if langs["go"] {
		goCode, err := generateGoBundle(opts.Pkg, rootName, orderedTypeNames, schemas)
		if err != nil {
			exitErr(err)
		}
		outFile := filepath.Join(opts.OutDir, "go.gen.go")
		if err := os.WriteFile(outFile, []byte(goCode), 0o644); err != nil {
			exitErr(err)
		}
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "generated %s\n", outFile)
		}
	}
	if langs["cs"] {
		csCode, err := generateCSBundle(rootName, orderedTypeNames, schemas)
		if err != nil {
			exitErr(err)
		}
		outFile := filepath.Join(opts.OutDir, "cs.gen.cs")
		if err := os.WriteFile(outFile, []byte(csCode), 0o644); err != nil {
			exitErr(err)
		}
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "generated %s\n", outFile)
		}
	}
	if langs["ts"] {
		tsCode, err := generateTSBundle(rootName, orderedTypeNames, schemas)
		if err != nil {
			exitErr(err)
		}
		outFile := filepath.Join(opts.OutDir, "ts.gen.ts")
		if err := os.WriteFile(outFile, []byte(tsCode), 0o644); err != nil {
			exitErr(err)
		}
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "generated %s\n", outFile)
		}
	}

	if opts.JSON {
		data, err := json.MarshalIndent(jsonPayload, "", "  ")
		if err != nil {
			exitErr(err)
		}
		jsonFile := filepath.Join(opts.OutDir, "all.json")
		if err := os.WriteFile(jsonFile, data, 0o644); err != nil {
			exitErr(err)
		}
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "generated %s\n", jsonFile)
		}
	}
}

func parseLangs(s string) (map[string]bool, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "all" {
		return map[string]bool{"go": true, "cs": true, "ts": true}, nil
	}
	parts := strings.Split(s, ",")
	out := map[string]bool{"go": false, "cs": false, "ts": false}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		switch p {
		case "go", "cs", "ts":
			out[p] = true
		default:
			return nil, fmt.Errorf("invalid --lang %q (expect go|cs|ts|all or comma-separated)", s)
		}
	}
	if !out["go"] && !out["cs"] && !out["ts"] {
		return nil, fmt.Errorf("invalid --lang %q (no targets)", s)
	}
	return out, nil
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}

func detectHeaderSpec(rows [][]string) (HeaderSpec, error) {
	if len(rows) >= 3 && rowHasFieldDefs(rows[2]) {
		ori := OrientationHorizontal
		a1 := ""
		if len(rows[0]) > 0 {
			a1 = strings.TrimSpace(rows[0][0])
		}
		if a1 == "2" {
			ori = OrientationVertical
		}
		return HeaderSpec{HeaderRows: 3, Orientation: ori, DefineRow: 3}, nil
	}
	if len(rows) >= 2 && rowHasFieldDefs(rows[1]) {
		return HeaderSpec{HeaderRows: 2, Orientation: OrientationHorizontal, DefineRow: 2}, nil
	}
	if len(rows) >= 1 && rowHasFieldDefs(rows[0]) {
		return HeaderSpec{HeaderRows: 1, Orientation: OrientationHorizontal, DefineRow: 1}, nil
	}
	return HeaderSpec{}, errors.New("cannot detect header")
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

func parseFieldsFromDefineRow(rows [][]string, defineRow int, exportFlag string) ([]Field, error) {
	if defineRow <= 0 || defineRow > len(rows) {
		return nil, fmt.Errorf("define row %d out of range", defineRow)
	}
	row := rows[defineRow-1]
	var fields []Field
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

		ff := FieldFlagAll
		switch flagCh {
		case "":
			ff = FieldFlagAll
		case "s":
			ff = FieldFlagServer
		case "c":
			ff = FieldFlagClient
		default:
			ff = FieldFlagAll
		}

		if exportFlag != "" {
			switch exportFlag {
			case "server":
				if ff == FieldFlagClient {
					continue
				}
			case "client":
				if ff == FieldFlagServer {
					continue
				}
			default:
				return nil, fmt.Errorf("invalid --flag %q (expect server|client)", exportFlag)
			}
		}

		goType, ok := mapGoType(rawType)
		if !ok {
			return nil, fmt.Errorf("unsupported type %q", rawType)
		}
		fields = append(fields, Field{
			RawName:  rawName,
			Name:     exportName(rawName),
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

func exportName(name string) string {
	if name == "" {
		return name
	}
	// If it's already camelCase, keep inner casing and just capitalize first letter.
	if !strings.ContainsAny(name, "_-") {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	// cid => Cid, data_id => DataId
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, "")
}

func mapGoType(t string) (string, bool) {
	switch strings.ToLower(t) {
	case "int", "int32", "int64":
		return "int", true
	case "int[]":
		return "[]int", true
	case "int[][]":
		return "[][]int", true
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

func mapCSType(t string) (string, bool) {
	switch strings.ToLower(t) {
	case "int", "int32", "int64":
		return "int", true
	case "int[]":
		return "List<int>", true
	case "int[][]":
		return "List<List<int>>", true
	case "float", "float32", "float64":
		return "double", true
	case "bool":
		return "bool", true
	case "string":
		return "string", true
	default:
		return "", false
	}
}

func mapTSType(t string) (string, bool) {
	switch strings.ToLower(t) {
	case "int", "int32", "int64", "float", "float32", "float64":
		return "number", true
	case "int[]":
		return "number[]", true
	case "int[][]":
		return "number[][]", true
	case "bool":
		return "boolean", true
	case "string":
		return "string", true
	default:
		return "", false
	}
}

func generateGo(pkg, rootName, itemName string, fields []Field) (string, error) {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")

	b.WriteString("type ")
	b.WriteString(rootName)
	b.WriteString(" struct {\n")
	b.WriteString("\tItems []")
	b.WriteString(itemName)
	b.WriteString("\n")
	b.WriteString("}\n\n\n")

	b.WriteString("type ")
	b.WriteString(itemName)
	b.WriteString(" struct {\n")
	for _, f := range fields {
		b.WriteString("\t")
		b.WriteString(f.Name)
		b.WriteString(" ")
		b.WriteString(f.GoType)
		b.WriteString(" `json:\"")
		b.WriteString(f.RawName)
		b.WriteString("\"`")
		b.WriteString("\n")
	}
	b.WriteString("}\n")

	return b.String(), nil
}

func generateCS(rootName, itemName string, fields []Field) (string, error) {
	var b strings.Builder
	b.WriteString("using System.Collections.Generic;\n")
	b.WriteString("using System.Text.Json.Serialization;\n\n")

	b.WriteString("public class ")
	b.WriteString(rootName)
	b.WriteString("\n{\n")
	b.WriteString("    public List<")
	b.WriteString(itemName)
	b.WriteString("> Items { get; set; }\n")
	b.WriteString("}\n\n")

	b.WriteString("public class ")
	b.WriteString(itemName)
	b.WriteString("\n{\n")
	for _, f := range fields {
		csType, ok := mapCSType(f.RawType)
		if !ok {
			return "", fmt.Errorf("unsupported type %q", f.RawType)
		}
		b.WriteString("    [JsonPropertyName(\"")
		b.WriteString(f.RawName)
		b.WriteString("\")]\n")
		b.WriteString("    public ")
		b.WriteString(csType)
		b.WriteString(" ")
		b.WriteString(f.Name)
		b.WriteString(" { get; set; }\n\n")
	}
	b.WriteString("}\n")
	return b.String(), nil
}

func generateGoBundle(pkg, rootName string, orderedTypeNames []string, schemas map[string][]Field) (string, error) {
	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")

	// Root config
	b.WriteString("type ")
	b.WriteString(rootName)
	b.WriteString(" struct {\n")
	for _, typeName := range orderedTypeNames {
		fieldName := pluralizeTypeName(typeName)
		jsonKey := lowerFirst(fieldName)
		b.WriteString("\t")
		b.WriteString(fieldName)
		b.WriteString(" []")
		b.WriteString(typeName)
		b.WriteString(" `json:\"")
		b.WriteString(jsonKey)
		b.WriteString("\"`\n")
	}
	b.WriteString("}\n\n")

	// Types
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

func generateCSBundle(rootName string, orderedTypeNames []string, schemas map[string][]Field) (string, error) {
	var b strings.Builder
	b.WriteString("using System.Collections.Generic;\n")
	b.WriteString("using System.Text.Json.Serialization;\n\n")

	b.WriteString("public class ")
	b.WriteString(rootName)
	b.WriteString("\n{\n")
	for _, typeName := range orderedTypeNames {
		fieldName := pluralizeTypeName(typeName)
		jsonKey := lowerFirst(fieldName)
		b.WriteString("    [JsonPropertyName(\"")
		b.WriteString(jsonKey)
		b.WriteString("\")]\n")
		b.WriteString("    public List<")
		b.WriteString(typeName)
		b.WriteString("> ")
		b.WriteString(fieldName)
		b.WriteString(" { get; set; }\n\n")
	}
	b.WriteString("}\n\n")

	for _, typeName := range orderedTypeNames {
		fields := schemas[typeName]
		b.WriteString("public class ")
		b.WriteString(typeName)
		b.WriteString("\n{\n")
		for _, f := range fields {
			csType, ok := mapCSType(f.RawType)
			if !ok {
				return "", fmt.Errorf("unsupported type %q", f.RawType)
			}
			b.WriteString("    [JsonPropertyName(\"")
			b.WriteString(f.RawName)
			b.WriteString("\")]\n")
			b.WriteString("    public ")
			b.WriteString(csType)
			b.WriteString(" ")
			b.WriteString(f.Name)
			b.WriteString(" { get; set; }\n\n")
		}
		b.WriteString("}\n\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

func generateTSBundle(rootName string, orderedTypeNames []string, schemas map[string][]Field) (string, error) {
	var b strings.Builder
	for _, typeName := range orderedTypeNames {
		fields := schemas[typeName]
		b.WriteString("export interface ")
		b.WriteString(typeName)
		b.WriteString(" {\n")
		for _, f := range fields {
			tsType, ok := mapTSType(f.RawType)
			if !ok {
				return "", fmt.Errorf("unsupported type %q", f.RawType)
			}
			b.WriteString("  ")
			b.WriteString(f.RawName)
			b.WriteString(": ")
			b.WriteString(tsType)
			b.WriteString(";\n")
		}
		b.WriteString("}\n\n")
	}

	b.WriteString("export interface ")
	b.WriteString(rootName)
	b.WriteString(" {\n")
	for _, typeName := range orderedTypeNames {
		fieldName := pluralizeTypeName(typeName)
		jsonKey := lowerFirst(fieldName)
		b.WriteString("  ")
		b.WriteString(jsonKey)
		b.WriteString(": ")
		b.WriteString(typeName)
		b.WriteString("[];\n")
	}
	b.WriteString("}\n")

	return b.String(), nil
}

func generateTS(rootName, itemName string, fields []Field) (string, error) {
	var b strings.Builder
	b.WriteString("export interface ")
	b.WriteString(itemName)
	b.WriteString(" {\n")
	for _, f := range fields {
		tsType, ok := mapTSType(f.RawType)
		if !ok {
			return "", fmt.Errorf("unsupported type %q", f.RawType)
		}
		b.WriteString("  ")
		b.WriteString(f.RawName)
		b.WriteString(": ")
		b.WriteString(tsType)
		b.WriteString(";\n")
	}
	b.WriteString("}\n\n")

	b.WriteString("export interface ")
	b.WriteString(rootName)
	b.WriteString(" {\n")
	b.WriteString("  Items: ")
	b.WriteString(itemName)
	b.WriteString("[];\n")
	b.WriteString("}\n")
	return b.String(), nil
}

func readHorizontalItems(rows [][]string, dataStartRow int, fields []Field) ([]map[string]any, error) {
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
			v, err := parseCellValue(field.RawType, cell)
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

func parseCellValue(rawType string, s string) (any, error) {
	if s == "" {
		switch strings.ToLower(rawType) {
		case "int", "int32", "int64":
			return 0, nil
		case "int[]":
			return []int{}, nil
		case "int[][]":
			return [][]int{}, nil
		case "float", "float32", "float64":
			return float64(0), nil
		case "bool":
			return false, nil
		case "string":
			return "", nil
		default:
			return nil, fmt.Errorf("unsupported type %q", rawType)
		}
	}

	switch strings.ToLower(rawType) {
	case "int", "int32", "int64":
		v, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		return v, nil
	case "int[]":
		var v []int
		if err := parseBraceArrayJSON(s, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "int[][]":
		var v [][]int
		if err := parseBraceArrayJSON(s, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "float", "float32", "float64":
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		return v, nil
	case "bool":
		ls := strings.ToLower(s)
		if ls == "1" {
			return true, nil
		}
		if ls == "0" {
			return false, nil
		}
		v, err := strconv.ParseBool(ls)
		if err != nil {
			return nil, err
		}
		return v, nil
	case "string":
		return s, nil
	default:
		return nil, fmt.Errorf("unsupported type %q", rawType)
	}
}

func parseBraceArrayJSON(s string, out any) error {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	if s == "" || s == "{}" {
		s = "[]"
	}
	// Convert Lua-like braces to JSON arrays.
	s = strings.ReplaceAll(s, "{", "[")
	s = strings.ReplaceAll(s, "}", "]")
	if !strings.HasPrefix(strings.TrimSpace(s), "[") {
		s = "[" + s + "]"
	}
	return json.Unmarshal([]byte(s), out)
}
