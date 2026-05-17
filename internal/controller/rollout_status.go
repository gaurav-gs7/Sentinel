package controller

import (
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type RolloutGuardSpec struct {
	ServiceRef      string
	DeploymentRef   string
	Strategy        string
	RollbackEnabled bool
}

func (s RolloutGuardSpec) DeploymentName(guardName string) string {
	if s.DeploymentRef != "" {
		return s.DeploymentRef
	}
	if s.ServiceRef != "" {
		return s.ServiceRef
	}
	return strings.TrimSuffix(guardName, "-rollout")
}

type RolloutEvaluation struct {
	Phase             string
	Reason            string
	Message           string
	DesiredReplicas   int32
	UpdatedReplicas   int32
	AvailableReplicas int32
	Healthy           bool
	Blocked           bool
}

type RolloutGuardStatus struct {
	ObservedGeneration   int64
	DeploymentGeneration int64
	Phase                string
	Reason               string
	Message              string
	ServiceRef           string
	DeploymentRef        string
	Strategy             string
	DesiredReplicas      int32
	UpdatedReplicas      int32
	AvailableReplicas    int32
	RollbackEnabled      bool
	RollbackRevision     string
	LastReconciledAt     metav1.Time
	Conditions           []metav1.Condition
}

func rolloutGuardSpecFrom(guard *unstructured.Unstructured) RolloutGuardSpec {
	serviceRef, _, _ := unstructured.NestedString(guard.Object, "spec", "serviceRef")
	deploymentRef, _, _ := unstructured.NestedString(guard.Object, "spec", "deploymentRef")
	strategy, _, _ := unstructured.NestedString(guard.Object, "spec", "strategy")
	rollbackEnabled, found, _ := unstructured.NestedBool(guard.Object, "spec", "rollback", "enabled")
	if !found {
		rollbackEnabled = true
	}
	return RolloutGuardSpec{
		ServiceRef:      serviceRef,
		DeploymentRef:   deploymentRef,
		Strategy:        strategy,
		RollbackEnabled: rollbackEnabled,
	}
}

func EvaluateDeploymentRollout(deployment *appsv1.Deployment) RolloutEvaluation {
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	result := RolloutEvaluation{
		Phase:             "Progressing",
		Reason:            "RolloutProgressing",
		Message:           fmt.Sprintf("deployment %s is progressing", deployment.Name),
		DesiredReplicas:   desired,
		UpdatedReplicas:   deployment.Status.UpdatedReplicas,
		AvailableReplicas: deployment.Status.AvailableReplicas,
	}
	if deployment.Spec.Paused {
		result.Phase = "Blocked"
		result.Reason = "DeploymentPaused"
		result.Message = fmt.Sprintf("deployment %s is paused", deployment.Name)
		result.Blocked = true
		return result
	}
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing &&
			condition.Status == coreConditionFalse &&
			condition.Reason == "ProgressDeadlineExceeded" {
			result.Phase = "Blocked"
			result.Reason = condition.Reason
			result.Message = condition.Message
			result.Blocked = true
			return result
		}
	}
	if deployment.Status.ObservedGeneration < deployment.Generation {
		result.Message = fmt.Sprintf("waiting for deployment controller to observe generation %d", deployment.Generation)
		return result
	}
	if deployment.Status.UpdatedReplicas == desired &&
		deployment.Status.AvailableReplicas >= desired &&
		deployment.Status.UnavailableReplicas == 0 {
		result.Phase = "Healthy"
		result.Reason = "RolloutHealthy"
		result.Message = fmt.Sprintf("deployment %s has %d/%d updated replicas available", deployment.Name, deployment.Status.AvailableReplicas, desired)
		result.Healthy = true
		return result
	}
	if deployment.Status.UnavailableReplicas > 0 && deployment.Status.UpdatedReplicas == 0 {
		result.Phase = "Blocked"
		result.Reason = "NoUpdatedReplicasAvailable"
		result.Message = fmt.Sprintf("deployment %s has no updated replicas available", deployment.Name)
		result.Blocked = true
		return result
	}
	result.Message = fmt.Sprintf("deployment %s has %d/%d updated replicas and %d/%d available", deployment.Name, deployment.Status.UpdatedReplicas, desired, deployment.Status.AvailableReplicas, desired)
	return result
}

func statusForMissingDeployment(guard *unstructured.Unstructured, spec RolloutGuardSpec, deploymentName string) RolloutGuardStatus {
	now := metav1.Now()
	status := RolloutGuardStatus{
		ObservedGeneration: guard.GetGeneration(),
		Phase:              "Blocked",
		Reason:             "DeploymentMissing",
		Message:            fmt.Sprintf("deployment %s was not found", deploymentName),
		ServiceRef:         spec.ServiceRef,
		DeploymentRef:      deploymentName,
		Strategy:           spec.Strategy,
		RollbackEnabled:    spec.RollbackEnabled,
		LastReconciledAt:   now,
	}
	status.SetCondition("Reconciled", metav1.ConditionTrue, "ReconcileComplete", "RolloutGuard reconciled")
	status.SetCondition("DeploymentFound", metav1.ConditionFalse, "DeploymentMissing", status.Message)
	status.SetCondition("Blocked", metav1.ConditionTrue, "DeploymentMissing", status.Message)
	return status
}

func statusForEvaluation(guard *unstructured.Unstructured, spec RolloutGuardSpec, deployment *appsv1.Deployment, evaluation RolloutEvaluation) RolloutGuardStatus {
	now := metav1.Now()
	status := RolloutGuardStatus{
		ObservedGeneration:   guard.GetGeneration(),
		DeploymentGeneration: deployment.Generation,
		Phase:                evaluation.Phase,
		Reason:               evaluation.Reason,
		Message:              evaluation.Message,
		ServiceRef:           spec.ServiceRef,
		DeploymentRef:        deployment.Name,
		Strategy:             spec.Strategy,
		DesiredReplicas:      evaluation.DesiredReplicas,
		UpdatedReplicas:      evaluation.UpdatedReplicas,
		AvailableReplicas:    evaluation.AvailableReplicas,
		RollbackEnabled:      spec.RollbackEnabled,
		LastReconciledAt:     now,
	}
	status.SetCondition("Reconciled", metav1.ConditionTrue, "ReconcileComplete", "RolloutGuard reconciled")
	status.SetCondition("DeploymentFound", metav1.ConditionTrue, "DeploymentFound", "Deployment found")
	if evaluation.Healthy {
		status.SetCondition("Healthy", metav1.ConditionTrue, evaluation.Reason, evaluation.Message)
		status.SetCondition("Blocked", metav1.ConditionFalse, "RolloutHealthy", "rollout is not blocked")
		return status
	}
	if evaluation.Blocked {
		status.SetCondition("Healthy", metav1.ConditionFalse, evaluation.Reason, evaluation.Message)
		status.SetCondition("Blocked", metav1.ConditionTrue, evaluation.Reason, evaluation.Message)
		return status
	}
	status.SetCondition("Healthy", metav1.ConditionFalse, evaluation.Reason, evaluation.Message)
	status.SetCondition("Blocked", metav1.ConditionFalse, evaluation.Reason, "rollout is still progressing")
	return status
}

func (s *RolloutGuardStatus) SetCondition(conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.NewTime(time.Now().UTC())
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: s.ObservedGeneration,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
	for i := range s.Conditions {
		if s.Conditions[i].Type == conditionType {
			if s.Conditions[i].Status == status {
				condition.LastTransitionTime = s.Conditions[i].LastTransitionTime
			}
			s.Conditions[i] = condition
			return
		}
	}
	s.Conditions = append(s.Conditions, condition)
}

func (s RolloutGuardStatus) AsMap() map[string]any {
	status := map[string]any{
		"observedGeneration":   s.ObservedGeneration,
		"deploymentGeneration": s.DeploymentGeneration,
		"phase":                s.Phase,
		"reason":               s.Reason,
		"message":              s.Message,
		"serviceRef":           s.ServiceRef,
		"deploymentRef":        s.DeploymentRef,
		"strategy":             s.Strategy,
		"desiredReplicas":      s.DesiredReplicas,
		"updatedReplicas":      s.UpdatedReplicas,
		"availableReplicas":    s.AvailableReplicas,
		"rollbackEnabled":      s.RollbackEnabled,
		"lastReconciledAt":     s.LastReconciledAt.Format(time.RFC3339),
		"conditions":           s.Conditions,
	}
	if s.RollbackRevision != "" {
		status["rollbackRevision"] = s.RollbackRevision
	}
	return status
}

const coreConditionFalse = "False"
