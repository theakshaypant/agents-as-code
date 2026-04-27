package matcher

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/theakshaypant/agents-as-code/pkg/apis/agent"
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

func validateAgent(a *agentv1alpha1.Agent) error {
	if a.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if a.Spec.Purpose == "" {
		return fmt.Errorf("spec.purpose is required")
	}
	if len(a.Spec.On) == 0 {
		return fmt.Errorf("spec.on requires at least one trigger")
	}
	for i, trigger := range a.Spec.On {
		if trigger.Event == "" {
			return fmt.Errorf("spec.on[%d].event is required", i)
		}
	}
	if a.Spec.Limits.MaxTokens <= 0 {
		return fmt.Errorf("spec.limits.maxTokens must be positive")
	}
	if a.Spec.Limits.TimeoutSeconds <= 0 {
		return fmt.Errorf("spec.limits.timeoutSeconds must be positive")
	}
	return nil
}
