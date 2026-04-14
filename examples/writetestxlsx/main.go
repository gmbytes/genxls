// writetestxlsx：生成 test/0.struct.xlsx 与 test/1.test.xlsx（供本模块测试与 xlsgen 演示输入）。
//
// 请在 xlsgen 模块根目录（tools/xlsgen）执行：
//
//	go run ./examples/writetestxlsx
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

func writeSheet(path, sheet string, rows [][]string) error {
	f := excelize.NewFile()
	idx, err := f.GetSheetIndex(f.GetSheetName(0))
	if err != nil {
		return err
	}
	_ = f.SetSheetName(f.GetSheetName(idx), sheet)

	for r, row := range rows {
		for c, v := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				return err
			}
			if err := f.SetCellStr(sheet, cell, v); err != nil {
				return err
			}
		}
	}
	return f.SaveAs(path)
}

func main() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	testDir := filepath.Join(wd, "test")

	// 与 test/0.struct.xlsx 一致：供 1.test.xlsx 引用 Vector、Attr
	structPath := filepath.Join(testDir, "0.struct.xlsx")
	structSheet := "struct_define"
	structRows := [][]string{
		{"struct_def", "X轴", "Y轴", "Z轴"},
		{"Vector", "x#int64", "y#int64", "z#int64"},
		{"struct_def", "属性类型", "属性值", "属性比率"},
		{"Attr", "type#int", "val#int64", "rate#int64"},
	}
	if err := writeSheet(structPath, structSheet, structRows); err != nil {
		panic(err)
	}
	fmt.Println("wrote", structPath)

	row0 := `{{10,20,30}}`
	row1 := `{{1,0,0},{2,0,0}}`
	vecGridRow3 := row0 + `,` + row1

	testPath := filepath.Join(testDir, "1.test.xlsx")
	sheet := "test"
	rows := [][]string{
		{},
		{"唯一id", "位置", "名字", "整型数组", "布尔", "分数", "整数", "属性", "向量一维Vector[]", "向量二维Vector[][]", "二维整数", "浮点数组", "64位数组", "描述"},
		{"cid#int64", "pos#Vector", "name#string", "abc#int[]", "flag#bool", "score#float64", "ival#int", "attr#Attr", "vecs#Vector[]", "vecGrid#Vector[][]", "grid#int[][]", "lf#float64[]", "i64s#int64[]", "desc#string"},
		{
			"1",
			"{100,100,100}",
			"小明",
			"{1,2,3}",
			"true",
			"3.14",
			"42",
			"{2,9,1}",
			`{{1,2,3},{4,5,6}}`,
			`{{{1,2,3},{4,5,6}},{{7,8,9}}}`,
			"{{1,2},{3,4}}",
			"{1.5,2.5}",
			"{9,8,7}",
			"row-A",
		},
		{
			"2",
			`{"x":0,"y":0,"z":0}`,
			"",
			"{}",
			"false",
			"-2.5",
			"-1",
			"{}",
			"{}",
			"{}",
			"{}",
			"{}",
			"{}",
			"",
		},
		{
			"3",
			"{1,2,3}",
			"中文emoji🙂",
			"{10,11}",
			"1",
			"1e-7",
			"0",
			`{"type":5,"val":7000000000,"rate":3}`,
			`{{0,0,0}}`,
			vecGridRow3,
			"{{1}}",
			"{0.1}",
			"{1}",
			"末尾空格 ",
		},
	}
	if err := writeSheet(testPath, sheet, rows); err != nil {
		panic(err)
	}
	fmt.Println("wrote", testPath)
}
