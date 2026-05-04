package matcher

import (
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/theakshaypant/agents-as-code/pkg/apis/agent"
	"github.com/theakshaypant/agents-as-code/pkg/apis/agent/keys"
	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
)

const agentDir = ".tekton/agents"

func AgentDirPath() string {
	return agentDir
}

func ParseAgentDefinitions(rawYAML string) ([]agentv1alpha1.Agent, error) {
	var agents []agentv1alpha1.Agent

	docs := strings.Split(rawYAML, "\n---\n")
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" || doc == "---" {
			continue
		}

		var a agentv1alpha1.Agent
		if err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(doc), 4096).Decode(&a); err != nil {
			return nil, fmt.Errorf("decoding agent definition: %w", err)
		}

		if a.Kind != agent.AgentKind {
			continue
		}

		if a.APIVersion != agentv1alpha1.SchemeGroupVersion.String() {
			return nil, fmt.Errorf("agent %q has unsupported apiVersion %q, expected %q",
				a.Name, a.APIVersion, agentv1alpha1.SchemeGroupVersion.String())
		}

		if err := validateAgent(&a); err != nil {
			return nil, fmt.Errorf("agent %q: %w", a.Name, err)
		}

		agents = append(agents, a)
	}

	return agents, nil
}

var validEvents = map[string]bool{
	"push": true, "pull_request": true, "pull_request_review": true,
	"issue_comment": true, "issues_labeled": true, "pull_request_labeled": true,
}

func validateAgent(a *agentv1alpha1.Agent) error {
	if a.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if a.Spec.SystemPrompt == "" {
		return fmt.Errorf("spec.system_prompt is required")
	}

	annots := a.GetAnnotations()
	eventVal, ok := annots[keys.OnEvent]
	if !ok {
		return fmt.Errorf("annotation %s is required", keys.OnEvent)
	}
	events, err := getAnnotationValues(eventVal)
	if err != nil {
		return fmt.Errorf("annotation %s: %w", keys.OnEvent, err)
	}
	if len(events) == 0 {
		return fmt.Errorf("annotation %s must have at least one value", keys.OnEvent)
	}
	for _, e := range events {
		if !validEvents[e] {
			return fmt.Errorf("annotation %s contains invalid event type %q", keys.OnEvent, e)
		}
	}

	if commentVal, ok := annots[keys.OnComment]; ok {
		patterns, err := getAnnotationValues(commentVal)
		if err != nil {
			return fmt.Errorf("annotation %s: %w", keys.OnComment, err)
		}
		for _, p := range patterns {
			if _, err := regexp.Compile(p); err != nil {
				return fmt.Errorf("annotation %s contains invalid regex %q: %w", keys.OnComment, p, err)
			}
		}
	}

	if a.Spec.Limits.MaxTokens <= 0 {
		return fmt.Errorf("spec.limits.max_tokens must be positive")
	}
	if a.Spec.Limits.TimeoutSeconds <= 0 {
		return fmt.Errorf("spec.limits.timeout_seconds must be positive")
	}
	return nil
}
