package repository

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/agents-as-code/pkg/knowledgegraph"
)

type Reconciler struct {
	client    client.Client
	kgBuilder knowledgegraph.Builder
}

func NewReconciler(c client.Client, builder knowledgegraph.Builder) *Reconciler {
	r := &Reconciler{
		client: c,
	}
	if builder != nil {
		r.kgBuilder = builder
	}
	return r
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var repo agentv1alpha1.Repository
	if err := r.client.Get(ctx, req.NamespacedName, &repo); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Repository deleted, skipping")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching Repository: %w", err)
	}

	if repo.Spec.KnowledgeGraph == nil || !repo.Spec.KnowledgeGraph.Enabled {
		logger.Info("knowledge graph not enabled", "repository", repo.Name)
		return ctrl.Result{}, nil
	}

	logger.Info("knowledge graph enabled",
		"repository", repo.Name,
		"branches", len(repo.Spec.KnowledgeGraph.Branches),
		"storageClass", repo.Spec.KnowledgeGraph.StorageClassName,
	)

	// TODO: provision PVCs per branch
	// TODO: clone repo and run kgBuilder.Build() for initial graph creation
	// TODO: update Repository status with KG branch conditions

	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.Repository{}).
		Complete(r)
}
