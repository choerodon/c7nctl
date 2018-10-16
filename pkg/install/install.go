package install

import (
	"bytes"
	"github.com/choerodon/c7n/pkg/config"
	"github.com/choerodon/c7n/pkg/helm"
	"github.com/choerodon/c7n/pkg/kube"
	"github.com/choerodon/c7n/pkg/slaver"
	"github.com/vinkdong/gox/log"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"os"
	"text/template"
	"fmt"
)

type Install struct {
	Version      string
	Metadata     Metadata
	Spec         Spec
	Client       kubernetes.Interface
	UserConfig   *config.Config
	HelmClient   *helm.Client
	CommonLabels map[string]string
}

type Metadata struct {
	Name string
}

type InfraResource struct {
	Name         string
	Chart        string
	Namespace    string
	RepoURL      string
	Version      string
	Values       []ChartValue
	Persistence  []*Persistence
	Client       *helm.Client
	Home         *Install
	Resource     *config.Resource
	PreInstall   []PreInstall
	PreValues    PreValueList
	Requirements []string
}

type Spec struct {
	Basic     Basic
	Resources v1.ResourceRequirements
	Infra     []InfraResource
}

type Basic struct {
	RepoURL string
	Slaver  slaver.Slaver
}

type PreInstall struct {
	Name     string
	Commands []string
	InfraRef string `yaml:"infraRef"`
}

type PreValueList []*PreValue

func (pl *PreValueList) prepareValues() error {
	for _, v := range *pl {
		if err := v.renderValue(); err != nil {
			return err
		}
	}
	return nil
}

func (pl *PreValueList) getValues(key string) string {
	for _, v := range *pl {
		if v.Name == key {
			return v.Value
		}
	}
	return ""
}

type ChartValue struct {
	Name  string
	Value string
	Input Input
}

type PreValue struct {
	Name  string
	Value string
	Check string
}

func (p *PreValue) renderValue() error {
	tpl, err := template.New(p.Name).Parse(p.Value)
	if err != nil {
		return err
	}
	var data bytes.Buffer
	err = tpl.Execute(&data, p)
	if err != nil {
		return err
	}
	log.Infof("check %s %s", p.Check, data.String())
	switch p.Check {
	case "domain":
		//todo: add check domain
	}

	p.Value = data.String()
	return nil
}

// 获取基础组件信息
func (p *PreValue) GetResource(key string) *config.Resource {
	news := Ctx.GetSucceed(key, ReleaseTYPE)
	// get info from succeed
	if news != nil {
		return &news.Resource
	} else {
		if r, ok := Ctx.UserConfig.Spec.Resources[key]; ok {
			return r
		}
	}
	log.Errorf("can't get required resource [%s]", key)
	os.Exit(188)
	return nil
}

type Input struct {
	Enabled bool
	Regex   string
	Tip     string
}

func (i *Install) InstallInfra() error {
	// 安装基础组件
	for _, infra := range i.Spec.Infra {
		// 准备pv和pvc
		if err := infra.preparePersistence(i.Client, i.UserConfig); err != nil {
			return err
		}
		infra.Client = i.HelmClient
		infra.Namespace = i.UserConfig.Metadata.Namespace
		infra.Home = i
		if infra.RepoURL == "" {
			infra.RepoURL = i.Spec.Basic.RepoURL
		}
		if err := infra.CheckInstall(); err != nil {
			return err
		}
	}
	return nil
}

func (i *Install) CheckResource() bool {
	request := i.Spec.Resources.Requests
	reqMemory := request.Memory().Value()
	reqCpu := request.Cpu().Value()
	clusterMemory, clusterCpu := getClusterResource(i.Client)
	if clusterMemory < reqMemory {
		log.Errorf("cluster memory not enough, request %dGi", reqMemory/(1024*1024*1024))
		return false
	}
	if clusterCpu < reqCpu {
		log.Errorf("cluster cpu not enough, request %dc", reqCpu/1000)
		return false
	}
	return true
}

func (i *Install) CheckNamespace() bool {
	_, err := i.Client.CoreV1().Namespaces().Get(i.UserConfig.Metadata.Namespace, meta_v1.GetOptions{})
	if err != nil {
		if errorStatus, ok := err.(*errors.StatusError); ok {
			if errorStatus.ErrStatus.Code == 404 && i.createNamespace() {
				return true
			}
		}
		log.Error(err)
		return false
	}
	log.Infof("namespace %s already exists", i.UserConfig.Metadata.Namespace)
	return true
}

func (i *Install) createNamespace() bool {
	ns := &v1.Namespace{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: i.UserConfig.Metadata.Namespace,
		},
	}
	namespace, err := i.Client.CoreV1().Namespaces().Create(ns)
	log.Infof("creating namespace %s", namespace.Name)
	if err != nil {
		log.Error(err)
		return false
	}
	return true
}

func getClusterResource(client kubernetes.Interface) (int64, int64) {
	var sumMemory int64
	var sumCpu int64
	list, _ := client.CoreV1().Nodes().List(meta_v1.ListOptions{})
	for _, v := range list.Items {
		sumMemory += v.Status.Capacity.Memory().Value()
		sumCpu += v.Status.Capacity.Cpu().Value()
	}
	return sumMemory, sumCpu
}

func (i *Install) Run() error {

	if i.Client == nil {
		i.Client = kube.GetClient()
	}
	if !i.CheckResource() {
		os.Exit(126)
	}

	if !i.CheckNamespace() {
		os.Exit(127)
	}

	if i.HelmClient == nil {
		log.Info("reinit helm client")
		tunnel := kube.GetTunnel()
		i.HelmClient = &helm.Client{
			Tunnel: tunnel,
		}
	}

	Ctx = Context{
		Client:       i.Client,
		Namespace:    i.UserConfig.Metadata.Namespace,
		CommonLabels: i.CommonLabels,
		UserConfig:   i.UserConfig,
	}

	// prepare slaver to execute sql or make directory ..

	s := &i.Spec.Basic.Slaver
	s.Client = i.Client
	s.CommonLabels = i.CommonLabels
	s.Namespace = i.UserConfig.Metadata.Namespace

	Ctx.Slaver = s

	if _, err := s.CheckInstall(); err != nil {
		return err
	}

	stopCh := make(chan struct{})

	port := s.ForwardPort(stopCh)

	Ctx.SlaverAddress = fmt.Sprintf("http://127.0.0.1:%d", port)
	defer func() {
		stopCh <- struct{}{}
	}()

	// install 基础组件
	if err := i.InstallInfra(); err != nil {
		return err
	}

	return nil
}
