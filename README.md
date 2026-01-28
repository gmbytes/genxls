# genxls
game config xls to json  cs go

## Usage

```bash
./run.sh
```

Or run directly:

```bash
go run . --in ./xls --out ./out --lang all --pkg config
```

This will generate:

- `go.gen.go`
- `cs.gen.cs`
- `ts.gen.ts`
- `all.json` (default, can disable with `--json=false`)

Notes:

- `--in` can be a file or a directory. If omitted, it defaults to `./xls`.
- If a file has `.xls/.xlsx` extension but its content is actually tab-separated text, it will still be parsed.
- Output is aggregated by sheet name (see "Output format").

## Header rules

- **1 row header**
  - Row1: field definitions

- **2 rows header**
  - Row1: comment (ignored)
  - Row2: field definitions (exported)
  - default: horizontal table

- **3 rows header**
  - Row1(A1): orientation marker
    - empty or `1`: horizontal
    - `2`: vertical
  - Row2: comment (ignored)
  - Row3: field definitions (exported)

Field definition format:

`name#type[,s|c]`

- `#comment` / `#common`: ignored (not exported)
- `,s`: only export for `--flag server`
- `,c`: only export for `--flag client`

## Supported types

- `int`
- `float`
- `bool`
- `string`
- `int[]`
- `int[][]`

## Cell value format

- `int/float/bool/string`: normal cell values
- `int[]`: use brace-array (string cell) like `"{1,2,3}"` or `{}` for empty
- `int[][]`: use brace-array like `"{{1,2,3},{4,5,6}}"` or `{}` for empty

The tool converts `{}`/`"{}"` to an empty JSON array.

## Output format

### all.json

The output JSON is an object keyed by sheet name (pluralized, lower camel case):

```json
{
  "items": [ ... ],
  "quests": [ ... ]
}
```

### Go

`go.gen.go` contains:

- `type AllConfig struct { Items []Item \`json:"items"\`; Quests []Quest \`json:"quests"\` }`
- One `type <SheetName> struct { ... }` per sheet

You can deserialize like:

```go
var cfg config.AllConfig
_ = json.Unmarshal(data, &cfg)
```

### C#

`cs.gen.cs` uses `System.Text.Json.Serialization.JsonPropertyName` so `all.json` can be deserialized into `AllConfig`.

### TypeScript

`ts.gen.ts` exports `interface AllConfig` with keys matching `all.json` (e.g. `items`, `quests`).


