package keys

import "github.com/theakshaypant/agents-as-code/pkg/apis/agent"

const (
	OnEvent        = agent.GroupName + "/on-event"
	OnTargetBranch = agent.GroupName + "/on-target-branch"
	OnComment      = agent.GroupName + "/on-comment"
	OnPathChange   = agent.GroupName + "/on-path-change"
	OnLabel        = agent.GroupName + "/on-label"
)

var AllTriggerAnnotations = []string{
	OnEvent,
	OnTargetBranch,
	OnComment,
	OnPathChange,
	OnLabel,
}
