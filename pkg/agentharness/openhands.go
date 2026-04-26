package agentharness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"

	agentv1alpha1 "github.com/theakshaypant/yeet/pkg/apis/agent/v1alpha1"
)

// Sandbox paths. TaskFile is written by the controller before execution.
// ResultFile is written by the agent and read by the controller after
// execution. The result file convention is part of the harness config
// (injected via OpenHands system prompt), not the user's purpose.
const (
	TaskFile   = "/workspace/task.md"
	ResultFile = "/workspace/.yeet/result.json"
)

// LLM provider name → default OpenHands model identifier.
var defaultModels = map[string]string{
	"anthropic": "anthropic/claude-sonnet-4-6",
	"openai":    "openai/gpt-4o",
	"gemini":    "google/gemini-2.0-flash",
}

// Config holds the LLM credentials needed for an OpenHands run.
// Git provider credentials stay with the controller — the agent never
// talks to the git provider directly.
type Config struct {
	LLMProvider string
	LLMModel    string // overrides default model for provider
	LLMAPIKey   string
	LLMBaseURL  string // optional, for custom endpoints
}

// Result is the structured output of an agent run. The controller uses
// these fields to decide what git provider actions to take:
//   - Comment non-empty → post as issue/PR comment
//   - PRTitle non-empty + code changes in sandbox → create PR
//   - Code changes but no PRTitle → controller generates title from purpose
type Result struct {
	Comment    string `json:"comment,omitempty"`
	PRTitle    string `json:"pr_title,omitempty"`
	PRBody     string `json:"pr_body,omitempty"`
	TokensUsed int    `json:"-"`
}

// BuildTask assembles the task prompt from the agent's purpose and configured
// context. The purpose IS the prompt — the agent author defines what the agent
// does. We only append context the agent was configured to receive.
func BuildTask(purpose string, context *agentv1alpha1.AgentRunContext) string {
	if context == nil || context.SubgraphRef == "" {
		return purpose
	}
	return purpose + "\n\n---\n\n" + context.SubgraphRef
}

// BuildEnv returns the environment variables needed to run OpenHands in
// headless mode with the configured LLM provider.
func BuildEnv(cfg Config) map[string]string {
	model := cfg.LLMModel
	if model == "" {
		model = defaultModels[cfg.LLMProvider]
	}
	if model == "" {
		model = defaultModels["anthropic"]
	}

	env := map[string]string{
		"LLM_MODEL":   model,
		"LLM_API_KEY": cfg.LLMAPIKey,
	}

	if cfg.LLMBaseURL != "" {
		env["LLM_BASE_URL"] = cfg.LLMBaseURL
	}

	return env
}

// BuildCommand returns the shell command to run OpenHands in headless mode.
// envVars are prepended as inline env assignments so they take effect without
// modifying the container image.
func BuildCommand(taskFile string, envVars map[string]string) string {
	var parts []string
	for k, v := range envVars {
		parts = append(parts, fmt.Sprintf("%s=%s", k, shellQuote(v)))
	}
	parts = append(parts, "openhands", "--headless", "--json", "--override-with-envs", "-f", taskFile)
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// outputEvent represents a single JSONL line from OpenHands stdout.
type outputEvent struct {
	Action  string         `json:"action,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Message string         `json:"message,omitempty"`
	Extras  map[string]any `json:"extras,omitempty"`
}

// CollectResult resolves the agent's output into a single Result.
// It takes the JSONL stdout from OpenHands and the raw bytes of the
// result file (nil if the agent didn't write one).
//
// Precedence: result file wins for Comment/PRTitle/PRBody when present.
// Token usage always comes from the JSONL stream. If no result file
// exists, the JSONL finish message becomes the Comment.
func CollectResult(jsonlOutput string, resultFileData []byte) (*Result, error) {
	finishMessage, tokensUsed, err := parseJSONL(jsonlOutput)
	if err != nil {
		return nil, fmt.Errorf("parsing JSONL output: %w", err)
	}

	result := &Result{TokensUsed: tokensUsed}

	if len(resultFileData) > 0 {
		if err := json.Unmarshal(resultFileData, result); err != nil {
			return nil, fmt.Errorf("parsing result file: %w", err)
		}
		result.TokensUsed = tokensUsed
		return result, nil
	}

	result.Comment = finishMessage
	return result, nil
}

func parseJSONL(output string) (finishMessage string, tokensUsed int, err error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var evt outputEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}

		tokensUsed += extractTokens(evt.Extras)

		if evt.Action == "finish" {
			if outputs, ok := evt.Args["outputs"].(map[string]any); ok {
				if content, ok := outputs["content"].(string); ok {
					finishMessage = content
				}
			}
			if finishMessage == "" {
				finishMessage = evt.Message
			}
		}
	}

	return finishMessage, tokensUsed, scanner.Err()
}

func extractTokens(extras map[string]any) int {
	if extras == nil {
		return 0
	}
	for _, key := range []string{"tokens", "total_tokens", "token_usage"} {
		if v, ok := extras[key]; ok {
			switch t := v.(type) {
			case float64:
				return int(t)
			case json.Number:
				n, _ := t.Int64()
				return int(n)
			}
		}
	}
	return 0
}
