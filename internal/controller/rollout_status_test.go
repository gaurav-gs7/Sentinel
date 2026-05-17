package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestEvaluateDeploymentRolloutHealthy(t *testing.T) {
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-api", Generation: 3},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 3,
			UpdatedReplicas:    2,
			AvailableReplicas:  2,
		},
	}
	evaluation := EvaluateDeploymentRollout(deployment)
	if !evaluation.Healthy || evaluation.Blocked || evaluation.Phase != "Healthy" {
		t.Fatalf("expected healthy rollout, got %+v", evaluation)
	}
}

func TestEvaluateDeploymentRolloutBlockedOnProgressDeadline(t *testing.T) {
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-api", Generation: 3},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 3,
			Conditions: []appsv1.DeploymentCondition{{
				Type:    appsv1.DeploymentProgressing,
				Status:  corev1.ConditionFalse,
				Reason:  "ProgressDeadlineExceeded",
				Message: "ReplicaSet failed to progress",
			}},
		},
	}
	evaluation := EvaluateDeploymentRollout(deployment)
	if !evaluation.Blocked || evaluation.Reason != "ProgressDeadlineExceeded" {
		t.Fatalf("expected blocked rollout, got %+v", evaluation)
	}
}

func TestRollbackRestoresPreviousReplicaSetTemplate(t *testing.T) {
	replicas := int32(2)
	deploymentUID := types.UID("deployment-uid")
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payments-api",
			Namespace: "platform",
			UID:       deploymentUID,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "payments-api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "payments-api"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "app",
					Image: "payments-api:canary",
				}}},
			},
		},
	}
	current := replicaSetForRevision("payments-api-2", "platform", deploymentUID, 2, "payments-api:canary")
	previous := replicaSetForRevision("payments-api-1", "platform", deploymentUID, 1, "payments-api:stable")
	kube := kubefake.NewSimpleClientset(deployment, current, previous)
	ctrl := NewRolloutGuardControllerWithClients(kube, fake.NewSimpleDynamicClient(runtime.NewScheme()), Options{})

	revision, err := ctrl.rollbackToPreviousReplicaSet(context.Background(), deployment)
	if err != nil {
		t.Fatal(err)
	}
	if revision != "1" {
		t.Fatalf("expected rollback to revision 1, got %s", revision)
	}
	updated, err := kube.AppsV1().Deployments("platform").Get(context.Background(), "payments-api", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.Spec.Template.Spec.Containers[0].Image; got != "payments-api:stable" {
		t.Fatalf("expected stable image, got %s", got)
	}
	if _, ok := updated.Spec.Template.Labels["pod-template-hash"]; ok {
		t.Fatal("expected pod-template-hash label to be removed from restored template")
	}
}

func replicaSetForRevision(name, namespace string, deploymentUID types.UID, revision int, image string) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app": "payments-api",
			},
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": string(rune('0' + revision)),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "payments-api",
				UID:        deploymentUID,
			}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "payments-api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":               "payments-api",
						"pod-template-hash": name,
					},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "app",
					Image: image,
				}}},
			},
		},
	}
}
