/*
Copyright 2025 baranitharan.chittharanjan@spark.co.nz.

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

package controller

import (
	"context"
	"fmt"
	"time"

	ocphealthcheckv1 "github.com/barani129/monitoring-wo-webhooks/api/v1"
	"github.com/barani129/monitoring-wo-webhooks/internal/ocphealthcheck/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// OcpHealthCheckReconciler reconciles a OcpHealthCheck object
type OcpHealthCheckReconciler struct {
	client.Client
	RESTClient               rest.Interface
	RESTConfig               *rest.Config
	Kind                     string
	ClusterResourceNamespace string
	recorder                 record.EventRecorder
	Scheme                   *runtime.Scheme
	InformerCount            int64
}

func (r *OcpHealthCheckReconciler) newOcpHealthChecker() (client.Object, error) {
	OcpHealthcheckKind := ocphealthcheckv1.GroupVersion.WithKind(r.Kind)
	ro, err := r.Scheme.New(OcpHealthcheckKind)
	if err != nil {
		return nil, err
	}
	return ro.(client.Object), nil
}

// +kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=ocphealthchecks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=ocphealthchecks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=ocphealthchecks/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="machineconfiguration.openshift.io",resources=machineconfigpools,verbs=get;list;watch
// +kubebuilder:rbac:groups="config.openshift.io",resources=clusteroperators,verbs=get;list;watch
// +kubebuilder:rbac:groups="operators.coreos.com",resources=catalogsources,verbs=get;list;watch
// +kubebuilder:rbac:groups="operators.coreos.com",resources=clusterserviceversions,verbs=get;list;watch
// +kubebuilder:rbac:groups="nmstate.io",resources=nodenetworkconfigurationpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups="policy.open-cluster-management.io",resources=policies,verbs=get;list;watch
// +kubebuilder:rbac:groups="cluster.open-cluster-management.io",resources=managedclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="argoproj.io",resources=argocds,verbs=get;list;watch
// +kubebuilder:rbac:groups="tuned.openshift.io",resources=profiles,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the OcpHealthCheck object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/reconcile
func (r *OcpHealthCheckReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	ocpScan, err := r.newOcpHealthChecker()
	if err != nil {
		log.Log.Error(err, "unrecognized ocphealthcheck type")
		return ctrl.Result{}, err
	}
	if err = r.Get(ctx, req.NamespacedName, ocpScan); err != nil {
		if err := client.IgnoreNotFound(err); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to retrieve OcpHealthCheck")
		}
		log.Log.Info("OcpHealthCheck is not found")
		return ctrl.Result{}, nil
	}
	spec, status, err := util.GetSpecAndStatus(ocpScan)
	if err != nil {
		log.Log.Error(err, "unable to retrieve OcpHealthCheck spec and status")
		return ctrl.Result{}, err
	}

	if spec.Suspend != nil && *spec.Suspend {
		log.Log.Info("OcpHealthCheck is suspended, skipping...")
		return ctrl.Result{RequeueAfter: time.Minute * 30}, nil
	}

	if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
		if spec.Email == "" || spec.RelayHost == "" {
			return ctrl.Result{}, fmt.Errorf("please configure valid email address/relay host in spec")
		}
	}

	// switch ocpScan.(type) {
	// case *ocphealthcheckv1.OcpHealthCheck:
	// 	// do nothing
	// default:
	// 	log.Log.Error(fmt.Errorf("unexpected ocphealthcheckscan object type: %s", ocpScan), "not retrying")
	// 	return ctrl.Result{}, nil
	// }

	// report gives feedback by updating the Ready condition of the ocphealthcheck scan
	report := func(conditionStatus ocphealthcheckv1.ConditionStatus, message string, err error) {
		eventType := corev1.EventTypeNormal
		if err != nil {
			log.Log.Error(err, message)
			eventType = corev1.EventTypeWarning
			message = fmt.Sprintf("%s: %v", message, err)
		} else {
			log.Log.Info(message)
		}
		r.recorder.Event(ocpScan, eventType, ocphealthcheckv1.EventReasonIssuerReconciler, message)
		util.SetReadyCondition(status, conditionStatus, ocphealthcheckv1.EventReasonIssuerReconciler, message)
	}

	defer func() {
		if err != nil {
			report(ocphealthcheckv1.ConditionFalse, "Trouble running OcpHealthCheckScan", err)
		}
		if updateErr := r.Status().Update(ctx, ocpScan); updateErr != nil {
			err = utilerrors.NewAggregate([]error{err, updateErr})
			result = ctrl.Result{}
		}
	}()

	if readyCond := util.GetReadyCondition(status); readyCond == nil {
		report(ocphealthcheckv1.ConditionUnknown, "First Seen", nil)
		return ctrl.Result{}, nil
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return ctrl.Result{}, err
	}

	clientset, err := dynamic.NewForConfig(config)
	if err != nil {
		return ctrl.Result{}, err
	}

	staticClientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return ctrl.Result{}, err
	}

	var runningHost string
	domain, err := util.GetAPIName(*staticClientSet)
	if err == nil && domain == "" {
		if spec.Cluster != nil {
			runningHost = *spec.Cluster
		}
	} else if err == nil && domain != "" {
		runningHost = domain
	} else {
		log.Log.Error(err, "unable to retrieve ocp config")
		runningHost = "local-cluster"
	}

	podResource := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}
	nodeResource := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "nodes",
	}

	mcpResource := schema.GroupVersionResource{
		Group:    "machineconfiguration.openshift.io",
		Version:  "v1",
		Resource: "machineconfigpools",
	}

	policyResource := schema.GroupVersionResource{
		Group:    "policy.open-cluster-management.io",
		Version:  "v1",
		Resource: "policies",
	}

	coResource := schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusteroperators",
	}

	nncpResource := schema.GroupVersionResource{
		Group:    "nmstate.io",
		Version:  "v1",
		Resource: "nodenetworkconfigurationpolicies",
	}

	catalogResource := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "catalogsources",
	}

	csvResource := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}

	mcResource := schema.GroupVersionResource{
		Group:    "cluster.open-cluster-management.io",
		Version:  "v1",
		Resource: "managedclusters",
	}

	argoResource := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1beta1",
		Resource: "argocds",
	}

	tpResource := schema.GroupVersionResource{
		Group:    "tuned.openshift.io",
		Version:  "v1",
		Resource: "profiles",
	}

	nsFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(clientset, time.Minute*10, corev1.NamespaceAll, nil)
	mcpInformer := nsFactory.ForResource(mcpResource).Informer()
	podInformer := nsFactory.ForResource(podResource).Informer()
	nodeInformer := nsFactory.ForResource(nodeResource).Informer()
	policyInformer := nsFactory.ForResource(policyResource).Informer()
	coInformer := nsFactory.ForResource(coResource).Informer()
	nncpInformer := nsFactory.ForResource(nncpResource).Informer()
	catalogInformer := nsFactory.ForResource(catalogResource).Informer()
	csvInformer := nsFactory.ForResource(csvResource).Informer()
	tpInformer := nsFactory.ForResource(tpResource).Informer()
	var mcInformer cache.SharedIndexInformer
	var argoInformer cache.SharedIndexInformer
	if spec.HubCluster != nil && *spec.HubCluster {
		mcInformer = nsFactory.ForResource(mcResource).Informer()
		argoInformer = nsFactory.ForResource(argoResource).Informer()
	}

	// mux := &sync.RWMutex{}
	// synced := false
	// logic for mcp handling: check if mcp is in progress, if in progress, fetch the node based on labels
	// mcp.spec.nodeSelector.matchLabels
	// check if annotation["machineconfiguration.openshift.io/state"] is set to other than Done
	// if not, assuming that mcp is actually in progress and exiting, otherwise continue with the flow
	mcpInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			util.OnMCPUpdate(newObj, staticClientSet, status, spec, runningHost)
			util.CleanUpRunningPods(staticClientSet, spec, status, runningHost)
		},
	})
	log.Log.Info("Adding add pod events to pod informer")
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			util.OnPodUpdate(newObj, spec, status, runningHost, staticClientSet)
		},
		DeleteFunc: func(obj interface{}) {
			util.OnPodDelete(obj, staticClientSet, spec, status, runningHost)
		},
	})
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			util.OnNodeUpdate(newObj, spec, status, runningHost)
		},
	})
	policyInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		// AddFunc: func(obj interface{}) {
		// 	util.OnPolicyAdd(obj, spec, status)
		// },
		UpdateFunc: func(oldObj, newObj interface{}) {
			util.OnPolicyUpdate(newObj, staticClientSet, spec, status, runningHost)
		},
		DeleteFunc: func(obj interface{}) {
			util.OnPolicyDelete(obj, spec, status, runningHost)
		},
	})
	coInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			util.OnCoUpdate(newObj, staticClientSet, spec, runningHost)
		},
	})
	catalogInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			util.OnCatalogSourceUpdate(newObj, staticClientSet, spec, runningHost)
		},
	})
	nncpInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			util.OnNNCPUpdate(newObj, staticClientSet, spec, runningHost)
		},
	})
	csvInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			util.OnCsvUpdate(newObj, staticClientSet, spec, runningHost)
		},
	})
	tpInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			util.OnTunedProfileUpdate(newObj, spec, runningHost)
		},
	})
	if spec.HubCluster != nil && *spec.HubCluster {
		mcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				util.OnManagedClusterUpdate(newObj, spec, runningHost)
			},
		})
		argoInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				util.OnArgoUpdate(newObj, spec, runningHost)
			},
		})
	}
	// go podInformer.Run(context.Background().Done())
	log.Log.Info("Starting dynamic informer factory")
	nsFactory.Start(ctx.Done())
	report(ocphealthcheckv1.ConditionTrue, "pod informers compiled successfully", nil)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OcpHealthCheckReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor(ocphealthcheckv1.EventSource)
	return ctrl.NewControllerManagedBy(mgr).
		For(&ocphealthcheckv1.OcpHealthCheck{}).
		Named("ocphealthcheck").
		Complete(r)
}
