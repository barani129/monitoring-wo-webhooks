package portutil

import (
	"bytes"
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
	"strings"
	"time"

	"github.com/barani129/container-scan/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func GetSpecAndStatus(PortScan client.Object) (*v1alpha1.PortScanSpec, *v1alpha1.PortScanStatus, error) {
	switch t := PortScan.(type) {
	case *v1alpha1.PortScan:
		return &t.Spec, &t.Status, nil
	default:
		return nil, nil, fmt.Errorf("not a managed cluster type: %t", t)
	}
}

func GetReadyCondition(status *v1alpha1.PortScanStatus) *v1alpha1.PortScanCondition {
	for _, c := range status.Conditions {
		if c.Type == v1alpha1.PortScanConditionReady {
			return &c
		}
	}
	return nil
}

func IsReady(status *v1alpha1.PortScanStatus) bool {
	if c := GetReadyCondition(status); c != nil {
		return c.Status == v1alpha1.ConditionTrue
	}
	return false
}

func SetReadyCondition(status *v1alpha1.PortScanStatus, conditionStatus v1alpha1.ConditionStatus, reason, message string) {
	ready := GetReadyCondition(status)
	if ready == nil {
		ready = &v1alpha1.PortScanCondition{
			Type: v1alpha1.PortScanConditionReady,
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
		if c.Type == v1alpha1.PortScanConditionReady {
			status.Conditions[i] = *ready
			return
		}
	}
}

func CheckServerAliveness(target string, status *v1alpha1.PortScanStatus) error {
	targets := strings.SplitN(target, ":", 2)
	command := fmt.Sprintf("/usr/bin/nc -w 3 -zv %s %s", targets[0], targets[1])
	cmd := exec.Command("/bin/bash", "-c", command)
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("target %s is unreachable", targets[0])
	}
	return nil
}

func randomString(length int) string {
	b := make([]byte, length+2)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[2 : length+2]
}

func SetIncidentID(spec *v1alpha1.PortScanSpec, status *v1alpha1.PortScanStatus, username string, password string, fingerprint string) (string, error) {
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
	fmt.Println(req)
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

func SubNotifyExternalSystem(data map[string]string, status string, target string, url string, username string, password string, filename string, clstatus *v1alpha1.PortScanStatus) error {
	targets := strings.SplitN(target, ":", 2)
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
	data["alertName"] = fmt.Sprintf("target %s is unreachable on port %s, please check", targets[0], targets[1])
	data["message"] = fmt.Sprintf("target %s is unreachable on port %s, please check", targets[0], targets[1])
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
	clstatus.ExternalNotified = true
	now := metav1.Now()
	clstatus.ExternalNotifiedTime = &now
	return nil
}

func NotifyExternalSystem(data map[string]string, status string, target string, url string, username string, password string, filename string, clstatus *v1alpha1.PortScanStatus) error {
	targets := strings.SplitN(target, ":", 2)
	fig, _ := ReadFile(filename)
	if fig != "" {
		log.Printf("External system has already been notified for target %s . Exiting", url)
		return nil
	}
	fingerprint := randomString(10)
	data["fingerprint"] = fingerprint
	data["status"] = status
	data["startsAt"] = time.Now().String()
	data["alertName"] = fmt.Sprintf("target %s is unreachable on port %s, please check", targets[0], targets[1])
	data["message"] = fmt.Sprintf("target %s is unreachable on port %s, please check", targets[0], targets[1])
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
	clstatus.ExternalNotified = true
	now := metav1.Now()
	clstatus.ExternalNotifiedTime = &now
	return nil
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func SendEmailAlert(target string, filename string, spec *v1alpha1.PortScanSpec) {
	targets := strings.SplitN(target, ":", 2)
	if _, err := os.Stat(filename); os.IsNotExist(err) {

		message := fmt.Sprintf(`/bin/echo "target %s is unreachable on port %s" | /usr/sbin/sendmail -f %s -S %s %s`, targets[0], targets[1], spec.Email, spec.RelayHost, spec.Email)
		cmd3 := exec.Command("/bin/bash", "-c", message)
		err := cmd3.Run()
		if err != nil {
			fmt.Printf("Failed to send the alert: %s", err)
		}
		writeFile(filename, "sent")

	} else {
		data, _ := ReadFile(filename)
		fmt.Println(data)
		if data != "sent" {
			message := fmt.Sprintf(`/bin/echo "target %s is unreachable on port %s" | /usr/sbin/sendmail -f %s -S %s %s`, targets[0], targets[1], spec.Email, spec.RelayHost, spec.Email)
			cmd3 := exec.Command("/bin/bash", "-c", message)
			err := cmd3.Run()
			if err != nil {
				fmt.Printf("Failed to send the alert: %s", err)
			}
		}

	}
}

func SendEmailReachableAlert(target string, filename string, spec *v1alpha1.PortScanSpec) {
	targets := strings.SplitN(target, ":", 2)
	data, err := ReadFile(filename)
	if err != nil {
		fmt.Println(err)
	}
	if data == "sent" {
		message := fmt.Sprintf(`/bin/echo "target %s is reachable again on port %s" | /usr/sbin/sendmail -f %s -S %s %s`, targets[0], targets[1], spec.Email, spec.RelayHost, spec.Email)
		cmd3 := exec.Command("/bin/bash", "-c", message)
		err := cmd3.Run()
		if err != nil {
			fmt.Printf("Failed to send the alert: %s", err)
		}
	}

}

func writeFile(filename string, data string) error {
	err := os.WriteFile(filename, []byte(data), 0644)
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
