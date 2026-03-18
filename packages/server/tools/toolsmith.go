package tools

// Toolsmith — meta-tools for writing new caboose-mcp tools.
//
// Workflow:
//  1. tool_scaffold  — generate boilerplate Go source for a new tool given name,
//                      description, and parameter definitions. Returns ready-to-edit code.
//  2. tool_write     — write arbitrary Go source to tools/<filename>.go and update
//                      main.go to register the new Register* func.
//  3. tool_rebuild   — run `go build -o caboose-mcp .` in the project root.
//  4. tool_list      — list existing tool source files + the tools each registers.

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// projectRoot is the directory that contains main.go.
// We locate it relative to the running binary's directory at build time
// by using the module root embedded in go.mod, but at runtime we just
// look for the go.mod file walking upward from the binary's location.
func projectRoot() (string, error) {
	// Try well-known path first
	candidates := []string{
		"/home/caboose/dev/caboose-mcp",
	}
	// Walk upward from current working directory
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				candidates = append([]string{dir}, candidates...)
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "go.mod")); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("could not locate project root (go.mod not found)")
}

// ---- Parameter definition ----

type ToolParam struct {
	Name        string
	Type        string // string | number | boolean
	Required    bool
	Description string
}

func RegisterToolsmith(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("tool_scaffold",
		mcp.WithDescription("Generate boilerplate Go source code for a new MCP tool. Returns source you can review, edit, then pass to tool_write."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Tool name (snake_case, e.g. weather_get)")),
		mcp.WithString("description", mcp.Required(), mcp.Description("What the tool does")),
		mcp.WithString("params", mcp.Description(`JSON array of param definitions: [{"name":"city","type":"string","required":true,"description":"City name"}]`)),
		mcp.WithString("register_func", mcp.Description("Name of the Register* function (default: Register<CamelName>)")),
		mcp.WithString("filename", mcp.Description("Output filename without extension (default: <name>)")),
	), toolScaffoldHandler(cfg))

	s.AddTool(mcp.NewTool("tool_write",
		mcp.WithDescription("Write Go source to tools/<filename>.go and patch main.go to call the Register* function. Then call tool_rebuild to apply."),
		mcp.WithString("filename", mcp.Required(), mcp.Description("Filename without path or extension (e.g. weather) → writes tools/weather.go")),
		mcp.WithString("source", mcp.Required(), mcp.Description("Complete Go source for the file")),
		mcp.WithString("register_func", mcp.Description("Register* function name to add to main.go (optional; skip if already registered or no registration needed)")),
	), toolWriteHandler(cfg))

	s.AddTool(mcp.NewTool("tool_rebuild",
		mcp.WithDescription("Rebuild the caboose-mcp binary (go build -o caboose-mcp .). Returns compiler output."),
	), toolRebuildHandler(cfg))

	s.AddTool(mcp.NewTool("tool_list",
		mcp.WithDescription("List tool source files and the MCP tools each one registers."),
	), toolListHandler(cfg))
}

// ---- scaffold ----

var scaffoldTmpl = template.Must(template.New("tool").Funcs(template.FuncMap{
	"title": func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	},
	"camel": snakeToCamel,
	"goType": func(t string) string {
		switch t {
		case "number":
			return "float64"
		case "boolean":
			return "bool"
		default:
			return "string"
		}
	},
	"mcpType": func(t string) string {
		switch t {
		case "number":
			return "mcp.WithNumber"
		case "boolean":
			return "mcp.WithBoolean"
		default:
			return "mcp.WithString"
		}
	},
	"getter": func(p ToolParam) string {
		switch p.Type {
		case "number":
			return fmt.Sprintf(`req.GetFloat("%s", 0)`, p.Name)
		case "boolean":
			return fmt.Sprintf(`req.GetBool("%s", false)`, p.Name)
		default:
			if p.Required {
				return fmt.Sprintf(`req.RequireString("%s")`, p.Name)
			}
			return fmt.Sprintf(`req.GetString("%s", "")`, p.Name)
		}
	},
}).Parse(`package tools

import (
	"context"
	"fmt"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func {{.RegisterFunc}}(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("{{.ToolName}}",
		mcp.WithDescription("{{.Description}}"),
{{- range .Params}}
		{{mcpType .Type}}("{{.Name}}"{{if .Required}}, mcp.Required(){{end}}, mcp.Description("{{.Description}}")),
{{- end}}
	), {{.HandlerFunc}}(cfg))
}

func {{.HandlerFunc}}(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
{{- range .Params}}
{{- if .Required}}
{{- if eq .Type "string"}}
		{{.Name}}, err := {{getter .}}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
{{- else}}
		{{.Name}} := {{getter .}}
{{- end}}
{{- else}}
		{{.Name}} := {{getter .}}
{{- end}}
{{- end}}

		// TODO: implement {{.ToolName}} logic
		_ = fmt.Sprintf("") // remove if unused
{{- range .Params}}
		_ = {{.Name}}
{{- end}}

		return mcp.NewToolResultText("not implemented"), nil
	}
}
`))

type scaffoldData struct {
	RegisterFunc string
	HandlerFunc  string
	ToolName     string
	Description  string
	Params       []ToolParam
}

func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func toolScaffoldHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		description, err := req.RequireString("description")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		camel := snakeToCamel(name)
		registerFunc := req.GetString("register_func", "Register"+camel)
		handlerFunc := name + "Handler"

		// Parse params JSON if provided
		var params []ToolParam
		paramsJSON := req.GetString("params", "")
		if paramsJSON != "" {
			// Simple JSON parse — avoid importing encoding/json just for this by using it directly
			import_json_parse(&params, paramsJSON)
		}

		data := scaffoldData{
			RegisterFunc: registerFunc,
			HandlerFunc:  handlerFunc,
			ToolName:     name,
			Description:  description,
			Params:       params,
		}

		var buf bytes.Buffer
		if err := scaffoldTmpl.Execute(&buf, data); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("template error: %v", err)), nil
		}

		filename := req.GetString("filename", name)
		return mcp.NewToolResultText(fmt.Sprintf(
			"// Suggested filename: tools/%s.go\n// Register func: %s\n// Call tool_write with filename=%q and source=<below>\n\n%s",
			filename, registerFunc, filename, buf.String(),
		)), nil
	}
}

// import_json_parse is a minimal JSON array parser for ToolParam to avoid
// needing a runtime import only used during scaffolding.
// It delegates to encoding/json via a local unmarshal.
func import_json_parse(params *[]ToolParam, raw string) {
	// Use encoding/json — it's already in the binary via other tools
	import_json_unmarshal(params, raw)
}

// ---- write ----

func toolWriteHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename, err := req.RequireString("filename")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		source, err := req.RequireString("source")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		registerFunc := req.GetString("register_func", "")

		// Validate filename — no path separators
		if strings.ContainsAny(filename, "/\\") {
			return mcp.NewToolResultError("filename must not contain path separators"), nil
		}
		// Strip .go suffix if provided
		filename = strings.TrimSuffix(filename, ".go")

		root, err := projectRoot()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		destPath := filepath.Join(root, "tools", filename+".go")

		// Quick parse check — make sure it at least has `package tools`
		if !strings.Contains(source, "package tools") {
			return mcp.NewToolResultError("source must declare `package tools`"), nil
		}

		if err := os.WriteFile(destPath, []byte(source), 0644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write error: %v", err)), nil
		}

		msg := fmt.Sprintf("wrote %s (%d bytes)", destPath, len(source))

		// Patch main.go if registerFunc provided
		if registerFunc != "" {
			if err := patchMainGo(root, registerFunc, cfg); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("file written but main.go patch failed: %v\nAdd `tools.%s(s, cfg)` to main.go manually.", err, registerFunc)), nil
			}
			msg += fmt.Sprintf("\npatched main.go to call tools.%s(s, cfg)", registerFunc)
		}

		msg += "\nRun tool_rebuild to compile."
		return mcp.NewToolResultText(msg), nil
	}
}

// patchMainGo inserts `tools.RegisterFunc(s, cfg)` before the ServeStdio call.
func patchMainGo(root, registerFunc string, cfg *config.Config) error {
	mainPath := filepath.Join(root, "main.go")
	data, err := os.ReadFile(mainPath)
	if err != nil {
		return err
	}
	content := string(data)

	call := fmt.Sprintf("tools.%s(s, cfg)", registerFunc)
	if strings.Contains(content, call) {
		return nil // already registered
	}

	// Insert before `if err := server.ServeStdio`
	marker := "\n\tif err := server.ServeStdio"
	replacement := fmt.Sprintf("\n\t%s%s", call, marker)
	if !strings.Contains(content, marker) {
		return fmt.Errorf("could not find ServeStdio marker in main.go")
	}
	content = strings.Replace(content, marker, replacement, 1)
	return os.WriteFile(mainPath, []byte(content), 0644)
}

// ---- rebuild ----

func toolRebuildHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		root, err := projectRoot()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		goExe, err := findGo()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		cmd := exec.Command(goExe, "build", "-o", "caboose-mcp", ".")
		cmd.Dir = root
		// Add Go bin to PATH so `go` can find toolchain
		cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/usr/local/go/bin")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("build failed:\n%s", out)), nil
		}
		if len(strings.TrimSpace(string(out))) == 0 {
			return mcp.NewToolResultText("build succeeded — binary updated"), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("build succeeded:\n%s", out)), nil
	}
}

func findGo() (string, error) {
	for _, p := range []string{"/usr/local/go/bin/go", "/usr/bin/go", "/usr/local/bin/go"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if path, err := exec.LookPath("go"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("go binary not found; ensure Go is installed")
}

// ---- list ----

func toolListHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		root, err := projectRoot()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		entries, err := os.ReadDir(filepath.Join(root, "tools"))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("readdir: %v", err)), nil
		}

		fset := token.NewFileSet()
		var lines []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			path := filepath.Join(root, "tools", e.Name())
			toolNames := extractToolNames(fset, path)
			if len(toolNames) > 0 {
				lines = append(lines, fmt.Sprintf("  %s\n    %s", e.Name(), strings.Join(toolNames, ", ")))
			} else {
				lines = append(lines, "  "+e.Name())
			}
		}

		return mcp.NewToolResultText("Tool source files:\n" + strings.Join(lines, "\n")), nil
	}
}

// extractToolNames parses a Go source file and finds all string literals
// passed as the first argument to mcp.NewTool(...) — these are the tool names.
func extractToolNames(fset *token.FileSet, path string) []string {
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil
	}
	var names []string
	re := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "NewTool" {
			return true
		}
		if len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name := strings.Trim(lit.Value, `"`)
		if re.MatchString(name) {
			names = append(names, name)
		}
		return true
	})
	return names
}
