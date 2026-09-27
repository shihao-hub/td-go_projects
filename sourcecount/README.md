# sourcecount

`sourcecount` 是按文件后缀统计项目源码的 Go CLI/MCP 工具。

## 统计规则

- 文本文件：文件数、逻辑行数、Unicode code point 字符数。
- 二进制文件：文件数、字节数。
- 默认递归扫描当前目录；可传入多个文件或目录根路径。
- 默认不跟随 symbolic link、junction 和其他 reparse point。
- 默认排除 `.git`、`node_modules`、`vendor`、`dist`、`build`、`target` 等目录。

## CLI

```powershell
sourcecount.exe .
sourcecount.exe --json .
sourcecount.exe --config .sourcestats.json .
sourcecount.exe --no-default-excludes .
sourcecount.exe schema
sourcecount.exe mcp
```

参数错误退出码为 `2`；扫描过程中存在不可读文件时返回部分结果并退出码 `1`。

## 配置

配置文件名为 `.sourcestats.json`。显式 `--config` 优先，其次查找当前目录，再查找单根目录。

```json
{
  "version": 1,
  "include": ["**/*.go", "cmd/**"],
  "exclude": ["**/generated/**"],
  "text_extensions": [".tmpl"],
  "binary_extensions": [".dat"],
  "default_excludes": true
}
```

include 先缩小范围，exclude 最终否决。`*`、`?`、字符类匹配单个路径段，`**` 匹配零个或多个路径段。

## MCP

```json
{
  "mcpServers": {
    "sourcecount": {
      "command": "sourcecount.exe",
      "args": ["mcp"]
    }
  }
}
```

工具名为 `sourcecount.project.scan`。CLI 与 MCP 共用同一个 `internal/service.Server`，不会分别实现扫描逻辑。

## 构建

```powershell
uv run scripts/build.py --version 1.0.0
go build ./...
```