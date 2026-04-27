package matcher

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/theakshaypant/agents-as-code/pkg/apis/agent/keys"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
)

var reValidateTag = regexp.MustCompile(`^\[(.*)\]$|^[^[\]\s]*$`)

func getAnnotationValues(annotation string) ([]string, error) {
	annotation = strings.TrimSpace(annotation)
	if annotation == "" {
		return nil, nil
	}

	if !reValidateTag.MatchString(annotation) {
		return nil, fmt.Errorf("annotation value in wrong format: %s", annotation)
	}

	if !strings.HasPrefix(annotation, "[") {
		v := strings.ReplaceAll(annotation, "&#44;", ",")
		return []string{v}, nil
	}

	inner := reValidateTag.FindStringSubmatch(annotation)[1]
	if inner == "" {
		return nil, fmt.Errorf("annotation %q has empty values", annotation)
	}

	parts := strings.Split(inner, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(strings.ReplaceAll(p, "&#44;", ","))
		if v != "" {
			result = append(result, v)
		}
	}
	return result, nil
}

func matchEvent(annots map[string]string, evt *provider.Event) bool {
	val, ok := annots[keys.OnEvent]
	if !ok {
		return false
	}
	targets, err := getAnnotationValues(val)
	if err != nil || len(targets) == 0 {
		return false
	}
	evtType := string(evt.TriggerType)
	for _, t := range targets {
		if t == evtType {
			return true
		}
	}
	return false
}

func matchTargetBranch(annots map[string]string, evt *provider.Event) bool {
	val, ok := annots[keys.OnTargetBranch]
	if !ok {
		return true
	}
	targets, err := getAnnotationValues(val)
	if err != nil || len(targets) == 0 {
		return false
	}
	for _, pattern := range targets {
		if branchMatch(pattern, evt.BaseBranch) {
			return true
		}
	}
	return false
}

type CommentMatchResult int

const (
	CommentNotApplicable CommentMatchResult = iota
	CommentMatched
	CommentNotMatched
)

func matchComment(annots map[string]string, evt *provider.Event) CommentMatchResult {
	val, ok := annots[keys.OnComment]
	if !ok {
		return CommentNotApplicable
	}
	if evt.TriggerType != provider.TriggerIssueComment {
		return CommentNotMatched
	}
	patterns, err := getAnnotationValues(val)
	if err != nil || len(patterns) == 0 {
		return CommentNotMatched
	}
	comment := strings.TrimSpace(evt.CommentBody)
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		if re.MatchString(comment) {
			return CommentMatched
		}
	}
	return CommentNotMatched
}

func matchPathChange(annots map[string]string, changedFiles []string) bool {
	val, ok := annots[keys.OnPathChange]
	if !ok {
		return true
	}
	if changedFiles == nil {
		return false
	}
	patterns, err := getAnnotationValues(val)
	if err != nil || len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		for _, f := range changedFiles {
			if matched, _ := filepath.Match(pattern, f); matched {
				return true
			}
		}
	}
	return false
}

func matchLabel(annots map[string]string, evt *provider.Event) bool {
	val, ok := annots[keys.OnLabel]
	if !ok {
		// Label events without on-label annotation should not match (PaC behavior)
		if evt.TriggerType == provider.TriggerIssueLabeled || evt.TriggerType == provider.TriggerPRLabeled {
			return false
		}
		return true
	}
	if evt.TriggerType != provider.TriggerIssueLabeled && evt.TriggerType != provider.TriggerPRLabeled {
		return false
	}
	targets, err := getAnnotationValues(val)
	if err != nil || len(targets) == 0 {
		return false
	}
	for _, t := range targets {
		if t == evt.Label {
			return true
		}
	}
	return false
}

func branchMatch(pattern, branch string) bool {
	pattern = strings.TrimPrefix(pattern, "refs/heads/")
	branch = strings.TrimPrefix(branch, "refs/heads/")
	matched, err := filepath.Match(pattern, branch)
	if err != nil {
		return false
	}
	return matched
}

func TriggerAnnotations(annots map[string]string) map[string]string {
	if annots == nil {
		return nil
	}
	result := make(map[string]string)
	for _, key := range keys.AllTriggerAnnotations {
		if v, ok := annots[key]; ok {
			result[key] = v
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
