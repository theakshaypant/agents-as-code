package result

import _ "embed"

//go:embed instruction.md
var instructionContent string

// Instruction returns the embedded result hooks instruction content.
// This is injected into the agent's instructions when the
// agent.tekton.dev/result-hooks annotation is set to "true".
func Instruction() string {
	return instructionContent
}
