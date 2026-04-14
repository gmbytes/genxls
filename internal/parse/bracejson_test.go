package parse

import (
	"testing"
)

func TestParseBraceStructMatrix2D(t *testing.T) {
	row0 := `{{"x":10,"y":20,"z":30}}`
	row1 := `{{"x":1,"y":0,"z":0},{"x":2,"y":0,"z":0}}`
	s := row0 + `,` + row1
	rows, err := parseBraceStructMatrix2D(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(rows[0]) != 1 || len(rows[1]) != 2 {
		t.Fatalf("shape: %#v", rows)
	}
}

func TestBracesToStandardJSON_tupleAndNested(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"{1,2,3}", "[1,2,3]"},
		{"{{1,2},{3,4}}", "[[1,2],[3,4]]"},
		{`{{"x":1,"y":2,"z":3}}`, `[{"x":1,"y":2,"z":3}]`},
		{`{{"a":1},{"b":2}}`, `[{"a":1},{"b":2}]`},
	}
	for _, tc := range tests {
		got, err := bracesToStandardJSON(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q\ngot  %s\nwant %s", tc.in, got, tc.want)
		}
	}
}

func TestParseBraceArrayJSON_prefersValidJSON(t *testing.T) {
	var v []int
	if err := parseBraceArrayJSON("[1,2,3]", &v); err != nil {
		t.Fatal(err)
	}
	if len(v) != 3 || v[0] != 1 || v[1] != 2 || v[2] != 3 {
		t.Fatalf("%v", v)
	}
}
