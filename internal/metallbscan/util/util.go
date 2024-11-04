package util

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/barani129/monitoring-wo-webhooks/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RemResponse struct {
	ID                  int    `json:"id"`
	ServiceType         string `json:"ServiceType"`
	Summary             string `json:"Summary"`
	Acknowledged        int    `json:"Acknowledged"`
	Type                int    `json:"Type"`
	Location            string `json:"Location"`
	Node                string `json:"Node"`
	Note                string `json:"Note"`
	Severity            int    `json:"Severity"`
	Agent               string `json:"Agent"`
	AlertGroup          string `json:"AlertGroup"`
	NodeAlias           string `json:"NodeAlias"`
	Manager             string `json:"Manager"`
	EquipRole           string `json:"EquipRole"`
	Tally               int    `json:"Tally"`
	X733SpecificProb    string `json:"X733SpecificProb"`
	Oseventid           string `json:"OSEVENTID"`
	EquipType           string `json:"EquipType"`
	LastOccurrence      string `json:"LastOccurrence"`
	AlertKey            string `json:"AlertKey"`
	SourceServerName    string `json:"SourceServerName"`
	SuppressEscl        int    `json:"SuppressEscl"`
	CorrelationID       string `json:"CorrelationID"`
	Serial              string `json:"Serial"`
	Identifier          string `json:"Identifier"`
	Class               int    `json:"Class"`
	StateChange         string `json:"StateChange"`
	FirstOccurrence     string `json:"FirstOccurrence"`
	Grade               int    `json:"Grade"`
	Flash               int    `json:"Flash"`
	EventID             string `json:"EventId"`
	ExpireTime          int    `json:"ExpireTime"`
	Customer            string `json:"Customer"`
	NmosDomainName      string `json:"NmosDomainName"`
	X733EventType       int    `json:"X733EventType"`
	X733ProbableCause   string `json:"X733ProbableCause"`
	ServerName          string `json:"ServerName"`
	ServerSerial        int    `json:"ServerSerial"`
	ExtendedAttr        string `json:"ExtendedAttr"`
	OldRow              int    `json:"OldRow"`
	ProbeSubSecondID    int    `json:"ProbeSubSecondId"`
	CollectionFirst     any    `json:"CollectionFirst"`
	AggregationFirst    any    `json:"AggregationFirst"`
	DisplayFirst        any    `json:"DisplayFirst"`
	LocalObjRelate      int    `json:"LocalObjRelate"`
	RemoteTertObj       string `json:"RemoteTertObj"`
	RemoteObjRelate     int    `json:"RemoteObjRelate"`
	CorrScore           int    `json:"CorrScore"`
	CauseType           int    `json:"CauseType"`
	AdvCorrCauseType    int    `json:"AdvCorrCauseType"`
	AdvCorrServerName   string `json:"AdvCorrServerName"`
	AdvCorrServerSerial int    `json:"AdvCorrServerSerial"`
	TTNumber            string `json:"TTNumber"`
	TicketState         string `json:"TicketState"`
	JournalSent         int    `json:"JournalSent"`
	ProbeSerial         string `json:"ProbeSerial"`
	AdditionalText      string `json:"AdditionalText"`
	AlarmID             string `json:"AlarmID"`
	OriginalSeverity    int    `json:"OriginalSeverity"`
	SentToJDBC          int    `json:"SentToJDBC"`
	Service             string `json:"Service"`
	URL                 string `json:"url"`
	AutomationState     any    `json:"automationState"`
	Cleared             any    `json:"cleared"`
	DedupeColumns       any    `json:"dedupeColumns"`
	SuppressAggregation bool   `json:"suppressAggregation"`
	QueueMessageKey     any    `json:"queueMessageKey"`
	Aggregationaction   any    `json:"aggregationaction"`
	Correlationobject   any    `json:"correlationobject"`
	CorrelationNode     string `json:"correlationNode"`
	BaseNode            string `json:"baseNode"`
	Enrichment          any    `json:"Enrichment"`
}

type OcpAPIConfig struct {
	APIServerArguments struct {
		AuditLogFormat         []string `json:"audit-log-format"`
		AuditLogMaxbackup      []string `json:"audit-log-maxbackup"`
		AuditLogMaxsize        []string `json:"audit-log-maxsize"`
		AuditLogPath           []string `json:"audit-log-path"`
		AuditPolicyFile        []string `json:"audit-policy-file"`
		EtcdHealthcheckTimeout []string `json:"etcd-healthcheck-timeout"`
		EtcdReadycheckTimeout  []string `json:"etcd-readycheck-timeout"`
		FeatureGates           []string `json:"feature-gates"`
		ShutdownDelayDuration  []string `json:"shutdown-delay-duration"`
		ShutdownSendRetryAfter []string `json:"shutdown-send-retry-after"`
	} `json:"apiServerArguments"`
	APIServers struct {
		PerGroupOptions []any `json:"perGroupOptions"`
	} `json:"apiServers"`
	APIVersion    string `json:"apiVersion"`
	Kind          string `json:"kind"`
	ProjectConfig struct {
		ProjectRequestMessage string `json:"projectRequestMessage"`
	} `json:"projectConfig"`
	RoutingConfig struct {
		Subdomain string `json:"subdomain"`
	} `json:"routingConfig"`
	ServingInfo struct {
		BindNetwork   string   `json:"bindNetwork"`
		CipherSuites  []string `json:"cipherSuites"`
		MinTLSVersion string   `json:"minTLSVersion"`
	} `json:"servingInfo"`
	StorageConfig struct {
		Urls []string `json:"urls"`
	} `json:"storageConfig"`
}

func GetSpecAndStatus(metallbscan client.Object) (*v1alpha1.MetallbScanSpec, *v1alpha1.MetallbScanStatus, error) {
	switch t := metallbscan.(type) {
	case *v1alpha1.MetallbScan:
		return &t.Spec, &t.Status, nil
	default:
		return nil, nil, fmt.Errorf("not an metallbscan type: %t", t)
	}
}

func GetReadyCondition(status *v1alpha1.MetallbScanStatus) *v1alpha1.MetallbScanCondition {
	for _, c := range status.Conditions {
		if c.Type == v1alpha1.MetallbScanConditionReady {
			return &c
		}
	}
	return nil
}

func IsReady(status *v1alpha1.MetallbScanStatus) bool {
	if c := GetReadyCondition(status); c != nil {
		return c.Status == v1alpha1.ConditionTrue
	}
	return false
}

func SetReadyCondition(status *v1alpha1.MetallbScanStatus, conditionStatus v1alpha1.ConditionStatus, reason, message string) {
	ready := GetReadyCondition(status)
	if ready == nil {
		ready = &v1alpha1.MetallbScanCondition{
			Type: v1alpha1.MetallbScanConditionReady,
		}
		status.Conditions = append(status.Conditions, *ready)
	}
	if ready.Status != conditionStatus {
		ready.Status = conditionStatus
		now := metav1.Now()
		ready.LastTransitionTime = &now
	}
	ready.Reason = reason
	ready.Message = message

	for i, c := range status.Conditions {
		if c.Type == v1alpha1.MetallbScanConditionReady {
			status.Conditions[i] = *ready
			return
		}
	}
}

func randomString(length int) string {
	b := make([]byte, length+2)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[2 : length+2]
}

func HandleCNString(cn string) string {
	var nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9 ]+`)
	return nonAlphanumericRegex.ReplaceAllString(cn, "")
}

func SetIncidentID(spec *v1alpha1.MetallbScanSpec, username string, password string, fingerprint string) (string, error) {
	url := spec.ExternalURL
	nurl := strings.SplitAfter(url, "co.nz")
	getUrl := nurl[0] + "/rem/api/event/v1/query"
	var client *http.Client
	if strings.Contains(getUrl, "https://") {
		tr := http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{
			Timeout:   5 * time.Second,
			Transport: &tr,
		}
	}
	client = &http.Client{
		Timeout: 5 * time.Second,
	}
	req, err := http.NewRequest("GET", getUrl, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "Basic "+basicAuth(username, password))
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Add("alertKey", fingerprint)
	q.Add("maxalarms", "1")
	req.URL.RawQuery = q.Encode()
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	dat, err := io.ReadAll(resp.Body)
	sdata := string(dat)
	s1 := strings.TrimPrefix(sdata, "[")
	s2 := strings.TrimSuffix(s1, "]")
	if err != nil {
		return "", err
	}
	var x RemResponse
	err = json.Unmarshal([]byte(s2), &x)
	if err != nil {
		return "", err
	}
	return x.TTNumber, nil
}

func SubNotifyExternalSystem(data map[string]string, status string, url string, username string, password string, filename string, alertName string) error {
	var fingerprint string
	var err error
	if status == "resolved" {
		fingerprint, err = ReadFile(filename)
		if err != nil || fingerprint == "" {
			return fmt.Errorf("unable to notify the system for the %s status due to missing fingerprint in the file %s", status, filename)
		}
	} else {
		fingerprint, _ = ReadFile(filename)
		if fingerprint != "" {
			return nil
		}
		fingerprint = randomString(10)
	}
	data["fingerprint"] = fingerprint
	data["status"] = status
	data["startsAt"] = time.Now().String()
	data["alertName"] = alertName
	data["message"] = alertName
	m, b := data, new(bytes.Buffer)
	json.NewEncoder(b).Encode(m)
	var client *http.Client
	if strings.Contains(url, "https://") {
		tr := http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{
			Timeout:   5 * time.Second,
			Transport: &tr,
		}
	}
	client = &http.Client{
		Timeout: 5 * time.Second,
	}
	req, err := http.NewRequest("POST", url, b)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", "Basic "+basicAuth(username, password))
	req.Header.Set("User-Agent", "Openshift")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp == nil {
		return err
	}
	writeFile(filename, fingerprint)
	return nil
}

func NotifyExternalSystem(data map[string]string, status string, url string, username string, password string, filename string, alertName string) error {
	fig, _ := ReadFile(filename)
	if fig != "" {
		log.Println("External system has already been notified for target %s . Exiting")
		return nil
	}
	fingerprint := randomString(10)
	data["fingerprint"] = fingerprint
	data["status"] = status
	data["startsAt"] = time.Now().String()
	data["alertName"] = alertName
	data["message"] = alertName
	m, b := data, new(bytes.Buffer)
	json.NewEncoder(b).Encode(m)
	var client *http.Client
	if strings.Contains(url, "https://") {
		tr := http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{
			Timeout:   5 * time.Second,
			Transport: &tr,
		}
	}
	client = &http.Client{
		Timeout: 5 * time.Second,
	}
	req, err := http.NewRequest("POST", url, b)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", "Basic "+basicAuth(username, password))
	req.Header.Set("User-Agent", "Openshift")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp == nil {
		return err
	}
	writeFile(filename, fingerprint)
	return nil
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func writeFile(filename string, data string) error {
	err := os.WriteFile(filename, []byte(data), 0666)
	if err != nil {
		return err
	}
	return nil
}

func ReadFile(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func SendEmailAlert(nodeName string, filename string, spec *v1alpha1.MetallbScanSpec, alert string) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		message := fmt.Sprintf(`/usr/bin/printf '%s\n' "Subject: Alert from %s" "" "Alert: %s" | /usr/sbin/sendmail -f %s -S %s %s`, "%s", nodeName, alert, spec.Email, spec.RelayHost, spec.Email)
		cmd3 := exec.Command("/bin/bash", "-c", message)
		err := cmd3.Run()
		if err != nil {
			fmt.Printf("Failed to send the alert: %s", err)
		}
		writeFile(filename, "sent")
	} else {
		data, _ := ReadFile(filename)
		if data != "sent" {
			message := fmt.Sprintf(`/usr/bin/printf '%s\n' "Subject: Alert from %s" "" "Alert: %s" | /usr/sbin/sendmail -f %s -S %s %s`, "%s", nodeName, alert, spec.Email, spec.RelayHost, spec.Email)
			cmd3 := exec.Command("/bin/bash", "-c", message)
			err := cmd3.Run()
			if err != nil {
				fmt.Printf("Failed to send the alert: %s", err)
			}
			os.Truncate(filename, 0)
			writeFile(filename, "sent")
		}
	}
}

func GetAPIName(clientset kubernetes.Clientset) (domain string, err error) {
	var apiconfig OcpAPIConfig
	cm, err := clientset.CoreV1().ConfigMaps("openshift-apiserver").Get(context.Background(), "config", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	data := cm.Data["config.yaml"]
	err = json.Unmarshal([]byte(data), &apiconfig)
	if err != nil {
		return "", err
	}
	return apiconfig.RoutingConfig.Subdomain, nil
}

func GetLoadBalancerSevices(clientset kubernetes.Clientset) ([]string, []string, error) {
	svcList, err := clientset.CoreV1().Services("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	var loadbalancersvc []string
	var loadbalancersvcnoip []string
	for _, svc := range svcList.Items {
		if svc.Spec.Type == "LoadBalancer" {
			if svc.Spec.LoadBalancerIP != "" {
				loadbalancersvc = append(loadbalancersvc, svc.Name+":"+svc.Namespace+":"+svc.Spec.LoadBalancerIP)
			} else if svc.Status.LoadBalancer.Ingress != nil {
				for _, ip := range svc.Status.LoadBalancer.Ingress {
					if ip.IP != "" {
						loadbalancersvc = append(loadbalancersvc, svc.Name+":"+svc.Namespace+":"+ip.IP)
					}
				}
			} else {
				loadbalancersvcnoip = append(loadbalancersvcnoip, svc.Name+":"+svc.Namespace)
			}
		}
	}
	return loadbalancersvc, loadbalancersvcnoip, nil
}

func GetpodsFromNodeBasedonLabels(clientset kubernetes.Clientset, nodeName string, namespace string, labelSelector metav1.LabelSelector) (int, []string, error) {
	var pods []string
	fieldSelector := fmt.Sprintf(`spec.nodeName=%s`, nodeName)
	podList, err := clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return 0, nil, err
	}
	for _, pod := range podList.Items {
		if pod.Status.Phase == "Running" {
			pods = append(pods, pod.Name)
		}
	}
	return len(pods), pods, nil
}

func RetrieveBGPHop(outFile *os.File) ([]string, error) {
	var ipRE = regexp.MustCompile(`[0-9][0-9][0-9].[0-9][0-9][0-9].[0-9][0-9][0-9].[0-9][0-9][0-9]`)
	var ip2RE = regexp.MustCompile(`[0-9][0-9].[0-9][0-9].[0-9][0-9].[0-9][0-9]`)
	var ip3RE = regexp.MustCompile(`[0-9][0-9].[0-9][0-9].[0-9][0-9][0-9].[0-9][0-9][0-9]`)
	var ip4RE = regexp.MustCompile(`[0-9][0-9].[0-9][0-9].[0-9][0-9][0-9].[0-9][0-9]`)
	var ip5RE = regexp.MustCompile(`[0-9][0-9].[0-9][0-9][0-9].[0-9][0-9][0-9].[0-9][0-9][0-9]`)
	var ip6RE = regexp.MustCompile(`[0-9][0-9][0-9].[0-9][0-9].[0-9][0-9].[0-9][0-9]`)
	var ip7RE = regexp.MustCompile(`[0-9][0-9][0-9].[0-9][0-9][0-9].[0-9][0-9].[0-9][0-9][0-9]`)
	var ip8RE = regexp.MustCompile(`[0-9][0-9][0-9].[0-9][0-9][0-9].[0-9][0-9][0-9].[0-9][0-9]`)
	var ip9RE = regexp.MustCompile(`[0-9][0-9].[0-9][0-9][0-9].[0-9][0-9].[0-9][0-9]`)
	var ip10RE = regexp.MustCompile(`[0-9][0-9].[0-9][0-9][0-9].[0-9][0-9].[0-9]`)
	var ip11RE = regexp.MustCompile(`[0-9][0-9].[0-9][0-9].[0-9][0-9][0-9].[0-9]`)
	var ip12RE = regexp.MustCompile(`[0-9][0-9].[0-9][0-9][0-9].[0-9][0-9].[0-9]`)
	var ip13RE = regexp.MustCompile(`[0-9][0-9].[0-9][0-9][0-9].[0-9][0-9][0-9].[0-9]`)
	var ip14RE = regexp.MustCompile(`[0-9][0-9][0-9].[0-9][0-9][0-9].[0-9][0-9][0-9].[0-9]`)
	var hops []string
	// s := bufio.NewScanner(outFile)
	content, err := os.ReadFile(outFile.Name())
	if err != nil {
		return nil, err
	}
	contents := strings.Split(string(content), "\n")
	for _, line := range contents {
		line = strings.Trim(line, " ")
		if ipRE.Match([]byte(line)) || ip2RE.Match([]byte(line)) || ip3RE.Match([]byte(line)) || ip4RE.Match([]byte(line)) || ip5RE.Match([]byte(line)) || ip6RE.Match([]byte(line)) || ip7RE.Match([]byte(line)) || ip8RE.Match([]byte(line)) || ip9RE.Match([]byte(line)) || ip10RE.Match([]byte(line)) || ip11RE.Match([]byte(line)) || ip12RE.Match([]byte(line)) || ip13RE.Match([]byte(line)) || ip14RE.Match([]byte(line)) {
			lines := strings.Split(line, " ")
			if ipRE.Match([]byte(lines[0])) || ip2RE.Match([]byte(lines[0])) || ip3RE.Match([]byte(lines[0])) || ip4RE.Match([]byte(lines[0])) || ip5RE.Match([]byte(lines[0])) || ip6RE.Match([]byte(lines[0])) || ip7RE.Match([]byte(lines[0])) || ip8RE.Match([]byte(lines[0])) || ip9RE.Match([]byte(lines[0])) || ip10RE.Match([]byte(lines[0])) || ip11RE.Match([]byte(lines[0])) || ip12RE.Match([]byte(lines[0])) || ip13RE.Match([]byte(lines[0])) || ip14RE.Match([]byte(lines[0])) {
				hops = append(hops, lines[0]+":"+lines[1])
			}
		}
	}
	os.Remove(outFile.Name())
	return hops, nil
}

func CheckLBIPRoute(outFile *os.File, LBIP string) (bool, bool, string, error) {
	lbRouteIP := LBIP + "/32"
	content, err := os.ReadFile(outFile.Name())
	if err != nil {
		return false, false, "", err
	}
	contents := strings.Split(string(content), "\n")
	os.Remove(outFile.Name())
	for _, line := range contents {
		if strings.Contains(line, lbRouteIP) {
			flds := strings.Split(line, " ")
			if flds[0] == "*>" && flds[1] == lbRouteIP {
				return true, true, flds[0], nil
			} else if flds[0] != "*>" && flds[1] == lbRouteIP {
				return true, false, flds[0], nil
			} else {
				return false, false, "", nil
			}
		}
	}

	return false, false, "", nil
}

func SendEmailRecoveredAlert(nodeName string, filename string, spec *v1alpha1.MetallbScanSpec, commandToRun string) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		//
	} else {
		data, err := ReadFile(filename)
		if err != nil {
			fmt.Printf("Failed to send the alert: %s", err)
		}
		if data == "sent" {
			message := fmt.Sprintf(`/usr/bin/printf '%s\n' "Subject: Alert from %s" ""  "Resolved: %s" | /usr/sbin/sendmail -f %s -S %s %s`, "%s", nodeName, commandToRun, spec.Email, spec.RelayHost, spec.Email)
			cmd3 := exec.Command("/bin/bash", "-c", message)
			err := cmd3.Run()
			if err != nil {
				fmt.Printf("Failed to send the alert: %s", err)
			}
		}
	}
}
