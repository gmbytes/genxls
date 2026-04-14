// load_all_json：演示读取 xlsgen 生成的聚合 JSON，按表名打印行数（不依赖具体 Go 生成类型）。
//
// 请先用 xlsgen 生成 JSON，例如默认路径：
//
//	go run . --in ./test --out ./gen --lang go
//
// 然后在本模块根目录执行：
//
//	go run ./examples/load_all_json -json ./gen/all.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	jsonPath := flag.String("json", "gen/all.json", "path to aggregated JSON (relative to cwd or absolute)")
	flag.Parse()

	raw, err := os.ReadFile(*jsonPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", *jsonPath, err)
		os.Exit(1)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		fmt.Fprintf(os.Stderr, "json unmarshal: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("loaded %s: %d top-level table(s)\n", *jsonPath, len(root))
	for key, msg := range root {
		var rows []map[string]any
		if err := json.Unmarshal(msg, &rows); err != nil {
			fmt.Fprintf(os.Stderr, "table %q: not a JSON array of objects: %v\n", key, err)
			continue
		}
		fmt.Printf("  %s: %d row(s)\n", key, len(rows))
	}
}
