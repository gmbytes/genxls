package genbundle_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"xlsgen/internal/genbundle"
	"xlsgen/internal/jsonout"
)

func canonicalJSON(t *testing.T, b []byte) string {
	t.Helper()
	var v any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(out)
}

func testInputPaths(t *testing.T) []string {
	t.Helper()
	return []string{
		filepath.Join("..", "..", "test", "0.struct.xlsx"),
		filepath.Join("..", "..", "test", "1.test.xlsx"),
	}
}

func TestAllJSONMatchesBundle(t *testing.T) {
	paths := testInputPaths(t)
	res, err := genbundle.Build(paths, "")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	allPath := filepath.Join(dir, "all.json")
	if err := jsonout.WriteAllJSON(allPath, res.JSONPayload); err != nil {
		t.Fatal(err)
	}
	fileBytes, err := os.ReadFile(allPath)
	if err != nil {
		t.Fatal(err)
	}

	direct, err := json.MarshalIndent(res.JSONPayload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := canonicalJSON(t, fileBytes), canonicalJSON(t, direct); got != want {
		t.Fatalf("all.json 与解析结果在 JSON 语义上不一致\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestBundleFormatsMatchExcel(t *testing.T) {
	paths := testInputPaths(t)
	res, err := genbundle.Build(paths, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := res.JSONPayload["tests"]
	if !ok {
		t.Fatalf("缺少 tests 键，得到键: %v", keysOf(res.JSONPayload))
	}
	arr, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("tests 类型 %T 期望 []map[string]any", raw)
	}
	if len(arr) != 3 {
		t.Fatalf("期望 3 行测试数据，得到 %d", len(arr))
	}

	row := arr[0]
	assertNum(t, row["cid"], int64(1), "cid")
	assertMapNum(t, row["pos"], "x", 100)
	assertMapNum(t, row["pos"], "y", 100)
	assertMapNum(t, row["pos"], "z", 100)
	if row["name"] != "小明" {
		t.Fatalf("name: got %#v", row["name"])
	}
	assertIntSlice(t, row["abc"], []int{1, 2, 3})
	if row["flag"] != true {
		t.Fatalf("flag: got %#v", row["flag"])
	}
	assertFloat(t, row["score"], 3.14)
	assertNum(t, row["ival"], 42, "ival")

	attr, ok := row["attr"].(map[string]any)
	if !ok {
		t.Fatalf("attr type %T", row["attr"])
	}
	assertNum(t, attr["type"], 2, "attr.type")
	assertNum(t, attr["val"], int64(9), "attr.val")
	assertNum(t, attr["rate"], int64(1), "attr.rate")

	vecs, ok := row["vecs"].([]map[string]any)
	if !ok || len(vecs) != 2 {
		t.Fatalf("vecs: %#v", row["vecs"])
	}
	assertMapNum(t, vecs[0], "x", 1)
	assertMapNum(t, vecs[1], "z", 6)

	vecGrid, ok := row["vecGrid"].([][]map[string]any)
	if !ok || len(vecGrid) != 2 {
		t.Fatalf("vecGrid: %#v", row["vecGrid"])
	}
	if len(vecGrid[0]) != 2 || len(vecGrid[1]) != 1 {
		t.Fatalf("vecGrid shape: %#v", vecGrid)
	}
	assertMapNum(t, vecGrid[0][0], "x", 1)
	assertMapNum(t, vecGrid[0][1], "y", 5)
	assertMapNum(t, vecGrid[1][0], "z", 9)

	grid, ok := row["grid"].([][]int)
	if !ok || len(grid) != 2 {
		t.Fatalf("grid: %#v", row["grid"])
	}
	if grid[0][0] != 1 || grid[0][1] != 2 || grid[1][0] != 3 || grid[1][1] != 4 {
		t.Fatalf("grid values: %#v", grid)
	}

	lf, ok := row["lf"].([]float64)
	if !ok || len(lf) != 2 {
		t.Fatalf("lf: %#v", row["lf"])
	}
	assertFloat(t, lf[0], 1.5)
	assertFloat(t, lf[1], 2.5)

	i64s, ok := row["i64s"].([]int64)
	if !ok || len(i64s) != 3 {
		// 解析器可能产出 []int —— 兼容
		if ii, ok2 := row["i64s"].([]int); ok2 && len(ii) == 3 {
			if int64(ii[0]) != 9 || int64(ii[1]) != 8 || int64(ii[2]) != 7 {
				t.Fatalf("i64s int slice: %#v", ii)
			}
		} else {
			t.Fatalf("i64s: %#v", row["i64s"])
		}
	} else {
		if i64s[0] != 9 || i64s[1] != 8 || i64s[2] != 7 {
			t.Fatalf("i64s: %#v", i64s)
		}
	}

	if row["desc"] != "row-A" {
		t.Fatalf("desc: got %#v", row["desc"])
	}

	row2 := arr[1]
	if row2["name"] != "" {
		t.Fatalf("row2 name empty: got %#v", row2["name"])
	}
	if row2["flag"] != false {
		t.Fatalf("row2 flag: %#v", row2["flag"])
	}
	v2, ok := row2["vecGrid"].([][]map[string]any)
	if !ok || len(v2) != 0 {
		t.Fatalf("row2 vecGrid empty: %#v", row2["vecGrid"])
	}

	row3 := arr[2]
	if row3["name"] != "中文emoji🙂" {
		t.Fatalf("row3 name: %#v", row3["name"])
	}
	if row3["flag"] != true { // "1" -> true
		t.Fatalf("row3 flag: %#v", row3["flag"])
	}
	attr3, ok := row3["attr"].(map[string]any)
	if !ok {
		t.Fatal("row3 attr")
	}
	assertNum(t, attr3["val"], int64(7000000000), "attr3.val")
	if row3["desc"] != "末尾空格" { // Excel 单元格 trim 后
		t.Fatalf("row3 desc: %#v", row3["desc"])
	}
	g3, ok := row3["vecGrid"].([][]map[string]any)
	if !ok || len(g3) != 2 {
		t.Fatalf("row3 vecGrid: %#v", row3["vecGrid"])
	}
	if len(g3[0]) != 1 || len(g3[1]) != 2 {
		t.Fatalf("row3 vecGrid shape: %#v", g3)
	}
	assertMapNum(t, g3[0][0], "x", 10)
	assertMapNum(t, g3[0][0], "y", 20)
	assertMapNum(t, g3[0][0], "z", 30)
	assertMapNum(t, g3[1][0], "x", 1)
	assertMapNum(t, g3[1][1], "x", 2)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func assertNum(t *testing.T, v any, want int64, label string) {
	t.Helper()
	switch x := v.(type) {
	case int:
		if int64(x) != want {
			t.Fatalf("%s: got %d want %d", label, x, want)
		}
	case int64:
		if x != want {
			t.Fatalf("%s: got %d want %d", label, x, want)
		}
	case float64:
		if int64(x) != want {
			t.Fatalf("%s: got %v want %d", label, x, want)
		}
	default:
		t.Fatalf("%s: got %T %#v", label, v, v)
	}
}

func assertFloat(t *testing.T, v any, want float64) {
	t.Helper()
	x, ok := toFloat64(v)
	if !ok {
		t.Fatalf("float: got %T %#v", v, v)
	}
	d := x - want
	if d < 0 {
		d = -d
	}
	if d > 1e-9 {
		t.Fatalf("float: got %v want %v", x, want)
	}
}

func assertMapNum(t *testing.T, m any, key string, want int) {
	t.Helper()
	mm, ok := m.(map[string]any)
	if !ok {
		t.Fatalf("%s: not map", key)
	}
	got, ok := toInt64(mm[key])
	if !ok {
		t.Fatalf("map key %q: got %T %#v", key, mm[key], mm[key])
	}
	if int(got) != want {
		t.Fatalf("%s: got %d want %d", key, got, want)
	}
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	default:
		return 0, false
	}
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	default:
		return 0, false
	}
}

func assertIntSlice(t *testing.T, v any, want []int) {
	t.Helper()
	sl, ok := v.([]int)
	if !ok {
		t.Fatalf("[]int: got %T", v)
	}
	if len(sl) != len(want) {
		t.Fatalf("len got %d want %d", len(sl), len(want))
	}
	for i := range want {
		if sl[i] != want[i] {
			t.Fatalf("[%d] got %d want %d", i, sl[i], want[i])
		}
	}
}
