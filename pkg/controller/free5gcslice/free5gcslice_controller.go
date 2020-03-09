package free5gcslice

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"

	bansv1alpha1 "github.com/stevenchiu30801/free5gc-operator/pkg/apis/bans/v1alpha1"
	helmaction "helm.sh/helm/v3/pkg/action"
	helmloader "helm.sh/helm/v3/pkg/chart/loader"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var reqLogger = logf.Log.WithName("controller_free5gcslice")
var helmLogger = logf.Log.WithName("helm")

const helmChartsPath string = "/helm-charts"

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
	reqLogger.Info("Reconciling Free5GCSlice", "Request.Namespace", request.Namespace, "Request.Name", request.Name)

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
			return reconcile.Result{}, err
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

	// Create free5GC Helm values
	vals := map[string]interface{}{
		"global": map[string]interface{}{
			"image": map[string]interface{}{
				"free5gc": map[string]interface{}{
					"repository": "free5gc-private-build",
					"tag":        "latest",
				},
			},
		},
	}

	// Check if AMF, in representation of free5GC common NFs, already exists, if not create new free5GC cluster
	free5gc := &appsv1.Deployment{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: "free5gc-amf", Namespace: instance.Namespace}, free5gc)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating free5GC common NFs", "Namespace", instance.Namespace, "Name", "free5gc-common-nf")

		// Load free5GC common NF chart
		free5gcChartPath := helmChartsPath + "/free5gc-common-nf"
		free5gcChart, err := helmloader.Load(free5gcChartPath)
		if err != nil {
			helmLogger.Error(err, "Failed to load free5GC commmon NF chart at", free5gcChartPath)
			return reconcile.Result{}, err
		}

		// Install free5GC common NF chart
		free5gcInstall, err := newHelmInstall(instance.Namespace)
		if err != nil {
			return reconcile.Result{}, err
		}
		free5gcInstall.Namespace = instance.Namespace
		free5gcInstall.ReleaseName = "free5gc"
		free5gcInstall.Wait = true
		free5gcRelease, err := free5gcInstall.Run(free5gcChart, vals)
		if err != nil {
			helmLogger.Error(err, "Failed to install free5GC common NFs")
			return reconcile.Result{}, err
		}
		reqLogger.Info("Successfully create free5GC common NFs", "Release", free5gcRelease.Name)
	} else if err != nil {
		return reconcile.Result{}, err
	} else {
		// free5GC common NFs already exists
		reqLogger.Info("free5GC common NFs already exists", "Namespace", free5gc.Namespace, "Name", free5gc.Name)
	}

	// Create free5GC slice Helm values
	vals["sliceIdx"] = 1
	vals["supportedSnssaiList"] = instance.Spec.SnssaiList

	// Create a new slice UPF
	reqLogger.Info("Creating free5GC new slice UPF", "Namespace", instance.Namespace, "Name", "free5gc-upf", "S-NSSAIList", instance.Spec.SnssaiList)

	// Load free5GC UPF chart
	upfChartPath := helmChartsPath + "/free5gc-upf"
	upfChart, err := helmloader.Load(upfChartPath)
	if err != nil {
		helmLogger.Error(err, "Failed to load free5GC UPF chart at", upfChartPath)
		return reconcile.Result{}, err
	}

	// Install free5GC UPF chart
	upfInstall, err := newHelmInstall(instance.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}
	upfInstall.Namespace = instance.Namespace
	upfInstall.ReleaseName = "free5gc-upf"
	upfInstall.Wait = true
	upfRelease, err := upfInstall.Run(upfChart, vals)
	if err != nil {
		helmLogger.Error(err, "Failed to install free5GC UPF")
		return reconcile.Result{}, err
	}
	reqLogger.Info("Successfully create free5GC UPF", "Release", upfRelease.Name)

	// Create a new slice SMF
	reqLogger.Info("Creating free5GC new slice SMF", "Namespace", instance.Namespace, "Name", "free5gc-smf", "S-NSSAIList", instance.Spec.SnssaiList)

	// Load free5GC SMF chart
	smfChartPath := helmChartsPath + "/free5gc-smf"
	smfChart, err := helmloader.Load(smfChartPath)
	if err != nil {
		helmLogger.Error(err, "Failed to load free5GC SMF chart at", smfChartPath)
		return reconcile.Result{}, err
	}

	// Install free5GC SMF chart
	smfInstall, err := newHelmInstall(instance.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}
	smfInstall.Namespace = instance.Namespace
	smfInstall.ReleaseName = "free5gc-smf"
	smfInstall.Wait = true
	smfRelease, err := smfInstall.Run(smfChart, vals)
	if err != nil {
		helmLogger.Error(err, "Failed to install free5GC SMF")
		return reconcile.Result{}, err
	}
	reqLogger.Info("Successfully create free5GC SMF", "Release", smfRelease.Name)

	reqLogger.Info("Successfully create free5GC network slice", "SliceID", 1, "S-NSSAIList", instance.Spec.SnssaiList)

	return reconcile.Result{}, nil
}

// newHelmInstall creates a new Install object under the given namespace
func newHelmInstall(namespace string) (*helmaction.Install, error) {
	const (
		tokenFile  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
		rootCAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	)

	// Create Kubernetes client config to access the api from within a pod
	// https://kubernetes.io/docs/tasks/administer-cluster/access-cluster-api/#accessing-the-api-from-within-a-pod
	serviceHost, servicePort := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	apiServer := "https://" + net.JoinHostPort(serviceHost, servicePort)
	tokenBuf, err := ioutil.ReadFile(tokenFile)
	if err != nil {
		helmLogger.Error(err, "Failed to read Kubernetes token file")
		return nil, err
	}
	bearerToken := string(tokenBuf)
	caFile := rootCAFile

	cf := genericclioptions.NewConfigFlags(true)
	cf.Namespace = &namespace
	cf.APIServer = &apiServer
	cf.BearerToken = &bearerToken
	cf.CAFile = &caFile

	// Create Helm action.Configuration object
	actionConfig := new(helmaction.Configuration)
	err = actionConfig.Init(cf, namespace, "", helmDebugLog)
	if err != nil {
		helmLogger.Error(err, "Failed to get Kubernetes client config for Helm")
		return nil, err
	}

	// Create Helm action.Install obeject
	actionInstall := helmaction.NewInstall(actionConfig)

	return actionInstall, nil
}

// helmDebugLog returns a logger that writes debug strings
func helmDebugLog(format string, v ...interface{}) {
	debugMsg := fmt.Sprintf(format, v...)
	helmLogger.Info(debugMsg)
}
