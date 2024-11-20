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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	monitoringv1alpha1 "github.com/barani129/monitoring-wo-webhooks/api/v1alpha1"
	vmUtil "github.com/barani129/monitoring-wo-webhooks/internal/vmscan/util"
	corev1 "k8s.io/api/core/v1"
	kubev1 "kubevirt.io/api/core/v1"
)

// VmScanReconciler reconciles a VmScan object
type VmScanReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	Kind                     string
	ClusterResourceNamespace string
	recorder                 record.EventRecorder
}

func (r *VmScanReconciler) newVmscan() (client.Object, error) {
	VmScanGVK := monitoringv1alpha1.GroupVersion.WithKind(r.Kind)
	ro, err := r.Scheme.New(VmScanGVK)
	if err != nil {
		return nil, err
	}
	return ro.(client.Object), nil
}

//+kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=vmscans,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=vmscans/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitoring.spark.co.nz,resources=vmscans/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VmScan object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.2/pkg/reconcile
func (r *VmScanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)

	vm, err := r.newVmscan()

	if err != nil {
		log.Log.Error(err, "unrecognized VM scan type")
		return ctrl.Result{}, err
	}

	if err := r.Get(ctx, req.NamespacedName, vm); err != nil {
		if err := client.IgnoreNotFound(err); err != nil {
			return ctrl.Result{}, fmt.Errorf("unexpected get error : %v", err)
		}
		log.Log.Info("vm scan is not found, ignoring")
		return ctrl.Result{}, nil
	}
	vmSpec, vmStatus, err := vmUtil.GetSpecAndStatus(vm)
	if err != nil {
		log.Log.Error(err, "unexpected error while getting vm scan spec and status, not trying.")
		return ctrl.Result{}, nil
	}

	secretName := types.NamespacedName{
		Name: vmSpec.ExternalSecret,
	}

	configmapName := types.NamespacedName{
		Name: vmSpec.ExternalData,
	}

	switch vm.(type) {
	case *monitoringv1alpha1.VmScan:
		secretName.Namespace = r.ClusterResourceNamespace
		configmapName.Namespace = r.ClusterResourceNamespace
	default:
		log.Log.Error(fmt.Errorf("unexpected issuer type: %s", vm), "not retrying")
		return ctrl.Result{}, nil
	}

	var secret corev1.Secret
	var configmap corev1.ConfigMap
	var username []byte
	var password []byte
	var data map[string]string
	if vmSpec.NotifyExtenal != nil && *vmSpec.NotifyExtenal {
		if err := r.Get(ctx, secretName, &secret); err != nil {
			return ctrl.Result{}, fmt.Errorf("%w, secret name: %s, reason: %v", errGetAuthSecret, secretName, err)
		}
		username = secret.Data["username"]
		password = secret.Data["password"]
	}

	if vmSpec.NotifyExtenal != nil && *vmSpec.NotifyExtenal {
		if err := r.Get(ctx, configmapName, &configmap); err != nil {
			return ctrl.Result{}, fmt.Errorf("%w, configmap name: %s, reason: %v", errGetAuthConfigMap, configmapName, err)
		}
		data = configmap.Data
	}

	report := func(conditionStatus monitoringv1alpha1.VmConditionStatus, message string, err error) {
		eventType := corev1.EventTypeNormal
		if err != nil {
			log.Log.Error(err, message)
			eventType = corev1.EventTypeWarning
			message = fmt.Sprintf("%s: %v", message, err)
		} else {
			log.Log.Info(message)
		}
		r.recorder.Event(vm, eventType, monitoringv1alpha1.PortEventReasonIssuerReconciler, message)
		vmUtil.SetNonViolationCondition(vmStatus, conditionStatus, monitoringv1alpha1.VmScanEventReasonIssuerReconciler, message)
	}

	defer func() {
		if err != nil {
			report(monitoringv1alpha1.ConditionViolated, "Trouble reaching the one of the target or all targets", err)
		}
		if updateErr := r.Status().Update(ctx, vm); updateErr != nil {
			err = utilerrors.NewAggregate([]error{err, updateErr})
			result = ctrl.Result{}
		}
	}()

	config, err := rest.InClusterConfig()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to retrieve in cluster configuration due to %s", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to retrieve in cluster configuration due to %s", err)
	}
	nodeList, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return ctrl.Result{}, err
	}
	vmiNodes := make(map[string][]string)
	var defaultHealthCheckIntervalVm time.Duration
	if vmSpec.CheckInterval != nil {
		defaultHealthCheckIntervalVm = time.Minute * time.Duration(*vmSpec.CheckInterval)
	} else {
		defaultHealthCheckIntervalVm = time.Minute * 30
	}
	if vmSpec.Suspend != nil && *vmSpec.Suspend {
		log.Log.Info("vm scan is suspended, skipping..")
		return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalVm}, nil
	}
	if vmStatus.LastPollTime == nil {
		log.Log.Info("triggering VMI migration check")
		for _, ns := range vmSpec.TargetNamespace {
			vmList := kubev1.VirtualMachineInstanceList{}
			err := clientset.RESTClient().Get().AbsPath(fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachineinstances", ns)).Do(context.Background()).Into(&vmList)
			if err != nil {
				log.Log.Info("unable to get the Virtual Machine Instance Migrations Lists in target namespace")
			}
			for _, vm := range vmList.Items {
				if vm.Name != "" && strings.Contains(vm.Name, "controller") {
					if vm.Status.MigrationState != nil {
						if !vm.Status.MigrationState.Completed {
							if !slices.Contains(vmStatus.Migrations, vm.Name+":"+ns) {
								vmStatus.Migrations = append(vmStatus.Migrations, vm.Name+":"+ns)
							}
						} else {
							if slices.Contains(vmStatus.Migrations, vm.Name+":"+ns) {
								idx := slices.Index(vmStatus.Migrations, vm.Name+":"+ns)
								vmStatus.Migrations = deleteElementSlice(vmStatus.Migrations, idx)
							}
						}
					}
				}
			}
		}
		var isAffected = false
		log.Log.Info("triggering VMI placement violation check")
		for _, ns := range vmSpec.TargetNamespace {
			result := kubev1.VirtualMachineInstanceList{}
			err := clientset.RESTClient().Get().AbsPath(fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachineinstances", ns)).Do(context.Background()).Into(&result)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("unable to get the Virtual Machine Instance Lists in target namespace %s", ns)
			}
			for _, vmi := range result.Items {
				if vmi.Name != "" && strings.Contains(vmi.Name, "controller") {
					vmiNodes[vmi.Status.NodeName] = append(vmiNodes[vmi.Status.NodeName], vmi.Name)
				}
			}
			for _, node := range nodeList.Items {
				vminodelist := vmiNodes[node.Name]
				if len(vminodelist) > 1 {
					isAffected = true
					for _, vmi := range vminodelist {
						if !slices.Contains(vmStatus.AffectedTargets, node.Name+":ns:"+vmi) {
							vmStatus.AffectedTargets = append(vmStatus.AffectedTargets, node.Name+":ns:"+vmi)
						}
					}
					if vmSpec.SuspendEmailAlert != nil && !*vmSpec.SuspendEmailAlert {
						vmUtil.SendEmailAlert(ns, node.Name, fmt.Sprintf("/home/golanguser/%s-%s.txt", ns, node.Name), vmSpec, node.Name)
					}
					if vmSpec.NotifyExtenal != nil && *vmSpec.NotifyExtenal && !vmStatus.ExternalNotified {
						err := vmUtil.NotifyExternalSystem(data, "firing", ns, node.Name, vmSpec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name), vmStatus)
						if err != nil {
							log.Log.Info(fmt.Sprintf("Failed to notify the external system with err %s", err.Error()))
						}
						now := metav1.Now()
						vmStatus.ExternalNotifiedTime = &now
						vmStatus.ExternalNotified = true
						fingerprint, err := vmUtil.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name))
						if err != nil {
							log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
						}
						incident, err := vmUtil.SetIncidentID(vmSpec, vmStatus, string(username), string(password), fingerprint)
						if err != nil || incident == "" {
							log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
						}
						if !slices.Contains(vmStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
							vmStatus.IncidentID = append(vmStatus.IncidentID, incident)
						}

					}
				} else {
					isAffected = false
					vmStatus.AffectedTargets = nil

					if _, err := os.Stat(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name)); os.IsNotExist(err) {
						// no action
					} else {

						if vmSpec.SuspendEmailAlert != nil && !*vmSpec.SuspendEmailAlert {
							vmUtil.SendEmailRecoveredAlert(ns, node.Name, fmt.Sprintf("/home/golanguser/%s-%s.txt", ns, node.Name), vmSpec, node.Name)
						}
						if vmSpec.NotifyExtenal != nil && *vmSpec.NotifyExtenal && vmStatus.ExternalNotified {
							fingerprint, err := vmUtil.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name))
							if err != nil {
								log.Log.Info("Failed to get the incident ID. Couldn't find the fingerprint in the file")
							}
							incident, err := vmUtil.SetIncidentID(vmSpec, vmStatus, string(username), string(password), fingerprint)
							if err != nil || incident == "" {
								log.Log.Info("Failed to get the incident ID, either incident is getting created or other issues.")
							}
							if slices.Contains(vmStatus.IncidentID, incident) {
								idx := slices.Index(vmStatus.IncidentID, incident)
								vmStatus.IncidentID = deleteElementSlice(vmStatus.IncidentID, idx)
							}
							err = vmUtil.SubNotifyExternalSystem(data, "resolved", ns, node.Name, vmSpec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/%s-%s.txt", ns, node.Name), vmStatus)
							if err != nil {
								log.Log.Info(fmt.Sprintf("Failed to notify the external system with err %s", err.Error()))
							}
							now := metav1.Now()
							vmStatus.ExternalNotifiedTime = &now
							os.Remove(fmt.Sprintf("/home/golanguser/%s-%s.txt", ns, node.Name))
							os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name))
						}
					}
				}
			}
		}
		if isAffected {
			log.Log.Info("one or more VMIs are running on the same node")
			return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalVm}, fmt.Errorf("one or more VMIs are running on the same node in target namespace")
		}
		now := metav1.Now()
		vmStatus.LastPollTime = &now
		vmStatus.ExternalNotified = false
		report(monitoringv1alpha1.ConditionNonViolated, "Success. No VMI placement violation found.", nil)
	} else {
		pastTime := time.Now().Add(-1 * defaultHealthCheckIntervalVm)
		timeDiff := vmStatus.LastPollTime.Time.Before(pastTime)
		if timeDiff {
			log.Log.Info("triggering VMI migration check")
			for _, ns := range vmSpec.TargetNamespace {
				vmList := kubev1.VirtualMachineInstanceList{}
				err := clientset.RESTClient().Get().AbsPath(fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachineinstances", ns)).Do(context.Background()).Into(&vmList)
				if err != nil {
					log.Log.Info("unable to get the Virtual Machine Instance Migrations Lists in target namespace")
				}
				for _, vm := range vmList.Items {
					if vm.Name != "" && strings.Contains(vm.Name, "controller") {
						if vm.Status.MigrationState != nil {
							if !vm.Status.MigrationState.Completed {
								if !slices.Contains(vmStatus.Migrations, vm.Name+":"+ns) {
									vmStatus.Migrations = append(vmStatus.Migrations, vm.Name+":"+ns)
								}
							} else {
								if slices.Contains(vmStatus.Migrations, vm.Name+":"+ns) {
									idx := slices.Index(vmStatus.Migrations, vm.Name+":"+ns)
									vmStatus.Migrations = deleteElementSlice(vmStatus.Migrations, idx)
								}
							}
						}
					}
				}
			}
			var isAffected = false
			log.Log.Info("triggering VMI placement violation check as the time elapsed")
			for _, ns := range vmSpec.TargetNamespace {
				result := kubev1.VirtualMachineInstanceList{}
				err := clientset.RESTClient().Get().AbsPath(fmt.Sprintf("/apis/kubevirt.io/v1/namespaces/%s/virtualmachineinstances", ns)).Do(context.Background()).Into(&result)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("unable to get the Virtual Machine Instance Lists in target namespace %s", ns)
				}
				for _, vmi := range result.Items {
					if vmi.Name != "" && strings.Contains(vmi.Name, "controller") {
						vmiNodes[vmi.Status.NodeName] = append(vmiNodes[vmi.Status.NodeName], vmi.Name)
					}
				}
				for _, node := range nodeList.Items {
					vminodelist := vmiNodes[node.Name]
					if len(vminodelist) > 1 {
						isAffected = true
						for _, vmi := range vminodelist {
							if !slices.Contains(vmStatus.AffectedTargets, node.Name+":ns:"+vmi) {
								vmStatus.AffectedTargets = append(vmStatus.AffectedTargets, node.Name+":ns:"+vmi)
							}
							if vmSpec.SuspendEmailAlert != nil && !*vmSpec.SuspendEmailAlert {
								vmUtil.SendEmailAlert(ns, node.Name, fmt.Sprintf("/home/golanguser/%s-%s.txt", ns, node.Name), vmSpec, node.Name)
							}
							if vmSpec.NotifyExtenal != nil && *vmSpec.NotifyExtenal && !vmStatus.ExternalNotified {
								err := vmUtil.SubNotifyExternalSystem(data, "firing", ns, node.Name, vmSpec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name), vmStatus)
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								fingerprint, err := vmUtil.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name))
								if err != nil {
									log.Log.Info("Failed to update the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := vmUtil.SetIncidentID(vmSpec, vmStatus, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to update the incident ID, either incident is getting created or other issues.")
								}
								if !slices.Contains(vmStatus.IncidentID, incident) && incident != "" && incident != "[Pending]" {
									vmStatus.IncidentID = append(vmStatus.IncidentID, incident)
								}
								now := metav1.Now()
								vmStatus.ExternalNotifiedTime = &now
								vmStatus.ExternalNotified = true
							}
						}

					} else {
						isAffected = false
						vmStatus.AffectedTargets = nil

						if _, err := os.Stat(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name)); os.IsNotExist(err) {
							// no action
						} else {
							if vmSpec.SuspendEmailAlert != nil && !*vmSpec.SuspendEmailAlert {
								vmUtil.SendEmailRecoveredAlert(ns, node.Name, fmt.Sprintf("/home/golanguser/%s-%s.txt", ns, node.Name), vmSpec, node.Name)
							}
							if vmSpec.NotifyExtenal != nil && *vmSpec.NotifyExtenal && vmStatus.ExternalNotified {
								fingerprint, err := vmUtil.ReadFile(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name))
								if err != nil {
									log.Log.Info("Failed to get the incident ID. Couldn't find the fingerprint in the file")
								}
								incident, err := vmUtil.SetIncidentID(vmSpec, vmStatus, string(username), string(password), fingerprint)
								if err != nil || incident == "" {
									log.Log.Info("Failed to get the incident ID, either incident is getting created or other issues.")
								}
								if slices.Contains(vmStatus.IncidentID, incident) {
									idx := slices.Index(vmStatus.IncidentID, incident)
									vmStatus.IncidentID = deleteElementSlice(vmStatus.IncidentID, idx)
								}
								err = vmUtil.SubNotifyExternalSystem(data, "resolved", ns, node.Name, vmSpec.ExternalURL, string(username), string(password), fmt.Sprintf("/home/golanguser/%s-%s.txt", ns, node.Name), vmStatus)
								if err != nil {
									log.Log.Error(err, "Failed to notify the external system")
								}
								now := metav1.Now()
								vmStatus.ExternalNotifiedTime = &now
								os.Remove(fmt.Sprintf("/home/golanguser/%s-%s.txt", ns, node.Name))
								os.Remove(fmt.Sprintf("/home/golanguser/%s-%s-ext.txt", ns, node.Name))
							}
						}
					}
				}
			}
			if isAffected {
				log.Log.Info("one or more VMIs are running on the same node, exiting and requeuing...")
				return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalVm}, fmt.Errorf("one or more VMIs are running on the same node in target namespace")
			}
			now := metav1.Now()
			vmStatus.LastPollTime = &now
			vmStatus.ExternalNotified = false
			report(monitoringv1alpha1.ConditionNonViolated, "Success. No VMI placement violation found.", nil)
		}
	}

	return ctrl.Result{RequeueAfter: defaultHealthCheckIntervalVm}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VmScanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor(monitoringv1alpha1.VmScanEventSource)
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringv1alpha1.VmScan{}).
		Complete(r)
}
