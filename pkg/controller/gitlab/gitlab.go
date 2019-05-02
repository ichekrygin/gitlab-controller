/*
Copyright 2019 The GitLab-Controller Authors.

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

package gitlab

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"

	xpcorev1alpha1 "github.com/crossplaneio/crossplane/pkg/apis/core/v1alpha1"

	xpcachev1alpha1 "github.com/crossplaneio/crossplane/pkg/apis/cache/v1alpha1"
	xpstoragev1alpha1 "github.com/crossplaneio/crossplane/pkg/apis/storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"

	xpworkloadv1alpha1 "github.com/crossplaneio/crossplane/pkg/apis/workload/v1alpha1"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	crthandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/crossplaneio/gitlab-controller/pkg/apis/controller/v1alpha1"
	"github.com/crossplaneio/gitlab-controller/pkg/controller/gitlab/resources/sharedsecrets"
	"github.com/crossplaneio/gitlab-controller/pkg/logging"
)

const (
	controllerName      = "gitlab-controller"
	reconcileTimeout    = 5 * time.Minute
	requeueAfterSuccess = 1 * time.Minute

	applicationPrefix                = "gitlab-"
	failedToCreateApplicationsReason = "failed to create gitlab applications"
	failedToFindResourceClassReason  = "failed to find resource class"

	postgresEngineVersion = "9.6"
	redisEngineVersion    = "3.2"
)

var (
	log = logging.Logger.WithName("controller." + controllerName)

	reconcileSuccess = reconcile.Result{RequeueAfter: requeueAfterSuccess}
)

type operations interface {
	// client operations
	createObject(context.Context, runtime.Object) error
	updateStatus(context.Context) error

	// findResourceClass by provider name and field key/value
	findResourceClass(context.Context, string, string, string) (*corev1.ObjectReference, error)

	//
	createPostgresResource(context.Context) error

	// object operations
	GetApplicationName() string
	GetEndpoint() string
	GetNamespace() string
	GetName() string
	GetProviderName() string
	SetReady()
	SetEndpoint(string)

	failReconcile(ctx context.Context, reason, msg string) error
}

type handler struct {
	*v1alpha1.GitLab
	client client.Client
}

var _ operations = &handler{}

func (h *handler) failReconcile(ctx context.Context, reason, msg string) error {
	log.Info("reconciliation failure", "reason", reason, "msg", msg)
	h.SetFailed(reason, msg)
	return h.client.Status().Update(ctx, h.GitLab)
}

func (h *handler) createObject(ctx context.Context, obj runtime.Object) error {
	log.V(logging.Debug).Info("creating", "object", obj)
	return h.client.Create(ctx, obj)
}

// findResourceClass with matching provider name and field key/value combination
func (h *handler) findResourceClass(ctx context.Context, name, key, val string) (*corev1.ObjectReference, error) {
	rcs := &xpcorev1alpha1.ResourceClassList{}
	opts := client.MatchingField(key, val).MatchingField("providerRef/name", name)
	if err := h.client.List(ctx, opts, rcs); err != nil {
		return nil, errors.Wrapf(err, "failed to list resource classes, provider: %s, field %s:%s", name, key, val)
	}
	if len(rcs.Items) == 0 {
		return nil, errors.Errorf("resource class not found for provider: %s, with field: %s:%s", name, key, val)
	}
	return &corev1.ObjectReference{
		Namespace: rcs.Items[0].Namespace,
		Name:      rcs.Items[0].Name,
	}, nil
}

func (h *handler) updateStatus(ctx context.Context) error {
	log.V(logging.Debug).Info("updating status", "object", h.GitLab)
	return h.client.Status().Update(ctx, h.GitLab)
}

// opsMaker interface to create new operations instances
type opsMaker interface {
	newOperations(*v1alpha1.GitLab, client.Client) operations
}

// handleMaker implementation of opsMaker
type handleMaker struct{}

func (h *handleMaker) newOperations(gitlab *v1alpha1.GitLab, client client.Client) operations {
	return &handler{
		GitLab: gitlab,
		client: client,
	}
}

type creator struct {
	operations
}

// create GitLab application artifacts
func (g *creator) create(ctx context.Context) (reconcile.Result, error) {
	ns := g.GetNamespace()
	name := applicationPrefix + g.GetNamespace()
	log.V(logging.Debug).Info("creating", ns, name)

	if err := g.createApps(ctx, ns, name); err != nil {
		return reconcile.Result{Requeue: true}, g.failReconcile(ctx, failedToCreateApplicationsReason, err.Error())
	}

	g.SetEndpoint(g.GetEndpoint())
	return reconcileSuccess, errors.Wrapf(g.updateStatus(ctx), "failed to update object status")
}

func (g *creator) createResources(ctx context.Context) error {

}

func (g *creator) createResourcePostgres(ctx context.Context, ns, name, resourceClass string) (*xpstoragev1alpha1.PostgreSQLInstance, error) {
	ref, err := g.findResourceClass(ctx, resourceClass, "provisioner", xpstoragev1alpha1.PostgreSQLInstanceKindAPIVersion)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find postgres resource class")
	}

	pg := &xpstoragev1alpha1.PostgreSQLInstance{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: xpstoragev1alpha1.PostgreSQLInstanceSpec{
			ClassRef:      ref,
			EngineVersion: postgresEngineVersion,
		},
	}
	key := types.NamespacedName{Namespace: ns, Name: name}

	if err := g.getObject(ctx, key, pg); err != nil {
		if kerrors.IsNotFound(err) {
			return pg, errors.Wrapf(g.createObject(ctx, pg), "failed to create postgres: %v", pg)
		}
		return nil, errors.Errorf("faieled to retrieve posgtres instance: %s", key)
	}
	return pg, nil
}

func (g *creator) createResourceRedis(ctx context.Context, ns, name string, classRef *corev1.ObjectReference) error {
	redis := &xpcachev1alpha1.RedisCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: xpcachev1alpha1.RedisClusterSpec{
			ClassRef:      classRef,
			EngineVersion: redisEngineVersion,
		},
	}

	log.V(logging.Debug).Info("creating", "redis", redis)
	return errors.Wrapf(g.createObject(ctx, redis), "failed to create redis: %v", redis)
}

func (g *creator) createApps(ctx context.Context, ns, name string) error {
	log.V(logging.Debug).Info("creating applications", ns, name)

	sharedSecrets, err := SharedSecretsApp(ns, name, g.GetApplicationName())
	if err != nil {
		return err
	}
	if err := g.createObject(ctx, sharedSecrets); !kerrors.IsAlreadyExists(err) {
		return errors.Wrapf(err, "failed to create application")
	}

	return nil
}

// reconciler interface for GitLab objects
type reconciler interface {
	reconcile(context.Context, *v1alpha1.GitLab) (reconcile.Result, error)
}

type gitlabReconciler struct {
}

func (gl *gitlabReconciler) reconcile(ctx context.Context, gitlab *v1alpha1.GitLab) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func newGitLabRconciler() *gitlabReconciler {
	return &gitlabReconciler{}
}

// reconcilerMill creates a new instance reconciler
type reconcilerMill interface {
	newReconciler() reconciler
}

type gitlabReconcilerMill struct{}

func (g *gitlabReconcilerMill) newRconciler() reconciler {
	return newGitLabRconciler()
}

// Add creates a new GitLab Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	r := &Reconciler{Client: mgr.GetClient(), opsMaker: &handleMaker{}, scheme: mgr.GetScheme()}

	// Create a new controller
	c, err := controller.New("gitlab-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to GitLab
	if err := c.Watch(&source.Kind{Type: &v1alpha1.GitLab{}}, &crthandler.EnqueueRequestForObject{}); err != nil {
		return errors.Wrapf(err, "cannot watch for %s", v1alpha1.GitLabKindAPIVersion)
	}

	// Watch for changes to crossplane KubernetesApplication
	return errors.Wrapf(c.Watch(&source.Kind{Type: &xpworkloadv1alpha1.KubernetesApplication{}},
		&crthandler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &v1alpha1.GitLab{},
		}), "cannot watch for %s", xpworkloadv1alpha1.KubernetesApplicationKindAPIVersion)
}

var _ reconcile.Reconciler = &Reconciler{}

// Reconciler reconciles a GitLab object
type Reconciler struct {
	client.Client
	opsMaker
	reconciler
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a GitLab object and makes changes based on the state read
// and what is in the GitLab.Spec
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=controller.gitlab.io,resources=gitlabs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controller.gitlab.io,resources=gitlabs/status,verbs=get;update;patch
func (r *Reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.V(logging.Debug).Info("reconciling", "kind", v1alpha1.GitLabKind, "request", request)

	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()

	// Fetch the GitLab instance
	instance := &v1alpha1.GitLab{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// handle GitLab resources - create GitLab resources and
	// wait until all resources are in ready state to proceed to the next
	// reconciliation phase
	//

	// handle GitLab applications
	c := &creator{operations: r.newOperations(instance, r.Client)}

	return c.create(ctx)
}

func convert(o runtime.Object) (*unstructured.Unstructured, error) {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{}
	u.SetUnstructuredContent(content)
	return u, nil
}

// templates slice converted from the runtime.Object map
func templates(objects map[string]runtime.Object, ns, name string) ([]xpworkloadv1alpha1.KubernetesApplicationResourceTemplate, error) {
	templates := make([]xpworkloadv1alpha1.KubernetesApplicationResourceTemplate, len(objects))

	for suffix, obj := range objects {
		template, err := convert(obj)
		if err != nil {
			return nil, err
		}
		templates = append(templates, xpworkloadv1alpha1.KubernetesApplicationResourceTemplate{
			ObjectMeta: meta(ns, name, suffix),
			Spec:       xpworkloadv1alpha1.KubernetesApplicationResourceSpec{Template: template},
		})
	}

	return templates, nil
}

func meta(ns, name, suffix string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: ns,
		Name:      name + "-" + suffix,
	}
}

// SharedSecretsApp gitlab sub-application
func SharedSecretsApp(ns, name, prefix string) (*xpworkloadv1alpha1.KubernetesApplication, error) {
	templates, err := templates(sharedsecrets.Objects(ns, prefix), ns, name)
	if err != nil {
		return nil, err
	}

	return &xpworkloadv1alpha1.KubernetesApplication{
		Spec: xpworkloadv1alpha1.KubernetesApplicationSpec{
			ResourceTemplates: templates,
		},
	}, nil
}
