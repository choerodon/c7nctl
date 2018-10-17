package slaver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/choerodon/c7n/pkg/config"
	"github.com/choerodon/c7n/pkg/kube"
	"github.com/vinkdong/gox/log"
	"io/ioutil"
	core_v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Slaver struct {
	Client       kubernetes.Interface
	Version      string
	Namespace    string
	Name         string
	CommonLabels map[string]string
	Image        string
	Ports        []core_v1.ContainerPort
	Env          []core_v1.EnvVar
	volumeMounts []core_v1.VolumeMount
	PodList      *core_v1.PodList
	Address      string
}

const IngressCheckPath = "/c7n/acme-challenge"

type Dir struct {
	Mode string
	Path string
}

/**
Type: httpGet or socket
*/
type Checker struct {
	Type   string
	Host   string
	Schema string
	Port   int
}

func (s *Slaver) CheckInstall() (*v1beta1.DaemonSet, error) {
	ds, err := s.Client.ExtensionsV1beta1().DaemonSets(s.Namespace).Get(s.Name, meta_v1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof("deploying daemonSet %s", s.Name)
			return s.Install()
		}
		return nil, err
	}
	return ds, err
}

func (s *Slaver) Install() (*v1beta1.DaemonSet, error) {

	dsContainer := core_v1.Container{
		Name:         s.Name,
		Image:        s.Image,
		Ports:        s.Ports,
		Env:          s.Env,
		VolumeMounts: s.volumeMounts,
	}

	tmp := core_v1.PodTemplateSpec{
		ObjectMeta: meta_v1.ObjectMeta{
			Labels: s.CommonLabels,
		},
		Spec: core_v1.PodSpec{
			Containers: []core_v1.Container{dsContainer},
		},
	}

	selector := &meta_v1.LabelSelector{
		MatchLabels: s.CommonLabels,
	}
	s.CommonLabels["app"] = s.Name
	ds := &v1beta1.DaemonSet{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "v1beta2",
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:   s.Name,
			Labels: s.CommonLabels,
		},
		Spec: v1beta1.DaemonSetSpec{
			Template: tmp,
			Selector: selector,
		},
	}
	daemonSet, err := s.Client.ExtensionsV1beta1().DaemonSets(s.Namespace).Create(ds)

	if err != nil {
		return nil, err
	}
	return daemonSet, err
}

func (s *Slaver) GetPods() (*core_v1.PodList, error) {
	set := labels.Set(s.CommonLabels)
	fmt.Println(set.AsSelector().String())
	opts := meta_v1.ListOptions{
		LabelSelector: set.AsSelector().String(),
	}
	return s.Client.CoreV1().Pods(s.Namespace).List(opts)
}

func (s *Slaver) CheckRunning() bool {
	poList, err := s.GetPods()
	if err != nil || poList.Size() < 1 {
		log.Error(err)
		return false
	}
	for _, po := range poList.Items {
		if po.Status.Phase != core_v1.PodRunning {
			return false
		}
	}
	s.PodList = poList
	return true
}

func (s *Slaver) getForwardPorts(localPort int) string {
	for _, port := range s.Ports {
		if port.Name == "http" {
			return fmt.Sprintf("%d:%d", localPort, port.ContainerPort)
		}
	}
	log.Error("no slave http port found")
	os.Exit(129)
	return ""
}

func (s *Slaver) ForwardPort(stopCh <-chan struct{}) int {

	rest := s.Client.CoreV1().RESTClient()

	var pod core_v1.Pod

	if !s.CheckRunning() {
		return 0
	}
	pod = s.PodList.Items[0]

	req := rest.Post().Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("portforward")

	config, err := kube.GetConfig()
	if err != nil {
		log.Error(err)
	}

	transport, upgrader, err := spdy.RoundTripperFor(config)
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	readyCh := make(chan struct{})

	port := 8000
getFreePort:
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("", strconv.Itoa(port)), time.Second)
	if conn != nil {
		port += 1
		goto getFreePort
		conn.Close()
	}
	log.Info(port)

	fw, err := portforward.New(dialer, []string{s.getForwardPorts(port)}, stopCh, readyCh, os.Stdout, os.Stderr)

	if err != nil {
		log.Error(err)
	}
	go fw.ForwardPorts()
	<-readyCh

	s.Address = fmt.Sprintf("http://127.0.0.1:%d", port)
	return port
}

func (s *Slaver) MakeDir(dir Dir) error {
	log.Infof("create dir %s with mode %s", dir.Path, dir.Mode)
	url := fmt.Sprint(s.Address, "/cmd")

	jsonContext := fmt.Sprintf(`{"commond":"mkdir -p %s -m %s"}`, dir.Path, dir.Mode)
	var jsonStr = []byte(jsonContext)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	type result struct {
		Success bool `json:"success"`
	}
	body, _ := ioutil.ReadAll(resp.Body)
	Request := &result{}
	json.Unmarshal(body, Request)
	if Request.Success == false {
		return fmt.Errorf("can't create dir %s with mode %s", dir.Path, dir.Mode)
	}
	return nil
}

func (s *Slaver) ExecuteSql(sql string, r *config.Resource) error {
	log.Infof("executed sql %s", sql)
	sql = strings.Replace(sql, "\"", "\\\"", -1)
	url := fmt.Sprint(s.Address, "/mysql")

	jsonContext := fmt.Sprintf(`{"scop": "database","mysql_info": {"mysql_host": "%s","mysql_port": "%d","mysql_name": "%s","mysql_pwd": "%s"},"sql": "%s"}`, r.Host, r.Port, r.Username, r.Password, sql)
	var jsonStr = []byte(jsonContext)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	type result struct {
		Success bool `json:"success"`
	}
	body, _ := ioutil.ReadAll(resp.Body)
	Request := &result{}
	json.Unmarshal(body, Request)
	if Request.Success == false {
		return fmt.Errorf("can't execute sql %s ", sql)
	}
	return nil
}

func (s *Slaver) CheckHealth(checker Checker) bool {
	log.Info("check health")
	return true
}

func (s *Slaver) InstallService() (*core_v1.Service, error) {
	svcInterface := s.Client.CoreV1().Services(s.Namespace)

	svc, err := svcInterface.Get(s.Name, meta_v1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}
	if svc != nil && err == nil {
		return svc, err
	}
	port := intstr.IntOrString{
		Type:   1,
		StrVal: "http",
	}
	servicePort := core_v1.ServicePort{
		Port:       80,
		Protocol:   "TCP",
		TargetPort: port,
	}

	service := &core_v1.Service{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:   s.Name,
			Labels: s.CommonLabels,
		},
		Spec: core_v1.ServiceSpec{
			Ports:    []core_v1.ServicePort{servicePort},
			Selector: s.CommonLabels,
		},
	}

	return svcInterface.Create(service)
}

func (s *Slaver) UpdateIngress(ingress *v1beta1.Ingress, domain string) error {
	for _, r := range ingress.Spec.Rules {
		if r.Host == domain {
			return nil
		}
	}
	ruleList := ingress.Spec.Rules
	ingressRule, err := s.getIngressRule(domain)
	if err != nil {
		return err
	}
	ingress.Spec.Rules = append(ruleList, ingressRule)

	ingressInterface := s.Client.ExtensionsV1beta1().Ingresses(s.Namespace)

	_, err = ingressInterface.Update(ingress)
	return err
}

func (s *Slaver) getIngressRule(domain string) (v1beta1.IngressRule, error) {
	port := intstr.IntOrString{
		Type:   1,
		StrVal: "http",
	}
	svc, err := s.InstallService()

	if err != nil {
		return v1beta1.IngressRule{}, err
	}

	backend := v1beta1.IngressBackend{
		ServiceName: svc.Name,
		ServicePort: port,
	}

	ingressPath := v1beta1.HTTPIngressPath{
		Path:    IngressCheckPath,
		Backend: backend,
	}
	ingressRuleValue := v1beta1.IngressRuleValue{
		HTTP: &v1beta1.HTTPIngressRuleValue{
			Paths: []v1beta1.HTTPIngressPath{ingressPath},
		},
	}
	ingressRule := v1beta1.IngressRule{
		Host:             domain,
		IngressRuleValue: ingressRuleValue,
	}

	return ingressRule, nil
}

func (s *Slaver) InstallIngress(domain string) error {

	ingressInterface := s.Client.ExtensionsV1beta1().Ingresses(s.Namespace)

	ing, err := ingressInterface.Get(s.Name+"checker", meta_v1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err == nil {
		return s.UpdateIngress(ing, domain)
	}

	ingressRule, err := s.getIngressRule(domain)

	if err != nil {
		return err
	}
	ingress := &v1beta1.Ingress{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "extensions/v1beta1",
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:   s.Name + "checker",
			Labels: s.CommonLabels,
		},
		Spec: v1beta1.IngressSpec{
			Rules: []v1beta1.IngressRule{ingressRule},
		},
	}
	_, err = ingressInterface.Create(ingress)
	return err
}

func (s *Slaver) CheckClusterDomain(domain string) error {
	err := s.InstallIngress(domain)
	if err != nil {
		return err
	}
	log.Infof("send msg to check domain %s", domain)
	return nil
}

func (s *Slaver) Uninstall() error {
	return s.Client.AppsV1().DaemonSets(s.Namespace).Delete(s.Name, &meta_v1.DeleteOptions{})
}
