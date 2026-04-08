package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	InPath           string
	OutDir           string
	GdOutDir         string
	ResImporterOut   string // if set, res_importer.gd is written here only, not under gd-out
	GclientDir       string
	GodotBin         string
	Flag             string
	Lang             string
	Pkg              string
	JSON             bool
	SplitJSON        bool
	Verbose          bool
}

func main() {
	var opts Options
	flag.StringVar(&opts.InPath, "in", "", "input xlsx file or directory (default: ./xls)")
	flag.StringVar(&opts.OutDir, "out", ".", "output directory")
	flag.StringVar(&opts.GdOutDir, "gd-out", "", "GDScript output directory (default: <out>/gd)")
	flag.StringVar(&opts.ResImporterOut, "res-importer-out", "", "if set, write res_importer.gd only to this directory (not under gd-out)")
	flag.StringVar(&opts.GclientDir, "gclient", "", "Godot client project root (enables .res import via protoc-gen-gd.exe next to genxls)")
	flag.StringVar(&opts.GodotBin, "godot", "", "Godot executable (default: GODOT env, else <genxls_dir>/protoc-gen-gd.exe)")
	flag.StringVar(&opts.Flag, "flag", "", "export flag: server|client (optional)")
	flag.StringVar(&opts.Lang, "lang", "all", "target lang: go|gd|all (or comma-separated)")
	flag.StringVar(&opts.Pkg, "pkg", "config", "go package name")
	flag.BoolVar(&opts.JSON, "json", true, "export json data")
	flag.BoolVar(&opts.SplitJSON, "split-json", false, "split each table into separate json file + manifest")
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
	// - generate one go.gen.go and/or gd/ directory
	// - generate one all.json with keys based on sheet name (pluralized)
	schemas := make(map[string][]Field)        // typeName -> fields
	jsonPayload := make(map[string]any)        // jsonKey -> []object
	seenKeys := make(map[string]string)        // jsonKey -> origin (file/sheet)
	orderedTypeNames := make([]string, 0, 8)   // stable output order
	jsonKeyToTypeName := make(map[string]string) // jsonKey -> typeName (for manifest)

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
		hasCid := false
		for _, f := range fields {
			if f.RawName == "cid" {
				hasCid = true
				break
			}
		}
		if !hasCid {
			exitErr(fmt.Errorf("%s: sheet missing required field 'cid#int' (unique key)", origin))
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
		jsonKeyToTypeName[jsonKey] = typeName
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
	if langs["gd"] {
		gdFiles, err := generateGDBundle(orderedTypeNames, schemas)
		if err != nil {
			exitErr(err)
		}
		gdDir := opts.GdOutDir
		if gdDir == "" {
			gdDir = filepath.Join(opts.OutDir, "gd")
		}
		if err := os.MkdirAll(gdDir, 0o755); err != nil {
			exitErr(err)
		}
		impOut := strings.TrimSpace(opts.ResImporterOut)
		if impOut != "" {
			impOut, err = filepath.Abs(impOut)
			if err != nil {
				exitErr(err)
			}
			if err := os.MkdirAll(impOut, 0o755); err != nil {
				exitErr(err)
			}
			impCode, ok := gdFiles["res_importer.gd"]
			if !ok {
				exitErr(errors.New("internal: res_importer.gd missing from gd bundle"))
			}
			impPath := filepath.Join(impOut, "res_importer.gd")
			if err := os.WriteFile(impPath, []byte(impCode), 0o644); err != nil {
				exitErr(err)
			}
			if opts.Verbose {
				fmt.Fprintf(os.Stderr, "generated %s\n", impPath)
			}
			delete(gdFiles, "res_importer.gd")
		}
		for name, content := range gdFiles {
			outFile := filepath.Join(gdDir, name)
			if err := os.WriteFile(outFile, []byte(content), 0o644); err != nil {
				exitErr(err)
			}
			if opts.Verbose {
				fmt.Fprintf(os.Stderr, "generated %s\n", outFile)
			}
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

	if opts.SplitJSON {
		if err := writeSplitJSONAndManifest(opts.OutDir, jsonPayload, orderedTypeNames, jsonKeyToTypeName, opts.Verbose); err != nil {
			exitErr(err)
		}
	}

	if strings.TrimSpace(opts.GclientDir) != "" {
		if err := runGclientResImport(&opts, langs); err != nil {
			exitErr(err)
		}
	}
}

// runGclientResImport copies res_importer into gclient/data/generated/gd and runs Godot headless (-s res://data/generated/gd/res_importer.gd).
func runGclientResImport(opts *Options, langs map[string]bool) error {
	if !langs["gd"] {
		return errors.New("--gclient requires --lang to include gd")
	}
	if !opts.JSON {
		return errors.New("--gclient requires --json=true (res_importer reads res://data/config/all.json)")
	}
	gclientAbs, err := filepath.Abs(strings.TrimSpace(opts.GclientDir))
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(gclientAbs, "project.godot")); err != nil {
		return fmt.Errorf("Godot project not found: %w", err)
	}
	gdDir := opts.GdOutDir
	if gdDir == "" {
		gdDir = filepath.Join(opts.OutDir, "gd")
	}
	gdAbs, err := filepath.Abs(gdDir)
	if err != nil {
		return err
	}
	wantGd := filepath.Join(gclientAbs, "data", "generated", "gd")
	wantGdAbs, _ := filepath.Abs(wantGd)
	if filepath.Clean(gdAbs) != filepath.Clean(wantGdAbs) {
		return fmt.Errorf("--gd-out must be <gclient>/data/generated/gd (got %s)", gdAbs)
	}
	jsonAbs, err := filepath.Abs(filepath.Join(opts.OutDir, "all.json"))
	if err != nil {
		return err
	}
	wantJSON, _ := filepath.Abs(filepath.Join(gclientAbs, "data", "config", "all.json"))
	if filepath.Clean(jsonAbs) != filepath.Clean(wantJSON) {
		return fmt.Errorf("--out must be <gclient>/data/config so all.json matches res_importer (got out dir %s)", opts.OutDir)
	}
	godotPath, err := resolveGodotForGenxls(opts.GodotBin)
	if err != nil {
		return err
	}
	if err := syncResImporterToGD(opts, gdAbs, gclientAbs); err != nil {
		return err
	}
	args := []string{"--headless", "--path", gclientAbs, "-s", "res://data/generated/gd/res_importer.gd"}
	if opts.Verbose {
		fmt.Fprintf(os.Stderr, "running: %s %v\n", godotPath, args)
	}
	cmd := exec.Command(godotPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("godot res import: %w", err)
	}
	if opts.Verbose {
		fmt.Fprintf(os.Stderr, "generated resources under %s\n", filepath.Join(gclientAbs, "data", "generated"))
	}
	return nil
}

func resolveGodotForGenxls(explicit string) (string, error) {
	if s := strings.TrimSpace(explicit); s != "" {
		if _, err := os.Stat(s); err != nil {
			return "", fmt.Errorf("--godot: %w", err)
		}
		return s, nil
	}
	if env := strings.TrimSpace(os.Getenv("GODOT")); env != "" {
		if _, err := os.Stat(env); err != nil {
			return "", fmt.Errorf("GODOT env: %w", err)
		}
		return env, nil
	}
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	exeDir := filepath.Dir(exePath)
	name := "protoc-gen-gd"
	if runtime.GOOS == "windows" {
		name = "protoc-gen-gd.exe"
	}
	candidate := filepath.Join(exeDir, name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("Godot not found (need %s beside genxls.exe, or set --godot / GODOT)", name)
}

func syncResImporterToGD(opts *Options, gdAbs, gclientAbs string) error {
	var src string
	if s := strings.TrimSpace(opts.ResImporterOut); s != "" {
		abs, err := filepath.Abs(s)
		if err != nil {
			return err
		}
		src = filepath.Join(abs, "res_importer.gd")
	} else {
		src = filepath.Join(gdAbs, "res_importer.gd")
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("res_importer.gd: %w", err)
	}
	if err := os.MkdirAll(gdAbs, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(gdAbs, "res_importer.gd")
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func parseLangs(s string) (map[string]bool, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "all" {
		return map[string]bool{"go": true, "gd": true}, nil
	}
	parts := strings.Split(s, ",")
	out := map[string]bool{"go": false, "gd": false}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		switch p {
		case "go", "gd":
			out[p] = true
		default:
			return nil, fmt.Errorf("invalid --lang %q (expect go|gd|all or comma-separated)", s)
		}
	}
	if !out["go"] && !out["gd"] {
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

// ── GDScript code generation ──

func mapGDType(t string) (string, bool) {
	switch strings.ToLower(t) {
	case "int", "int32", "int64":
		return "int", true
	case "int[]":
		return "Array[int]", true
	case "int[][]":
		return "Array", true
	case "float", "float32", "float64":
		return "float", true
	case "bool":
		return "bool", true
	case "string":
		return "String", true
	default:
		return "", false
	}
}

func gdDefaultValue(gdType string) string {
	switch gdType {
	case "int":
		return ""
	case "float":
		return ""
	case "bool":
		return ""
	case "String":
		return ""
	case "Array[int]":
		return " = []"
	case "Array":
		return " = []"
	default:
		return ""
	}
}

func gdClassName(typeName string) string {
	return "C" + typeName
}

func gdPluralClassName(typeName string) string {
	return "C" + pluralizeTypeName(typeName)
}

func gdFileName(typeName string) string {
	return "c_" + toSnakeCase(typeName) + ".gd"
}

func gdPluralFileName(typeName string) string {
	return "c_" + toSnakeCase(pluralizeTypeName(typeName)) + ".gd"
}

func generateGDRowClass(typeName string, fields []Field) (string, error) {
	var b strings.Builder
	b.WriteString("# Auto-generated by genxls. DO NOT EDIT.\n")
	b.WriteString("extends Resource\n")
	b.WriteString("class_name ")
	b.WriteString(gdClassName(typeName))
	b.WriteString("\n\n")

	cidFirst := reorderCidFirst(fields)
	for _, f := range cidFirst {
		gdType, ok := mapGDType(f.RawType)
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
	b.WriteString("# Auto-generated by genxls. DO NOT EDIT.\n")
	b.WriteString("extends Resource\n")
	b.WriteString("class_name ")
	b.WriteString(gdPluralClassName(typeName))
	b.WriteString("\n\n")
	// Untyped Array so headless res_importer can load() scripts without global class_name order.
	b.WriteString("@export var items: Array = []\n")
	return b.String()
}

func generateGDAllConfig(orderedTypeNames []string) string {
	var b strings.Builder
	b.WriteString("# Auto-generated by genxls. DO NOT EDIT.\n")
	b.WriteString("extends Resource\n")
	b.WriteString("class_name CAllConfig\n\n")

	for _, typeName := range orderedTypeNames {
		snakeKey := toSnakeCase(pluralizeTypeName(typeName))
		b.WriteString("@export var ")
		b.WriteString(snakeKey)
		b.WriteString(": Array = []\n")
	}
	return b.String()
}

func generateGDImporter(orderedTypeNames []string, schemas map[string][]Field) (string, error) {
	var b strings.Builder
	b.WriteString("# Auto-generated by genxls. DO NOT EDIT.\n")
	b.WriteString("extends SceneTree\n\n")
	b.WriteString("# Uses load() for row/container scripts so --headless -s works without global class_name parse order.\n")
	b.WriteString("const JSON_PATH := \"res://data/config/all.json\"\n")
	b.WriteString("const OUT_ALL := \"res://data/generated/all.res\"\n")
	b.WriteString("const OUT_TABLES := \"res://data/generated/tables\"\n\n")

	// Helper functions
	b.WriteString("func _to_int_array(arr) -> Array[int]:\n")
	b.WriteString("\tvar result: Array[int] = []\n")
	b.WriteString("\tif arr is Array:\n")
	b.WriteString("\t\tfor v in arr:\n")
	b.WriteString("\t\t\tresult.append(int(v))\n")
	b.WriteString("\treturn result\n\n")

	b.WriteString("func _to_int_array_2d(arr) -> Array:\n")
	b.WriteString("\tvar result: Array = []\n")
	b.WriteString("\tif arr is Array:\n")
	b.WriteString("\t\tfor sub in arr:\n")
	b.WriteString("\t\t\tresult.append(_to_int_array(sub))\n")
	b.WriteString("\treturn result\n\n")

	b.WriteString("func _initialize() -> void:\n")
	b.WriteString("\tvar file = FileAccess.open(JSON_PATH, FileAccess.READ)\n")
	b.WriteString("\tif file == null:\n")
	b.WriteString("\t\tprinterr(\"Failed to open: \", JSON_PATH)\n")
	b.WriteString("\t\tquit(1)\n")
	b.WriteString("\t\treturn\n")
	b.WriteString("\tvar text = file.get_as_text()\n")
	b.WriteString("\tfile.close()\n")
	b.WriteString("\tvar data = JSON.parse_string(text)\n")
	b.WriteString("\tif data == null:\n")
	b.WriteString("\t\tprinterr(\"Failed to parse JSON\")\n")
	b.WriteString("\t\tquit(1)\n")
	b.WriteString("\t\treturn\n\n")
	b.WriteString("\tDirAccess.make_dir_recursive_absolute(\"res://data/generated\")\n")
	b.WriteString("\tDirAccess.make_dir_recursive_absolute(OUT_TABLES)\n")
	b.WriteString("\tvar AllCfgScript = load(\"res://data/generated/gd/c_all_config.gd\")\n")
	b.WriteString("\tvar config = AllCfgScript.new()\n\n")

	for _, typeName := range orderedTypeNames {
		fields := schemas[typeName]
		jsonKey := lowerFirst(pluralizeTypeName(typeName))
		snakeKey := toSnakeCase(pluralizeTypeName(typeName))
		containerVar := snakeKey + "_container"
		rowScriptVar := snakeKey + "_row_script"
		ctrScriptVar := snakeKey + "_ctr_script"

		b.WriteString("\t# --- ")
		b.WriteString(typeName)
		b.WriteString(" ---\n")
		b.WriteString("\tvar ")
		b.WriteString(rowScriptVar)
		b.WriteString(" = load(\"res://data/generated/gd/")
		b.WriteString(gdFileName(typeName))
		b.WriteString("\")\n")
		b.WriteString("\tvar ")
		b.WriteString(ctrScriptVar)
		b.WriteString(" = load(\"res://data/generated/gd/")
		b.WriteString(gdPluralFileName(typeName))
		b.WriteString("\")\n")
		b.WriteString("\tvar ")
		b.WriteString(containerVar)
		b.WriteString(" = ")
		b.WriteString(ctrScriptVar)
		b.WriteString(".new()\n")
		b.WriteString("\tfor entry in data.get(\"")
		b.WriteString(jsonKey)
		b.WriteString("\", []):\n")
		b.WriteString("\t\tvar item = ")
		b.WriteString(rowScriptVar)
		b.WriteString(".new()\n")

		for _, f := range fields {
			gdType, ok := mapGDType(f.RawType)
			if !ok {
				return "", fmt.Errorf("unsupported type %q for GDScript", f.RawType)
			}
			snakeName := toSnakeCase(f.Name)
			b.WriteString("\t\titem.")
			b.WriteString(snakeName)
			b.WriteString(" = ")
			switch gdType {
			case "int":
				b.WriteString("int(entry.get(\"")
				b.WriteString(f.RawName)
				b.WriteString("\", 0))")
			case "float":
				b.WriteString("float(entry.get(\"")
				b.WriteString(f.RawName)
				b.WriteString("\", 0.0))")
			case "bool":
				b.WriteString("bool(entry.get(\"")
				b.WriteString(f.RawName)
				b.WriteString("\", false))")
			case "String":
				b.WriteString("str(entry.get(\"")
				b.WriteString(f.RawName)
				b.WriteString("\", \"\"))")
			case "Array[int]":
				b.WriteString("_to_int_array(entry.get(\"")
				b.WriteString(f.RawName)
				b.WriteString("\", []))")
			case "Array":
				b.WriteString("_to_int_array_2d(entry.get(\"")
				b.WriteString(f.RawName)
				b.WriteString("\", []))")
			}
			b.WriteString("\n")
		}

		b.WriteString("\t\t")
		b.WriteString(containerVar)
		b.WriteString(".items.append(item)\n")
		b.WriteString("\t\tconfig.")
		b.WriteString(snakeKey)
		b.WriteString(".append(item)\n")
		b.WriteString("\tResourceSaver.save(")
		b.WriteString(containerVar)
		b.WriteString(", OUT_TABLES + \"/")
		b.WriteString(jsonKey)
		b.WriteString(".res\")\n")
		b.WriteString("\tprint(\"  saved: ")
		b.WriteString(jsonKey)
		b.WriteString(".res\")\n\n")
	}

	b.WriteString("\tResourceSaver.save(config, OUT_ALL)\n")
	b.WriteString("\tprint(\"saved: all.res + tables/*.res (")
	b.WriteString(fmt.Sprintf("%d", len(orderedTypeNames)))
	b.WriteString(" tables)\")\n")
	b.WriteString("\tquit()\n")
	return b.String(), nil
}

func generateGDBundle(orderedTypeNames []string, schemas map[string][]Field) (map[string]string, error) {
	files := make(map[string]string)

	for _, typeName := range orderedTypeNames {
		fields := schemas[typeName]

		rowCode, err := generateGDRowClass(typeName, fields)
		if err != nil {
			return nil, err
		}
		files[gdFileName(typeName)] = rowCode
		files[gdPluralFileName(typeName)] = generateGDContainerClass(typeName)
	}

	files["c_all_config.gd"] = generateGDAllConfig(orderedTypeNames)

	importerCode, err := generateGDImporter(orderedTypeNames, schemas)
	if err != nil {
		return nil, err
	}
	files["res_importer.gd"] = importerCode

	return files, nil
}

func reorderCidFirst(fields []Field) []Field {
	result := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.RawName == "cid" {
			result = append([]Field{f}, result...)
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

// ── Split JSON + Manifest ──

type ManifestTableEntry struct {
	File     string `json:"file"`
	Sha256   string `json:"sha256"`
	Size     int64  `json:"size"`
	RowCount int    `json:"row_count"`
	TypeName string `json:"type_name"`
}

type ManifestFile struct {
	Version string                        `json:"version"`
	Tables  map[string]ManifestTableEntry `json:"tables"`
}

func writeSplitJSONAndManifest(outDir string, jsonPayload map[string]any, orderedTypeNames []string, jsonKeyToTypeName map[string]string, verbose bool) error {
	tablesDir := filepath.Join(outDir, "tables")
	if err := os.MkdirAll(tablesDir, 0o755); err != nil {
		return err
	}

	manifest := ManifestFile{
		Version: time.Now().Format("20060102"),
		Tables:  make(map[string]ManifestTableEntry),
	}

	for _, typeName := range orderedTypeNames {
		fieldName := pluralizeTypeName(typeName)
		jsonKey := lowerFirst(fieldName)
		items := jsonPayload[jsonKey]

		data, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal %s: %w", jsonKey, err)
		}

		relPath := "tables/" + jsonKey + ".json"
		absPath := filepath.Join(outDir, relPath)
		if err := os.WriteFile(absPath, data, 0o644); err != nil {
			return err
		}

		h := sha256.Sum256(data)
		rowCount := 0
		if arr, ok := items.([]map[string]any); ok {
			rowCount = len(arr)
		}

		manifest.Tables[jsonKey] = ManifestTableEntry{
			File:     relPath,
			Sha256:   hex.EncodeToString(h[:]),
			Size:     int64(len(data)),
			RowCount: rowCount,
			TypeName: typeName,
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "generated %s\n", absPath)
		}
	}

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(outDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		return err
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "generated %s\n", manifestPath)
	}

	return nil
}
