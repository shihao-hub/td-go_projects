package cli

// schema.go 保留 CLI 的 Schema 入口归属；实际工具定义由 internal/mcp
// 通过 in-memory transport 导出，避免 CLI 与 MCP 维护两份契约。
