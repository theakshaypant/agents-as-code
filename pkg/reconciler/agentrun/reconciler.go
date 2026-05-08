package agentrun

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/result"
	"github.com/theakshaypant/agents-as-code/pkg/sandbox"
)

type Reconciler struct {
	client          client.Client
	sandboxRuntime  sandbox.Runtime
	providerFactory ProviderFactory
	logger          *zap.SugaredLogger
}

func NewReconciler(c client.Client, sb sandbox.Runtime, pf ProviderFactory, logger *zap.SugaredLogger) *Reconciler {
	return &Reconciler{
		client:          c,
		sandboxRuntime:  sb,
		providerFactory: pf,
		logger:          logger,
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.logger.With("agentrun", req.NamespacedName)

	var run agentv1alpha1.AgentRun
	if err := r.client.Get(ctx, req.NamespacedName, &run); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching AgentRun: %w", err)
	}

	if IsTerminal(&run) {
		return ctrl.Result{}, nil
	}

	phase := GetCurrentPhase(&run)
	logger.Infow("reconciling", "phase", phase)

	var err error
	switch phase {
	case ConditionSandboxReady:
		err = r.reconcileSandbox(ctx, &run)
	case ConditionAgentExecuted:
		err = r.reconcileAgent(ctx, &run)
	case ConditionResultsCollected:
		err = r.reconcileResults(ctx, &run)
	case ConditionActionsExecuted:
		err = r.reconcileActions(ctx, &run, logger)
	}

	if err != nil {
		logger.Errorw("phase failed", "phase", phase, "error", err)
		r.markFailed(ctx, &run, phase, err)
		r.destroySandbox(ctx, &run, logger)
		return ctrl.Result{}, nil
	}

	if err := r.client.Status().Update(ctx, &run); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status: %w", err)
	}

	if IsTerminal(&run) {
		r.destroySandbox(ctx, &run, logger)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{RequeueAfter: time.Second}, nil
}

func (r *Reconciler) reconcileSandbox(ctx context.Context, run *agentv1alpha1.AgentRun) error {
	mgr := conditionManager(run)

	if run.Status.SandboxName != "" {
		handle, err := r.sandboxRuntime.Get(ctx, run.Status.SandboxName, run.Namespace)
		if err != nil {
			return fmt.Errorf("reconnecting to sandbox %s: %w", run.Status.SandboxName, err)
		}
		_ = handle
		mgr.MarkTrue(ConditionSandboxReady)
		return nil
	}

	now := metav1.Now()
	run.Status.StartTime = &now

	handle, err := r.sandboxRuntime.Create(ctx, sandbox.CreateOpts{
		Namespace: run.Namespace,
	})
	if err != nil {
		return fmt.Errorf("creating sandbox: %w", err)
	}

	run.Status.SandboxName = handle.ClaimName
	mgr.MarkTrue(ConditionSandboxReady)
	return nil
}

func (r *Reconciler) reconcileAgent(ctx context.Context, run *agentv1alpha1.AgentRun) error {
	mgr := conditionManager(run)

	handle, err := r.sandboxRuntime.Get(ctx, run.Status.SandboxName, run.Namespace)
	if err != nil {
		return fmt.Errorf("getting sandbox: %w", err)
	}

	var repo agentv1alpha1.Repository
	if err := r.client.Get(ctx, client.ObjectKey{
		Namespace: run.Namespace,
		Name:      run.Spec.RepositoryRef,
	}, &repo); err != nil {
		return fmt.Errorf("fetching Repository %s: %w", run.Spec.RepositoryRef, err)
	}

	builder := NewRuntimeConfigBuilder(r.client)
	configData, err := builder.Build(ctx, run, &repo)
	if err != nil {
		return fmt.Errorf("building runtime config: %w", err)
	}

	if err := r.sandboxRuntime.WriteFile(ctx, handle, ConfigPath, configData); err != nil {
		return fmt.Errorf("writing config to sandbox: %w", err)
	}

	runResult, err := r.sandboxRuntime.Run(ctx, handle, "agent-run")
	if err != nil {
		return fmt.Errorf("executing agent: %w", err)
	}

	if runResult.ExitCode != 0 {
		return fmt.Errorf("agent exited with code %d: %s", runResult.ExitCode, runResult.Stderr)
	}

	mgr.MarkTrue(ConditionAgentExecuted)
	return nil
}

func (r *Reconciler) reconcileResults(ctx context.Context, run *agentv1alpha1.AgentRun) error {
	mgr := conditionManager(run)

	handle, err := r.sandboxRuntime.Get(ctx, run.Status.SandboxName, run.Namespace)
	if err != nil {
		return fmt.Errorf("getting sandbox: %w", err)
	}

	data, err := r.sandboxRuntime.ReadFile(ctx, handle, ResultPath)
	if err != nil {
		return fmt.Errorf("reading result file: %w", err)
	}

	parsed, err := result.Parse(data)
	if err != nil {
		return fmt.Errorf("parsing result: %w", err)
	}

	if err := result.Validate(parsed); err != nil {
		return fmt.Errorf("validating result: %w", err)
	}

	run.Status.TokensUsed = parsed.TokensUsed
	run.Status.CostUSD = parsed.CostUSD
	mgr.MarkTrue(ConditionResultsCollected)
	return nil
}

func (r *Reconciler) reconcileActions(ctx context.Context, run *agentv1alpha1.AgentRun, logger *zap.SugaredLogger) error {
	mgr := conditionManager(run)

	handle, err := r.sandboxRuntime.Get(ctx, run.Status.SandboxName, run.Namespace)
	if err != nil {
		return fmt.Errorf("getting sandbox: %w", err)
	}

	data, err := r.sandboxRuntime.ReadFile(ctx, handle, ResultPath)
	if err != nil {
		return fmt.Errorf("re-reading result file: %w", err)
	}

	parsed, err := result.Parse(data)
	if err != nil {
		return fmt.Errorf("re-parsing result: %w", err)
	}

	if len(parsed.Actions) == 0 {
		mgr.MarkTrue(ConditionActionsExecuted)
		now := metav1.Now()
		run.Status.CompletionTime = &now
		mgr.MarkTrue(apis.ConditionSucceeded)
		return nil
	}

	var repo agentv1alpha1.Repository
	if err := r.client.Get(ctx, client.ObjectKey{
		Namespace: run.Namespace,
		Name:      run.Spec.RepositoryRef,
	}, &repo); err != nil {
		return fmt.Errorf("fetching Repository for actions: %w", err)
	}

	prov, err := r.providerFactory(ctx, &repo, r.client, logger)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	evt := CreateProviderEvent(run, &repo)
	executor := result.NewExecutor(prov, logger)

	actions, err := executor.Execute(ctx, parsed, evt)
	run.Status.Actions = actions

	if err != nil {
		return fmt.Errorf("executing actions: %w", err)
	}

	mgr.MarkTrue(ConditionActionsExecuted)
	now := metav1.Now()
	run.Status.CompletionTime = &now
	mgr.MarkTrue(apis.ConditionSucceeded)
	return nil
}

func (r *Reconciler) markFailed(ctx context.Context, run *agentv1alpha1.AgentRun, phase apis.ConditionType, phaseErr error) {
	mgr := conditionManager(run)
	mgr.MarkFalse(phase, "PhaseFailed", phaseErr.Error())

	now := metav1.Now()
	run.Status.CompletionTime = &now

	if err := r.client.Status().Update(ctx, run); err != nil {
		r.logger.Errorw("failed to update status after failure", "error", err)
	}
}

func (r *Reconciler) destroySandbox(ctx context.Context, run *agentv1alpha1.AgentRun, logger *zap.SugaredLogger) {
	if run.Status.SandboxName == "" {
		return
	}
	handle, err := r.sandboxRuntime.Get(ctx, run.Status.SandboxName, run.Namespace)
	if err != nil {
		logger.Warnw("could not get sandbox for cleanup", "error", err)
		return
	}
	if err := r.sandboxRuntime.Destroy(ctx, handle); err != nil {
		logger.Warnw("sandbox cleanup failed", "error", err)
	}
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.AgentRun{}).
		Complete(r)
}
