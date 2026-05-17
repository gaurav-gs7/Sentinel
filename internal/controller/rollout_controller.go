package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	dynamicinformer "k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

var rolloutGuardGVR = schema.GroupVersionResource{
	Group:    "sentinel.io",
	Version:  "v1",
	Resource: "rolloutguards",
}

type Options struct {
	Namespace string
	Resync    time.Duration
	Logger    *slog.Logger
}

type RolloutGuardController struct {
	kube        kubernetes.Interface
	dynamic     dynamic.Interface
	informer    cache.SharedIndexInformer
	deployments cache.SharedIndexInformer
	queue       workqueue.TypedRateLimitingInterface[string]
	recorder    record.EventRecorder
	namespace   string
	logger      *slog.Logger
}

func NewRolloutGuardController(cfg *rest.Config, opts Options) (*RolloutGuardController, error) {
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return NewRolloutGuardControllerWithClients(kube, dyn, opts), nil
}

func NewRolloutGuardControllerWithClients(kube kubernetes.Interface, dyn dynamic.Interface, opts Options) *RolloutGuardController {
	resync := opts.Resync
	if resync <= 0 {
		resync = 30 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	dynamicFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dyn, resync, opts.Namespace, nil)
	informer := dynamicFactory.ForResource(rolloutGuardGVR).Informer()
	typedFactory := informers.NewSharedInformerFactoryWithOptions(kube, resync, informers.WithNamespace(opts.Namespace))
	deployments := typedFactory.Apps().V1().Deployments().Informer()

	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kube.CoreV1().Events("")})
	recorder := broadcaster.NewRecorder(runtime.NewScheme(), corev1.EventSource{Component: "sentinel-controller"})

	c := &RolloutGuardController{
		kube:        kube,
		dynamic:     dyn,
		informer:    informer,
		deployments: deployments,
		queue:       workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]()),
		recorder:    recorder,
		namespace:   opts.Namespace,
		logger:      logger,
	}
	_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.enqueueObject,
		UpdateFunc: func(_, next any) { c.enqueueObject(next) },
		DeleteFunc: c.enqueueObject,
	})
	_, _ = deployments.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.enqueueDeployment,
		UpdateFunc: func(_, next any) { c.enqueueDeployment(next) },
	})
	return c
}

func (c *RolloutGuardController) Run(ctx context.Context, workers int) error {
	if workers <= 0 {
		workers = 1
	}
	defer c.queue.ShutDown()

	go c.informer.Run(ctx.Done())
	go c.deployments.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), c.informer.HasSynced, c.deployments.HasSynced) {
		return errors.New("rollout guard controller cache sync failed")
	}
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.worker, time.Second)
	}
	<-ctx.Done()
	return nil
}

func (c *RolloutGuardController) worker(ctx context.Context) {
	for c.processNext(ctx) {
	}
}

func (c *RolloutGuardController) processNext(ctx context.Context) bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	if err := c.Reconcile(ctx, key); err != nil {
		c.logger.Error("reconcile rollout guard", "key", key, "error", err)
		c.queue.AddRateLimited(key)
		return true
	}
	c.queue.Forget(key)
	return true
}

func (c *RolloutGuardController) enqueueObject(obj any) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err == nil {
		c.queue.Add(key)
	}
}

func (c *RolloutGuardController) enqueueDeployment(obj any) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		return
	}
	guards, err := c.dynamic.Resource(rolloutGuardGVR).Namespace(deployment.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		c.logger.Warn("list rollout guards for deployment event", "namespace", deployment.Namespace, "deployment", deployment.Name, "error", err)
		return
	}
	for i := range guards.Items {
		guard := &guards.Items[i]
		spec := rolloutGuardSpecFrom(guard)
		if spec.DeploymentName(guard.GetName()) == deployment.Name {
			c.queue.Add(deployment.Namespace + "/" + guard.GetName())
		}
	}
}

func (c *RolloutGuardController) Reconcile(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	guard, err := c.dynamic.Resource(rolloutGuardGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	spec := rolloutGuardSpecFrom(guard)
	deploymentName := spec.DeploymentName(guard.GetName())
	deployment, err := c.kube.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		status := statusForMissingDeployment(guard, spec, deploymentName)
		if err := c.patchStatus(ctx, guard, status); err != nil {
			return err
		}
		c.recorder.Event(guard, corev1.EventTypeWarning, "DeploymentMissing", fmt.Sprintf("deployment %s was not found", deploymentName))
		return nil
	}
	if err != nil {
		return err
	}

	evaluation := EvaluateDeploymentRollout(deployment)
	status := statusForEvaluation(guard, spec, deployment, evaluation)
	switch {
	case evaluation.Blocked && spec.RollbackEnabled:
		rolledBackTo, rollbackErr := c.rollbackToPreviousReplicaSet(ctx, deployment)
		if rollbackErr != nil {
			status.Phase = "RollbackFailed"
			status.Reason = "RollbackFailed"
			status.Message = rollbackErr.Error()
			status.SetCondition("RolledBack", metav1.ConditionFalse, "RollbackFailed", rollbackErr.Error())
			c.recorder.Event(guard, corev1.EventTypeWarning, "RollbackFailed", rollbackErr.Error())
		} else {
			status.Phase = "RolledBack"
			status.Reason = "RollbackTriggered"
			status.Message = fmt.Sprintf("restored Deployment %s to ReplicaSet revision %s", deployment.Name, rolledBackTo)
			status.RollbackRevision = rolledBackTo
			status.SetCondition("RolledBack", metav1.ConditionTrue, "RollbackTriggered", status.Message)
			c.recorder.Event(guard, corev1.EventTypeWarning, "RollbackTriggered", status.Message)
		}
	case evaluation.Blocked:
		c.recorder.Event(guard, corev1.EventTypeWarning, "RolloutBlocked", evaluation.Message)
	case evaluation.Healthy:
		c.recorder.Event(guard, corev1.EventTypeNormal, "RolloutHealthy", evaluation.Message)
	default:
		c.recorder.Event(guard, corev1.EventTypeNormal, "RolloutProgressing", evaluation.Message)
	}
	if err := c.patchStatus(ctx, guard, status); err != nil {
		return err
	}
	c.logger.Info("reconciled rollout guard",
		"namespace", namespace,
		"name", name,
		"deployment", deploymentName,
		"phase", status.Phase,
		"reason", status.Reason,
	)
	return nil
}

func (c *RolloutGuardController) rollbackToPreviousReplicaSet(ctx context.Context, deployment *appsv1.Deployment) (string, error) {
	selector := metav1.FormatLabelSelector(deployment.Spec.Selector)
	replicaSets, err := c.kube.AppsV1().ReplicaSets(deployment.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", err
	}

	type revisionedReplicaSet struct {
		Revision int64
		RS       appsv1.ReplicaSet
	}
	var owned []revisionedReplicaSet
	for _, rs := range replicaSets.Items {
		if !isOwnedByDeployment(rs.OwnerReferences, deployment.UID) {
			continue
		}
		revision, ok := parseRevision(rs.Annotations)
		if !ok {
			continue
		}
		owned = append(owned, revisionedReplicaSet{Revision: revision, RS: rs})
	}
	if len(owned) < 2 {
		return "", fmt.Errorf("no previous ReplicaSet revision found for deployment %s", deployment.Name)
	}
	sort.Slice(owned, func(i, j int) bool {
		return owned[i].Revision > owned[j].Revision
	})
	current := owned[0].Revision
	var previous *revisionedReplicaSet
	for i := range owned {
		if owned[i].Revision < current {
			previous = &owned[i]
			break
		}
	}
	if previous == nil {
		return "", fmt.Errorf("no ReplicaSet revision older than %d found for deployment %s", current, deployment.Name)
	}

	updated := deployment.DeepCopy()
	updated.Spec.Template = *previous.RS.Spec.Template.DeepCopy()
	delete(updated.Spec.Template.Labels, "pod-template-hash")
	if updated.Spec.Template.Annotations == nil {
		updated.Spec.Template.Annotations = map[string]string{}
	}
	updated.Spec.Template.Annotations["sentinel.io/rollback-from-revision"] = strconv.FormatInt(current, 10)
	updated.Spec.Template.Annotations["sentinel.io/rollback-to-revision"] = strconv.FormatInt(previous.Revision, 10)
	updated.Spec.Template.Annotations["sentinel.io/rollback-at"] = time.Now().UTC().Format(time.RFC3339)
	_, err = c.kube.AppsV1().Deployments(deployment.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(previous.Revision, 10), nil
}

func (c *RolloutGuardController) patchStatus(ctx context.Context, guard *unstructured.Unstructured, status RolloutGuardStatus) error {
	payload, err := json.Marshal(map[string]any{"status": status.AsMap()})
	if err != nil {
		return err
	}
	_, err = c.dynamic.Resource(rolloutGuardGVR).Namespace(guard.GetNamespace()).Patch(
		ctx,
		guard.GetName(),
		types.MergePatchType,
		payload,
		metav1.PatchOptions{},
		"status",
	)
	return err
}

func isOwnedByDeployment(refs []metav1.OwnerReference, uid types.UID) bool {
	for _, ref := range refs {
		if ref.Kind == "Deployment" && ref.UID == uid {
			return true
		}
	}
	return false
}

func parseRevision(annotations map[string]string) (int64, bool) {
	if annotations == nil {
		return 0, false
	}
	value := annotations["deployment.kubernetes.io/revision"]
	revision, err := strconv.ParseInt(value, 10, 64)
	return revision, err == nil
}
