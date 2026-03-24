package tools

// terraform_ops — Terraform plan/apply/status operations for AWS infrastructure.
//
// Tools:
//   terraform_plan — run `tofu plan` on a Terraform patch and return diff
//   terraform_apply — apply a previously generated plan
//   terraform_status — show current resource state summary
//
// Plans are stored in ~/.claude/terraform-plans/ with a UUID and expire after 30 minutes.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caboose-mcp/server/config"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// terraformPlan holds metadata about a planned Terraform change
type terraformPlan struct {
	ID        string    `json:"id"`
	HCL       string    `json:"hcl"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Summary   string    `json:"summary"`
}

// terraformPlanStore manages active plans (in-memory with disk persistence)
type terraformPlanStore struct {
	mu    sync.Mutex
	plans map[string]*terraformPlan
	dir   string
}

var planStore *terraformPlanStore
var planStoreMu sync.Mutex

// initPlanStore initializes the global plan store and starts cleanup goroutine
func initPlanStore(claudeDir string) *terraformPlanStore {
	planStoreMu.Lock()
	defer planStoreMu.Unlock()

	if planStore != nil {
		return planStore
	}

	dir := filepath.Join(claudeDir, "terraform-plans")
	os.MkdirAll(dir, 0755)

	planStore = &terraformPlanStore{plans: make(map[string]*terraformPlan), dir: dir}

	// Load existing plans from disk
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				id := strings.TrimSuffix(e.Name(), ".json")
				data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
				var plan terraformPlan
				if err := json.Unmarshal(data, &plan); err == nil {
					planStore.plans[id] = &plan
				}
			}
		}
	}

	// Start expiration cleanup (every 5 minutes)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			planStore.expireOldPlans()
		}
	}()

	return planStore
}

func (s *terraformPlanStore) add(plan *terraformPlan) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.plans[plan.ID] = plan
	data, _ := json.MarshalIndent(plan, "", "  ")
	os.WriteFile(filepath.Join(s.dir, plan.ID+".json"), data, 0644)
	return plan.ID
}

func (s *terraformPlanStore) get(id string) *terraformPlan {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plans[id]
}

func (s *terraformPlanStore) expireOldPlans() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, plan := range s.plans {
		if now.After(plan.ExpiresAt) {
			delete(s.plans, id)
			os.Remove(filepath.Join(s.dir, id+".json"))
		}
	}
}

func RegisterTerraformOps(s *server.MCPServer, cfg *config.Config) {
	// Initialize plan store
	initPlanStore(cfg.ClaudeDir)

	s.AddTool(mcp.NewTool("terraform_plan",
		mcp.WithDescription("Generate a Terraform plan for proposed AWS infrastructure changes. Returns plan diff and ID for later apply."),
		mcp.WithString("description", mcp.Required(), mcp.Description("What resource(s) to create/modify (e.g. 'S3 bucket for logs')")),
		mcp.WithString("hcl_patch", mcp.Description("(Optional) HCL code snippet to add to main.tf")),
	), terraformPlanHandler(cfg))

	s.AddTool(mcp.NewTool("terraform_apply",
		mcp.WithDescription("Apply a previously planned Terraform change (after approval)."),
		mcp.WithString("plan_id", mcp.Required(), mcp.Description("Plan ID from terraform_plan output")),
	), terraformApplyHandler(cfg))

	s.AddTool(mcp.NewTool("terraform_status",
		mcp.WithDescription("Show current Terraform state summary (resources, counts)."),
	), terraformStatusHandler(cfg))
}

func terraformPlanHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		description, err := req.RequireString("description")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		hclPatch := req.GetString("hcl_patch", "")

		// Validate terraform dir
		tfDir := cfg.TerraformDir
		if tfDir == "" {
			tfDir = filepath.Join(os.Getenv("HOME"), "dev/fafb/terraform/aws")
		}

		// Check if main.tf exists
		mainTF := filepath.Join(tfDir, "main.tf")
		if _, err := os.Stat(mainTF); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Terraform directory not found: %s", tfDir)), nil
		}

		// If no HCL patch provided, have user provide it (this tool is called by the bot, which should generate it)
		if hclPatch == "" {
			return mcp.NewToolResultError("hcl_patch parameter required. Provide the HCL code to add to main.tf."), nil
		}

		// Write HCL patch to temp main.tf
		tempDir := filepath.Join(cfg.ClaudeDir, "terraform-temp")
		os.MkdirAll(tempDir, 0755)
		defer os.RemoveAll(tempDir)

		tempMain := filepath.Join(tempDir, "main.tf")
		originalContent, _ := os.ReadFile(mainTF)
		patchedContent := string(originalContent) + "\n\n" + hclPatch
		os.WriteFile(tempMain, []byte(patchedContent), 0644)

		// Run terraform plan in temp directory
		cmd := exec.Command(cfg.TofuBin, "plan", "-no-color", "-out=tfplan")
		cmd.Dir = tempDir

		// Copy .terraform lock and modules to temp (if they exist)
		for _, name := range []string{".terraform", ".terraform.lock.hcl", "variables.tf", "outputs.tf", "terraform.tfvars.example"} {
			src := filepath.Join(tfDir, name)
			dst := filepath.Join(tempDir, name)
			if fi, err := os.Stat(src); err == nil {
				if fi.IsDir() {
					os.RemoveAll(dst)
					copyDir(src, dst)
				} else {
					data, _ := os.ReadFile(src)
					os.WriteFile(dst, data, 0644)
				}
			}
		}

		// Run plan
		planOut, err := cmd.CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("terraform plan failed: %v\n%s", err, string(planOut))), nil
		}

		// Get diff from plan
		cmd2 := exec.Command(cfg.TofuBin, "show", "-no-color", "tfplan")
		cmd2.Dir = tempDir
		diffOut, err := cmd2.CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("terraform show failed: %v\n%s", err, string(diffOut))), nil
		}

		diff := string(diffOut)

		// Store plan
		plan := &terraformPlan{
			ID:        uuid.New().String()[:8],
			HCL:       hclPatch,
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(30 * time.Minute),
			Summary:   description,
		}
		planStore.add(plan)

		// Return plan ID and diff
		result := fmt.Sprintf("**Plan ID: %s** (expires in 30 min)\n\n**Change:** %s\n\n```\n%s\n```\n\nReview the diff above. Once Copilot reviews, reply with **approve** to apply.", plan.ID, description, diff)
		return mcp.NewToolResultText(result), nil
	}
}

func terraformApplyHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		planID, err := req.RequireString("plan_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Retrieve plan
		plan := planStore.get(planID)
		if plan == nil {
			return mcp.NewToolResultError(fmt.Sprintf("Plan not found: %s (may have expired)", planID)), nil
		}

		// Verify plan hasn't expired
		if time.Now().After(plan.ExpiresAt) {
			return mcp.NewToolResultError(fmt.Sprintf("Plan %s has expired", planID)), nil
		}

		tfDir := cfg.TerraformDir
		if tfDir == "" {
			tfDir = filepath.Join(os.Getenv("HOME"), "dev/fafb/terraform/aws")
		}

		// Read original main.tf
		mainTF := filepath.Join(tfDir, "main.tf")
		originalContent, _ := os.ReadFile(mainTF)

		// Append HCL patch
		patchedContent := string(originalContent) + "\n\n" + plan.HCL
		os.WriteFile(mainTF, []byte(patchedContent), 0644)

		// Run terraform init + apply
		initCmd := exec.Command(cfg.TofuBin, "init")
		initCmd.Dir = tfDir
		if _, err := initCmd.CombinedOutput(); err != nil {
			// Restore original
			os.WriteFile(mainTF, originalContent, 0644)
			return mcp.NewToolResultError(fmt.Sprintf("terraform init failed")), nil
		}

		applyCmd := exec.Command(cfg.TofuBin, "apply", "-auto-approve", "-no-color")
		applyCmd.Dir = tfDir
		applyOut, err := applyCmd.CombinedOutput()
		if err != nil {
			// Restore original
			os.WriteFile(mainTF, originalContent, 0644)
			return mcp.NewToolResultError(fmt.Sprintf("terraform apply failed: %v\n%s", err, string(applyOut))), nil
		}

		result := fmt.Sprintf("✓ **Terraform applied successfully**\n\nChange: %s\n\n```\n%s\n```", plan.Summary, string(applyOut))
		return mcp.NewToolResultText(result), nil
	}
}

func terraformStatusHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tfDir := cfg.TerraformDir
		if tfDir == "" {
			tfDir = filepath.Join(os.Getenv("HOME"), "dev/fafb/terraform/aws")
		}

		// Run terraform state show (summary)
		cmd := exec.Command(cfg.TofuBin, "state", "list")
		cmd.Dir = tfDir
		listOut, err := cmd.CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("terraform state list failed: %v", err)), nil
		}

		resources := strings.Split(strings.TrimSpace(string(listOut)), "\n")
		return mcp.NewToolResultText(fmt.Sprintf("**Terraform State Summary**\n\nTotal resources: %d\n\n%s", len(resources), strings.Join(resources, "\n"))), nil
	}
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyDir(srcPath, dstPath)
		} else {
			data, _ := io.ReadAll(open(srcPath))
			os.WriteFile(dstPath, data, 0644)
		}
	}
	return nil
}

func open(path string) *os.File {
	f, _ := os.Open(path)
	return f
}
