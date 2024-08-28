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
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/barani129/monitoring-wo-webhooks/api/v1alpha1"
	"github.com/barani129/monitoring-wo-webhooks/internal/containerscan/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/rest"
)

var (
	errGetNamespace     = errors.New("failed to get the target namespace in the cluster")
	errGetAuthSecret    = errors.New("failed to get Secret containing External alert system credentials")
	errGetAuthConfigMap = errors.New("failed to get ConfigMap containing the data to be sent to the external alert system")
)

// ContainerScanReconciler reconciles a ContainerScan object
type ContainerScanReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	Kind                     string
	ClusterResourceNamespace string
	recorder                 record.EventRecorder
}

// +kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=containerscans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=containerscans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=containerscans/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
func (r *ContainerScanReconciler) newContainer() (client.Object, error) {
	ContainerScanGVK := monitoringv1alpha1.GroupVersion.WithKind(r.Kind)
	ro, err := r.Scheme.New(ContainerScanGVK)
	if err != nil {
		return nil, err
	}
	return ro.(client.Object), nil
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ContainerScan object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.2/pkg/reconcile
func (r *ContainerScanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here
	containerScan, err := r.newContainer()
	if err != nil {
		log.Log.Error(err, "unrecognized container scan type")
		return ctrl.Result{}, err
	}

	if err := r.Get(ctx, req.NamespacedName, containerScan); err != nil {
		if err := client.IgnoreNotFound(err); err != nil {
			return ctrl.Result{}, fmt.Errorf("unexpected get error : %v", err)
		}
		log.Log.Info("Container scan resource is not found, ignoring")
		return ctrl.Result{}, nil
	}

	containerSpec, containerStatus, err := util.GetSpecAndStatus(containerScan)
	if err != nil {
		log.Log.Error(err, "unexpected error while getting container scan spec and status, not trying.")
		return ctrl.Result{}, nil
	}

	// report gives feedback by updating the Ready condition of the Container scan
	report := func(conditionStatus monitoringv1alpha1.ConditionStatus, message string, err error) {
		eventType := corev1.EventTypeNormal
		if err != nil {
			log.Log.Error(err, message)
			eventType = corev1.EventTypeWarning
			message = fmt.Sprintf("%s: %v", message, err)
		} else {
			log.Log.Info(message)
		}
		r.recorder.Event(containerScan, eventType, monitoringv1alpha1.EventReasonIssuerReconciler, message)
		util.SetReadyCondition(containerStatus, conditionStatus, monitoringv1alpha1.EventReasonIssuerReconciler, message)
	}

	defer func() {
		if err != nil {
			report(monitoringv1alpha1.ConditionFalse, fmt.Sprintf("One or more containers have non-zero terminated in namespace %s", containerSpec.TargetNamespace), err)
		}
		if updateErr := r.Status().Update(ctx, containerScan); updateErr != nil {
			err = utilerrors.NewAggregate([]error{err, updateErr})
			result = ctrl.Result{}
		}
	}()
	var username string
	var password string
	var data map[string]string
	if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal {
		secretName := types.NamespacedName{
			Name: containerSpec.ExternalSecret,
		}

		configmapName := types.NamespacedName{
			Name: containerSpec.ExternalData,
		}

		switch containerScan.(type) {
		case *monitoringv1alpha1.ContainerScan:
			secretName.Namespace = r.ClusterResourceNamespace
			configmapName.Namespace = r.ClusterResourceNamespace
		default:
			log.Log.Error(fmt.Errorf("unexpected issuer type: %s", containerScan), "not retrying")
			return ctrl.Result{}, nil
		}
		var secret corev1.Secret
		var configmap corev1.ConfigMap
		if err := r.Get(ctx, secretName, &secret); err != nil {
			return ctrl.Result{}, fmt.Errorf("%w, secret name: %s, reason: %v", errGetAuthSecret, secretName, err)
		}
		username = string(secret.Data["username"])
		password = string(secret.Data["password"])
		if err := r.Get(ctx, configmapName, &configmap); err != nil {
			return ctrl.Result{}, fmt.Errorf("%w, configmap name: %s, reason: %v", errGetAuthConfigMap, configmapName, err)
		}
		data = configmap.Data

	}

	if ready := util.GetReadyCondition(containerStatus); ready == nil {
		report(monitoringv1alpha1.ConditionUnknown, "First Seen", nil)
		return ctrl.Result{}, nil
	}
	namespaces := containerSpec.TargetNamespace
	config, err := rest.InClusterConfig()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to retrieve in cluster configuration due to %s", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to retrieve in cluster configuration due to %s", err)
	}
	var defaultHealthCheckInterval time.Duration
	if containerSpec.CheckInterval != nil {
		defaultHealthCheckInterval = time.Minute * time.Duration(*containerSpec.CheckInterval)
	} else {
		defaultHealthCheckInterval = time.Minute * 30
	}

	if containerSpec.Suspend != nil && *containerSpec.Suspend {
		log.Log.Info("container scan is suspended, skipping..")
		return ctrl.Result{}, nil
	}
	if containerStatus.LastRunTime == nil {
		var afcontainers []string
		var afpods []string
		log.Log.Info("Checking for containers that have exited with non-zero code")
		for _, actualNamespace := range namespaces {
			ns, err := clientset.CoreV1().Namespaces().Get(ctx, actualNamespace, metav1.GetOptions{})
			if err != nil || ns.Name != actualNamespace {
				return ctrl.Result{}, fmt.Errorf("%w, namespace: %s, reason: %v", errGetNamespace, actualNamespace, err)
			}
			if len(containerStatus.AffectedPods) > 0 {
				for _, p := range containerStatus.AffectedPods {
					po := strings.SplitN(p, ":ns:", 2)
					fmt.Println("pods name:", po[0])
					_, err := clientset.CoreV1().Pods(actualNamespace).Get(context.Background(), po[0], metav1.GetOptions{})
					if err != nil {
						if slices.Contains(containerStatus.AffectedPods, po[0]+":ns:"+actualNamespace) {
							idx := slices.Index(containerStatus.AffectedPods, po[0]+":ns:"+actualNamespace)
							deleteElementSlice(containerStatus.AffectedPods, idx)
						}
						if containerSpec.AggregateAlerts != nil && *containerSpec.AggregateAlerts {
							os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", "pod", po[0], actualNamespace))
							os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", "pod", po[0], actualNamespace))
						} else {
							files, err := os.ReadDir("/home/golanguser")
							if err != nil {
								log.Log.Info("Unable to read the directory /")
							}
							for _, file := range files {
								if strings.Contains(file.Name(), po[0]) {
									os.Remove(file.Name())
								}
							}
						}
					}
				}
			}
			pods, err := clientset.CoreV1().Pods(actualNamespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("unable to retrieve the pods in the namespace %s %s", actualNamespace, err)
			}
			for _, pod := range pods.Items {
				host := pod.Spec.NodeName
				for _, container := range pod.Status.ContainerStatuses {
					if container.State.Terminated != nil {
						if container.State.Terminated.ExitCode != 0 {
							if !slices.Contains(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace) {
								containerStatus.AffectedPods = append(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace)
							}
							if containerSpec.AggregateAlerts != nil && *containerSpec.AggregateAlerts {
								afpods = append(afpods, container.Name)
								if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
									util.SendEmailAlert(pod.Name, "cont", containerSpec, fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", "pod", pod.Name, actualNamespace), host)
								}
								if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal {
									err := util.NotifyExternalSystem(data, "firing", containerSpec.ExternalURL, username, password, pod.Name, "cont", containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", "pod", pod.Name, actualNamespace))
									if err != nil {
										log.Log.Info("Failed to notify the external system for pod %s", pod.Name)
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", "pod", pod.Name))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(containerStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										containerStatus.IncidentID = append(containerStatus.IncidentID, incident)
									}
									now := metav1.Now()
									containerStatus.ExternalNotifiedTime = &now
									containerStatus.ExternalNotified = true
								}
							} else {
								afcontainers = append(afcontainers, container.Name)
								if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
									util.SendEmailAlert(pod.Name, container.Name, containerSpec, fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", container.Name, pod.Name, actualNamespace), host)
								}
								if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal {
									err := util.NotifyExternalSystem(data, "firing", containerSpec.ExternalURL, username, password, pod.Name, container.Name, containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
									if err != nil {
										log.Log.Info("Failed to notify the external system for pod %s and container %s", pod.Name, container.Name)
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
									fmt.Println(fingerprint)
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(containerStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										containerStatus.IncidentID = append(containerStatus.IncidentID, incident)
									}
									now := metav1.Now()
									containerStatus.ExternalNotifiedTime = &now
									containerStatus.ExternalNotified = true
								}
							}
						}
					} else if container.State.Waiting != nil {
						if container.State.Waiting.Reason == "CrashLoopBackOff" {

							if !slices.Contains(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace) {
								containerStatus.AffectedPods = append(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace)
							}
							if containerSpec.AggregateAlerts != nil && *containerSpec.AggregateAlerts {
								afpods = append(afpods, container.Name)
								if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
									util.SendEmailAlert(pod.Name, "cont", containerSpec, fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", "pod", pod.Name, actualNamespace), host)
								}
								if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal {
									err := util.NotifyExternalSystem(data, "firing", containerSpec.ExternalURL, username, password, pod.Name, "cont", containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", "pod", pod.Name, actualNamespace))
									if err != nil {
										log.Log.Info("Failed to notify the external system for pod %s", pod.Name)
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", "pod", pod.Name))
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(containerStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										containerStatus.IncidentID = append(containerStatus.IncidentID, incident)
									}
									now := metav1.Now()
									containerStatus.ExternalNotifiedTime = &now
									containerStatus.ExternalNotified = true
								}
							} else {
								afcontainers = append(afcontainers, container.Name)
								if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
									util.SendEmailAlert(pod.Name, container.Name, containerSpec, fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", container.Name, pod.Name, actualNamespace), host)
								}
								if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal {
									err := util.NotifyExternalSystem(data, "firing", containerSpec.ExternalURL, username, password, pod.Name, container.Name, containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
									if err != nil {
										log.Log.Info("Failed to notify the external system for pod %s and container %s", pod.Name, container.Name)
									}
									fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
									fmt.Println(fingerprint)
									if err != nil {
										log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
									}
									incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
									if err != nil || incident == "" {
										log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
									}
									if !slices.Contains(containerStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
										containerStatus.IncidentID = append(containerStatus.IncidentID, incident)
									}
									now := metav1.Now()
									containerStatus.ExternalNotifiedTime = &now
									containerStatus.ExternalNotified = true
								}
							}
						}
					}
				}
			}
			if containerSpec.AggregateAlerts != nil && *containerSpec.AggregateAlerts {
				if len(afpods) > 0 {
					return ctrl.Result{}, fmt.Errorf("containers with non-zero exit code found in namespace %s", actualNamespace)
				} else {
					now := metav1.Now()
					containerStatus.LastRunTime = &now
					containerStatus.ExternalNotified = false
					afcontainers = nil
					err := remoteFiles(*clientset, actualNamespace, containerSpec)
					if err != nil {
						log.Log.Error(err, "unable to retrieve the pods")
					}
					report(monitoringv1alpha1.ConditionTrue, fmt.Sprintf("Success. All containers in the target namespace %s have running/zero terminated state", actualNamespace), nil)
				}
			} else {
				if len(afcontainers) > 0 {
					return ctrl.Result{}, fmt.Errorf("containers with non-zero exit code found in namespace %s", actualNamespace)
				} else {
					now := metav1.Now()
					containerStatus.LastRunTime = &now
					containerStatus.ExternalNotified = false
					afcontainers = nil
					err := remoteFiles(*clientset, actualNamespace, containerSpec)
					if err != nil {
						log.Log.Error(err, "unable to retrieve the pods")
					}
					report(monitoringv1alpha1.ConditionTrue, fmt.Sprintf("Success. All containers in the target namespace %s have running/zero terminated state", actualNamespace), nil)
				}
			}
		}

	} else {
		var affcontainers []string
		var affpods []string
		pastTime := time.Now().Add(-1 * defaultHealthCheckInterval)
		timeDiff := containerStatus.LastRunTime.Time.Before(pastTime)
		if timeDiff {
			log.Log.Info("Checking for containers that have exited with non-zero code as the time elapsed")
			for _, actualNamespace := range namespaces {
				ns, err := clientset.CoreV1().Namespaces().Get(ctx, actualNamespace, metav1.GetOptions{})
				if err != nil || ns.Name != actualNamespace {
					return ctrl.Result{}, fmt.Errorf("%w, namespace: %s, reason: %v", errGetNamespace, actualNamespace, err)
				}
				if len(containerStatus.AffectedPods) > 0 {
					for _, p := range containerStatus.AffectedPods {
						po := strings.SplitN(p, ":ns:", 2)
						_, err := clientset.CoreV1().Pods(actualNamespace).Get(context.Background(), po[0], metav1.GetOptions{})
						if err != nil {
							if slices.Contains(containerStatus.AffectedPods, po[0]+":ns:"+actualNamespace) {
								idx := slices.Index(containerStatus.AffectedPods, po[0]+":ns:"+actualNamespace)
								deleteElementSlice(containerStatus.AffectedPods, idx)
							}
							if containerSpec.AggregateAlerts != nil && *containerSpec.AggregateAlerts {
								os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", "pod", po[0], actualNamespace))
								os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", "pod", po[0], actualNamespace))
							} else {
								files, err := os.ReadDir("/home/golanguser")
								if err != nil {
									log.Log.Info("Unable to read the directory /")
								}
								for _, file := range files {
									if strings.Contains(file.Name(), po[0]) {
										os.Remove(file.Name())
									}
								}
							}
						}
					}
				}
				pods, err := clientset.CoreV1().Pods(actualNamespace).List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("unable to retrieve the pods in the namespace %s %s", actualNamespace, err)
				}
				for _, pod := range pods.Items {
					host := pod.Spec.NodeName
					for _, container := range pod.Status.ContainerStatuses {
						if container.State.Terminated != nil {
							if container.State.Terminated.ExitCode != 0 {

								if !slices.Contains(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace) {
									containerStatus.AffectedPods = append(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace)
								}
								if containerSpec.AggregateAlerts != nil && *containerSpec.AggregateAlerts {
									affpods = append(affpods, pod.Name)
									if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
										util.SendEmailAlert(pod.Name, "cont", containerSpec, fmt.Sprintf("/home/golanguser/%s-%s.txt", "pod", pod.Name), host)
									}
									if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal {
										err := util.SubNotifyExternalSystem(data, "firing", containerSpec.ExternalURL, username, password, pod.Name, "cont", containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", "pod", pod.Name, actualNamespace))
										if err != nil {
											log.Log.Info("Failed to notify the external system for pod %s", pod.Name)
										}
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", "pod", pod.Name))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if !slices.Contains(containerStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											containerStatus.IncidentID = append(containerStatus.IncidentID, incident)
										}
										containerStatus.ExternalNotified = true
										now := metav1.Now()
										containerStatus.ExternalNotifiedTime = &now
									}
								} else {
									affcontainers = append(affcontainers, container.Name)
									if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
										util.SendEmailAlert(pod.Name, container.Name, containerSpec, fmt.Sprintf("/home/golanguser/%s-%s.txt", container.Name, pod.Name), host)
									}
									if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal {
										err := util.SubNotifyExternalSystem(data, "firing", containerSpec.ExternalURL, username, password, pod.Name, container.Name, containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
										if err != nil {
											log.Log.Info("Failed to notify the external system for pod %s and container %s", pod.Name, container.Name)
										}
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", container.Name, pod.Name))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}

										incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to get the incident ID, either incident is getting created or other issues.")
										}
										if !slices.Contains(containerStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											containerStatus.IncidentID = append(containerStatus.IncidentID, incident)
										}
										containerStatus.ExternalNotified = true
										now := metav1.Now()
										containerStatus.ExternalNotifiedTime = &now
									}
								}
							} else {
								if slices.Contains(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace) {
									idx := slices.Index(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace)
									deleteElementSlice(containerStatus.AffectedPods, idx)
								}
								if containerSpec.AggregateAlerts != nil && *containerSpec.AggregateAlerts {
									if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
										util.SendEmailRecoverAlert(pod.Name, "cont", containerSpec, fmt.Sprintf("/home/golanguser/%s-%s.txt", container.Name, pod.Name), host)
									}
									if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal && containerStatus.ExternalNotified {
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", "pod", pod.Name))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if slices.Contains(containerStatus.IncidentID, incident) {
											idx := slices.Index(containerStatus.IncidentID, incident)
											deleteElementSlice(containerStatus.IncidentID, idx)
										}
										err = util.SubNotifyExternalSystem(data, "resolved", containerSpec.ExternalURL, username, password, pod.Name, container.Name, containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
										if err != nil {
											log.Log.Info("Failed to notify the external system for pod %s", pod.Name)
										}
										now := metav1.Now()
										containerStatus.ExternalNotifiedTime = &now

									}
								} else {
									if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
										util.SendEmailRecoverAlert(pod.Name, container.Name, containerSpec, fmt.Sprintf("/home/golanguser/%s-%s.txt", container.Name, pod.Name), host)
									}
									if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal && containerStatus.ExternalNotified {
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if slices.Contains(containerStatus.IncidentID, incident) {
											idx := slices.Index(containerStatus.IncidentID, incident)
											deleteElementSlice(containerStatus.IncidentID, idx)
										}
										err = util.SubNotifyExternalSystem(data, "resolved", containerSpec.ExternalURL, username, password, pod.Name, container.Name, containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
										if err != nil {
											log.Log.Info("Failed to notify the external system for pod %s and container %s", pod.Name, container.Name)
										}
										now := metav1.Now()
										containerStatus.ExternalNotifiedTime = &now

									}
								}

							}
						} else if container.State.Waiting != nil {
							if container.State.Waiting.Reason == "CrashLoopBackOff" {
								host := pod.Spec.NodeName
								if !slices.Contains(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace) {
									containerStatus.AffectedPods = append(containerStatus.AffectedPods, pod.Name+":ns:"+actualNamespace)
								}
								if containerSpec.AggregateAlerts != nil && *containerSpec.AggregateAlerts {
									affpods = append(affpods, container.Name)
									if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
										util.SendEmailAlert(pod.Name, "cont", containerSpec, fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", "pod", pod.Name, actualNamespace), host)
									}
									if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal {
										err := util.NotifyExternalSystem(data, "firing", containerSpec.ExternalURL, username, password, pod.Name, "cont", containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", "pod", pod.Name, actualNamespace))
										if err != nil {
											log.Log.Info("Failed to notify the external system for pod %s", pod.Name)
										}
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", "pod", pod.Name))
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if !slices.Contains(containerStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											containerStatus.IncidentID = append(containerStatus.IncidentID, incident)
										}
										containerStatus.ExternalNotified = true
										now := metav1.Now()
										containerStatus.ExternalNotifiedTime = &now
									}
								} else {
									affcontainers = append(affcontainers, container.Name)
									if containerSpec.SuspendEmailAlert != nil && !*containerSpec.SuspendEmailAlert {
										util.SendEmailAlert(pod.Name, container.Name, containerSpec, fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", container.Name, pod.Name, actualNamespace), host)
									}
									if containerSpec.NotifyExtenal != nil && *containerSpec.NotifyExtenal {
										err := util.NotifyExternalSystem(data, "firing", containerSpec.ExternalURL, username, password, pod.Name, container.Name, containerStatus, fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
										if err != nil {
											log.Log.Info("Failed to notify the external system for pod %s and container %s", pod.Name, container.Name)
										}
										fingerprint, err := util.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, actualNamespace))
										fmt.Println(fingerprint)
										if err != nil {
											log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
										}
										incident, err := util.SetIncidentID(containerSpec, containerStatus, username, password, fingerprint)
										if err != nil || incident == "" {
											log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
										}
										if !slices.Contains(containerStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
											containerStatus.IncidentID = append(containerStatus.IncidentID, incident)
										}
										containerStatus.ExternalNotified = true
										now := metav1.Now()
										containerStatus.ExternalNotifiedTime = &now
									}
								}
							}
						}

					}
				}
				if containerSpec.AggregateAlerts != nil && *containerSpec.AggregateAlerts {
					if len(affpods) > 0 {
						return ctrl.Result{}, fmt.Errorf("containers with non-zero exit code found in namespace %s", actualNamespace)
					} else {
						now := metav1.Now()
						containerStatus.LastRunTime = &now
						containerStatus.ExternalNotified = false
						affcontainers = nil
						err := remoteFiles(*clientset, actualNamespace, containerSpec)
						if err != nil {
							log.Log.Error(err, "unable to retrieve the pods")
						}
						report(monitoringv1alpha1.ConditionTrue, fmt.Sprintf("Success. All containers in the target namespace %s have running/zero terminated state", actualNamespace), nil)
					}
				} else {
					if len(affcontainers) > 0 {
						return ctrl.Result{}, fmt.Errorf("containers with non-zero exit code found in namespace %s", actualNamespace)
					}
					now := metav1.Now()
					containerStatus.LastRunTime = &now
					containerStatus.ExternalNotified = false
					err = remoteFiles(*clientset, actualNamespace, containerSpec)
					if err != nil {
						log.Log.Error(err, "unable to retrieve the pods")
					}
					report(monitoringv1alpha1.ConditionTrue, fmt.Sprintf("Success. All containers in the target namespace %s have running/zero terminated state", actualNamespace), nil)
				}
			}
		}
	}

	return ctrl.Result{RequeueAfter: defaultHealthCheckInterval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ContainerScanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor(monitoringv1alpha1.EventSource)
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.ContainerScan{}).
		Complete(r)
}

func remoteFiles(clientset kubernetes.Clientset, namespace string, spec *monitoringv1alpha1.ContainerScanSpec) error {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if *spec.AggregateAlerts {
				os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", "pod", pod.Name, namespace))
				os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", "pod", pod.Name, namespace))
			} else {
				os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-%s-ext.txt", container.Name, pod.Name, namespace))
				os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-%s.txt", container.Name, pod.Name, namespace))
			}

		}
	}
	return nil
}

func deleteElementSlice(slice []string, index int) []string {
	return append(slice[:index], slice[index+1:]...)
}
