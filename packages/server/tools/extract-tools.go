//go:build ignore
// +build ignore

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  []Parameter `json:"parameters"`
	Tier        string      `json:"tier"`
	Category    string      `json:"category"`
	Tags        []string    `json:"tags"`
}

type Parameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

var (
	// Tool category mappings based on file names
	fileTierMap = map[string]string{
		"docker.go":         "local",
		"chezmoi.go":        "local",
		"execute.go":        "local",
		"bambu.go":          "local",
		"blender.go":        "local",
		"toolsmith.go":      "local",
		"agency.go":         "local",
		"calendar.go":       "hosted",
		"slack.go":          "hosted",
		"discord.go":        "hosted",
		"github.go":         "hosted",
		"notes.go":          "hosted",
		"focus.go":          "hosted",
		"learning.go":       "hosted",
		"sources.go":        "hosted",
		"cloudsync.go":      "hosted",
		"audit.go":          "hosted",
		"auth.go":           "hosted",
		"health.go":         "hosted",
		"secrets.go":        "hosted",
		"database.go":       "hosted",
		"env.go":            "hosted",
		"mermaid.go":        "hosted",
		"greptile.go":       "hosted",
		"sandbox.go":        "hosted",
		"persona.go":        "hosted",
		"jokes.go":          "hosted",
		"setup.go":          "hosted",
		"claude.go":         "hosted",
		"files.go":          "hosted",
		"si_improve.go":     "hosted",
		"repo.go":           "hosted",
		"gamma.go":          "hosted",
		"oauth_provider.go": "hosted",
	}

	fileCategoryMap = map[string]string{
		"auth.go":       "Auth",
		"calendar.go":   "Calendar",
		"slack.go":      "Slack",
		"discord.go":    "Discord",
		"github.go":     "GitHub",
		"notes.go":      "Notes",
		"focus.go":      "Focus",
		"learning.go":   "Learning",
		"sources.go":    "Sources",
		"docker.go":     "Docker",
		"chezmoi.go":    "Chezmoi",
		"execute.go":    "Execute",
		"bambu.go":      "Bambu",
		"blender.go":    "Blender",
		"toolsmith.go":  "Toolsmith",
		"agency.go":     "Agency",
		"audit.go":      "Audit",
		"health.go":     "Health",
		"secrets.go":    "Secrets",
		"database.go":   "Database",
		"env.go":        "Environment",
		"mermaid.go":    "Mermaid",
		"greptile.go":   "Greptile",
		"sandbox.go":    "Sandbox",
		"persona.go":    "Persona",
		"jokes.go":      "Jokes",
		"setup.go":      "Setup",
		"claude.go":     "Claude",
		"files.go":      "Files",
		"cloudsync.go":  "CloudSync",
		"si_improve.go": "Self-Improve",
		"repo.go":       "Repository",
		"gamma.go":      "Gamma",
	}
)

func main() {
	toolsDir := "."
	tools, err := extractTools(toolsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting tools: %v\n", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(tools, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(data))
}

func extractTools(dir string) ([]Tool, error) {
	var tools []Tool
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			if entry.Name() == "extract-tools.go" {
				continue
			}
			fileTools, err := extractFromFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: error reading %s: %v\n", entry.Name(), err)
				continue
			}

			tier := fileTierMap[entry.Name()]
			if tier == "" {
				tier = "hosted" // default
			}
			category := fileCategoryMap[entry.Name()]
			if category == "" {
				category = strings.TrimSuffix(entry.Name(), ".go")
			}

			for i := range fileTools {
				fileTools[i].Tier = tier
				fileTools[i].Category = category
			}

			tools = append(tools, fileTools...)
		}
	}

	return tools, nil
}

func extractFromFile(filepath string) ([]Tool, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filepath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var tools []Tool

	// Walk the AST looking for s.AddTool calls
	ast.Inspect(node, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			// Look for s.AddTool(mcp.NewTool("name", ...))
			if isAddToolCall(call) {
				if tool := extractToolFromCall(call); tool != nil {
					tools = append(tools, *tool)
				}
			}
		}
		return true
	})

	return tools, nil
}

func isAddToolCall(call *ast.CallExpr) bool {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok {
			return ident.Name == "s" && sel.Sel.Name == "AddTool"
		}
	}
	return false
}

func extractToolFromCall(call *ast.CallExpr) *Tool {
	if len(call.Args) == 0 {
		return nil
	}

	// First argument should be mcp.NewTool(...)
	newToolCall, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return nil
	}

	// Extract tool name from mcp.NewTool("name", ...)
	if len(newToolCall.Args) == 0 {
		return nil
	}

	nameExpr, ok := newToolCall.Args[0].(*ast.BasicLit)
	if !ok {
		return nil
	}

	name := strings.Trim(nameExpr.Value, `"`)

	// Extract description from mcp.WithDescription("...")
	description := ""
	for i := 1; i < len(newToolCall.Args); i++ {
		if call, ok := newToolCall.Args[i].(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "WithDescription" && len(call.Args) > 0 {
					if lit, ok := call.Args[0].(*ast.BasicLit); ok {
						description = strings.Trim(lit.Value, `"`)
					}
				}
			}
		}
	}

	// Extract parameters from remaining mcp.With* calls
	params := extractParameters(newToolCall)

	return &Tool{
		Name:        name,
		Description: description,
		Parameters:  params,
		Tags:        []string{},
	}
}

func extractParameters(call *ast.CallExpr) []Parameter {
	var params []Parameter

	for i := 1; i < len(call.Args); i++ {
		if call, ok := call.Args[i].(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				paramName, paramType, required, paramDesc := extractParamInfo(sel.Sel.Name, call)
				if paramName != "" {
					params = append(params, Parameter{
						Name:        paramName,
						Type:        paramType,
						Required:    required,
						Description: paramDesc,
					})
				}
			}
		}
	}

	return params
}

func extractParamInfo(withFuncName string, call *ast.CallExpr) (name, typ string, required bool, desc string) {
	if len(call.Args) == 0 {
		return
	}

	// First argument is usually the parameter name
	if lit, ok := call.Args[0].(*ast.BasicLit); ok {
		name = strings.Trim(lit.Value, `"`)
	} else {
		// If it's not a string literal, it might be mcp.Required() call or similar
		// In that case, skip parsing this parameter
		return
	}

	// Determine type from function name
	switch withFuncName {
	case "WithString":
		typ = "string"
	case "WithNumber":
		typ = "number"
	case "WithBoolean":
		typ = "boolean"
	case "WithObject":
		typ = "object"
	case "WithArray":
		typ = "array"
	default:
		typ = "string"
	}

	// Check if required (usually second or later argument)
	for _, arg := range call.Args[1:] {
		if sel, ok := arg.(*ast.SelectorExpr); ok {
			if sel.Sel.Name == "Required" {
				required = true
			}
		} else if ident, ok := arg.(*ast.Ident); ok {
			if ident.Name == "Required" {
				required = true
			}
		}
	}

	// Extract description from mcp.Description("...")
	for _, arg := range call.Args[1:] {
		if argCall, ok := arg.(*ast.CallExpr); ok {
			if sel, ok := argCall.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "Description" && len(argCall.Args) > 0 {
					if lit, ok := argCall.Args[0].(*ast.BasicLit); ok {
						desc = strings.Trim(lit.Value, `"`)
					}
				}
			}
		}
	}

	return
}
