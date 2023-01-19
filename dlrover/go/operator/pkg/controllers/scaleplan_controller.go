/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	logger "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	elasticv1alpha1 "github.com/intelligent-machine-learning/easydl/dlrover/go/operator/api/v1alpha1"
	common "github.com/intelligent-machine-learning/easydl/dlrover/go/operator/pkg/common"
	commonv1 "github.com/intelligent-machine-learning/easydl/dlrover/go/operator/pkg/common/api/v1"
)

// ScalePlanReconciler reconciles a scalePlan object
type ScalePlanReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Log      logr.Logger
}

// NewScalePlanReconciler creates a ScalePlanReconciler
func NewScalePlanReconciler(mgr ctrl.Manager) *ScalePlanReconciler {
	r := &ScalePlanReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("scaleplan-controller"),
		Log:      ctrl.Log.WithName("controllers").WithName("ScalePlan"),
	}
	return r
}

//+kubebuilder:rbac:groups=elastic.iml.github.io,resources=scaleplans,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=elastic.iml.github.io,resources=scaleplans/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=elastic.iml.github.io,resources=scaleplans/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ElasticJob object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *ScalePlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// Fetch the scale
	scalePlan := &elasticv1alpha1.ScalePlan{}
	if err := r.Get(context.TODO(), req.NamespacedName, scalePlan); err != nil {
		return ctrl.Result{}, err
	}
	if scalePlan.Spec.ManualScaling {
		return ctrl.Result{}, nil
	}
	job, err := r.getOwnerJob(scalePlan)
	if err != nil {
		return ctrl.Result{}, err
	}
	result, err := r.updateJobToScaling(scalePlan, job, defaultPollInterval)
	return result, err
}

func (r *ScalePlanReconciler) getOwnerJob(scalePlan *elasticv1alpha1.ScalePlan) (*elasticv1alpha1.ElasticJob, error) {
	job := &elasticv1alpha1.ElasticJob{}
	nsn := types.NamespacedName{}
	nsn.Namespace = scalePlan.GetNamespace()
	nsn.Name = scalePlan.Spec.OwnerJob
	err := r.Get(context.Background(), nsn, job)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Warnf("%s not found elasticJob: %v, namespace: %v", scalePlan.Name, nsn.Name, nsn.Namespace)
			return job, nil
		}
		return job, err
	}
	return job, nil
}

func (r *ScalePlanReconciler) updateJobToScaling(
	scalePlan *elasticv1alpha1.ScalePlan,
	job *elasticv1alpha1.ElasticJob,
	pollInterval time.Duration) (ctrl.Result, error) {
	job.Status.ScalePlan = scalePlan.Name
	for taskType, resourceSpec := range scalePlan.Spec.ReplicaResourceSpecs {
		if job.Status.ReplicaStatuses[taskType].Initial == 0 {
			job.Status.ReplicaStatuses[taskType].Initial = int32(resourceSpec.Replicas)
		}
	}
	msg := fmt.Sprintf("ElasticJob %s is scaling by %s.", job.Name, scalePlan.Name)
	common.UpdateStatus(&job.Status, commonv1.JobScaling, common.JobScalingReason, msg)
	err := r.Status().Update(context.Background(), job)
	if err != nil {
		logger.Errorf("Failed to update job %s status to scaling with %s, err: %v", job.Name, scalePlan.Name, err)
		return ctrl.Result{RequeueAfter: pollInterval}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalePlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticv1alpha1.ScalePlan{}).
		Complete(r)
}
