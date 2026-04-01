# genxls

game config xls to json / Go / C# / TypeScript / Rust

## Usage

```bash
./run.sh
```

Or run directly:

```bash
go run . --in ./xls --out ./out --lang all --pkg config
```

This will generate (with `--lang all`):

- `go.gen.go`
- `Pb.gen.Pb` (C#)
- `ts.gen.ts`
- `all.json` (default, can disable with `--json=false`)

> `--lang all` generates go + Pb + ts. Rust is **not** included in `all`; pass `--lang rust` or `--lang go,rust` explicitly.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--in` | `./xls` | Input xlsx file or directory |
| `--out` | `.` | Output directory |
| `--lang` | `all` | Target languages: `go`, `pb`, `ts`, `rust`, `all` (comma-separated) |
| `--pkg` | `config` | Go package name |
| `--flag` | _(none)_ | Export filter: `server` \| `client` |
| `--json` | `true` | Export aggregated `all.json` |
| `--split-json` | `false` | Export each table as a separate JSON file + `manifest.json` |
| `-v` | `false` | Verbose output |

Notes:

- `--in` can be a file or a directory. If omitted, it defaults to `./xls`.
- If a file fails to open as xlsx, the tool will retry parsing it as tab-separated text (TSV).
- Output is aggregated by sheet name (see "Output format").
- Multiple sheets within one xlsx file are all processed.

## Header rules

- **1 row header**
  - Row1: field definitions

- **2 rows header**
  - Row1: comment (ignored)
  - Row2: field definitions (exported)
  - default: horizontal table

- **3 rows header**
  - Row1 (cell A1): orientation marker
    - empty or `1`: horizontal
    - `2`: vertical _(not yet supported, reserved)_
  - Row2: comment (ignored)
  - Row3: field definitions (exported)

Field definition format:

`name#type[,s|c]`

- `#comment` / `#common`: ignored (not exported)
- `,s`: only export for `--flag server`
- `,c`: only export for `--flag client`

## Supported types

| Type | Go | C# | TypeScript | Rust |
|------|----|----|------------|------|
| `int` / `int32` / `int64` | `int` | `int` | `number` | `i64` |
| `float` / `float32` / `float64` | `float64` | `double` | `number` | `f64` |
| `bool` | `bool` | `bool` | `boolean` | `bool` |
| `string` | `string` | `string` | `string` | `String` |
| `int[]` | `[]int` | `List<int>` | `number[]` | `Vec<i64>` |
| `int[][]` | `[][]int` | `List<List<int>>` | `number[][]` | `Vec<Vec<i64>>` |

## Cell value format

- `int / float / bool / string`: normal cell values
- `int[]`: use brace-array (string cell) like `"{1,2,3}"` or `{}` for empty
- `int[][]`: use brace-array like `"{{1,2,3},{4,5,6}}"` or `{}` for empty
- `bool`: accepts `true`/`false`, `1`/`0`

The tool converts `{}` / `"{}"` to an empty JSON array.

## Output format

### all.json

The output JSON is an object keyed by sheet name (pluralized, lower camel case):

```json
{
  "items": [ ... ],
  "quests": [ ... ]
}
```

### Split JSON (`--split-json`)

When `--split-json` is enabled, each table is written to a separate file under `tables/`:

```
out/
  tables/
    items.json
    quests.json
  manifest.json
```

`manifest.json` contains metadata for each table:

```json
{
  "version": "20260402",
  "tables": {
    "items": {
      "file": "tables/items.json",
      "sha256": "...",
      "size": 1234,
      "row_count": 10,
      "rust_type": "Item"
    }
  }
}
```

### Go

`go.gen.go` contains:

- `type AllConfig struct { Items []Item \`json:"items"\`; Quests []Quest \`json:"quests"\` }`
- One `type <SheetName> struct { ... }` per sheet

Deserialize example:

```go
var cfg config.AllConfig
_ = json.Unmarshal(data, &cfg)
```

### C#

`Pb.gen.Pb` uses `System.Text.Json.Serialization.JsonPropertyName` so `all.json` can be deserialized into `AllConfig`.

### TypeScript

`ts.gen.ts` exports `interface AllConfig` with keys matching `all.json` (e.g. `items`, `quests`), plus individual `interface <SheetName>` per sheet.

### Rust

`config.gen.rs` uses `serde::Deserialize`. Field names are converted to `snake_case` with `#[serde(rename = "...")]` as needed.

```rust
let cfg: AllConfig = serde_json::from_str(&data)?;
```

Dependency required in `Cargo.toml`:

```toml
serde = { version = "1", features = ["derive"] }
serde_json = "1"
```
