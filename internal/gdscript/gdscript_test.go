package gdscript

import (
	"testing"

	"xlsgen/internal/model"
)

func TestGdWorkbookFileSlug(t *testing.T) {
	cases := []struct {
		stem string
		want string
	}{
		{"1.character", "01.character"},
		{"0.struct", "00.struct"},
		{"10.character", "10.character"},
		{"foo", "foo"},
		{"MyTable", "mytable"},
		{"", "config"},
	}
	for _, tc := range cases {
		if got := gdWorkbookFileSlug(tc.stem); got != tc.want {
			t.Errorf("gdWorkbookFileSlug(%q) = %q, want %q", tc.stem, got, tc.want)
		}
	}
}

func TestGdWorkbookNumericPrefix(t *testing.T) {
	cases := []struct {
		stem string
		want string
	}{
		{"1.character", "01"},
		{"00.struct", "00"},
		{"10.character", "10"},
		{"100.tables", "100"},
		{"foo", "00"},
		{"", "00"},
		{"01something", "01"},
	}
	for _, tc := range cases {
		if got := gdWorkbookNumericPrefix(tc.stem); got != tc.want {
			t.Errorf("gdWorkbookNumericPrefix(%q) = %q, want %q", tc.stem, got, tc.want)
		}
	}
}

func TestGdConfigRowFileName(t *testing.T) {
	src := model.ConfigTableSource{WorkbookStem: "1.character", Sheet: "Charachter"}
	got := gdConfigRowFileName("Charachter", src)
	want := "c_01_charachter.gd"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGdSheetFileSlug(t *testing.T) {
	if got := gdSheetFileSlug("My Sheet"); got != "my_sheet" {
		t.Fatalf("got %q want my_sheet", got)
	}
}
