package template

import (
	"regexp"
	"strings"

	"go.uber.org/zap"
)

var templateVarRegex = regexp.MustCompile(`{{([^}]{2,})}}`)

// ExtractVariables scans text for {{ variable_name }} patterns and returns unique variable names.
func ExtractVariables(text string) []string {
	matches := templateVarRegex.FindAllStringSubmatch(text, -1)
	seen := make(map[string]struct{}, len(matches))
	var result []string
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	return result
}

// ResolveVariables replaces {{ variable_name }} placeholders in text with values
// from the vars map. Unresolvable variables are replaced with empty string and
// logged as warnings.
func ResolveVariables(text string, vars map[string]string, logger *zap.SugaredLogger) string {
	return templateVarRegex.ReplaceAllStringFunc(text, func(match string) string {
		parts := templateVarRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		name := strings.TrimSpace(parts[1])
		if value, ok := vars[name]; ok {
			return value
		}
		logger.Warnf("unresolvable template variable: %s", name)
		return ""
	})
}
