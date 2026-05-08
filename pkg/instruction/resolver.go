package instruction

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/apis/agent/keys"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
	"github.com/theakshaypant/agents-as-code/pkg/template"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type Resolver struct {
	provider provider.Interface
	event    *provider.Event
	logger   *zap.SugaredLogger
}

func NewResolver(prov provider.Interface, evt *provider.Event, logger *zap.SugaredLogger) *Resolver {
	return &Resolver{
		provider: prov,
		event:    evt,
		logger:   logger,
	}
}

// ParseAnnotations extracts instruction annotation values in order.
// Looks for "agent.tekton.dev/instruction", then "agent.tekton.dev/instruction-1",
// "agent.tekton.dev/instruction-2", etc. Stops at the first gap.
func ParseAnnotations(annotations map[string]string) []string {
	var refs []string

	if v, ok := annotations[keys.Instruction]; ok && v != "" {
		refs = append(refs, v)
	}

	type numbered struct {
		index int
		value string
	}
	var items []numbered

	for k, v := range annotations {
		if !strings.HasPrefix(k, keys.InstructionBase) || v == "" {
			continue
		}
		suffix := strings.TrimPrefix(k, keys.InstructionBase)
		n, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		items = append(items, numbered{index: n, value: v})
	}

	sort.Slice(items, func(i, j int) bool { return items[i].index < items[j].index })

	for _, item := range items {
		refs = append(refs, item.value)
	}

	return refs
}

// ResolveAll fetches and resolves all instruction references.
// Template variables in instruction content are resolved using the provided resolver.
// Fetch failures skip the instruction with a warning.
func (r *Resolver) ResolveAll(ctx context.Context, refs []string, varResolver *template.VariableResolver) []agentv1alpha1.InstructionRef {
	var instructions []agentv1alpha1.InstructionRef

	for _, ref := range refs {
		instr, err := r.resolve(ctx, ref, varResolver)
		if err != nil {
			r.logger.Warnf("skipping instruction %q: %v", ref, err)
			continue
		}
		instructions = append(instructions, instr)
	}

	return instructions
}

func (r *Resolver) resolve(ctx context.Context, ref string, varResolver *template.VariableResolver) (agentv1alpha1.InstructionRef, error) {
	isURL := strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")

	var content string
	var err error

	if isURL {
		content, err = r.fetchFromURL(ctx, ref)
	} else {
		content, err = r.fetchFromRepo(ctx, ref)
	}
	if err != nil {
		return agentv1alpha1.InstructionRef{}, err
	}

	needed := template.ExtractVariables(content)
	if len(needed) > 0 {
		vars := varResolver.Resolve(ctx, needed)
		content = template.ResolveVariables(content, vars, r.logger)
	}

	instr := agentv1alpha1.InstructionRef{
		Name:    nameFromRef(ref),
		Content: content,
	}
	if isURL {
		instr.URL = ref
	} else {
		instr.Path = ref
	}

	return instr, nil
}

func (r *Resolver) fetchFromRepo(ctx context.Context, path string) (string, error) {
	content, err := r.provider.GetFile(ctx, r.event, path)
	if err != nil {
		return "", fmt.Errorf("fetching repo file %q: %w", path, err)
	}
	return content, nil
}

func (r *Resolver) fetchFromURL(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request for %q: %w", url, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching URL %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching URL %q: status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response from %q: %w", url, err)
	}

	return string(body), nil
}

func nameFromRef(ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
		return ref
	}
	return filepath.Base(ref)
}
