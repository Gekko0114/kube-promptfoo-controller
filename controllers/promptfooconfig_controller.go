/*
Copyright 2024.

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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kbatch "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	promptfoov1 "kube-promptfoo-controller/api/v1"
)

var (
	nodeImage = "node:20"
	command   = []string{
		"sh", "-c", "npm install -g promptfoo && cp /prompt/promptfooconfig.yaml promptfooconfig.yaml && promptfoo eval --output output.json",
	}
)

// PromptFooConfigReconciler reconciles a PromptFooConfig object
type PromptFooConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=promptfoo.promptfoo.x-k8s.io,resources=promptfooconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=promptfoo.promptfoo.x-k8s.io,resources=promptfooconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=promptfoo.promptfoo.x-k8s.io,resources=promptfooconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PromptFooConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.1/pkg/reconcile
func (r *PromptFooConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling")
	var promptfooconfig promptfoov1.PromptFooConfig
	if err := r.Get(ctx, req.NamespacedName, &promptfooconfig); err != nil {
		log.Error(err, "unable to fetch PromptFooConfig")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.reconcileConfigMap(ctx, &promptfooconfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileCronJob(ctx, &promptfooconfig); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PromptFooConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&promptfoov1.PromptFooConfig{}).
		Owns(&kbatch.CronJob{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

func (r *PromptFooConfigReconciler) reconcileCronJob(ctx context.Context, promptFooConfig *promptfoov1.PromptFooConfig) error {
	logger := log.FromContext(ctx)

	name := fmt.Sprintf("%s-%s", "promptfoo", promptFooConfig.Name)
	cronJob := &kbatch.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        name,
			Namespace:   promptFooConfig.Namespace,
		},
		Spec: kbatch.CronJobSpec{
			Schedule: promptFooConfig.Spec.Schedule,
			JobTemplate: kbatch.JobTemplateSpec{
				Spec: kbatch.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    name,
									Image:   nodeImage,
									Command: command,
									Env: []corev1.EnvVar{
										{
											Name:  "OPENAI_API_KEY",
											Value: promptFooConfig.Spec.OpenAIAPIKey,
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      name,
											MountPath: "/prompt",
											ReadOnly:  false,
										},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
							Volumes: []corev1.Volume{
								{
									Name: name,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: name,
											},
											Items: []corev1.KeyToPath{
												{
													Key:  "promptfooconfig.yaml",
													Path: "promptfooconfig.yaml",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if err := ctrl.SetControllerReference(promptFooConfig, cronJob, r.Scheme); err != nil {
		return err
	}
	op, err := ctrl.CreateOrUpdate(ctx, r.Client, cronJob, func() error {
		return nil
	})
	if err != nil {
		logger.Error(err, "unable to create or update cronJob")
		return err
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("reconcile cronJob successfully", "op", op)
	}
	return nil
}

func (r *PromptFooConfigReconciler) reconcileConfigMap(ctx context.Context, promptFooConfig *promptfoov1.PromptFooConfig) error {
	logger := log.FromContext(ctx)
	name := fmt.Sprintf("%s-%s", "promptfoo", promptFooConfig.Name)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: promptFooConfig.Namespace,
		},
		Data: map[string]string{
			"promptfooconfig.yaml": promptFooConfig.Spec.Prompt,
		},
	}

	if err := ctrl.SetControllerReference(promptFooConfig, cm, r.Scheme); err != nil {
		return err
	}
	op, err := ctrl.CreateOrUpdate(ctx, r.Client, cm, func() error {
		return nil
	})
	if err != nil {
		logger.Error(err, "unable to create or update configmap")
		return err
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("reconcile cronJob successfully", "op", op)
	}
	return nil
}
