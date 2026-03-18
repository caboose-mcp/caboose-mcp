package tools

// mermaid — diagram generation via Mermaid syntax.
// Returns a fenced ```mermaid``` code block for any diagram type
// (flowchart, sequence, ER, Gantt, etc.). Rendering happens client-side
// in any Markdown viewer that supports Mermaid (GitHub, Obsidian, etc.).

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterMermaid(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("mermaid_generate",
		mcp.WithDescription("Generate a Mermaid diagram. type: db_schema | docker | flowchart | sequence"),
		mcp.WithString("type", mcp.Required(), mcp.Description("Diagram type: db_schema, docker, flowchart, sequence")),
		mcp.WithString("source", mcp.Description("JSON data or freeform description (for flowchart/sequence)")),
		mcp.WithString("connection_string", mcp.Description("PostgreSQL connection string for db_schema type")),
	), mermaidGenerateHandler(cfg))
}

func mermaidGenerateHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		diagramType, err := req.RequireString("type")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		source := req.GetString("source", "")

		switch diagramType {
		case "db_schema":
			connStr := req.GetString("connection_string", cfg.PostgresURL)
			return mermaidDBSchema(connStr)
		case "docker":
			return mermaidDocker()
		case "flowchart":
			return mermaidFlowchart(source)
		case "sequence":
			return mermaidSequence(source)
		default:
			return mcp.NewToolResultError(fmt.Sprintf("unknown type '%s': use db_schema, docker, flowchart, or sequence", diagramType)), nil
		}
	}
}

func mermaidDBSchema(connStr string) (*mcp.CallToolResult, error) {
	if connStr == "" {
		return mcp.NewToolResultError("connection_string or POSTGRES_URL required for db_schema"), nil
	}

	query := `SELECT c.table_name, c.column_name, c.data_type, c.is_nullable,
		COALESCE(tc.constraint_type, '')
		FROM information_schema.columns c
		LEFT JOIN information_schema.key_column_usage kcu
			ON c.table_name = kcu.table_name AND c.column_name = kcu.column_name AND c.table_schema = kcu.table_schema
		LEFT JOIN information_schema.table_constraints tc
			ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
		WHERE c.table_schema = 'public'
		ORDER BY c.table_name, c.ordinal_position`

	out, err := exec.Command("psql", connStr, "-t", "-A", "-F", ",", "-c", query).Output()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("psql error: %v", err)), nil
	}

	tables := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, ",", 5)
		if len(parts) < 4 {
			continue
		}
		tbl, col, dtype, nullable := parts[0], parts[1], parts[2], parts[3]
		pk := ""
		if len(parts) == 5 && parts[4] == "PRIMARY KEY" {
			pk = " PK"
		}
		notNull := ""
		if nullable == "NO" {
			notNull = " \"NOT NULL\""
		}
		tables[tbl] = append(tables[tbl], fmt.Sprintf("    %s %s%s%s", col, dtype, pk, notNull))
	}

	var sb strings.Builder
	sb.WriteString("```mermaid\nerDiagram\n")
	for tbl, cols := range tables {
		sb.WriteString(fmt.Sprintf("  %s {\n", tbl))
		for _, col := range cols {
			sb.WriteString(col + "\n")
		}
		sb.WriteString("  }\n")
	}
	sb.WriteString("```")
	return mcp.NewToolResultText(sb.String()), nil
}

func mermaidDocker() (*mcp.CallToolResult, error) {
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Image}}\t{{.Ports}}").Output()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("docker error: %v", err)), nil
	}

	var sb strings.Builder
	sb.WriteString("```mermaid\ngraph TD\n")
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		name := parts[0]
		image := ""
		if len(parts) > 1 {
			image = parts[1]
		}
		ports := ""
		if len(parts) > 2 {
			ports = parts[2]
		}
		label := name
		if image != "" {
			label = fmt.Sprintf("%s\\n%s", name, image)
		}
		if ports != "" {
			label = fmt.Sprintf("%s\\n%s", label, ports)
		}
		sb.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", sanitizeMermaidID(name), label))
	}
	sb.WriteString("```")
	return mcp.NewToolResultText(sb.String()), nil
}

func mermaidFlowchart(description string) (*mcp.CallToolResult, error) {
	if description == "" {
		return mcp.NewToolResultError("source description is required for flowchart"), nil
	}
	diagram := fmt.Sprintf("```mermaid\nflowchart TD\n  A[\"%s\"]\n```", strings.ReplaceAll(description, `"`, `'`))
	return mcp.NewToolResultText(diagram), nil
}

func mermaidSequence(description string) (*mcp.CallToolResult, error) {
	if description == "" {
		return mcp.NewToolResultError("source description is required for sequence"), nil
	}
	diagram := fmt.Sprintf("```mermaid\nsequenceDiagram\n  Note over System: %s\n```", description)
	return mcp.NewToolResultText(diagram), nil
}

func sanitizeMermaidID(s string) string {
	return strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(s)
}
