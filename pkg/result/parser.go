package result

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

func Parse(data []byte) (*Result, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("parsing result: empty data")
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parsing result json: %w", err)
	}
	return &r, nil
}

func Validate(r *Result) error {
	if r == nil {
		return fmt.Errorf("validating result: nil result")
	}

	var errs []string
	for i, action := range r.Actions {
		if err := validateAction(&action); err != nil {
			errs = append(errs, fmt.Sprintf("action[%d]: %v", i, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func validateAction(a *Action) error {
	switch a.Type {
	case "comment":
		return validateComment(a)
	case "review":
		return validateReview(a)
	case "label":
		return validateLabel(a)
	case "create-pr":
		return validateCreatePR(a)
	case "status":
		return validateStatus(a)
	case "commit":
		return validateCommit(a)
	default:
		return fmt.Errorf("unknown action type %q", a.Type)
	}
}

func validateComment(a *Action) error {
	if a.Body == "" {
		return fmt.Errorf("comment action: body is required")
	}
	return nil
}

func validateReview(a *Action) error {
	if a.Event == "" {
		return fmt.Errorf("review action: event is required")
	}
	switch a.Event {
	case "COMMENT", "APPROVE", "REQUEST_CHANGES":
	default:
		return fmt.Errorf("review action: invalid event %q (must be COMMENT, APPROVE, or REQUEST_CHANGES)", a.Event)
	}
	for i, c := range a.Comments {
		if c.Path == "" {
			return fmt.Errorf("review action: comment[%d] missing path", i)
		}
		if c.Line <= 0 {
			return fmt.Errorf("review action: comment[%d] invalid line %d", i, c.Line)
		}
		if c.Body == "" {
			return fmt.Errorf("review action: comment[%d] missing body", i)
		}
	}
	return nil
}

func validateLabel(a *Action) error {
	if len(a.Add) == 0 && len(a.Remove) == 0 {
		return fmt.Errorf("label action: must specify at least one label to add or remove")
	}
	return nil
}

func validateCreatePR(a *Action) error {
	if a.Title == "" {
		return fmt.Errorf("create-pr action: title is required")
	}
	if a.Head == "" {
		return fmt.Errorf("create-pr action: head is required")
	}
	if a.Base == "" {
		return fmt.Errorf("create-pr action: base is required")
	}
	return nil
}

func validateStatus(a *Action) error {
	if a.Context == "" {
		return fmt.Errorf("status action: context is required")
	}
	if a.State == "" {
		return fmt.Errorf("status action: state is required")
	}
	switch a.State {
	case "success", "failure", "error", "pending":
	default:
		return fmt.Errorf("status action: invalid state %q (must be success, failure, error, or pending)", a.State)
	}
	return nil
}

func validateCommit(a *Action) error {
	if a.Message == "" {
		return fmt.Errorf("commit action: message is required")
	}
	if len(a.Files) == 0 {
		return fmt.Errorf("commit action: files is required and cannot be empty")
	}
	for filePath := range a.Files {
		if filePath == "" {
			return fmt.Errorf("commit action: file path cannot be empty")
		}
		if strings.HasPrefix(filePath, "/") {
			return fmt.Errorf("commit action: file path %q must be relative", filePath)
		}
		cleaned := path.Clean(filePath)
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return fmt.Errorf("commit action: file path %q escapes repository root", filePath)
		}
	}
	return nil
}
