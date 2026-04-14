package parse

import (
	"encoding/json"
	"testing"

	"xlsgen/internal/model"
)

func TestCoerceSingleStructElement_unwrapsSingleElementArrays(t *testing.T) {
	custom := map[string][]model.Field{
		"Vector": {
			{RawName: "x", RawType: "int64"},
			{RawName: "y", RawType: "int64"},
			{RawName: "z", RawType: "int64"},
		},
	}
	// 模拟 json.Unmarshal 后多包一层：[[{"x":1,"y":2,"z":3}]]
	var inner []any
	if err := json.Unmarshal([]byte(`[{"x":1,"y":2,"z":3}]`), &inner); err != nil {
		t.Fatal(err)
	}
	wrapped := []any{inner}
	m, err := coerceSingleStructElement("Vector", wrapped, custom)
	if err != nil {
		t.Fatal(err)
	}
	if x, _ := jsonToInt64(m["x"]); x != 1 {
		t.Fatalf("x=%v", m["x"])
	}
}

func TestParseCellValue_emptyBraceConvention(t *testing.T) {
	custom := map[string][]model.Field{
		"Vector": {
			{RawName: "x", RawType: "int64"},
			{RawName: "y", RawType: "int64"},
			{RawName: "z", RawType: "int64"},
		},
		"Attr": {
			{RawName: "type", RawType: "int"},
			{RawName: "val", RawType: "int64"},
			{RawName: "rate", RawType: "int"},
		},
	}
	cases := []struct {
		rawType string
		cell    string
	}{
		{"Vector", "{}"},
		{"Vector[]", "{}"},
		{"Vector[][]", "{}"},
		{"int[]", "{}"},
		{"int[][]", "{}"},
		{"Attr", "{}"},
		{"Vector[]", "[]"},
	}
	for _, tc := range cases {
		_, err := parseCellValue(tc.rawType, tc.cell, custom)
		if err != nil {
			t.Fatalf("%s %q: %v", tc.rawType, tc.cell, err)
		}
	}
	s, err := parseCellValue("string", "{}", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s != "{}" {
		t.Fatalf("string literal: got %v", s)
	}
}

func TestParseCustomSliceFromString_Vector2D_nestedBraces(t *testing.T) {
	custom := map[string][]model.Field{
		"Vector": {
			{RawName: "x", RawType: "int64"},
			{RawName: "y", RawType: "int64"},
			{RawName: "z", RawType: "int64"},
		},
	}
	s := `{{{1,2,3},{4,5,6}},{{7,8,9}}}`
	out, err := parseCustomSliceFromString("Vector", 2, s, custom)
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := out.([][]map[string]any)
	if !ok {
		t.Fatalf("type %T", out)
	}
	if len(rows) != 2 || len(rows[0]) != 2 || len(rows[1]) != 1 {
		t.Fatalf("shape len=%d row0=%d row1=%d", len(rows), len(rows[0]), len(rows[1]))
	}
	z, err := jsonToInt64(rows[1][0]["z"])
	if err != nil || z != 9 {
		t.Fatalf("z=%v err=%v", rows[1][0]["z"], err)
	}
}

func TestParseCustomSliceFromString_Vector2D_rowSepFlat(t *testing.T) {
	custom := map[string][]model.Field{
		"Vector": {
			{RawName: "x", RawType: "int64"},
			{RawName: "y", RawType: "int64"},
			{RawName: "z", RawType: "int64"},
		},
	}
	// 无最外层包裹时，行间仍可用 `}},{{` 串联多行（与 examples/writetestxlsx 中 vecGrid 一致）
	s := `{{10,20,30}},{{1,0,0},{2,0,0}}`
	out, err := parseCustomSliceFromString("Vector", 2, s, custom)
	if err != nil {
		t.Fatal(err)
	}
	rows := out.([][]map[string]any)
	if len(rows) != 2 || len(rows[0]) != 1 || len(rows[1]) != 2 {
		t.Fatalf("shape %#v", rows)
	}
}

func TestMapAsNumericKeyTuple(t *testing.T) {
	sch := []model.Field{
		{RawName: "x", RawType: "int64"},
		{RawName: "y", RawType: "int64"},
		{RawName: "z", RawType: "int64"},
	}
	raw := map[string]any{"0": float64(10), "1": float64(20), "2": float64(30)}
	custom := map[string][]model.Field{"Vector": sch}
	m, err := fillStructFromMap("Vector", raw, custom)
	if err != nil {
		t.Fatal(err)
	}
	if x, _ := jsonToInt64(m["x"]); x != 10 {
		t.Fatalf("x=%v", m["x"])
	}
}
