package c7nclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/choerodon/c7n/pkg/c7nclient/model"
	"github.com/gosuri/uitable"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
)

var Client C7NClient

var OrgMap = NewSafeMap()

var ProMap = NewSafeMap()

type C7NClient struct {
	BaseURL    string
	httpClient *http.Client
	token      string
	config     *C7NPlatformContext
}

func InitClient(config *C7NPlatformContext) {
	Client = C7NClient{
		BaseURL: config.Server,
		httpClient: &http.Client{
		},
		token:  config.Token,
		config: config,
	}
}

func (c *C7NClient) newRequest(method, path string, paras map[string]interface{}, body interface{}) (*http.Request, error) {
	rel := &url.URL{Path: path}
	base, _ := url.Parse(c.BaseURL)
	u := base.ResolveReference(rel)
	var buf io.ReadWriter
	if body != nil {
		buf = new(bytes.Buffer)
		err := json.NewEncoder(buf).Encode(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, u.String(), buf)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	if paras != nil {
		for key, value := range paras {
			q.Add(key, fmt.Sprintf("%v", value))
		}
	}
	req.URL.RawQuery = q.Encode()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "bearer "+c.token)
	return req, nil
}

func (c *C7NClient) do(req *http.Request, v interface{}) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return resp, err
	}
	defer resp.Body.Close()
	result, _ := ioutil.ReadAll(resp.Body)
	if string(result) == "" {
		return resp, nil
	}
	newRespBodyToErrorModel := ioutil.NopCloser(bytes.NewBuffer(result))
	newRespBodyToObjectModel := ioutil.NopCloser(bytes.NewBuffer(result))
	err = c.handleRep(resp, newRespBodyToErrorModel)
	if err != nil {
		return resp, err
	}
	err = json.NewDecoder(newRespBodyToObjectModel).Decode(v)
	defer newRespBodyToErrorModel.Close()
	defer newRespBodyToObjectModel.Close()
	return resp, err
}

func (c *C7NClient) doHandleString(req *http.Request, v *string) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return resp, err
	}
	defer resp.Body.Close()
	result, _ := ioutil.ReadAll(resp.Body)
	if string(result) == "" {
		return resp, nil
	}
	newRespBodyToErrorModel := ioutil.NopCloser(bytes.NewBuffer(result))
	newRespBodyToObjectModel := ioutil.NopCloser(bytes.NewBuffer(result))
	defer newRespBodyToErrorModel.Close()
	defer newRespBodyToObjectModel.Close()
	err = c.handleRep(resp, newRespBodyToErrorModel)
	if err != nil {
		return resp, err
	}
	resultNew, _ := ioutil.ReadAll(newRespBodyToObjectModel)
	*v = string(resultNew)
	return resp, err
}

func (c *C7NClient) handleRep(resp *http.Response, readCloser io.ReadCloser) error {

	if resp.StatusCode == 200 {
		var errModel = model.Error{}
		json.NewDecoder(readCloser).Decode(&errModel)
		if errModel.Failed  {
			return errors.New(errModel.Message)
		}
		return nil
	}
	if resp.StatusCode == 403 {
		return errors.New("You do not have the permissions!")
	} else {
		return errors.New(resp.Status)
	}
	return nil
}

func (c *C7NClient) getTime(time float64) string {
	if time < 60 {
		return "刚刚"
	} else if time/60 < 60 {
		return fmt.Sprintf("%.0f分钟前", math.Floor(time/60))
	} else if time/60/60 < 24 {
		return fmt.Sprintf("%.0f小时前", math.Floor(time/60/60))
	} else if time/60/60/24 < 30 {
		return fmt.Sprintf("%.0f天前", math.Floor(time/60/60/24))
	} else if time/60/60/24/30 < 12 {
		return fmt.Sprintf("%.0f月前", math.Floor(time/60/60/24/30))
	} else {
		return fmt.Sprintf("%.0f年前", math.Floor(time/60/60/24/30/12))
	}
}

func (c *C7NClient) printContextInfo() {
	fmt.Printf("organization: %s(%s) project: %s(%s)", c.config.Organization, c.config.OrganizationCode,
		c.config.Project, c.config.ProjectCode)
}

func PrintConfigInfo(config C7NPlatformContext, out io.Writer) {
	table := uitable.New()
	table.MaxColWidth = 60
	table.AddRow("Name", "Server", "Organization", "Project", "Token")
	table.AddRow(config.Name, config.Server,
		fmt.Sprintf("%s(%s)", config.Organization, config.OrganizationCode),
		fmt.Sprintf("%s(%s)", config.Project, config.ProjectCode), config.Token)
	fmt.Fprintf(out, table.String())
}
