/*
Copyright 2024 baranitharan.chittharanjan@spark.co.nz.

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
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"
	cidrranger "github.com/yl2chen/cidranger"
	metal1 "go.universe.tf/metallb/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/barani129/monitoring-wo-webhooks/api/v1alpha1"
	"github.com/barani129/monitoring-wo-webhooks/internal/metallbscan/util"
	tunedv1 "github.com/openshift/cluster-node-tuning-operator/pkg/apis/tuned/v1"
)

// MetallbScanReconciler reconciles a MetallbScan object
type MetallbScanReconciler struct {
	client.Client
	RESTClient               rest.Interface
	RESTConfig               *rest.Config
	Scheme                   *runtime.Scheme
	Kind                     string
	ClusterResourceNamespace string
	recorder                 record.EventRecorder
}

type BGPPeer struct {
	name      string
	namespace string
	ippool    string
	poolname  string
	conPool   bool
}

type BGPAd struct {
	svcname    string
	namespace  string
	poolname   string
	advertised bool
	advName    string
	peers      []string
}

type LBService struct {
	name      string
	namespace string
	lbip      string
	ep        []string
	epIP      []string
	epStatus  []bool
	epNode    []*string
}

type BGPRoute struct {
	svcname    string
	namespace  string
	lbip       string
	speakPod   string
	advertised bool
	validbest  bool
	status     string
	nodeName   string
}

type BGPHop struct {
	nodeName    string
	ip          string
	remoteAs    int
	valid       bool
	validstatus string
	established string
	upTimer     string
	bfdstatus   string
}

type BGPHopStatus struct {
	RemoteAs                                       int    `json:"remoteAs"`
	LocalAs                                        int64  `json:"localAs"`
	NbrExternalLink                                bool   `json:"nbrExternalLink"`
	BgpVersion                                     int    `json:"bgpVersion"`
	RemoteRouterID                                 string `json:"remoteRouterId"`
	LocalRouterID                                  string `json:"localRouterId"`
	BgpState                                       string `json:"bgpState"`
	BgpTimerUpMsec                                 int    `json:"bgpTimerUpMsec"`
	BgpTimerUpString                               string `json:"bgpTimerUpString"`
	BgpTimerUpEstablishedEpoch                     int    `json:"bgpTimerUpEstablishedEpoch"`
	BgpTimerLastRead                               int    `json:"bgpTimerLastRead"`
	BgpTimerLastWrite                              int    `json:"bgpTimerLastWrite"`
	BgpInUpdateElapsedTimeMsecs                    int    `json:"bgpInUpdateElapsedTimeMsecs"`
	BgpTimerHoldTimeMsecs                          int    `json:"bgpTimerHoldTimeMsecs"`
	BgpTimerKeepAliveIntervalMsecs                 int    `json:"bgpTimerKeepAliveIntervalMsecs"`
	BgpTimerConfiguredHoldTimeMsecs                int    `json:"bgpTimerConfiguredHoldTimeMsecs"`
	BgpTimerConfiguredKeepAliveIntervalMsecs       int    `json:"bgpTimerConfiguredKeepAliveIntervalMsecs"`
	ExtendedOptionalParametersLength               bool   `json:"extendedOptionalParametersLength"`
	BgpTimerConfiguredConditionalAdvertisementsSec int    `json:"bgpTimerConfiguredConditionalAdvertisementsSec"`
	NeighborCapabilities                           struct {
		FourByteAs      string `json:"4byteAs"`
		ExtendedMessage string `json:"extendedMessage"`
		AddPath         struct {
			Ipv4Unicast struct {
				RxAdvertised bool `json:"rxAdvertised"`
			} `json:"ipv4Unicast"`
			Ipv6Unicast struct {
				RxAdvertised bool `json:"rxAdvertised"`
			} `json:"ipv6Unicast"`
		} `json:"addPath"`
		Dynamic                        string `json:"dynamic"`
		ExtendedNexthop                string `json:"extendedNexthop"`
		ExtendedNexthopFamililesByPeer struct {
			Ipv4Unicast string `json:"ipv4Unicast"`
			Ipv4Vpn     string `json:"ipv4Vpn"`
		} `json:"extendedNexthopFamililesByPeer"`
		LongLivedGracefulRestart string `json:"longLivedGracefulRestart"`
		RouteRefresh             string `json:"routeRefresh"`
		EnhancedRouteRefresh     string `json:"enhancedRouteRefresh"`
		MultiprotocolExtensions  struct {
			Ipv4Unicast struct {
				AdvertisedAndReceived bool `json:"advertisedAndReceived"`
			} `json:"ipv4Unicast"`
			Ipv6Unicast struct {
				Advertised bool `json:"advertised"`
			} `json:"ipv6Unicast"`
		} `json:"multiprotocolExtensions"`
		HostName struct {
			AdvHostName   string `json:"advHostName"`
			AdvDomainName string `json:"advDomainName"`
		} `json:"hostName"`
		GracefulRestart                 string `json:"gracefulRestart"`
		GracefulRestartRemoteTimerMsecs int    `json:"gracefulRestartRemoteTimerMsecs"`
		AddressFamiliesByPeer           struct {
			Ipv4Unicast struct {
			} `json:"ipv4Unicast"`
		} `json:"addressFamiliesByPeer"`
	} `json:"neighborCapabilities"`
	GracefulRestartInfo struct {
		EndOfRibSend struct {
			Ipv4Unicast bool `json:"ipv4Unicast"`
		} `json:"endOfRibSend"`
		EndOfRibRecv struct {
			Ipv4Unicast bool `json:"ipv4Unicast"`
		} `json:"endOfRibRecv"`
		LocalGrMode  string `json:"localGrMode"`
		RemoteGrMode string `json:"remoteGrMode"`
		RBit         bool   `json:"rBit"`
		NBit         bool   `json:"nBit"`
		Timers       struct {
			ConfiguredRestartTimer int `json:"configuredRestartTimer"`
			ReceivedRestartTimer   int `json:"receivedRestartTimer"`
		} `json:"timers"`
		Ipv4Unicast struct {
			FBit           bool `json:"fBit"`
			EndOfRibStatus struct {
				EndOfRibSend            bool `json:"endOfRibSend"`
				EndOfRibSentAfterUpdate bool `json:"endOfRibSentAfterUpdate"`
				EndOfRibRecv            bool `json:"endOfRibRecv"`
			} `json:"endOfRibStatus"`
			Timers struct {
				StalePathTimer int `json:"stalePathTimer"`
			} `json:"timers"`
		} `json:"ipv4Unicast"`
		Ipv6Unicast struct {
			FBit           bool `json:"fBit"`
			EndOfRibStatus struct {
				EndOfRibSend            bool `json:"endOfRibSend"`
				EndOfRibSentAfterUpdate bool `json:"endOfRibSentAfterUpdate"`
				EndOfRibRecv            bool `json:"endOfRibRecv"`
			} `json:"endOfRibStatus"`
			Timers struct {
				StalePathTimer int `json:"stalePathTimer"`
			} `json:"timers"`
		} `json:"ipv6Unicast"`
	} `json:"gracefulRestartInfo"`
	MessageStats struct {
		DepthInq          int `json:"depthInq"`
		DepthOutq         int `json:"depthOutq"`
		OpensSent         int `json:"opensSent"`
		OpensRecv         int `json:"opensRecv"`
		NotificationsSent int `json:"notificationsSent"`
		NotificationsRecv int `json:"notificationsRecv"`
		UpdatesSent       int `json:"updatesSent"`
		UpdatesRecv       int `json:"updatesRecv"`
		KeepalivesSent    int `json:"keepalivesSent"`
		KeepalivesRecv    int `json:"keepalivesRecv"`
		RouteRefreshSent  int `json:"routeRefreshSent"`
		RouteRefreshRecv  int `json:"routeRefreshRecv"`
		CapabilitySent    int `json:"capabilitySent"`
		CapabilityRecv    int `json:"capabilityRecv"`
		TotalSent         int `json:"totalSent"`
		TotalRecv         int `json:"totalRecv"`
	} `json:"messageStats"`
	MinBtwnAdvertisementRunsTimerMsecs int `json:"minBtwnAdvertisementRunsTimerMsecs"`
	AddressFamilyInfo                  struct {
		Ipv4Unicast struct {
			UpdateGroupID                     int    `json:"updateGroupId"`
			SubGroupID                        int    `json:"subGroupId"`
			PacketQueueLength                 int    `json:"packetQueueLength"`
			CommAttriSentToNbr                string `json:"commAttriSentToNbr"`
			InboundPathPolicyConfig           bool   `json:"inboundPathPolicyConfig"`
			OutboundPathPolicyConfig          bool   `json:"outboundPathPolicyConfig"`
			RouteMapForIncomingAdvertisements string `json:"routeMapForIncomingAdvertisements"`
			RouteMapForOutgoingAdvertisements string `json:"routeMapForOutgoingAdvertisements"`
			AcceptedPrefixCounter             int    `json:"acceptedPrefixCounter"`
			SentPrefixCounter                 int    `json:"sentPrefixCounter"`
		} `json:"ipv4Unicast"`
		Ipv6Unicast struct {
			CommAttriSentToNbr                string `json:"commAttriSentToNbr"`
			InboundPathPolicyConfig           bool   `json:"inboundPathPolicyConfig"`
			OutboundPathPolicyConfig          bool   `json:"outboundPathPolicyConfig"`
			RouteMapForIncomingAdvertisements string `json:"routeMapForIncomingAdvertisements"`
			RouteMapForOutgoingAdvertisements string `json:"routeMapForOutgoingAdvertisements"`
			AcceptedPrefixCounter             int    `json:"acceptedPrefixCounter"`
		} `json:"ipv6Unicast"`
	} `json:"addressFamilyInfo"`
	ConnectionsEstablished    int    `json:"connectionsEstablished"`
	ConnectionsDropped        int    `json:"connectionsDropped"`
	LastResetTimerMsecs       int    `json:"lastResetTimerMsecs"`
	LastResetDueTo            string `json:"lastResetDueTo"`
	LastResetCode             int    `json:"lastResetCode"`
	ExternalBgpNbrMaxHopsAway int    `json:"externalBgpNbrMaxHopsAway"`
	HostLocal                 string `json:"hostLocal"`
	PortLocal                 int    `json:"portLocal"`
	HostForeign               string `json:"hostForeign"`
	PortForeign               int    `json:"portForeign"`
	Nexthop                   string `json:"nexthop"`
	NexthopGlobal             string `json:"nexthopGlobal"`
	NexthopLocal              string `json:"nexthopLocal"`
	BgpConnection             string `json:"bgpConnection"`
	ConnectRetryTimer         int    `json:"connectRetryTimer"`
	AuthenticationEnabled     int    `json:"authenticationEnabled"`
	ReadThread                string `json:"readThread"`
	WriteThread               string `json:"writeThread"`
	PeerBfdInfo               struct {
		Type             string `json:"type"`
		DetectMultiplier int    `json:"detectMultiplier"`
		RxMinInterval    int    `json:"rxMinInterval"`
		TxMinInterval    int    `json:"txMinInterval"`
		Status           string `json:"status"`
		LastUpdate       string `json:"lastUpdate"`
	} `json:"peerBfdInfo"`
}

func (r *MetallbScanReconciler) newIssuer() (client.Object, error) {
	MetallbScanKind := monitoringv1alpha1.GroupVersion.WithKind(r.Kind)
	ro, err := r.Scheme.New(MetallbScanKind)
	if err != nil {
		return nil, err
	}
	return ro.(client.Object), nil
}

//+kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=metallbscans,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=metallbscans/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=metallbscans/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;create;list;watch
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get;create;list;watch
//+kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;create;list;watch
//+kubebuilder:rbac:groups="",resources=pods/proxy,verbs=get;create;list;watch
//+kubebuilder:rbac:groups="",resources=pods/portforward,verbs=get;create;list;watch
//+kubebuilder:rbac:groups="",resources=pods/attach,verbs=get;create;list;watch
//+kubebuilder:rbac:groups="",resources=pods/log,verbs=get;create;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups="apps",resources=daemonsets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups="metallb.io",resources=ipaddresspools,verbs=get;list;watch
//+kubebuilder:rbac:groups="metallb.io",resources=bgpadvertisements,verbs=get;list;watch
//+kubebuilder:rbac:groups="tuned.openshift.io",resources=profiles,verbs=get;list;watch
//+kubebuilder:rbac:groups="machineconfiguration.openshift.io",resources=machineconfigpools,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the MetallbScan object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.2/pkg/reconcile
func (r *MetallbScanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	metallb, err := r.newIssuer()
	if err != nil {
		log.Log.Error(err, "unrecognized metallbscan type")
		return ctrl.Result{}, err
	}
	if err = r.Get(ctx, req.NamespacedName, metallb); err != nil {
		if err := client.IgnoreNotFound(err); err != nil {
			return ctrl.Result{}, fmt.Errorf("unexpected get error: %v", err)
		}
		log.Log.Info("metallbscan is not found")
		return ctrl.Result{}, nil
	}

	spec, status, err := util.GetSpecAndStatus(metallb)
	if err != nil {
		log.Log.Error(err, "unexpected error while getting Metallb scan spec and status, not trying.")
		return ctrl.Result{}, nil
	}

	secretName := types.NamespacedName{
		Name: spec.ExternalSecret,
	}

	configmapName := types.NamespacedName{
		Name: spec.ExternalData,
	}

	switch metallb.(type) {
	case *monitoringv1alpha1.MetallbScan:
		secretName.Namespace = r.ClusterResourceNamespace
		configmapName.Namespace = r.ClusterResourceNamespace
	default:
		log.Log.Error(fmt.Errorf("unexpected monitoring cr type: %s", metallb), "not retrying")
		return ctrl.Result{}, nil
	}

	var secret corev1.Secret
	var configmap corev1.ConfigMap
	var username []byte
	var password []byte
	var data map[string]string
	if spec.NotifyExternal != nil && *spec.NotifyExternal {
		if err := r.Get(ctx, secretName, &secret); err != nil {
			return ctrl.Result{}, fmt.Errorf("%w, secret name: %s, reason: %v", errGetAuthSecret, secretName, err)
		}
		username = secret.Data["username"]
		password = secret.Data["password"]
	}

	if spec.NotifyExternal != nil && *spec.NotifyExternal {
		if err := r.Get(ctx, configmapName, &configmap); err != nil {
			return ctrl.Result{}, fmt.Errorf("%w, configmap name: %s, reason: %v", errGetAuthConfigMap, configmapName, err)
		}
		data = configmap.Data
	}

	// report gives feedback by updating the Ready condition of the metallb scan
	report := func(conditionStatus monitoringv1alpha1.ConditionStatus, message string, err error) {
		eventType := corev1.EventTypeNormal
		if err != nil {
			log.Log.Error(err, message)
			eventType = corev1.EventTypeWarning
			message = fmt.Sprintf("%s: %v", message, err)
		} else {
			log.Log.Info(message)
		}
		r.recorder.Event(metallb, eventType, monitoringv1alpha1.MetallbEventReasonIssuerReconciler, message)
		util.SetReadyCondition(status, conditionStatus, monitoringv1alpha1.MetallbEventReasonIssuerReconciler, message)
	}

	defer func() {
		if err != nil {
			report(monitoringv1alpha1.ConditionFalse, "Trouble checking load balancer type services - error message to be improved", err)
		}
		if updateErr := r.Status().Update(ctx, metallb); updateErr != nil {
			err = utilerrors.NewAggregate([]error{err, updateErr})
			result = ctrl.Result{}
		}
	}()

	if ready := util.GetReadyCondition(status); ready == nil {
		report(monitoringv1alpha1.ConditionUnknown, "First Seen", nil)
		return ctrl.Result{}, nil
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to get in cluster configuration due to error %s", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to get in cluster configuration due to error %s", err)
	}

	var defaultHealthCheckIntervalMetal time.Duration
	if spec.CheckInterval != nil {
		defaultHealthCheckIntervalMetal = time.Minute * time.Duration(*spec.CheckInterval)
	} else {
		defaultHealthCheckIntervalMetal = time.Minute * 30
	}

	if spec.Suspend != nil && *spec.Suspend {
		log.Log.Info("Metallb scan is suspended, skipping...")
		return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
	}

	//get config from openshift's openshift-apiserver
	var runningHost string
	domain, err := util.GetAPIName(*clientset)
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

	var metallbNamespace string
	if spec.MetallbNamespace != nil && *spec.MetallbNamespace != "" {
		metallbNamespace = *spec.MetallbNamespace
	} else {
		metallbNamespace = "metallb-system"
	}
	var workerNodeLabel map[string]string
	if spec.WorkerLabel != nil {
		workerNodeLabel = *spec.WorkerLabel
	} else {
		workerNodeLabel = map[string]string{"node-role.kubernetes.io/worker": ""}
	}

	var speakerPodLabel map[string]string
	if spec.SpeakerPodLabel != nil {
		speakerPodLabel = *spec.SpeakerPodLabel
	} else {
		speakerPodLabel = map[string]string{"component": "speaker"}
	}
	nodeSelector := v1.LabelSelector{
		MatchLabels: workerNodeLabel,
	}
	speakerSelector := v1.LabelSelector{
		MatchLabels: speakerPodLabel,
	}

	var wg sync.WaitGroup
	if status.LastRunTime == nil {
		log.Log.Info(fmt.Sprintf("Staring metallbscan healthchecks in target cluster %s", runningHost))
		log.Log.Info("Checking if node rolling restart is in progress machineconfigpools.openshift.io/v1")
		mcpRunning, err := util.CheckMCPINProgress(clientset)
		// mcpRunning, mcpName, err := isMcpUpdating(*clientset)
		if err != nil && k8serrors.IsNotFound(err) {
			log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 is not configured in this cluster")
		} else if err != nil {
			log.Log.Error(err, "unable to retrieve machineconfigpools.machineconfiguration.openshift.io/v1")
		}
		if mcpRunning {
			log.Log.Info("machineconfigpool update is in progress, exiting")
			if !slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
				// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
				// 	util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool update is in progress in cluster %s", runningHost))
				// }
				status.FailedChecks = append(status.FailedChecks, "MachineConfigPool update is in progress")
			}
			return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
		} else {
			log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 update is not in progress, proceeding further.")
		}
		log.Log.Info("Checking for load balancer type services")
		lbsvcs, lbsvcsnoip, err := util.GetLoadBalancerSevices(*clientset)
		if err != nil {
			log.Log.Error(err, fmt.Sprintf("unable to retrieve loadbalancer type services from cluster %s", runningHost))
		}
		if len(lbsvcs) < 1 {
			log.Log.Info(fmt.Sprintf("Cluster %s doesn't have any services of load balancer type", runningHost))
			return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
		}
		if len(lbsvcsnoip) > 0 {
			for _, sv := range lbsvcsnoip {
				svc := strings.Split(sv, ":")
				if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s is found with no valid IP in namespace %s", svc[0], svc[1])) {
					if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
						util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "noip"), spec, fmt.Sprintf("Service %s in namespace %s is found with no valid IP in cluster %s", svc[0], svc[1], runningHost))
					}
					status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s is found with no valid IP in namespace %s", svc[0], svc[1]))
					if spec.NotifyExternal != nil && *spec.NotifyExternal {
						err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "alertnoip"), fmt.Sprintf("Service %s in namespace %s is found with no valid IP in cluster %s", svc[0], svc[1], runningHost))
						if err != nil {
							log.Log.Error(err, "Failed to notify the external system")
						}
						fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "alertnoip"))
						if err != nil {
							log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
						}
						incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
						if err != nil || incident == "" {
							log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
						}
						if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
							status.IncidentID = append(status.IncidentID, incident)
						}
					}
				}
			}
		}
		log.Log.Info("Configuration checks: Checking if load balancer services IP are part of configured ipaddresspools.metallb.io/v1beta1")
		bgpPeer, err := GetBGPIPPoolsPeer(*clientset, lbsvcs, metallbNamespace)
		if err != nil && k8serrors.IsNotFound(err) {
			log.Log.Error(err, fmt.Sprintf("Unable to retrieve ipaddresspools.metallb.io/v1beta1 from namespace %s in cluster %s", metallbNamespace, runningHost))
		} else if err != nil {
			log.Log.Error(err, "problems with retrieving ipaddresspools.metallb.io/v1beta1")
		}
		if len(bgpPeer) > 0 {
			wg.Add(len(bgpPeer))
			for _, peer := range bgpPeer {
				go func() {
					defer wg.Done()
					if !peer.conPool {
						if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools ", peer.name, peer.namespace)) {
							if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
								util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "ippool"), spec, fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools in target cluster %s ", peer.name, peer.namespace, runningHost))
							}
							status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools ", peer.name, peer.namespace))
							if spec.NotifyExternal != nil && *spec.NotifyExternal {
								err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "alertippool"), fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools", peer.name, peer.namespace))
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "alertippool"))
								if err != nil {
									log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
								}
								if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
									status.IncidentID = append(status.IncidentID, incident)
								}
							}
						}
					}
				}()
			}
			wg.Wait()
		}

		log.Log.Info("Configuration checks: Checking if IP addresspools.metallb.io/v1 are configured to be advertised bgpadvertisements.metallb.io/v1beta1")
		bgpAd, err := GetBGPIPAd(*clientset, bgpPeer, metallbNamespace)
		if err != nil && k8serrors.IsNotFound(err) {
			log.Log.Error(err, fmt.Sprintf("Unable to retrieve bgpadvertisements.metallb.io/v1beta1 from namespace %s in cluster %s", metallbNamespace, runningHost))
		} else if err != nil {
			log.Log.Error(err, "problems with retrieving bgpadvertisements.metallb.io/v1beta1")
		}
		if len(bgpAd) > 0 {
			wg.Add(len(bgpAd))
			for _, ad := range bgpAd {
				go func() {
					defer wg.Done()
					if ad.advName != "" {
						if !ad.advertised {
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 is not configured to be advertised bgpadvertisements.metallb.io/v1beta1", ad.svcname, ad.namespace, ad.poolname)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "advertised"), spec, fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 is not configured to be advertised bgpadvertisements.metallb.io/v1beta1 in cluster %s", ad.svcname, ad.namespace, ad.poolname, runningHost))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 is not configured to be advertised bgpadvertisements.metallb.io/v1beta1", ad.svcname, ad.namespace, ad.poolname))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "alertadvertised"), fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 which is not configured to be advertised bgpadvertisements.metallb.io/v1beta1", ad.svcname, ad.namespace, ad.poolname))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "alertadvertised"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						}
						if ad.peers == nil {
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "nopeer"), spec, fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "alertnopeer"), fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "alertnopeer"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						}
					}
				}()
			}
			wg.Wait()
		}

		log.Log.Info("Checking if node rolling restart is in progress machineconfigpools.openshift.io/v1")
		mcpRunning, err = util.CheckMCPINProgress(clientset)
		// mcpRunning, mcpName, err = isMcpUpdating(*clientset)
		if err != nil && k8serrors.IsNotFound(err) {
			log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 is not configured in this cluster")
		} else if err != nil {
			log.Log.Error(err, "unable to retrieve machineconfigpools.machineconfiguration.openshift.io/v1")
		}
		if mcpRunning {
			log.Log.Info("machineconfigpool update is in progress, exiting")
			if !slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
				// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
				// 	util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool update is in progress in cluster %s", runningHost))
				// }
				status.FailedChecks = append(status.FailedChecks, "MachineConfigPool update is in progress")
			}
			return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
		} else {
			log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 update is not in progress, proceeding further.")
		}
		log.Log.Info("Checking endpoints and target pods status for loadbalancer type services service.core/v1")
		lbService, err := GetSvcEndPoints(*clientset, lbsvcs)
		if err != nil {
			log.Log.Error(err, "problems with retrieving endpoints/pods")
		}
		var affectedLBService []string
		var lbWithNoEndpoints []string

		for _, lb := range lbService {
			for _, ep := range lb.epStatus {
				if !ep {
					affectedLBService = append(affectedLBService, lb.name)
				}
			}
			if lb.ep == nil {
				lbWithNoEndpoints = append(lbWithNoEndpoints, lb.name)
			}
		}

		if len(affectedLBService) < 1 && len(lbWithNoEndpoints) < 1 {
			log.Log.Info("All configured load balancer type services with a valid IP have healthy endpoints(pods)")
		}
		if len(affectedLBService) > 0 {
			wg.Add(len(affectedLBService))
			for _, lbs := range affectedLBService {
				go func() {
					defer wg.Done()
					for _, svc := range lbService {
						if svc.name == lbs {
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running", svc.name, svc.namespace)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "nonrunningendpoint"), spec, fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running in cluster %s", svc.name, svc.namespace, runningHost))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running", svc.name, svc.namespace))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnonrunningendpoint"), fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running in cluster %s", svc.name, svc.namespace, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnonrunningendpoint"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						}
					}
				}()
			}
			wg.Wait()
		}
		if len(lbWithNoEndpoints) > 0 {
			for _, sv := range lbWithNoEndpoints {
				for _, svc := range lbService {
					if sv == svc.name {
						if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s in namespace %s doesn't have any target pods", svc.name, svc.namespace)) {
							if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
								util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "noendpoint"), spec, fmt.Sprintf("Service %s in namespace %s doesn't have any target pods in cluster %s", svc.name, svc.namespace, runningHost))
							}
							status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s in namespace %s doesn't have any target pods", svc.name, svc.namespace))
							if spec.NotifyExternal != nil && *spec.NotifyExternal {
								err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnoendpoint"), fmt.Sprintf("Service %s in namespace %s doesn't have any target pods in cluster %s", svc.name, svc.namespace, runningHost))
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnoendpoint"))
								if err != nil {
									log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
								}
								if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
									status.IncidentID = append(status.IncidentID, incident)
								}
							}
						}
					}
				}
			}
		}
		mcpRunning, err = util.CheckMCPINProgress(clientset)
		// mcpRunning, mcpName, err = isMcpUpdating(*clientset)
		if err != nil && k8serrors.IsNotFound(err) {
			log.Log.Info("machineconfigpools.openshift.io/v1 is not configured in this cluster")
		} else if err != nil {
			log.Log.Error(err, "unable to retrieve machineconfigpools.openshift.io/v1")
		}
		if mcpRunning {
			log.Log.Info("machineconfigpool update is in progress, exiting")
			if !slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
				// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
				// 	util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool %s update is in progress in cluster %s", mcpName, runningHost))
				// }
				status.FailedChecks = append(status.FailedChecks, "MachineConfigPool update is in progress")
			}
			return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
		} else {
			log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 update is not in progress, proceeding further.")
		}
		log.Log.Info("Checking BGP next hop status from each worker's speaker pods")
		bgpHop, err := CheckBGPHopWorkers(r, *clientset, metallbNamespace, nodeSelector, speakerSelector, spec, status, runningHost)
		if err != nil {
			log.Log.Error(err, "unable to retrieve BGP next hop status")
		}
		if len(bgpHop) > 0 {
			wg.Add(len(bgpHop))
			for _, hop := range bgpHop {
				go func() {
					defer wg.Done()
					if hop.established != "Established" && hop.established != "Active" {
						if !slices.Contains(status.FailedChecks, fmt.Sprintf("BGP neighbor %s connectivity is not established from worker %s's speaker pod", hop.ip, hop.nodeName)) {
							if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
								util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "notestablished"), spec, fmt.Sprintf("Connectivity status of BGP neighbor %s is not established/active from worker %s speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
							}
							status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("BGP neighbor %s connectivity is not established from worker %s's speaker pod", hop.ip, hop.nodeName))
							if spec.NotifyExternal != nil && *spec.NotifyExternal {
								err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotestablished"), fmt.Sprintf("BGP neighbor %s connectivity is not established from worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotestablished"))
								if err != nil {
									log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
								}
								if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
									status.IncidentID = append(status.IncidentID, incident)
								}
							}
						}
					}
					if !hop.valid {
						if !slices.Contains(status.FailedChecks, fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName)) {
							if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
								util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "notvalid"), spec, fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod in cluster %s, current status is %s", hop.ip, hop.nodeName, runningHost, hop.validstatus))
							}
							status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName))
							if spec.NotifyExternal != nil && *spec.NotifyExternal {
								err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotvalid"), fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod in cluster %s, current status is %s", hop.ip, hop.nodeName, runningHost, hop.validstatus))
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotvalid"))
								if err != nil {
									log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
								}
								if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
									status.IncidentID = append(status.IncidentID, incident)
								}
							}
						}
					}
					if hop.bfdstatus != "" && hop.bfdstatus != "Up" {
						if spec.IgnoreBFD != nil && *spec.IgnoreBFD {
							log.Log.Info(fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod and spec.ignoreBFD is set, so ignoring", hop.ip, hop.nodeName))
						} else {
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "nobfd"), spec, fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnobfd"), fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnobfd"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						}
					}
				}()
			}
			wg.Wait()
		}

		log.Log.Info("Checking if node rolling restart is in progress machineconfigpools.openshift.io/v1")
		mcpRunning, err = util.CheckMCPINProgress(clientset)
		// mcpRunning, mcpName, err = isMcpUpdating(*clientset)
		if err != nil && k8serrors.IsNotFound(err) {
			log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 is not configured in this cluster")
		} else if err != nil {
			log.Log.Error(err, "unable to retrieve machineconfigpools.machineconfiguration.openshift.io/v1")
		}
		if mcpRunning {
			log.Log.Info("machineconfigpool update is in progress, exiting")
			if !slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
				// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
				// 	util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool %s update is in progress in cluster %s", mcpName, runningHost))
				// }
				status.FailedChecks = append(status.FailedChecks, "MachineConfigPool update is in progress")
			}
			return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
		} else {
			log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 update is not in progress, proceeding further.")
		}

		log.Log.Info("Checking if loadbalancer type service's external IP is advertised by speaker pods where endpoints are running")
		bgpRoute, err := GetBGPIPRoute(r, *clientset, metallbNamespace, speakerSelector, lbService, spec, status)
		if err != nil {
			log.Log.Error(err, "problem with retrieving BGP routes")
		}
		if len(bgpRoute) > 0 {
			wg.Add(len(bgpRoute))
			for _, route := range bgpRoute {
				go func() {
					defer wg.Done()
					if !route.advertised {
						if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s is not advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.speakPod, route.nodeName)) {
							if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
								util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName)), spec, fmt.Sprintf("Service %s's external IP %s in namespace %s is not advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.lbip, route.namespace, route.speakPod, route.nodeName, runningHost))
							}
							status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s is not advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.speakPod, route.nodeName))
							if spec.NotifyExternal != nil && *spec.NotifyExternal {
								err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alert"), fmt.Sprintf("Service %s's external IP %s is not advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.lbip, route.speakPod, route.nodeName, runningHost))
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alert"))
								if err != nil {
									log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
								}
								if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
									status.IncidentID = append(status.IncidentID, incident)
								}
							}
						}
					}
					if !route.validbest {
						if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s in namespace %s doesn't have the best route advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.namespace, route.speakPod, route.nodeName)) {
							if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
								util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "nonbest"), spec, fmt.Sprintf("Service %s's external IP %s doesn't have the best route advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.lbip, route.speakPod, route.nodeName, runningHost))
							}
							status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s in namespace %s doesn't have the best route advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.namespace, route.speakPod, route.nodeName))
							if spec.NotifyExternal != nil && *spec.NotifyExternal {
								err := util.NotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alertnonbest"), fmt.Sprintf("Service %s's external IP %s doesn't have the best route advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.lbip, route.speakPod, route.nodeName, runningHost))
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alertnonbest"))
								if err != nil {
									log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
								}
								if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
									status.IncidentID = append(status.IncidentID, incident)
								}
							}
						}
					}
				}()
			}
			wg.Wait()
		}
		now := v1.Now()
		status.LastRunTime = &now
		if len(status.FailedChecks) < 1 {
			status.Healthy = true
			now := v1.Now()
			status.LastSuccessfulRunTime = &now
			log.Log.Info("All configured load balancer type services with a valid IP have healthy endpoints(pods)")
			log.Log.Info("All configured load balancer type services are advertised from respective endpoint's worker nodes")
			log.Log.Info("All worker's speaker pods have established BGP session with remote hops.")
			report(monitoringv1alpha1.ConditionTrue, "All healthchecks are completed successfully.", nil)
		} else {
			status.Healthy = false
			report(monitoringv1alpha1.ConditionFalse, "Some checks are failing, please check status.FailedChecks for list of failures.", nil)
		}
	} else {
		pastTime := time.Now().Add(-1 * defaultHealthCheckIntervalMetal)
		timeDiff := status.LastRunTime.Time.Before(pastTime)
		if timeDiff {
			// // Check tuned profiles
			// // ignore errors with tuned profile, just need to check the status
			// allProfiles, affprofiles, err := retrieveTunedProfiles(*clientset)
			// if err != nil && k8serrors.IsNotFound(err) {
			// 	log.Log.Info("no tuned.openshift.io profiles configured")
			// } else if err != nil {
			// 	log.Log.Info("Error retrieving tuned.openshift.io profiles")
			// } else {
			// 	if len(affprofiles) > 0 {
			// 		for _, prof := range affprofiles {
			// 			profile := strings.Split(prof, ":")
			// 			if !slices.Contains(status.FailedChecks, fmt.Sprintf("Tuned profile %s in node %s is either degraded or not applied", profile[0], profile[1])) {
			// 				if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
			// 					util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", profile[0], profile[1]), spec, fmt.Sprintf("Tuned profile %s in node %s is either degraded or not applied in cluster %s", profile[0], profile[1], runningHost))
			// 				}
			// 			}
			// 		}
			// 		for _, prof := range allProfiles {
			// 			if !slices.Contains(affprofiles, prof) {
			// 				profile := strings.Split(prof, ":")
			// 				if slices.Contains(status.FailedChecks, fmt.Sprintf("Tuned profile %s in node %s is either degraded or not applied", profile[0], profile[1])) {
			// 					if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
			// 						util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", profile[0], profile[1]), spec, fmt.Sprintf("Tuned profile %s in node %s is now healthy in cluster %s", profile[0], profile[1], runningHost))
			// 					}
			// 					idx := slices.Index(status.FailedChecks, fmt.Sprintf("Tuned profile %s in node %s is either degraded or not applied", profile[0], profile[1]))
			// 					status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
			// 					os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", profile[0], profile[1]))
			// 				}
			// 			}
			// 		}
			// 	}
			// }
			log.Log.Info("Staring metallbscan healthchecks as configured time interval has elasped")
			log.Log.Info("Checking if node rolling restart is in progress machineconfigpools.openshift.io/v1")
			mcpRunning, err := util.CheckMCPINProgress(clientset)
			// mcpRunning, mcpName, err := isMcpUpdating(*clientset)
			if err != nil && k8serrors.IsNotFound(err) {
				log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 is not configured in this cluster")
			} else if err != nil {
				log.Log.Error(err, "unable to retrieve machineconfigpools.machineconfiguration.openshift.io/v1")
			}
			if mcpRunning {
				log.Log.Info("machineconfigpool update is in progress, exiting")
				if !slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
					// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
					// 	util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool %s update is in progress in cluster %s", mcpName, runningHost))
					// }
					status.FailedChecks = append(status.FailedChecks, "MachineConfigPool update is in progress")
				}
				return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
			} else {
				if slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
					// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
					// 	util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool update is not in progress in cluster %s", runningHost))
					// }
					idx := slices.Index(status.FailedChecks, "MachineConfigPool update is in progress")
					status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
					// os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"))
				}
				log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 update is not in progress, proceeding further.")
			}
			log.Log.Info("Checking for load balancer type services")
			lbsvcs, lbsvcsnoip, err := util.GetLoadBalancerSevices(*clientset)
			if err != nil {
				log.Log.Error(err, fmt.Sprintf("unable to retrieve loadbalancer type services from cluster %s", runningHost))
			}
			if len(lbsvcs) < 1 {
				log.Log.Info(fmt.Sprintf("Cluster %s doesn't have any services of load balancer type", runningHost))
				return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
			}
			if len(lbsvcsnoip) > 0 {
				for _, sv := range lbsvcsnoip {
					svc := strings.Split(sv, ":")
					if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s is found with no valid IP in namespace %s", svc[0], svc[1])) {
						if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
							util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "noip"), spec, fmt.Sprintf("Service %s in namespace %s is found with no valid IP in cluster %s", svc[0], svc[1], runningHost))
						}
						status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s is found with no valid IP in namespace %s", svc[0], svc[1]))
						if spec.NotifyExternal != nil && *spec.NotifyExternal {
							err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "alertnoip"), fmt.Sprintf("Service %s in namespace %s is found with no valid IP in cluster %s", svc[0], svc[1], runningHost))
							if err != nil {
								log.Log.Error(err, "Failed to notify the external system")
							}
							fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "alertnoip"))
							if err != nil {
								log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
							}
							incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
							if err != nil || incident == "" {
								log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
							}
							if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
								status.IncidentID = append(status.IncidentID, incident)
							}
						}
					}
				}
			} else {
				for _, sv := range lbsvcsnoip {
					svc := strings.Split(sv, ":")
					if slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s is found with no valid IP in namespace %s", svc[0], svc[1])) {
						if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
							util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "noip"), spec, fmt.Sprintf("Service %s in namespace %s is found with a valid IP in cluster %s", svc[0], svc[1], runningHost))
						}
						idx := slices.Index(status.FailedChecks, fmt.Sprintf("Service %s is found with no valid IP in namespace %s", svc[0], svc[1]))
						status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
						os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "noip"))
						if spec.NotifyExternal != nil && *spec.NotifyExternal {
							fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "alertnoip"))
							if err != nil {
								log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
							}
							incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
							if err != nil || incident == "" {
								log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
							}
							if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
								idx := slices.Index(status.IncidentID, incident)
								status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
							}
							err = util.SubNotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "alertnoip"), fmt.Sprintf("Service %s in namespace %s is found with no valid IP in cluster %s", svc[0], svc[1], runningHost))
							if err != nil {
								log.Log.Error(err, "Failed to notify the external system")
							}
						}
						os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc[0], svc[1], "alertnoip"))
					}
				}
			}
			log.Log.Info("Configuration checks: Checking if load balancer services IP are part of configured ipaddresspools.metallb.io/v1beta1")
			bgpPeer, err := GetBGPIPPoolsPeer(*clientset, lbsvcs, metallbNamespace)
			if err != nil && k8serrors.IsNotFound(err) {
				log.Log.Error(err, fmt.Sprintf("Unable to retrieve ipaddresspools.metallb.io/v1beta1 from namespace %s in cluster %s", metallbNamespace, runningHost))
			} else if err != nil {
				log.Log.Error(err, "problems with retrieving ipaddresspools.metallb.io/v1beta1")
			}
			if len(bgpPeer) > 0 {
				wg.Add(len(bgpPeer))
				for _, peer := range bgpPeer {
					go func() {
						defer wg.Done()
						if !peer.conPool {
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools ", peer.name, peer.namespace)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "ippool"), spec, fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools in target cluster %s ", peer.name, peer.namespace, runningHost))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools ", peer.name, peer.namespace))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "alertippool"), fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools", peer.name, peer.namespace))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "alertippool"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						} else {
							if slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools ", peer.name, peer.namespace)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "ippool"), spec, fmt.Sprintf("Service %s configured in namespace %s is now part of a configured IP pool %s in target cluster %s ", peer.name, peer.namespace, peer.ippool, runningHost))
								}
								idx := slices.Index(status.FailedChecks, fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools ", peer.name, peer.namespace))
								status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
								os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "ippool"))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "alertippool"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										idx := slices.Index(status.IncidentID, incident)
										status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
									}
									err = util.SubNotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "alertippool"), fmt.Sprintf("Service %s configured in namespace %s is not part of any configured IP pools", peer.name, peer.namespace))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", peer.name, "alertippool"))
								}
							}
						}
					}()
				}
				wg.Wait()
			}

			log.Log.Info("Configuration checks: Checking if IP addresspools.metallb.io/v1 are configured to be advertised bgpadvertisements.metallb.io/v1beta1")
			bgpAd, err := GetBGPIPAd(*clientset, bgpPeer, metallbNamespace)
			if err != nil && k8serrors.IsNotFound(err) {
				log.Log.Error(err, fmt.Sprintf("Unable to retrieve bgpadvertisements.metallb.io/v1beta1 from namespace %s in cluster %s", metallbNamespace, runningHost))
			} else if err != nil {
				log.Log.Error(err, "problems with retrieving bgpadvertisements.metallb.io/v1beta1")
			}
			if len(bgpAd) > 0 {
				wg.Add(len(bgpAd))
				for _, ad := range bgpAd {
					go func() {
						defer wg.Done()
						if ad.advName != "" {
							if !ad.advertised {
								if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 is not configured to be advertised bgpadvertisements.metallb.io/v1beta1", ad.svcname, ad.namespace, ad.poolname)) {
									if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
										util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "advertised"), spec, fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 is not configured to be advertised bgpadvertisements.metallb.io/v1beta1 in cluster %s", ad.svcname, ad.namespace, ad.poolname, runningHost))
									}
									status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 is not configured to be advertised bgpadvertisements.metallb.io/v1beta1", ad.svcname, ad.namespace, ad.poolname))
									if spec.NotifyExternal != nil && *spec.NotifyExternal {
										err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "alertadvertised"), fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 which is not configured to be advertised bgpadvertisements.metallb.io/v1beta1", ad.svcname, ad.namespace, ad.poolname))
										if err != nil {
											log.Log.Error(err, "Failed to notify the external system")
										}
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "alertadvertised"))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											status.IncidentID = append(status.IncidentID, incident)
										}
									}
								}
							} else {
								if slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 is not configured to be advertised bgpadvertisements.metallb.io/v1beta1", ad.svcname, ad.namespace, ad.poolname)) {
									if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
										util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "advertised"), spec, fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 is now configured to be advertised bgpadvertisements.metallb.io/v1beta1 in cluster %s", ad.svcname, ad.namespace, ad.poolname, runningHost))
									}
									idx := slices.Index(status.FailedChecks, fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 is not configured to be advertised bgpadvertisements.metallb.io/v1beta1", ad.svcname, ad.namespace, ad.poolname))
									status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
									os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "advertised"))
									if spec.NotifyExternal != nil && *spec.NotifyExternal {
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "alertadvertised"))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											idx := slices.Index(status.IncidentID, incident)
											status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
										}
										err = util.NotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "alertadvertised"), fmt.Sprintf("Service %s's in namespace %s is part of IP pool %s ipaddresspools.metallb.io/v1beta1 which is not configured to be advertised bgpadvertisements.metallb.io/v1beta1", ad.svcname, ad.namespace, ad.poolname))
										if err != nil {
											log.Log.Error(err, "Failed to notify the external system")
										}
										os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.poolname, "alertadvertised"))
									}
								}
							}
							if ad.peers == nil {
								if !slices.Contains(status.FailedChecks, fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName)) {
									if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
										util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "nopeer"), spec, fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName))
									}
									status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName))
									if spec.NotifyExternal != nil && *spec.NotifyExternal {
										err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "alertnopeer"), fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName))
										if err != nil {
											log.Log.Error(err, "Failed to notify the external system")
										}
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "alertnopeer"))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											status.IncidentID = append(status.IncidentID, incident)
										}
									}

								}
							} else {
								if slices.Contains(status.FailedChecks, fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName)) {
									if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
										util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "nopeer"), spec, fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 now has valid peers configured", ad.advName))
									}
									idx := slices.Index(status.FailedChecks, fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName))
									status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
									os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "nopeer"))
									if spec.NotifyExternal != nil && *spec.NotifyExternal {
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "alertnopeer"))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											idx := slices.Index(status.IncidentID, incident)
											status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
										}
										err = util.NotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "alertnopeer"), fmt.Sprintf("BGP IP advertisement %s bgpadvertisements.metallb.io/v1beta1 doesn't have any valid peers configured", ad.advName))
										if err != nil {
											log.Log.Error(err, "Failed to notify the external system")
										}
										os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s.txt", ad.advName, "alertnopeer"))
									}
								}
							}
						}
					}()
				}
				wg.Wait()
			}

			log.Log.Info("Checking if node rolling restart is in progress machineconfigpools.openshift.io/v1")
			mcpRunning, err = util.CheckMCPINProgress(clientset)
			// mcpRunning, mcpName, err = isMcpUpdating(*clientset)
			if err != nil && k8serrors.IsNotFound(err) {
				log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 is not configured in this cluster")
			} else if err != nil {
				log.Log.Error(err, "unable to retrieve machineconfigpools.machineconfiguration.openshift.io/v1")
			}
			if mcpRunning {
				log.Log.Info("machineconfigpool update is in progress, exiting")
				if !slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
					// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
					// 	util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool %s update is in progress in cluster %s", mcpName, runningHost))
					// }
					status.FailedChecks = append(status.FailedChecks, "MachineConfigPool update is in progress")
				}
				return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
			} else {
				if slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
					// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
					// 	util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool update is not in progress in cluster %s", runningHost))
					// }
					idx := slices.Index(status.FailedChecks, "MachineConfigPool update is in progress")
					status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
					// os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"))
				}
				log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 update is not in progress, proceeding further.")
			}

			log.Log.Info("Checking endpoints and target pods status for loadbalancer type services service.core/v1")
			lbService, err := GetSvcEndPoints(*clientset, lbsvcs)
			if err != nil {
				log.Log.Error(err, "problems with retrieving endpoints/pods")
			}
			var affectedLBService []string
			var lbWithNoEndpoints []string
			for _, lb := range lbService {
				for _, ep := range lb.epStatus {
					if !ep {
						affectedLBService = append(affectedLBService, lb.name)
					}
				}
				if lb.ep == nil {
					lbWithNoEndpoints = append(lbWithNoEndpoints, lb.name)
				}
			}

			if len(affectedLBService) < 1 && len(lbWithNoEndpoints) < 1 {
				log.Log.Info("All configured load balancer type services with a valid IP have healthy endpoints(pods)")
			}
			if len(affectedLBService) > 0 {
				wg.Add(len(affectedLBService))
				for _, lbs := range affectedLBService {
					go func() {
						defer wg.Done()
						for _, svc := range lbService {
							if svc.name == lbs {
								if !slices.Contains(status.FailedChecks, fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running", svc.name, svc.namespace)) {
									if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
										util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "nonrunningendpoint"), spec, fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running", svc.name, svc.namespace))
									}
									status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running", svc.name, svc.namespace))
									if spec.NotifyExternal != nil && *spec.NotifyExternal {
										err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnonrunningendpoint"), fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running in cluster %s", svc.name, svc.namespace, runningHost))
										if err != nil {
											log.Log.Error(err, "Failed to notify the external system")
										}
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnonrunningendpoint"))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											status.IncidentID = append(status.IncidentID, incident)
										}
									}
								}
							}
						}
					}()
				}
				wg.Wait()
			} else {
				wg.Add(len(lbService))
				for _, svc := range lbService {
					go func() {
						defer wg.Done()
						if slices.Contains(status.FailedChecks, fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running", svc.name, svc.namespace)) {
							if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
								util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "nonrunningendpoint"), spec, fmt.Sprintf("All endpoints of Service %s in namespace %s are now running", svc.name, svc.namespace))
							}
							idx := slices.Index(status.FailedChecks, fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running", svc.name, svc.namespace))
							status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
							os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "nonrunningendpoint"))
							if spec.NotifyExternal != nil && *spec.NotifyExternal {
								fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnonrunningendpoint"))
								if err != nil {
									log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
								}
								if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
									idx := slices.Index(status.IncidentID, incident)
									status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
								}
								err = util.NotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnonrunningendpoint"), fmt.Sprintf("One of the target pods of Service %s in namespace %s is not running in cluster %s", svc.name, svc.namespace, runningHost))
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnonrunningendpoint"))
							}
						}
					}()
				}
				wg.Wait()

			}
			if len(lbWithNoEndpoints) > 0 {
				for _, sv := range lbWithNoEndpoints {
					for _, svc := range lbService {
						if sv == svc.name {
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s in namespace %s doesn't have any target pods", svc.name, svc.namespace)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "noendpoint"), spec, fmt.Sprintf("Service %s in namespace %s doesn't have any target pods in cluster %s", svc.name, svc.namespace, runningHost))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s in namespace %s doesn't have any target pods", svc.name, svc.namespace))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnoendpoint"), fmt.Sprintf("Service %s in namespace %s doesn't have any target pods in cluster %s", svc.name, svc.namespace, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnoendpoint"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						}
					}
				}
			} else {
				wg.Add(len(lbService))
				for _, svc := range lbService {
					go func() {
						defer wg.Done()
						if slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s in namespace %s doesn't have any target pods", svc.name, svc.namespace)) {
							if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
								util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "noendpoint"), spec, fmt.Sprintf("Service %s in namespace %s does at least have one target pod in cluster %s", svc.name, svc.namespace, runningHost))
							}
							idx := slices.Index(status.FailedChecks, fmt.Sprintf("Service %s in namespace %s doesn't have any target pods", svc.name, svc.namespace))
							status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
							os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "noendpoint"))
							if spec.NotifyExternal != nil && *spec.NotifyExternal {
								fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnoendpoint"))
								if err != nil {
									log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
								}
								if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
									idx := slices.Index(status.IncidentID, incident)
									status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
								}
								err = util.SubNotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnoendpoint"), fmt.Sprintf("Service %s in namespace %s doesn't have any target pods in cluster %s", svc.name, svc.namespace, runningHost))
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", svc.name, svc.namespace, "alertnoendpoint"))
							}
						}
					}()
				}
				wg.Wait()
			}

			log.Log.Info("Checking if node rolling restart is in progress machineconfigpools.openshift.io/v1")
			mcpRunning, err = util.CheckMCPINProgress(clientset)
			// mcpRunning, mcpName, err = isMcpUpdating(*clientset)
			if err != nil && k8serrors.IsNotFound(err) {
				log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 is not configured in this cluster")
			} else if err != nil {
				log.Log.Error(err, "unable to retrieve machineconfigpools.machineconfiguration.openshift.io/v1")
			}
			if mcpRunning {
				log.Log.Info("machineconfigpool update is in progress, exiting")
				if !slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
					// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
					// 	util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool %s update is in progress in cluster %s", mcpName, runningHost))
					// }
					status.FailedChecks = append(status.FailedChecks, "MachineConfigPool update is in progress")
				}
				return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
			} else {
				if slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
					// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
					// 	util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool update is not in progress in cluster %s", runningHost))
					// }
					idx := slices.Index(status.FailedChecks, "MachineConfigPool update is in progress")
					status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
					// os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"))
				}
				log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 update is not in progress, proceeding further.")
			}

			log.Log.Info("Checking BGP next hop status from each worker's speaker pods")
			bgpHop, err := CheckBGPHopWorkers(r, *clientset, metallbNamespace, nodeSelector, speakerSelector, spec, status, runningHost)
			if err != nil {
				log.Log.Error(err, "unable to retrieve BGP next hop status")
			}
			if len(bgpHop) > 0 {
				wg.Add(len(bgpHop))
				for _, hop := range bgpHop {
					go func() {
						defer wg.Done()
						if hop.established != "Established" && hop.established != "Active" {
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("BGP neighbor %s connectivity is not established from worker %s's speaker pod", hop.ip, hop.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "notestablished"), spec, fmt.Sprintf("Connectivity status of BGP neighbor %s is not established/active from worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("BGP neighbor %s connectivity is not established from worker %s's speaker pod", hop.ip, hop.nodeName))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotestablished"), fmt.Sprintf("BGP neighbor %s connectivity is not established from worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotestablished"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						} else {
							if slices.Contains(status.FailedChecks, fmt.Sprintf("BGP neighbor %s connectivity is not established from worker %s's speaker pod", hop.ip, hop.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "notestablished"), spec, fmt.Sprintf("BGP neighbor %s connectivity has now been established/active from worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
								}
								idx := slices.Index(status.FailedChecks, fmt.Sprintf("BGP neighbor %s connectivity is not established from worker %s's speaker pod", hop.ip, hop.nodeName))
								status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
								os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "notestablished"))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {

									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotestablished"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										idx := slices.Index(status.IncidentID, incident)
										status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
									}
									err = util.SubNotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotestablished"), fmt.Sprintf("BGP neighbor %s connectivity is not established from worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
								}
							}
						}
						if !hop.valid {
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "notvalid"), spec, fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod in cluster %s, current status is %s", hop.ip, hop.nodeName, runningHost, hop.validstatus))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotvalid"), fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod in cluster %s, current status is %s", hop.ip, hop.nodeName, runningHost, hop.validstatus))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotvalid"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						} else {
							if slices.Contains(status.FailedChecks, fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "notvalid"), spec, fmt.Sprintf("BGP neighbor %s now has a valid status in worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
								}
								idx := slices.Index(status.FailedChecks, fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName))
								status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
								os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "notvalid"))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotvalid"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										idx := slices.Index(status.IncidentID, incident)
										status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
									}
									err = util.SubNotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotvalid"), fmt.Sprintf("BGP neighbor %s doesn't have a valid status in worker %s's speaker pod in cluster %s, current status is %s", hop.ip, hop.nodeName, runningHost, hop.validstatus))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnotvalid"))
								}
							}
						}
						if hop.bfdstatus != "" && hop.bfdstatus != "Up" {
							if spec.IgnoreBFD != nil && *spec.IgnoreBFD {
								log.Log.Info(fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod and spec.ignoreBFD is set, so ignoring", hop.ip, hop.nodeName))
							} else {
								if !slices.Contains(status.FailedChecks, fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName)) {
									if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
										util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "nobfd"), spec, fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
									}
									status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName))
									if spec.NotifyExternal != nil && *spec.NotifyExternal {
										err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnobfd"), fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
										if err != nil {
											log.Log.Error(err, "Failed to notify the external system")
										}
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnobfd"))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											status.IncidentID = append(status.IncidentID, incident)
										}
									}
								}
							}
						} else {
							if slices.Contains(status.FailedChecks, fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "nobfd"), spec, fmt.Sprintf("BGP neighbor %s's BFD is now up in worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
								}
								idx := slices.Index(status.FailedChecks, fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod", hop.ip, hop.nodeName))
								status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
								os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "nobfd"))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnobfd"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										idx := slices.Index(status.IncidentID, incident)
										status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
									}
									err = util.SubNotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnobfd"), fmt.Sprintf("BGP neighbor %s's BFD doesn't have a valid status in worker %s's speaker pod in cluster %s", hop.ip, hop.nodeName, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", util.HandleCNString(hop.ip), util.HandleCNString(hop.nodeName), "alertnobfd"))
								}
							}
						}
					}()
				}
				wg.Wait()
			}
			log.Log.Info("Running BGP neighbour cleanup")
			CheckIfBGPHopExists(r, *clientset, metallbNamespace, nodeSelector, speakerSelector, spec, status, runningHost)
			log.Log.Info("Checking if node rolling restart is in progress machineconfigpools.openshift.io/v1")
			mcpRunning, err = util.CheckMCPINProgress(clientset)
			// mcpRunning, mcpName, err = isMcpUpdating(*clientset)
			if err != nil && k8serrors.IsNotFound(err) {
				log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 is not configured in this cluster")
			} else if err != nil {
				log.Log.Error(err, "unable to retrieve machineconfigpools.machineconfiguration.openshift.io/v1")
			}
			if mcpRunning {
				log.Log.Info("machineconfigpool update is in progress, exiting")
				if !slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
					// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
					// 	util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool %s update is in progress in cluster %s", mcpName, runningHost))
					// }
					status.FailedChecks = append(status.FailedChecks, "MachineConfigPool update is in progress")
				}
				return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
			} else {
				if slices.Contains(status.FailedChecks, "MachineConfigPool update is in progress") {
					// if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
					// 	util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"), spec, fmt.Sprintf("MachineConfigPool update is not in progress in cluster %s", runningHost))
					// }
					idx := slices.Index(status.FailedChecks, "MachineConfigPool update is in progress")
					status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
					// os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s.txt", "mcp"))
				}
				log.Log.Info("machineconfigpools.machineconfiguration.openshift.io/v1 update is not in progress, proceeding further.")
			}

			log.Log.Info("Checking if loadbalancer type service's external IP is advertised by speaker pods where endpoints are running")
			bgpRoute, err := GetBGPIPRoute(r, *clientset, metallbNamespace, speakerSelector, lbService, spec, status)
			if err != nil {
				log.Log.Error(err, "problem with retrieving BGP routes")
			}
			var unAdvertisedExternalIPs []string
			var nonBestRoute []string
			if len(bgpRoute) > 0 {
				wg.Add(len(bgpRoute))
				for _, route := range bgpRoute {
					go func() {
						defer wg.Done()
						if !route.advertised {
							unAdvertisedExternalIPs = append(unAdvertisedExternalIPs, route.svcname)
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s is not advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.speakPod, route.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName)), spec, fmt.Sprintf("Service %s's external IP %s in namespace %s is not advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.lbip, route.namespace, route.speakPod, route.nodeName, runningHost))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s is not advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.speakPod, route.nodeName))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alert"), fmt.Sprintf("Service %s's external IP %s is not advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.lbip, route.speakPod, route.nodeName, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alert"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						} else {
							if slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s is not advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.speakPod, route.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName)), spec, fmt.Sprintf("Service %s's external IP %s is now advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.lbip, route.speakPod, route.nodeName, runningHost))
								}
								idx := slices.Index(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s is not advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.speakPod, route.nodeName))
								status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
								os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName)))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alert"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										idx := slices.Index(status.IncidentID, incident)
										status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
									}
									err = util.SubNotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alert"), fmt.Sprintf("Service %s's external IP %s is not advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.namespace, route.speakPod, route.nodeName, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alert"))
								}

							}
						}
						if !route.validbest {
							nonBestRoute = append(nonBestRoute, route.svcname)
							if !slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s in namespace %s doesn't have the best route advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.namespace, route.speakPod, route.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "nonbest"), spec, fmt.Sprintf("Service %s's external IP %s in namespace %s doesn't have the best route advertised by speaker pod %s running in node %s in cluster %s, current status %s", route.svcname, route.lbip, route.namespace, route.speakPod, route.nodeName, runningHost, route.status))
								}
								status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s in namespace %s doesn't have the best route advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.namespace, route.speakPod, route.nodeName))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									err := util.SubNotifyExternalSystem(data, "firing", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alertnonbest"), fmt.Sprintf("Service %s's external IP %s doesn't have the best route advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.namespace, route.speakPod, route.nodeName, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alertnonbest"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(status.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										status.IncidentID = append(status.IncidentID, incident)
									}
								}
							}
						} else {
							if slices.Contains(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s in namespace %s doesn't have the best route advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.namespace, route.speakPod, route.nodeName)) {
								if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
									util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "nonbest"), spec, fmt.Sprintf("Service %s's external IP %s doesn't have the best route advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.namespace, route.speakPod, route.nodeName, runningHost))
								}
								idx := slices.Index(status.FailedChecks, fmt.Sprintf("Service %s's external IP %s in namespace %s doesn't have the best route advertised by speaker pod %s running in node %s", route.svcname, route.lbip, route.namespace, route.speakPod, route.nodeName))
								status.FailedChecks = deleteMetalElementSlice(status.FailedChecks, idx)
								os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "nonbest"))
								if spec.NotifyExternal != nil && *spec.NotifyExternal {
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alertnonbest"))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(spec, string(username), string(password), fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if slices.Contains(status.IncidentID, incident) {
										idx := slices.Index(status.IncidentID, incident)
										status.IncidentID = deleteMetalElementSlice(status.IncidentID, idx)
									}
									err = util.SubNotifyExternalSystem(data, "resolved", spec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alertnonbest"), fmt.Sprintf("Service %s's external IP %s doesn't have the best route advertised by speaker pod %s running in node %s in cluster %s", route.svcname, route.namespace, route.speakPod, route.nodeName, runningHost))
									if err != nil {
										log.Log.Error(err, "Failed to notify the external system")
									}
									os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s-%s-%s-%s.txt", route.svcname, route.speakPod, util.HandleCNString(route.nodeName), "alertnonbest"))
								}
							}
						}
					}()
				}
				wg.Wait()
			}
			if len(unAdvertisedExternalIPs) < 1 {
				if slices.Contains(status.FailedChecks, "is not advertised by speaker pod") {
					for idx, val := range status.FailedChecks {
						if strings.Contains(val, "is not advertised by speaker pod") {
							if len(status.FailedChecks) == 1 {
								status.FailedChecks = nil
							} else {
								status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
							}
						}
					}
				}
			}
			if len(nonBestRoute) < 1 {
				if slices.Contains(status.FailedChecks, "doesn't have the best route advertised by speaker pod") {
					for idx, val := range status.FailedChecks {
						if strings.Contains(val, "doesn't have the best route advertised by speaker pod") {
							if len(status.FailedChecks) == 1 {
								status.FailedChecks = nil
							} else {
								status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
							}
						}
					}
				}
			}
			// clean deleted services
			lbsvcs, _, err = util.GetLoadBalancerSevices(*clientset)
			if err != nil {
				log.Log.Info("unable to retrieve loadbalancer services.")
			}
			var newLbSvc []string
			for _, lbsvc := range lbsvcs {
				svcs := strings.Split(lbsvc, ":")
				newLbSvc = append(newLbSvc, svcs[0])
			}
			if len(newLbSvc) > 0 {
				if len(status.FailedChecks) > 0 {
					for idx, check := range status.FailedChecks {
						if strings.Contains(check, "doesn't have any target pods") || strings.Contains(check, "is found with no valid IP") || strings.Contains(check, "is not part of any configured IP pools") {
							svcs := strings.Split(check, " ")
							if !slices.Contains(newLbSvc, svcs[1]) {
								if len(status.FailedChecks) == 1 {
									status.FailedChecks = nil
								} else {
									status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
								}
							}
						} else if strings.Contains(check, "is not configured to be advertised") {
							svcs := strings.Split(check, " ")
							svc, _, _ := strings.Cut(svcs[1], `'s`)
							if !slices.Contains(newLbSvc, svc) {
								if len(status.FailedChecks) == 1 {
									status.FailedChecks = nil
								} else {
									status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
								}
							}
						}
					}
				}
			}

			now := v1.Now()
			status.LastRunTime = &now
			if len(status.FailedChecks) < 1 {
				status.Healthy = true
				now := v1.Now()
				status.LastSuccessfulRunTime = &now
				log.Log.Info("All configured load balancer type services with a valid IP have healthy endpoints(pods)")
				log.Log.Info("All configured load balancer type services are advertised from respective endpoint's worker nodes")
				log.Log.Info("All worker's speaker pods have established BGP session with remote hops.")
				report(monitoringv1alpha1.ConditionTrue, "All healthchecks are completed successfully.", nil)
			} else {
				status.Healthy = false
				report(monitoringv1alpha1.ConditionFalse, "Some checks are failing, please check status.FailedChecks for list of failures.", nil)
			}
		}
	}

	return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalMetal}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MetallbScanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor(monitoringv1alpha1.MetallbEventSource)
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.MetallbScan{}).
		Complete(r)
}

func checkIpPool(clientset kubernetes.Clientset, namespace string, externalIP string) (bool, string, string, error) {
	ippoolList := metal1.IPAddressPoolList{}
	ranger := cidrranger.NewPCTrieRanger()
	var ippools []string
	var indeIPs []string
	err := clientset.RESTClient().Get().AbsPath(fmt.Sprintf("/apis/metallb.io/v1beta1/namespaces/%s/ipaddresspools", namespace)).Do(context.Background()).Into(&ippoolList)
	if err != nil {
		return false, "", "", err
	}
	for _, pool := range ippoolList.Items {
		for _, add := range pool.Spec.Addresses {
			if add != "" {
				if !strings.Contains(add, "/") {
					if strings.Contains(add, "-") {
						inIP := strings.Split(add, "-")
						for _, IP := range inIP {
							indeIPs = append(indeIPs, pool.Name+":"+IP)
						}
					} else {
						indeIPs = append(indeIPs, pool.Name+":"+add)
					}
				} else {
					ippools = append(ippools, pool.Name+":"+add)
				}
			}
		}
	}
	if len(ippools) < 1 && len(indeIPs) < 1 {
		return false, "", "", fmt.Errorf("no valid bgp IP pools found")
	}
	for _, ip := range indeIPs {
		ips := strings.Split(ip, ":")
		if externalIP == ips[1] {
			return true, ips[0], ips[1], nil
		}
	}
	for _, ips := range ippools {
		ip := strings.Split(ips, ":")
		_, netw, _ := net.ParseCIDR(ip[1])
		ranger.Insert(cidrranger.NewBasicRangerEntry(*netw))
		contains, err := ranger.Contains(net.ParseIP(externalIP))
		if err != nil {
			return false, "", "", err
		}
		if contains {
			return true, ip[0], ip[1], nil
		}
	}
	return false, "", "", nil
}

func GetBGPIPPoolsPeer(clientset kubernetes.Clientset, loadbalancersvc []string, metallbnamespace string) ([]BGPPeer, error) {
	var bgppeer []BGPPeer
	for _, svcIPs := range loadbalancersvc {
		svcIP := strings.Split(svcIPs, ":")
		contains, ipPoolName, ipPoolAdd, err := checkIpPool(clientset, metallbnamespace, svcIP[2])
		if err != nil {
			return nil, err
		}
		if !contains {
			bgppeer = append(bgppeer, BGPPeer{
				name:      svcIP[0],
				namespace: svcIP[1],
				ippool:    "",
				poolname:  "",
				conPool:   false,
			})
		} else {
			bgppeer = append(bgppeer, BGPPeer{
				name:      svcIP[0],
				namespace: svcIP[1],
				ippool:    ipPoolAdd,
				poolname:  ipPoolName,
				conPool:   true,
			})
		}
	}
	return bgppeer, nil
}

func GetBGPIPAd(clientset kubernetes.Clientset, bgppeer []BGPPeer, metalnamespace string) ([]BGPAd, error) {
	var bgppeerad []BGPAd
	for _, bgp := range bgppeer {
		if bgp.poolname != "" || bgp.ippool != "" {
			contains, advname, peers, err := checkBgpAdvertisment(clientset, metalnamespace, bgp.poolname)
			if err != nil {
				return nil, err
			}
			if !contains {
				bgppeerad = append(bgppeerad, BGPAd{
					svcname:    bgp.name,
					namespace:  bgp.namespace,
					poolname:   bgp.poolname,
					advertised: false,
					advName:    "",
					peers:      nil,
				})
			} else {
				bgppeerad = append(bgppeerad, BGPAd{
					svcname:    bgp.name,
					namespace:  bgp.namespace,
					poolname:   bgp.poolname,
					advertised: true,
					advName:    advname,
					peers:      peers,
				})
			}
		}

	}
	return bgppeerad, nil
}

func getIPAddressPoolLabels(clientset kubernetes.Clientset, poolname string, namespace string) (map[string]string, error) {
	ippoolList := metal1.IPAddressPoolList{}
	err := clientset.RESTClient().Get().AbsPath(fmt.Sprintf("/apis/metallb.io/v1beta1/namespaces/%s/ipaddresspools", namespace)).Do(context.Background()).Into(&ippoolList)
	if err != nil {
		return nil, err
	}

	for _, pool := range ippoolList.Items {
		if poolname == pool.Name {
			return pool.Labels, nil
		}
	}
	return nil, nil
}

func checkBgpAdvertisment(clientset kubernetes.Clientset, namespace string, pool string) (bool, string, []string, error) {
	advList := metal1.BGPAdvertisementList{}
	err := clientset.RESTClient().Get().AbsPath(fmt.Sprintf("/apis/metallb.io/v1beta1/namespaces/%s/bgpadvertisements", namespace)).Do(context.Background()).Into(&advList)
	if err != nil {
		return false, "", nil, err
	}
	for _, adv := range advList.Items {
		if adv.Name != "" {
			if slices.Contains(adv.Spec.IPAddressPools, pool) {
				return true, adv.Name, adv.Spec.Peers, nil
			}
		}
	}
	// checking pools based on labels
	for _, adv := range advList.Items {
		if adv.Name != "" {
			label, err := getIPAddressPoolLabels(clientset, pool, namespace)
			if err != nil || label == nil {
				return false, "", nil, err
			}
			for _, lab := range adv.Spec.IPAddressPoolSelectors {
				for k1, v1 := range label {
					for k, v := range lab.MatchLabels {
						if k1 == k && v1 == v {
							return true, adv.Name, adv.Spec.Peers, nil
						}
					}
				}
			}
		}
	}
	return false, "", nil, nil
}

func isMcpUpdating(clientset kubernetes.Clientset) (bool, string, error) {
	mcpList := mcfgv1.MachineConfigPoolList{}
	err := clientset.RESTClient().Get().AbsPath("/apis/machineconfiguration.openshift.io/v1/machineconfigpools").Do(context.Background()).Into(&mcpList)
	if err != nil {
		return false, "", err
	}
	for _, mcp := range mcpList.Items {
		for _, cond := range mcp.Status.Conditions {
			if cond.Type == "Updating" {
				if cond.Status == "True" {
					return true, mcp.Name, nil
				}
			}
		}
	}
	return false, "", nil
}

func isPodRunning(clientset kubernetes.Clientset, name string, namespace string) (bool, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), name, v1.GetOptions{})
	if err != nil {
		return false, err
	}
	if pod.Status.Phase == "Running" {
		return true, nil
	}
	return false, nil
}

func GetSvcEndPoints(clientset kubernetes.Clientset, loadbalancersvc []string) ([]LBService, error) {
	var lbservice []LBService
	for _, svcs := range loadbalancersvc {
		var epName []string
		var epIP []string
		var epNode []*string
		var epStatus []bool
		svc := strings.Split(svcs, ":")
		ep, err := clientset.CoreV1().Endpoints(svc[1]).Get(context.Background(), svc[0], v1.GetOptions{})
		if err != nil {
			return nil, err
		} else if err != nil {
			return nil, err
		}
		for _, sub := range ep.Subsets {
			if sub.Addresses == nil {
				return nil, err
			}
			for _, add := range sub.Addresses {
				if add.TargetRef.Kind == "Pod" {
					epName = append(epName, add.TargetRef.Name)
					epIP = append(epIP, add.IP)
					epNode = append(epNode, add.NodeName)
					running, err := isPodRunning(clientset, add.TargetRef.Name, add.TargetRef.Namespace)
					if err != nil {
						return nil, err
					}
					if err == nil && !running {
						epStatus = append(epStatus, false)
					} else {
						epStatus = append(epStatus, true)
					}
				}
			}
		}
		lbservice = append(lbservice, LBService{
			name:      svc[0],
			namespace: svc[1],
			lbip:      svc[2],
			ep:        epName,
			epIP:      epIP,
			epNode:    epNode,
			epStatus:  epStatus,
		})
	}
	return lbservice, nil
}

func GetBGPIPRoute(r *MetallbScanReconciler, clientset kubernetes.Clientset, metalnamespace string, speakerSelector v1.LabelSelector, lbservice []LBService, spec *monitoringv1alpha1.MetallbScanSpec, status *monitoringv1alpha1.MetallbScanStatus) ([]BGPRoute, error) {
	var bgproute []BGPRoute
	for _, sv := range lbservice {
		if sv.lbip != "" {
			for _, node := range sv.epNode {
				podCount, pods, err := util.GetpodsFromNodeBasedonLabels(clientset, *node, metalnamespace, speakerSelector)
				if err != nil {
					log.Log.Error(err, fmt.Sprintf("unable to get pods from node %s", *node))
				}
				if podCount < 1 {
					log.Log.Info(fmt.Sprintf("no speaker pod is in running status in node %s in namespace %s", *node, metalnamespace))
					if !slices.Contains(status.FailedChecks, fmt.Sprintf("no speaker pod is in running status in node %s in namespace %s", *node, metalnamespace)) {
						if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
							util.SendEmailAlert(*node, fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(*node), "norunningspeaker"), spec, fmt.Sprintf("no speaker pod is in running status in node %s in namespace %s", *node, metalnamespace))
						}
						status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("no speaker pod is in running status in node %s in namespace %s", *node, metalnamespace))
					}

				} else {
					if slices.Contains(status.FailedChecks, fmt.Sprintf("no speaker pod is in running status in node %s in namespace %s", *node, metalnamespace)) {
						if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
							util.SendEmailRecoveredAlert(*node, fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(*node), "norunningspeaker"), spec, fmt.Sprintf("speaker pod found in running status in node %s in namespace %s", *node, metalnamespace))
						}
						idx := slices.Index(status.FailedChecks, fmt.Sprintf("no speaker pod is in running status in node %s in namespace %s", *node, metalnamespace))
						status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
						os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(*node), "norunningspeaker"))
					}

					outFile, err := os.OpenFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-bgpoutput.txt", pods[0]), os.O_CREATE|os.O_RDWR, 0644)
					if err != nil {
						return nil, err
					}
					writeFile(r, fmt.Sprintf("%s", "show ip bgp"), outFile, pods[0], metalnamespace)
					exists, validBest, status, err := util.CheckLBIPRoute(outFile, sv.lbip)
					if err != nil {
						return nil, err
					}
					bgproute = append(bgproute, BGPRoute{
						svcname:    sv.name,
						namespace:  sv.namespace,
						lbip:       sv.lbip,
						speakPod:   pods[0],
						advertised: exists,
						validbest:  validBest,
						status:     status,
						nodeName:   *node,
					})

				}
			}
		}
	}

	return bgproute, nil
}

func checkSpeakerDaemonset(clientset kubernetes.Clientset, namespace string, selector v1.LabelSelector) (bool, error) {
	dList, err := clientset.AppsV1().DaemonSets(namespace).List(context.Background(), v1.ListOptions{
		LabelSelector: labels.Set(selector.MatchLabels).String(),
	})
	if err != nil {
		return false, err
	}
	for _, dae := range dList.Items {
		if dae.Name != "" {
			if dae.Status.DesiredNumberScheduled == dae.Status.CurrentNumberScheduled {
				return true, nil
			}
		}
	}
	return false, nil
}

func CheckIfBGPHopExists(r *MetallbScanReconciler, clientset kubernetes.Clientset, metalnamespace string, nodeSelector v1.LabelSelector, speakerSelector v1.LabelSelector, spec *monitoringv1alpha1.MetallbScanSpec, status *monitoringv1alpha1.MetallbScanStatus, runningHost string) {
	var nWorker string
	var hop string
	for idx, check := range status.FailedChecks {
		if strings.Contains(check, "connectivity is not established from worker") {
			strs := strings.Split(check, " ")
			hop = strs[2]
			work := strs[9]
			nWorker, _, _ = strings.Cut(work, `'s`)
		}
		if nWorker != "" {
			podCount, pods, err := util.GetpodsFromNodeBasedonLabels(clientset, nWorker, metalnamespace, speakerSelector)
			if err != nil {
				log.Log.Info(fmt.Sprintf("%s", err.Error()))
			}
			if podCount < 1 {
				daemon, err := checkSpeakerDaemonset(clientset, metalnamespace, speakerSelector)
				if err != nil {
					log.Log.Info("unable to retrieve daemonset")
				}
				if err == nil && !daemon {
					if !slices.Contains(status.FailedChecks, fmt.Sprintf("no speaker pod is found in node %s in namespace %s", nWorker, metalnamespace)) {
						status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("no speaker pod is found in node %s in namespace %s", nWorker, metalnamespace))
						if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
							util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(nWorker), "nospeaker"), spec, fmt.Sprintf("no speaker pod is found in status in node %s in namespace %s", nWorker, metalnamespace))
						}

					}
					log.Log.Info(fmt.Sprintf("no speaker pod is found in node %s in namespace %s", nWorker, metalnamespace))
				} else if err == nil && daemon {
					log.Log.Info(fmt.Sprintf("no speaker pod is found in node %s in namespace %s, but speaker daemonset has valid number of replicas, so ignoring...", nWorker, metalnamespace))
				}
			} else {
				if slices.Contains(status.FailedChecks, fmt.Sprintf("no speaker pod is found in node %s in namespace %s", nWorker, metalnamespace)) {
					idx := slices.Index(status.FailedChecks, fmt.Sprintf("no speaker pod is found in node %s in namespace %s", nWorker, metalnamespace))
					status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
					if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
						util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", nWorker, "nospeaker"), spec, fmt.Sprintf("speaker pod is found in node %s in namespace %s", nWorker, metalnamespace))
					}
					os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(nWorker), "nospeaker"))
				}
				outFile, err := os.OpenFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-bgphop.txt", pods[0]), os.O_CREATE|os.O_RDWR, 0644)
				if err != nil {
					log.Log.Info(fmt.Sprintf("unable to write to the file, %s", err.Error()))
				}
				writeFile(r, fmt.Sprintf("%s", "show ip bgp nexthop"), outFile, pods[0], metalnamespace)
				exists, err := util.IsExistBGPHop(outFile, hop)
				if err == nil && !exists {
					status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
				} else if err != nil {
					log.Log.Info(fmt.Sprintf("%s", err.Error()))
				}
			}
		}
	}
}

func CheckBGPHopWorkers(r *MetallbScanReconciler, clientset kubernetes.Clientset, metalnamespace string, nodeSelector v1.LabelSelector, speakerSelector v1.LabelSelector, spec *monitoringv1alpha1.MetallbScanSpec, status *monitoringv1alpha1.MetallbScanStatus, runningHost string) ([]BGPHop, error) {
	var bgphops []BGPHop
	nodeList, err := clientset.CoreV1().Nodes().List(context.Background(), v1.ListOptions{
		LabelSelector: labels.Set(nodeSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, err
	}
	//alternate
	for _, node := range nodeList.Items {
		podCount, pods, err := util.GetpodsFromNodeBasedonLabels(clientset, node.Name, metalnamespace, speakerSelector)
		if err != nil {
			return nil, err
		}
		if podCount < 1 {
			daemon, err := checkSpeakerDaemonset(clientset, metalnamespace, speakerSelector)
			if err != nil {
				log.Log.Info("unable to retrieve daemonset")
			}
			if err == nil && !daemon {
				if !slices.Contains(status.FailedChecks, fmt.Sprintf("no speaker pod is found in node %s in namespace %s", node.Name, metalnamespace)) {
					status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("no speaker pod is found in node %s in namespace %s", node.Name, metalnamespace))
					if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
						util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(node.Name), "nospeaker"), spec, fmt.Sprintf("no speaker pod is found in status in node %s in namespace %s", node.Name, metalnamespace))
					}
				}
				log.Log.Info(fmt.Sprintf("no speaker pod is found in node %s in namespace %s", node.Name, metalnamespace))
			} else if err == nil && daemon {
				log.Log.Info(fmt.Sprintf("no speaker pod is found in node %s in namespace %s, but speaker daemonset has valid number of replicas, so ignoring...", node.Name, metalnamespace))
			}
			// return nil, fmt.Errorf(fmt.Sprintf("no speaker pod is in running status in node %s in namespace %s", node.Name, metalnamespace))
		} else {
			if slices.Contains(status.FailedChecks, fmt.Sprintf("no speaker pod is found in node %s in namespace %s", node.Name, metalnamespace)) {
				idx := slices.Index(status.FailedChecks, fmt.Sprintf("no speaker pod is found in node %s in namespace %s", node.Name, metalnamespace))
				status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
				if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
					util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", node.Name, "nospeaker"), spec, fmt.Sprintf("speaker pod is found in node %s in namespace %s", node.Name, metalnamespace))
				}
				os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(node.Name), "nospeaker"))
			}
			outFile, err := os.OpenFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-bgphop.txt", pods[0]), os.O_CREATE|os.O_RDWR, 0644)
			if err != nil {
				return nil, err
			}
			writeFile(r, fmt.Sprintf("%s", "show ip bgp nexthop"), outFile, pods[0], metalnamespace)
			hops, err := util.RetrieveBGPHop(outFile)
			if err != nil {
				return nil, err
			}
			if len(hops) != 0 {
				if slices.Contains(status.FailedChecks, fmt.Sprintf("no configured hops are found in speaker pod %s running in node %s", pods[0], node.Name)) {
					if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
						util.SendEmailRecoveredAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(node.Name), "nobgphopsspeaker"), spec, fmt.Sprintf("hops are found in speaker pod %s running in node %s", pods[0], node.Name))
					}
					idx := slices.Index(status.FailedChecks, fmt.Sprintf("no configured hops are found in speaker pod %s running in node %s", pods[0], node.Name))
					status.FailedChecks = deleteElementSlice(status.FailedChecks, idx)
					os.Remove(fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(node.Name), "nobgphopsspeaker"))
				}
				for _, hopv := range hops {
					hop := strings.Split(hopv, ":")
					jsonHop := fmt.Sprintf("show ip bgp neighbor %s json", hop[0])
					hopFile, err := os.OpenFile(fmt.Sprintf("/home/golanguser/files/metallb/.%s-bgphoproute.txt", pods[0]), os.O_CREATE|os.O_RDWR, 0644)
					if err != nil {
						return nil, err
					}
					writeFile(r, fmt.Sprintf("%s", jsonHop), hopFile, pods[0], metalnamespace)
					hopStatus, err := checkBGPHopStatus(hopFile, hop[0])
					if err != nil {
						return nil, err
					}
					if hopStatus.RemoteAs != 0 {
						bgphops = append(bgphops, BGPHop{
							nodeName:    node.Name,
							ip:          hop[0],
							remoteAs:    hopStatus.RemoteAs,
							valid:       true,
							validstatus: hop[1],
							established: hopStatus.BgpState,
							upTimer:     hopStatus.BgpTimerUpString,
							bfdstatus:   hopStatus.PeerBfdInfo.Status,
						})
					}
				}

			} else {
				if spec.IgnoreNoBGPHop != nil && *spec.IgnoreNoBGPHop {
					log.Log.Info(fmt.Sprintf("no configured hops are found in speaker pod %s running in node %s, alering spec.ignoreNoBGPHop is set to be disabled so ignoring", pods[0], node.Name))
				} else {
					if !slices.Contains(status.FailedChecks, fmt.Sprintf("no configured hops are found in speaker pod %s running in node %s", pods[0], node.Name)) {
						if spec.SuspendEmailAlert != nil && !*spec.SuspendEmailAlert {
							util.SendEmailAlert(runningHost, fmt.Sprintf("/home/golanguser/files/metallb/.%s.%s.txt", util.HandleCNString(node.Name), "nobgphopsspeaker"), spec, fmt.Sprintf("no configured hops are found (show ip bgp nexthop) in speaker pod %s running in node %s", pods[0], node.Name))
						}
						status.FailedChecks = append(status.FailedChecks, fmt.Sprintf("no configured hops are found in speaker pod %s running in node %s", pods[0], node.Name))
					}
				}
			}
		}
	}
	return bgphops, nil
}

func writeFile(r *MetallbScanReconciler, commandToRun string, outFile *os.File, pod string, namespace string) error {
	req := r.RESTClient.Post().Resource("pods").Name(pod).Namespace(namespace).SubResource("exec").VersionedParams(
		&corev1.PodExecOptions{
			Container: "frr",
			Command:   []string{"vtysh", "-c", fmt.Sprintf("%s", commandToRun)},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, runtime.NewParameterCodec(r.Scheme))
	// ex, err := remotecommand.NewWebSocketExecutor(config, "GET", req.URL().String())
	ex, err := remotecommand.NewSPDYExecutor(r.RESTConfig, "POST", req.URL())
	if err != nil {
		return err
	}
	err = ex.StreamWithContext(context.Background(), remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: outFile,
		Stderr: os.Stderr,
		Tty:    false,
	})
	if err != nil {
		return err
	}
	return nil
}

func checkBGPHopStatus(outFile *os.File, hop string) (BGPHopStatus, error) {
	var hopstatus BGPHopStatus
	content, err := os.ReadFile(outFile.Name())
	if err != nil {
		return BGPHopStatus{}, err
	}
	contents := strings.Trim(string(content), "\n")
	contents = strings.TrimPrefix(string(contents), "{")
	contents = strings.Replace(contents, fmt.Sprintf(`"%s":{`, hop), "{", 1)
	contents = strings.TrimSuffix(string(contents), "}")
	// contents = strings.TrimSuffix(string(contents), "\n")
	// contents = strings.TrimSuffix(string(contents), "}")
	err = json.Unmarshal([]byte(contents), &hopstatus)
	if err != nil {
		return BGPHopStatus{}, err
	}
	os.Remove(outFile.Name())
	return hopstatus, nil
}

func deleteMetalElementSlice(slice []string, index int) []string {
	return append(slice[:index], slice[index+1:]...)
}

func retrieveTunedProfiles(clientset kubernetes.Clientset) (allProfiles []string, affectedProfiles []string, err error) {
	tunedProList := tunedv1.ProfileList{}
	err = clientset.RESTClient().Get().AbsPath("/apis/tuned.openshift.io/v1/profiles").Do(context.Background()).Into(&tunedProList)
	if err != nil {
		return nil, nil, err
	}
	for _, prof := range tunedProList.Items {
		allProfiles = append(allProfiles, prof.Spec.Config.TunedProfile+":"+prof.Name)
		for _, cond := range prof.Status.Conditions {
			if cond.Type == "Applied" {
				if cond.Status == "False" {
					affectedProfiles = append(affectedProfiles, prof.Spec.Config.TunedProfile+":"+prof.Name)
				}
			}
			if cond.Type == "Degraded" {
				if cond.Status == "True" {
					affectedProfiles = append(affectedProfiles, prof.Spec.Config.TunedProfile+":"+prof.Name)
				}
			}
		}
	}
	return allProfiles, affectedProfiles, nil
}
