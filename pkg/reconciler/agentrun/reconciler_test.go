package agentrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/provider"
	"github.com/theakshaypant/agents-as-code/pkg/sandbox"
)

type mockProvider struct {
	createCommentFn func(ctx context.Context, event *provider.Event, body string) (string, error)
}

func (m *mockProvider) Detect(_ *http.Request, _ string, _ *zap.SugaredLogger) (bool, bool, *zap.SugaredLogger, string, error) {
	return false, false, nil, "", nil
}
func (m *mockProvider) ParsePayload(_ context.Context, _ *http.Request, _ string) (*provider.Event, error) {
	return nil, nil
}
func (m *mockProvider) Validate(_ context.Context, _ *provider.Event) error { return nil }
func (m *mockProvider) SetClient(_ context.Context, _ *provider.Event, _, _ string) error {
	return nil
}
func (m *mockProvider) GetFiles(_ context.Context, _ *provider.Event) ([]string, error) {
	return nil, nil
}
func (m *mockProvider) GetFile(_ context.Context, _ *provider.Event, _ string) (string, error) {
	return "", nil
}
func (m *mockProvider) GetAgentDir(_ context.Context, _ *provider.Event, _ string) (string, error) {
	return "", nil
}
func (m *mockProvider) CheckPermission(_ context.Context, _ *provider.Event) (bool, error) {
	return false, nil
}
func (m *mockProvider) GetPullRequestDescription(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetPullRequestDiff(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetPullRequestReviews(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetPullRequestComments(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetIssueTitle(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetIssueBody(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetIssueComments(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) GetIssueLabels(_ context.Context, _ *provider.Event) (string, error) {
	return "", nil
}
func (m *mockProvider) CreateComment(ctx context.Context, event *provider.Event, body string) (string, error) {
	if m.createCommentFn != nil {
		return m.createCommentFn(ctx, event, body)
	}
	return "https://github.com/org/repo/issues/1#issuecomment-1", nil
}
func (m *mockProvider) CreateReview(_ context.Context, _ *provider.Event, _, _ string, _ []provider.ReviewComment) (string, error) {
	return "", nil
}
func (m *mockProvider) AddLabels(_ context.Context, _ *provider.Event, _ []string) error {
	return nil
}
func (m *mockProvider) RemoveLabels(_ context.Context, _ *provider.Event, _ []string) error {
	return nil
}
func (m *mockProvider) CreatePullRequest(_ context.Context, _ *provider.Event, _, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockProvider) SetCommitStatus(_ context.Context, _ *provider.Event, _, _, _, _ string) error {
	return nil
}
func (m *mockProvider) CreateCommit(_ context.Context, _ *provider.Event, _ string, _ map[string]string) (string, error) {
	return "", nil
}

func testLogger() *zap.SugaredLogger {
	l, _ := zap.NewDevelopment()
	return l.Sugar()
}

func newTestRunNamed(name string) *agentv1alpha1.AgentRun {
	return &agentv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: agentv1alpha1.AgentRunSpec{
			AgentRef:      "test-agent",
			RepositoryRef: "test-repo",
			SystemPrompt:  "You are a test agent.",
			Event: agentv1alpha1.AgentRunEventInfo{
				Type:   "pull_request",
				Action: "opened",
				Sender: "user1",
			},
			Limits: agentv1alpha1.AgentLimits{
				MaxTokens:      10000,
				TimeoutSeconds: 60,
			},
		},
	}
}

func newTestRepo() *agentv1alpha1.Repository {
	return &agentv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repo",
			Namespace: "default",
		},
		Spec: agentv1alpha1.RepositorySpec{
			URL: "https://github.com/org/repo",
		},
	}
}

func resultJSON(t *testing.T, actions []map[string]interface{}, tokens int, cost string) []byte {
	t.Helper()
	r := map[string]interface{}{
		"actions":     actions,
		"tokens_used": tokens,
		"cost_usd":    cost,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshaling test result: %v", err)
	}
	return data
}

func mockProviderFactory(prov provider.Interface) ProviderFactory {
	return func(_ context.Context, _ *agentv1alpha1.Repository, _ client.Client, _ *zap.SugaredLogger) (provider.Interface, error) {
		return prov, nil
	}
}

func failingProviderFactory(err error) ProviderFactory {
	return func(_ context.Context, _ *agentv1alpha1.Repository, _ client.Client, _ *zap.SugaredLogger) (provider.Interface, error) {
		return nil, err
	}
}

func getUpdatedRun(t *testing.T, c client.Client, name string) *agentv1alpha1.AgentRun {
	t.Helper()
	var run agentv1alpha1.AgentRun
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, &run); err != nil {
		t.Fatalf("fetching updated run: %v", err)
	}
	return &run
}

func TestReconcile_FullHappyPath(t *testing.T) {
	run := newTestRunNamed("happy")
	repo := newTestRepo()

	resultData := resultJSON(t, nil, 500, "0.05")

	sb := sandbox.NewStub()
	sb.ReadFileFunc = func(_ context.Context, _ *sandbox.Handle, _ string) ([]byte, error) {
		return resultData, nil
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(run, repo).
		WithStatusSubresource(run).
		Build()

	rec := NewReconciler(c, sb, mockProviderFactory(&mockProvider{}), testLogger())
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "happy", Namespace: "default"}}

	// Phase 1: SandboxReady
	res, err := rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("sandbox phase: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Fatal("expected requeue after sandbox phase")
	}
	updated := getUpdatedRun(t, c, "happy")
	if updated.Status.SandboxName == "" {
		t.Fatal("expected SandboxName to be set")
	}
	if updated.Status.StartTime == nil {
		t.Fatal("expected StartTime to be set")
	}

	// Phase 2: AgentExecuted
	res, err = rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("agent phase: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Fatal("expected requeue after agent phase")
	}

	// Phase 3: ResultsCollected
	res, err = rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("results phase: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Fatal("expected requeue after results phase")
	}
	updated = getUpdatedRun(t, c, "happy")
	if updated.Status.TokensUsed != 500 {
		t.Errorf("TokensUsed = %d, want 500", updated.Status.TokensUsed)
	}
	if updated.Status.CostUSD != "0.05" {
		t.Errorf("CostUSD = %q, want 0.05", updated.Status.CostUSD)
	}

	// Phase 4: ActionsExecuted (no actions → success)
	res, err = rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("actions phase: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Fatal("expected no requeue after terminal state")
	}
	updated = getUpdatedRun(t, c, "happy")
	if !IsTerminal(updated) {
		t.Fatal("expected terminal state")
	}
	if updated.Status.CompletionTime == nil {
		t.Fatal("expected CompletionTime to be set")
	}
	succeeded := conditionManager(updated).GetCondition(apis.ConditionSucceeded)
	if succeeded == nil || !succeeded.IsTrue() {
		t.Fatal("expected Succeeded=True")
	}
}

func TestReconcile_SandboxCreateFailure(t *testing.T) {
	run := newTestRunNamed("sb-fail")
	repo := newTestRepo()

	sb := sandbox.NewStub()
	sb.CreateFunc = func(_ context.Context, _ sandbox.CreateOpts) (*sandbox.Handle, error) {
		return nil, fmt.Errorf("no capacity")
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(run, repo).
		WithStatusSubresource(run).
		Build()

	rec := NewReconciler(c, sb, mockProviderFactory(&mockProvider{}), testLogger())
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sb-fail", Namespace: "default"}}

	res, err := rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Fatal("should not requeue after failure")
	}

	updated := getUpdatedRun(t, c, "sb-fail")
	if !IsTerminal(updated) {
		t.Fatal("expected terminal state after sandbox failure")
	}
	sbCond := conditionManager(updated).GetCondition(ConditionSandboxReady)
	if sbCond == nil || !sbCond.IsFalse() {
		t.Fatal("expected SandboxReady=False")
	}
}

func TestReconcile_AgentNonZeroExit(t *testing.T) {
	run := newTestRunNamed("agent-fail")
	repo := newTestRepo()

	sb := sandbox.NewStub()
	sb.RunFunc = func(_ context.Context, _ *sandbox.Handle, _ string) (*sandbox.RunResult, error) {
		return &sandbox.RunResult{ExitCode: 1, Stderr: "OOM killed"}, nil
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(run, repo).
		WithStatusSubresource(run).
		Build()

	rec := NewReconciler(c, sb, mockProviderFactory(&mockProvider{}), testLogger())
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "agent-fail", Namespace: "default"}}

	// Phase 1: sandbox succeeds
	_, err := rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("sandbox phase: %v", err)
	}

	// Phase 2: agent fails with non-zero exit
	res, err := rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Fatal("should not requeue after agent failure")
	}

	updated := getUpdatedRun(t, c, "agent-fail")
	if !IsTerminal(updated) {
		t.Fatal("expected terminal state after agent failure")
	}
	agentCond := conditionManager(updated).GetCondition(ConditionAgentExecuted)
	if agentCond == nil || !agentCond.IsFalse() {
		t.Fatal("expected AgentExecuted=False")
	}
}

func TestReconcile_ResultParseFailure(t *testing.T) {
	run := newTestRunNamed("bad-result")
	repo := newTestRepo()

	sb := sandbox.NewStub()
	sb.ReadFileFunc = func(_ context.Context, _ *sandbox.Handle, _ string) ([]byte, error) {
		return []byte("not json"), nil
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(run, repo).
		WithStatusSubresource(run).
		Build()

	rec := NewReconciler(c, sb, mockProviderFactory(&mockProvider{}), testLogger())
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "bad-result", Namespace: "default"}}

	// Phase 1 + 2
	rec.Reconcile(context.Background(), req)
	rec.Reconcile(context.Background(), req)

	// Phase 3: result parsing fails
	res, err := rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Fatal("should not requeue after result failure")
	}

	updated := getUpdatedRun(t, c, "bad-result")
	if !IsTerminal(updated) {
		t.Fatal("expected terminal state after result failure")
	}
	resCond := conditionManager(updated).GetCondition(ConditionResultsCollected)
	if resCond == nil || !resCond.IsFalse() {
		t.Fatal("expected ResultsCollected=False")
	}
}

func TestReconcile_WithActions(t *testing.T) {
	run := newTestRunNamed("with-actions")
	repo := newTestRepo()

	actions := []map[string]interface{}{
		{"type": "comment", "body": "LGTM"},
	}
	resultData := resultJSON(t, actions, 1000, "0.10")

	sb := sandbox.NewStub()
	sb.ReadFileFunc = func(_ context.Context, _ *sandbox.Handle, _ string) ([]byte, error) {
		return resultData, nil
	}

	var commentCreated bool
	mock := &mockProvider{
		createCommentFn: func(_ context.Context, _ *provider.Event, body string) (string, error) {
			commentCreated = true
			if body != "LGTM" {
				t.Errorf("comment body = %q, want LGTM", body)
			}
			return "https://github.com/org/repo/issues/1#issuecomment-1", nil
		},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(run, repo).
		WithStatusSubresource(run).
		Build()

	rec := NewReconciler(c, sb, mockProviderFactory(mock), testLogger())
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "with-actions", Namespace: "default"}}

	// Run through all 4 phases
	for i := 0; i < 4; i++ {
		if _, err := rec.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("phase %d: %v", i+1, err)
		}
	}

	if !commentCreated {
		t.Fatal("expected comment to be created")
	}

	updated := getUpdatedRun(t, c, "with-actions")
	if !IsTerminal(updated) {
		t.Fatal("expected terminal state")
	}
	if len(updated.Status.Actions) != 1 {
		t.Fatalf("expected 1 action audit, got %d", len(updated.Status.Actions))
	}
	if updated.Status.Actions[0].Type != "pr-comment" {
		t.Errorf("action type = %q, want pr-comment", updated.Status.Actions[0].Type)
	}
}

func TestReconcile_ProviderFactoryFailure(t *testing.T) {
	run := newTestRunNamed("pf-fail")
	repo := newTestRepo()

	actions := []map[string]interface{}{
		{"type": "comment", "body": "test"},
	}
	resultData := resultJSON(t, actions, 100, "0.01")

	sb := sandbox.NewStub()
	sb.ReadFileFunc = func(_ context.Context, _ *sandbox.Handle, _ string) ([]byte, error) {
		return resultData, nil
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(run, repo).
		WithStatusSubresource(run).
		Build()

	rec := NewReconciler(c, sb, failingProviderFactory(fmt.Errorf("no token")), testLogger())
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "pf-fail", Namespace: "default"}}

	// Phases 1-3
	for i := 0; i < 3; i++ {
		if _, err := rec.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("phase %d: %v", i+1, err)
		}
	}

	// Phase 4: provider factory fails
	res, err := rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Fatal("should not requeue after provider failure")
	}

	updated := getUpdatedRun(t, c, "pf-fail")
	if !IsTerminal(updated) {
		t.Fatal("expected terminal state")
	}
	actionCond := conditionManager(updated).GetCondition(ConditionActionsExecuted)
	if actionCond == nil || !actionCond.IsFalse() {
		t.Fatal("expected ActionsExecuted=False")
	}
}

func TestReconcile_TerminalNoOp(t *testing.T) {
	run := newTestRunNamed("terminal")
	mgr := conditionManager(run)
	mgr.MarkTrue(ConditionSandboxReady)
	mgr.MarkTrue(ConditionAgentExecuted)
	mgr.MarkTrue(ConditionResultsCollected)
	mgr.MarkTrue(ConditionActionsExecuted)
	mgr.MarkTrue(apis.ConditionSucceeded)

	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	sb := sandbox.NewStub()
	sb.CreateFunc = func(_ context.Context, _ sandbox.CreateOpts) (*sandbox.Handle, error) {
		t.Fatal("sandbox.Create should not be called for terminal runs")
		return nil, nil
	}

	rec := NewReconciler(c, sb, mockProviderFactory(&mockProvider{}), testLogger())
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "terminal", Namespace: "default"}}

	res, err := rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Fatal("terminal should not requeue")
	}
}

func TestReconcile_ControllerRestart(t *testing.T) {
	run := newTestRunNamed("restart")
	run.Status.SandboxName = "existing-claim"
	mgr := conditionManager(run)
	mgr.MarkTrue(ConditionSandboxReady)

	resultData := resultJSON(t, nil, 200, "0.02")

	sb := sandbox.NewStub()
	sb.ReadFileFunc = func(_ context.Context, _ *sandbox.Handle, _ string) ([]byte, error) {
		return resultData, nil
	}

	var createCalled bool
	sb.CreateFunc = func(_ context.Context, _ sandbox.CreateOpts) (*sandbox.Handle, error) {
		createCalled = true
		return nil, fmt.Errorf("should not be called")
	}

	repo := newTestRepo()
	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(run, repo).
		WithStatusSubresource(run).
		Build()

	rec := NewReconciler(c, sb, mockProviderFactory(&mockProvider{}), testLogger())
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "restart", Namespace: "default"}}

	// Should pick up from AgentExecuted phase, not re-create sandbox
	for i := 0; i < 3; i++ {
		if _, err := rec.Reconcile(context.Background(), req); err != nil {
			t.Fatalf("phase %d: %v", i+1, err)
		}
	}

	if createCalled {
		t.Fatal("sandbox.Create should not be called on restart with existing sandbox")
	}

	updated := getUpdatedRun(t, c, "restart")
	if !IsTerminal(updated) {
		t.Fatal("expected terminal state")
	}
}

func TestReconcile_NotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	sb := sandbox.NewStub()

	rec := NewReconciler(c, sb, mockProviderFactory(&mockProvider{}), testLogger())
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "gone", Namespace: "default"}}

	res, err := rec.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Fatal("should not requeue for not found")
	}
}
