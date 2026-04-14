// Package jsonout 负责写出聚合 JSON 与拆分 manifest。
package jsonout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"xlsgen/internal/model"
)

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

func WriteAllJSON(outPath string, payload map[string]any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, data, 0o644)
}

func WriteSplitAndManifest(outDir string, jsonPayload map[string]any, orderedTypeNames []string, verbose bool) error {
	tablesDir := filepath.Join(outDir, "tables")
	if err := os.MkdirAll(tablesDir, 0o755); err != nil {
		return err
	}

	manifest := ManifestFile{
		Version: time.Now().Format("20060102"),
		Tables:  make(map[string]ManifestTableEntry),
	}

	for _, typeName := range orderedTypeNames {
		fieldName := model.PluralizeTypeName(typeName)
		jsonKey := model.LowerFirst(fieldName)
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
