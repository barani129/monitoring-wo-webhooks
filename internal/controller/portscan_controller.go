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
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/barani129/container-scan/api/v1alpha1"
	clusterUtil "github.com/barani129/container-scan/internal/portscan/portutil"
)

// PortScanReconciler reconciles a PortScan object
type PortScanReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	Kind                     string
	ClusterResourceNamespace string
	recorder                 record.EventRecorder
}

//+kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=portscans,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=portscans/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *PortScanReconciler) newCluster() (client.Object, error) {
	PortScanGVK := monitoringv1alpha1.GroupVersion.WithKind(r.Kind)
	ro, err := r.Scheme.New(PortScanGVK)
	if err != nil {
		return nil, err
	}
	return ro.(client.Object), nil
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PortScan object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.2/pkg/reconcile
func (r *PortScanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	cluster, err := r.newCluster()

	if err != nil {
		log.Log.Error(err, "unrecognized Port scan type")
		return ctrl.Result{}, err
	}

	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if err := client.IgnoreNotFound(err); err != nil {
			return ctrl.Result{}, fmt.Errorf("unexpected get error : %v", err)
		}
		log.Log.Info("Port scan is not found, ignoring")
		return ctrl.Result{}, nil
	}
	clusterSpec, clusterStatus, err := clusterUtil.GetSpecAndStatus(cluster)
	if err != nil {
		log.Log.Error(err, "unexpected error while getting Port scan spec and status, not trying.")
		return ctrl.Result{}, nil
	}

	secretName := types.NamespacedName{
		Name: clusterSpec.ExternalSecret,
	}

	configmapName := types.NamespacedName{
		Name: clusterSpec.ExternalData,
	}

	switch cluster.(type) {
	case *monitoringv1alpha1.PortScan:
		secretName.Namespace = r.ClusterResourceNamespace
		configmapName.Namespace = r.ClusterResourceNamespace
	default:
		log.Log.Error(fmt.Errorf("unexpected issuer type: %s", cluster), "not retrying")
		return ctrl.Result{}, nil
	}

	var secret corev1.Secret
	var configmap corev1.ConfigMap
	var username []byte
	var password []byte
	var data map[string]string
	if *clusterSpec.NotifyExtenal {
		if err := r.Get(ctx, secretName, &secret); err != nil {
			return ctrl.Result{}, fmt.Errorf("%w, secret name: %s, reason: %v", errGetAuthSecret, secretName, err)
		}
		username = secret.Data["username"]
		password = secret.Data["password"]
	}

	if *clusterSpec.NotifyExtenal {
		if err := r.Get(ctx, configmapName, &configmap); err != nil {
			return ctrl.Result{}, fmt.Errorf("%w, configmap name: %s, reason: %v", errGetAuthConfigMap, configmapName, err)
		}
		data = configmap.Data
	}

	// report gives feedback by updating the Ready condition of the Port scan
	report := func(conditionStatus monitoringv1alpha1.ConditionStatus, message string, err error) {
		eventType := corev1.EventTypeNormal
		if err != nil {
			log.Log.Error(err, message)
			eventType = corev1.EventTypeWarning
			message = fmt.Sprintf("%s: %v", message, err)
		} else {
			log.Log.Info(message)
		}
		r.recorder.Event(cluster, eventType, monitoringv1alpha1.PortEventReasonIssuerReconciler, message)
		clusterUtil.SetReadyCondition(clusterStatus, conditionStatus, monitoringv1alpha1.PortEventReasonIssuerReconciler, message)
	}

	defer func() {
		if err != nil {
			report(monitoringv1alpha1.ConditionFalse, "Trouble reaching the one of the target or all targets", err)
		}
		if updateErr := r.Status().Update(ctx, cluster); updateErr != nil {
			err = utilerrors.NewAggregate([]error{err, updateErr})
			result = ctrl.Result{}
		}
	}()

	if ready := clusterUtil.GetReadyCondition(clusterStatus); ready == nil {
		report(monitoringv1alpha1.ConditionUnknown, "First Seen", nil)
		return ctrl.Result{}, nil
	}
	// filename := fmt.Sprintf("/%s.txt", clusterSpec.Target)
	// extFile := fmt.Sprintf("/%s-external.txt", clusterSpec.Target)
	defaultHealthCheckIntervalPort := time.Minute * time.Duration(*clusterSpec.CheckInterval)

	if clusterStatus.LastPollTime == nil {
		var errorp []string
		log.Log.Info("triggering server FQDN reachability")
		for _, target := range clusterSpec.Target {
			ip := strings.SplitN(target, ":", 2)
			err := clusterUtil.CheckServerAliveness(target, clusterStatus)
			if err != nil {
				log.Log.Error(err, fmt.Sprintf("target %s is unreachable.", clusterSpec.Target))
				errorp = append(errorp, ip[0])
				if !slices.Contains(clusterStatus.AffectedTargets, target) {
					clusterStatus.AffectedTargets = append(clusterStatus.AffectedTargets, target)
				}
				if !*clusterSpec.SuspendEmailAlert {
					clusterUtil.SendEmailAlert(target, fmt.Sprintf("%s-%s.txt", ip[0], ip[1]), clusterSpec, ip[0])
				}
				if *clusterSpec.NotifyExtenal {
					err := clusterUtil.NotifyExternalSystem(data, "firing", target, clusterSpec.ExternalURL, string(username), string(password), fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1]), clusterStatus)
					if err != nil {
						log.Log.Error(err, "Failed to notify the external system")
					}
					fingerprint, err := clusterUtil.ReadFile(fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1]))
					if err != nil {
						log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
					}
					incident, err := clusterUtil.SetIncidentID(clusterSpec, clusterStatus, string(username), string(password), fingerprint)
					if err != nil || incident == "" {
						log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
					}
					if !slices.Contains(clusterStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
						clusterStatus.IncidentID = append(clusterStatus.IncidentID, incident)
					}
					clusterStatus.ExternalNotified = true
					now := metav1.Now()
					clusterStatus.ExternalNotifiedTime = &now
				}
			} else {
				if _, err := os.Stat(fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1])); os.IsNotExist(err) {
					// no action
				} else {
					if slices.Contains(clusterStatus.AffectedTargets, target) {
						idx := slices.Index(clusterStatus.AffectedTargets, target)
						deleteElementSlice(clusterStatus.AffectedTargets, idx)
					}
					if !*clusterSpec.SuspendEmailAlert {
						clusterUtil.SendEmailReachableAlert(target, fmt.Sprintf("%s-%s.txt", ip[0], ip[1]), clusterSpec, ip[0])
					}
					if *clusterSpec.NotifyExtenal && clusterStatus.ExternalNotified {
						fingerprint, err := clusterUtil.ReadFile(fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1]))
						if err != nil {
							log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
						}
						incident, err := clusterUtil.SetIncidentID(clusterSpec, clusterStatus, string(username), string(password), fingerprint)
						if err != nil || incident == "" {
							log.Log.Info("Failed to get the incident ID, either incident is getting created or other issues.")
						}
						if slices.Contains(clusterStatus.IncidentID, incident) {
							idx := slices.Index(clusterStatus.IncidentID, incident)
							deleteElementSlice(clusterStatus.IncidentID, idx)
						}

						err = clusterUtil.SubNotifyExternalSystem(data, "resolved", target, clusterSpec.ExternalURL, string(username), string(password), fmt.Sprintf("%s-%s.txt", ip[0], ip[1]), clusterStatus)
						if err != nil {
							log.Log.Error(err, "Failed to notify the external system")
						}
						now := metav1.Now()
						clusterStatus.ExternalNotifiedTime = &now
					}
					os.Remove(fmt.Sprintf("%s-%s.txt", ip[0], ip[1]))
					os.Remove(fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1]))
				}
			}
		}

		if len(errorp) > 0 {
			return ctrl.Result{}, fmt.Errorf("%s", "one of the target or all are unreachable")
		}

		now := metav1.Now()
		clusterStatus.LastPollTime = &now
		clusterStatus.ExternalNotified = false
		report(monitoringv1alpha1.ConditionTrue, "Success. All targets are reachable.", nil)

	} else {
		pastTime := time.Now().Add(-1 * defaultHealthCheckIntervalPort)
		timeDiff := clusterStatus.LastPollTime.Time.Before(pastTime)
		var errorp []string
		if timeDiff {
			log.Log.Info("triggering server FQDN reachability as the time elapsed")
			for _, target := range clusterSpec.Target {
				ip := strings.SplitN(target, ":", 2)
				err := clusterUtil.CheckServerAliveness(target, clusterStatus)
				if err == nil {
					if _, err := os.Stat(fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1])); os.IsNotExist(err) {
						if slices.Contains(clusterStatus.AffectedTargets, target) {
							idx := slices.Index(clusterStatus.AffectedTargets, target)
							deleteElementSlice(clusterStatus.AffectedTargets, idx)
						}
					} else {
						if slices.Contains(clusterStatus.AffectedTargets, target) {
							idx := slices.Index(clusterStatus.AffectedTargets, target)
							deleteElementSlice(clusterStatus.AffectedTargets, idx)
						}
						if !*clusterSpec.SuspendEmailAlert {
							clusterUtil.SendEmailReachableAlert(target, fmt.Sprintf("%s-%s.txt", ip[0], ip[1]), clusterSpec, ip[0])
						}
						if *clusterSpec.NotifyExtenal && clusterStatus.ExternalNotified {
							fingerprint, err := clusterUtil.ReadFile(fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1]))
							if err != nil {
								log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
							}
							incident, err := clusterUtil.SetIncidentID(clusterSpec, clusterStatus, string(username), string(password), fingerprint)
							if err != nil || incident == "" {
								log.Log.Info("Failed to get the incident ID, either incident is getting created or other issues.")
							}
							if slices.Contains(clusterStatus.IncidentID, incident) {
								idx := slices.Index(clusterStatus.IncidentID, incident)
								deleteElementSlice(clusterStatus.IncidentID, idx)
							}

							err = clusterUtil.SubNotifyExternalSystem(data, "resolved", target, clusterSpec.ExternalURL, string(username), string(password), fmt.Sprintf("%s-%s.txt", ip[0], ip[1]), clusterStatus)
							if err != nil {
								log.Log.Error(err, "Failed to notify the external system")
							}
							now := metav1.Now()
							clusterStatus.ExternalNotifiedTime = &now

						}
						os.Remove(fmt.Sprintf("%s-%s.txt", ip[0], ip[1]))
						os.Remove(fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1]))
					}
				} else {
					errorp = append(errorp, ip[0])
					log.Log.Error(err, fmt.Sprintf("Target %s is unreachable.", clusterSpec.Target))
					if !slices.Contains(clusterStatus.AffectedTargets, target) {
						clusterStatus.AffectedTargets = append(clusterStatus.AffectedTargets, target)
					}
					if !*clusterSpec.SuspendEmailAlert {
						clusterUtil.SendEmailAlert(target, fmt.Sprintf("%s-%s.txt", ip[0], ip[1]), clusterSpec, ip[0])
					}
					if *clusterSpec.NotifyExtenal {
						err := clusterUtil.SubNotifyExternalSystem(data, "firing", target, clusterSpec.ExternalURL, string(username), string(password), fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1]), clusterStatus)
						if err != nil {
							log.Log.Error(err, "Failed to notify the external system")
						}
						fingerprint, err := clusterUtil.ReadFile(fmt.Sprintf("%s-%s-ext.txt", ip[0], ip[1]))
						if err != nil {
							log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
						}
						incident, err := clusterUtil.SetIncidentID(clusterSpec, clusterStatus, string(username), string(password), fingerprint)
						if err != nil || incident == "" {
							log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
						}
						if !slices.Contains(clusterStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
							clusterStatus.IncidentID = append(clusterStatus.IncidentID, incident)
						}
						clusterStatus.ExternalNotified = true
						now := metav1.Now()
						clusterStatus.ExternalNotifiedTime = &now
					}

				}
			}
			if len(errorp) > 0 {
				return ctrl.Result{}, fmt.Errorf("%s", "one of the target or all are unreachable")
			}

			now := metav1.Now()
			clusterStatus.LastPollTime = &now
			clusterStatus.ExternalNotified = false
			report(monitoringv1alpha1.ConditionTrue, "Success. All configured targets are reachable.", nil)
		}
	}
	return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalPort}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PortScanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor(monitoringv1alpha1.PortEventSource)
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.PortScan{}).
		Complete(r)
}
