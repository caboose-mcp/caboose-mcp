package tools

// org_dep_graph — Dependency graph indexing and visualization for GitHub organizations.
//
// Tools:
//   dep_index  — Scan all org repos, parse go.mod and package.json, build dependency graph
//   dep_graph  — Render dependency relationships as a Mermaid diagram
//   dep_search — Search the dependency cache for repos using a specific package

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// DepGraphCache holds the indexed dependency graph for all org repos
type DepGraphCache struct {
	Repos     []RepoDepInfo `json:"repos"`
	IndexedAt time.Time     `json:"indexed_at"`
}

// RepoDepInfo holds dependency information for a single repo
type RepoDepInfo struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Org      string    `json:"org"`
	Stack    []string  `json:"stack"` // e.g., ["Go", "Node.js"]
	GoDeps   []GoDep   `json:"go_deps"`
	NodeDeps []NodeDep `json:"node_deps"`
}

// GoDep represents a Go module dependency
type GoDep struct {
	Module     string `json:"module"`
	Version    string `json:"version"`
	IsIndirect bool   `json:"is_indirect"`
}

// NodeDep represents a Node.js package dependency
type NodeDep struct {
	Package string `json:"package"`
	Version string `json:"version"`
	IsDev   bool   `json:"is_dev"`
}

func RegisterOrgDepGraph(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("dep_index",
		mcp.WithDescription("Index dependencies across all GitHub org repos. Parses go.mod and package.json. Caches result to ~/.claude/dep-graph.json"),
		mcp.WithString("orgs", mcp.Description("Comma-separated org names (defaults to GITHUB_ORGS env var)")),
	), depIndexHandler(cfg))

	s.AddTool(mcp.NewTool("dep_graph",
		mcp.WithDescription("Render dependency graph as Mermaid diagram. Shows which repos depend on which."),
		mcp.WithString("filter", mcp.Description("Filter repos/packages by substring (optional)")),
		mcp.WithBoolean("show_external", mcp.Description("Include external (non-org) dependencies (default: false)")),
	), depGraphHandler(cfg))

	s.AddTool(mcp.NewTool("dep_search",
		mcp.WithDescription("Search dependency cache for repos using a specific package/module."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Package or module name substring (case-insensitive)")),
	), depSearchHandler(cfg))
}

func depIndexHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		orgsStr := req.GetString("orgs", "")
		if orgsStr == "" {
			orgsStr = strings.Join(cfg.GitHubOrgs, ",")
		}
		if orgsStr == "" {
			return mcp.NewToolResultError("No orgs configured. Set GITHUB_ORGS=org1,org2 or pass 'orgs' param"), nil
		}

		orgs := strings.Split(orgsStr, ",")
		for i := range orgs {
			orgs[i] = strings.TrimSpace(orgs[i])
		}

		cache, err := indexOrgDeps(orgs)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to index deps: %v", err)), nil
		}

		if err := saveDepCache(cfg, cache); err != nil {
			log.Printf("[dep_graph] Failed to save cache: %v", err)
			// Don't fail the tool — cache was built successfully
		}

		// Count dependencies
		goDepsCount := 0
		nodeDepsCount := 0
		for _, repo := range cache.Repos {
			goDepsCount += len(repo.GoDeps)
			nodeDepsCount += len(repo.NodeDeps)
		}

		result := fmt.Sprintf(`Dependency graph indexed:
  Repos indexed: %d
  Go dependencies found: %d
  Node.js dependencies found: %d
  Indexed at: %s
  Cache: ~/.claude/dep-graph.json`,
			len(cache.Repos), goDepsCount, nodeDepsCount, cache.IndexedAt.Format("2006-01-02 15:04:05"))

		return mcp.NewToolResultText(result), nil
	}
}

func depGraphHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filter := req.GetString("filter", "")
		showExternal := req.GetBool("show_external", false)

		cache, err := loadDepCache(cfg)
		if err != nil || cache == nil {
			return mcp.NewToolResultError("Dependency cache not found. Run dep_index first."), nil
		}

		diagram := renderDepGraph(cache, filter, showExternal)
		return mcp.NewToolResultText(diagram), nil
	}
}

func depSearchHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := req.GetString("query", "")
		if query == "" {
			return mcp.NewToolResultError("query parameter required"), nil
		}

		cache, err := loadDepCache(cfg)
		if err != nil || cache == nil {
			return mcp.NewToolResultError("Dependency cache not found. Run dep_index first."), nil
		}

		results := searchDeps(cache, query)
		if len(results) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No dependencies found matching '%s'", query)), nil
		}

		resultJSON, _ := json.MarshalIndent(results, "", "  ")
		return mcp.NewToolResultText(string(resultJSON)), nil
	}
}

// depCachePath returns the path to the dep-graph cache file
func depCachePath(cfg *config.Config) string {
	return filepath.Join(cfg.ClaudeDir, "dep-graph.json")
}

// loadDepCache loads the dependency graph cache from disk
func loadDepCache(cfg *config.Config) (*DepGraphCache, error) {
	path := depCachePath(cfg)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[dep_graph] Failed to read cache: %v", err)
		}
		return nil, err
	}

	var cache DepGraphCache
	if err := json.Unmarshal(data, &cache); err != nil {
		log.Printf("[dep_graph] Failed to unmarshal cache: %v", err)
		return nil, err
	}

	return &cache, nil
}

// saveDepCache persists the dependency graph cache to disk
func saveDepCache(cfg *config.Config, c *DepGraphCache) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	path := depCachePath(cfg)
	if err := os.WriteFile(path, data, 0600); err != nil {
		log.Printf("[dep_graph] Failed to save cache: %v", err)
		return err
	}

	return nil
}

// fetchFileFromRepo fetches a file from a GitHub repo via `gh api`
func fetchFileFromRepo(org, repo, path string) (string, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/%s/%s/contents/%s", org, repo, path))
	out, err := cmd.CombinedOutput()
	if err != nil {
		// File not found or API error — return empty string silently
		return "", nil
	}

	var result struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", nil
	}

	// GitHub API returns base64-encoded content
	decoded, err := base64.StdEncoding.DecodeString(result.Content)
	if err != nil {
		return "", nil
	}

	return string(decoded), nil
}

// parseGoMod parses a go.mod file and returns a list of dependencies
func parseGoMod(content string) []GoDep {
	var deps []GoDep
	scanner := bufio.NewScanner(strings.NewReader(content))

	inRequire := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if strings.HasPrefix(line, "//") || line == "" {
			continue
		}

		// Handle "require (" block
		if strings.HasPrefix(line, "require (") {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}

		// Parse single-line require
		if strings.HasPrefix(line, "require ") && !inRequire {
			parts := strings.Fields(strings.TrimPrefix(line, "require"))
			if len(parts) >= 2 {
				module := parts[0]
				version := parts[1]
				isIndirect := len(parts) > 2 && strings.HasPrefix(parts[2], "//")
				deps = append(deps, GoDep{Module: module, Version: version, IsIndirect: isIndirect})
			}
			continue
		}

		// Parse lines within require block
		if inRequire && line != "" && !strings.HasPrefix(line, ")") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				module := parts[0]
				version := parts[1]
				isIndirect := len(parts) > 2 && strings.HasPrefix(parts[2], "//")
				deps = append(deps, GoDep{Module: module, Version: version, IsIndirect: isIndirect})
			}
		}
	}

	return deps
}

// parsePackageJSON parses a package.json file and returns dependencies
func parsePackageJSON(content string) []NodeDep {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	if err := json.Unmarshal([]byte(content), &pkg); err != nil {
		return []NodeDep{}
	}

	var deps []NodeDep

	// Prod dependencies
	for name, version := range pkg.Dependencies {
		deps = append(deps, NodeDep{Package: name, Version: version, IsDev: false})
	}

	// Dev dependencies
	for name, version := range pkg.DevDependencies {
		deps = append(deps, NodeDep{Package: name, Version: version, IsDev: true})
	}

	return deps
}

// indexOrgDeps indexes dependencies for all repos in the given organizations
func indexOrgDeps(orgs []string) (*DepGraphCache, error) {
	cache := &DepGraphCache{
		Repos:     []RepoDepInfo{},
		IndexedAt: time.Now(),
	}

	for _, org := range orgs {
		repos, err := fetchRepoList(org)
		if err != nil {
			log.Printf("[dep_graph] Failed to fetch repo list for %s: %v", org, err)
			continue
		}

		for _, repoName := range repos {
			// Rate limit: small delay between API calls
			time.Sleep(200 * time.Millisecond)

			repoInfo := RepoDepInfo{
				Name:     repoName,
				Org:      org,
				URL:      fmt.Sprintf("https://github.com/%s/%s", org, repoName),
				Stack:    []string{},
				GoDeps:   []GoDep{},
				NodeDeps: []NodeDep{},
			}

			// Try to fetch go.mod
			if goModContent, err := fetchFileFromRepo(org, repoName, "go.mod"); err == nil && goModContent != "" {
				repoInfo.Stack = append(repoInfo.Stack, "Go")
				repoInfo.GoDeps = parseGoMod(goModContent)
			}

			// Try to fetch package.json
			if pkgContent, err := fetchFileFromRepo(org, repoName, "package.json"); err == nil && pkgContent != "" {
				repoInfo.Stack = append(repoInfo.Stack, "Node.js")
				repoInfo.NodeDeps = parsePackageJSON(pkgContent)
			}

			// Only add repos that have at least one manifest
			if len(repoInfo.Stack) > 0 {
				cache.Repos = append(cache.Repos, repoInfo)
			}
		}
	}

	return cache, nil
}

// renderDepGraph renders the dependency graph as a Mermaid diagram
func renderDepGraph(cache *DepGraphCache, filter string, showExternal bool) string {
	var sb strings.Builder

	sb.WriteString("```mermaid\n")
	sb.WriteString("graph TD\n")

	// Build node map: repoName -> RepoDepInfo
	repoMap := make(map[string]*RepoDepInfo)
	moduleToRepo := make(map[string]string) // module name -> repo name
	for i := range cache.Repos {
		repo := &cache.Repos[i]
		repoMap[repo.Name] = repo
		// Map common module paths to repo names (e.g., github.com/org/meml -> meml)
		for _, dep := range repo.GoDeps {
			parts := strings.Split(dep.Module, "/")
			if len(parts) > 0 {
				moduleToRepo[dep.Module] = parts[len(parts)-1]
			}
		}
	}

	// Draw repo nodes
	drawnNodes := make(map[string]bool)
	externalNodes := make(map[string]bool)

	for _, repo := range cache.Repos {
		if filter != "" && !strings.Contains(repo.Name, filter) && !strings.Contains(repo.Org, filter) {
			continue
		}

		nodeID := sanitizeMermaidID(repo.Name)
		label := fmt.Sprintf("%s (%s)", repo.Name, strings.Join(repo.Stack, "/"))

		sb.WriteString(fmt.Sprintf("  %s[\"%s\"]:::org\n", nodeID, label))
		drawnNodes[nodeID] = true

		// Draw edges to intra-org dependencies
		for _, dep := range repo.GoDeps {
			// Check if this module maps to another repo in our org
			if targetRepoName, exists := moduleToRepo[dep.Module]; exists {
				if targetRepo, ok := repoMap[targetRepoName]; ok && targetRepo.Org == repo.Org {
					targetNodeID := sanitizeMermaidID(targetRepoName)
					sb.WriteString(fmt.Sprintf("  %s -->|requires %s| %s\n", nodeID, dep.Version, targetNodeID))
				}
			}

			// External dependency
			if showExternal && !strings.Contains(dep.Module, repo.Org) {
				extNodeID := sanitizeMermaidID(dep.Module)
				label := fmt.Sprintf("%s (%s)", strings.Split(dep.Module, "/")[len(strings.Split(dep.Module, "/"))-1], dep.Version)
				if !externalNodes[extNodeID] {
					sb.WriteString(fmt.Sprintf("  %s[\"%s\"]:::external\n", extNodeID, label))
					externalNodes[extNodeID] = true
				}
				sb.WriteString(fmt.Sprintf("  %s -->|uses| %s\n", nodeID, extNodeID))
			}
		}
	}

	// CSS classes for styling
	sb.WriteString("  classDef org fill:#4a90d9,color:#fff,font-weight:bold\n")
	sb.WriteString("  classDef external fill:#999,color:#fff,opacity:0.7\n")

	sb.WriteString("```\n")
	sb.WriteString(fmt.Sprintf("\n*Indexed: %s*", cache.IndexedAt.Format("2006-01-02 15:04:05")))

	return sb.String()
}

// searchDeps searches the cache for repos using a given package/module
func searchDeps(cache *DepGraphCache, query string) []map[string]interface{} {
	var results []map[string]interface{}
	queryLower := strings.ToLower(query)

	for _, repo := range cache.Repos {
		// Search Go dependencies
		for _, dep := range repo.GoDeps {
			if strings.Contains(strings.ToLower(dep.Module), queryLower) {
				results = append(results, map[string]interface{}{
					"repo":     repo.Name,
					"org":      repo.Org,
					"package":  dep.Module,
					"version":  dep.Version,
					"type":     "go",
					"indirect": dep.IsIndirect,
				})
			}
		}

		// Search Node.js dependencies
		for _, dep := range repo.NodeDeps {
			if strings.Contains(strings.ToLower(dep.Package), queryLower) {
				results = append(results, map[string]interface{}{
					"repo":    repo.Name,
					"org":     repo.Org,
					"package": dep.Package,
					"version": dep.Version,
					"type":    "node",
					"is_dev":  dep.IsDev,
				})
			}
		}
	}

	return results
}
