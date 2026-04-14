package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xlsgen/internal/gdscript"
	"xlsgen/internal/genbundle"
	"xlsgen/internal/golang"
	"xlsgen/internal/jsonout"
	"xlsgen/internal/parse"
)

type options struct {
	InPath    string
	OutDir    string
	GdOutDir  string
	GoOut     string
	AllJSON   string
	Flag      string
	Lang      string
	Pkg       string
	JSON      bool
	SplitJSON bool
	Verbose   bool
}

func main() {
	var opts options
	flag.StringVar(&opts.InPath, "in", "", "input xlsx file or directory (default: ./xls)")
	flag.StringVar(&opts.OutDir, "out", ".", "output directory")
	flag.StringVar(&opts.GdOutDir, "gd-out", "", "GDScript output directory (default: <out>/gd)")
	flag.StringVar(&opts.Flag, "flag", "", "export flag: server|client (optional)")
	flag.StringVar(&opts.Lang, "lang", "all", "target lang: go|gd|all (or comma-separated)")
	flag.StringVar(&opts.Pkg, "pkg", "xls", "go package name for generated Go file")
	flag.StringVar(&opts.GoOut, "go-out", "", "generated Go file path (default: <out>/xls.gen.go)")
	flag.StringVar(&opts.AllJSON, "all-json", "", "aggregated JSON output path (default: <out>/all.json)")
	flag.BoolVar(&opts.JSON, "json", true, "export json data")
	flag.BoolVar(&opts.SplitJSON, "split-json", false, "split each table into separate json file + manifest")
	flag.BoolVar(&opts.Verbose, "v", false, "verbose")
	flag.Parse()

	if opts.InPath == "" {
		opts.InPath = "xls"
	}
	inPaths, err := parse.ResolveInputPaths(opts.InPath)
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

	const rootName = "AllConfig"
	res, err := genbundle.Build(inPaths, opts.Flag)
	if err != nil {
		exitErr(err)
	}
	jsonPayload := res.JSONPayload

	if langs["go"] {
		goCode, err := golang.GenerateBundle(opts.Pkg, rootName, res.CustomOrder, res.CustomStructs, res.OrderedTypeNames, res.Schemas)
		if err != nil {
			exitErr(err)
		}
		outFile := opts.GoOut
		if outFile == "" {
			outFile = filepath.Join(opts.OutDir, "xls.gen.go")
		} else {
			outFile = filepath.Clean(outFile)
		}
		if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
			exitErr(err)
		}
		if err := os.WriteFile(outFile, []byte(goCode), 0o644); err != nil {
			exitErr(err)
		}
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "generated %s\n", outFile)
		}
	}

	if langs["gd"] {
		gdFiles, err := gdscript.GenerateBundle(res.OrderedTypeNames, res.Schemas, res.CustomOrder, res.CustomStructs, res.ConfigTableSources)
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
		jsonFile := opts.AllJSON
		if jsonFile == "" {
			jsonFile = filepath.Join(opts.OutDir, "all.json")
		} else {
			jsonFile = filepath.Clean(jsonFile)
		}
		if err := os.MkdirAll(filepath.Dir(jsonFile), 0o755); err != nil {
			exitErr(err)
		}
		if err := jsonout.WriteAllJSON(jsonFile, jsonPayload); err != nil {
			exitErr(err)
		}
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "generated %s\n", jsonFile)
		}
	}

	if opts.SplitJSON {
		if err := jsonout.WriteSplitAndManifest(opts.OutDir, jsonPayload, res.OrderedTypeNames, opts.Verbose); err != nil {
			exitErr(err)
		}
	}
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
