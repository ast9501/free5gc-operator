package free5gcslice

import (
	"context"
	"fmt"

	bansv1alpha1 "github.com/stevenchiu30801/free5gc-operator/pkg/apis/bans/v1alpha1"
	helmaction "helm.sh/helm/v3/pkg/action"
	helmloader "helm.sh/helm/v3/pkg/chart/loader"
	helmkube "helm.sh/helm/v3/pkg/kube"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var controllerLogger = logf.Log.WithName("controller_free5gcslice")
var helmLogger = logf.Log.WithName("helm")

// TODO(user): Modify constant kubeConfigPath to your Kubernetes admin config
const kubeConfigPath string = "./admin.conf"
const helmChartsPath string = "./helm-charts"

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Free5GCSlice Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFree5GCSlice{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("free5gcslice-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Free5GCSlice
	err = c.Watch(&source.Kind{Type: &bansv1alpha1.Free5GCSlice{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Free5GCSlice
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &bansv1alpha1.Free5GCSlice{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileFree5GCSlice implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileFree5GCSlice{}

// ReconcileFree5GCSlice reconciles a Free5GCSlice object
type ReconcileFree5GCSlice struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Free5GCSlice object and makes changes based on the state read
// and what is in the Free5GCSlice.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileFree5GCSlice) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := controllerLogger.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Free5GCSlice")

	// Fetch the Free5GCSlice instance
	instance := &bansv1alpha1.Free5GCSlice{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Check if Mongo DB already exists, if not create a new one
	mongo := &appsv1.StatefulSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: "mongo", Namespace: instance.Namespace}, mongo)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Mongo DB", "Namespace", instance.Namespace, "Name", "mongo")

		// Load Mongo DB chart
		mongoChartPath := helmChartsPath + "/mongo"
		mongoChart, err := helmloader.Load(mongoChartPath)
		if err != nil {
			helmLogger.Error(err, "Failed to load Mongo DB chart at", mongoChartPath)
			return reconcile.Result{}, err
		}

		// Install Mongo DB chart
		mongoInstall, err := newHelmInstall(instance.Namespace)
		if err != nil {
			return reconcile.Result{}, nil
		}
		mongoInstall.Namespace = instance.Namespace
		mongoInstall.ReleaseName = "mongo"
		mongoInstall.Wait = true
		mongoRelease, err := mongoInstall.Run(mongoChart, nil)
		if err != nil {
			helmLogger.Error(err, "Failed to install Mongo DB")
			return reconcile.Result{}, err
		}

		reqLogger.Info("Successfully create Mongo DB", "Release", mongoRelease.Name)
	} else if err != nil {
		return reconcile.Result{}, err
	} else {
		// Mongo DB already exists
		reqLogger.Info("Mongo DB already exists", "Namespace", mongo.Namespace, "Name", mongo.Name)
	}

	return reconcile.Result{}, nil
}

// newHelmInstall creates a new Install object under the given namespace with kubeConfigPath
func newHelmInstall(namespace string) (*helmaction.Install, error) {
	actionConfig := new(helmaction.Configuration)
	err := actionConfig.Init(helmkube.GetConfig(kubeConfigPath, "", namespace), namespace, "", helmDebugLog)
	if err != nil {
		helmLogger.Error(err, "Failed to get Kubernetes client config for Helm")
		return nil, err
	}

	return helmaction.NewInstall(actionConfig), nil
}

// helmDebugLog returns a logger that writes debug strings
func helmDebugLog(format string, v ...interface{}) {
	debugMsg := fmt.Sprintf(format, v...)
	helmLogger.Info(debugMsg)
}
