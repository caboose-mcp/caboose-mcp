package tools

// docker — Docker container management via the docker CLI.
//
// All tools are thin wrappers around `docker` subprocess calls. Docker must
// be installed and the current user must have permission to run docker commands
// (typically via the docker group or sudo).
//
// Tools:
//   docker_list_containers — list running (or all) containers with name, image, status, ports
//   docker_inspect         — return full JSON config for a container by name or ID
//   docker_logs            — fetch the last N lines of logs from a container

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterDocker(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("docker_list_containers",
		mcp.WithDescription("List Docker containers (running by default, all with all=true)."),
		mcp.WithBoolean("all", mcp.Description("Include stopped containers")),
	), dockerListContainersHandler(cfg))

	s.AddTool(mcp.NewTool("docker_inspect",
		mcp.WithDescription("Inspect a Docker container and return its full JSON config."),
		mcp.WithString("container", mcp.Required(), mcp.Description("Container name or ID")),
	), dockerInspectHandler(cfg))

	s.AddTool(mcp.NewTool("docker_logs",
		mcp.WithDescription("Fetch logs from a Docker container."),
		mcp.WithString("container", mcp.Required(), mcp.Description("Container name or ID")),
		mcp.WithNumber("tail", mcp.Description("Number of lines from the end (default 50)")),
	), dockerLogsHandler(cfg))
}

func dockerListContainersHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := []string{"ps", "--format", "table {{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"}
		if req.GetBool("all", false) {
			args = append(args, "-a")
		}
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("docker error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}
}

func dockerInspectHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		container, err := req.RequireString("container")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out, err := exec.Command("docker", "inspect", container).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("docker error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}
}

func dockerLogsHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		container, err := req.RequireString("container")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		tail := req.GetInt("tail", 50)
		out, err := exec.Command("docker", "logs", "--tail", fmt.Sprintf("%d", tail), container).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("docker error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}
}
