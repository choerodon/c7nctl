package common

import (
	"github.com/ugorji/go/codec"
	"fmt"
	"sync"
	"net/http"
	"bytes"
	"github.com/vinkdong/gox/log"
	"net"
	"bufio"
)

type Metrics struct {
	CPU        int64
	Memory     int64
	Province   string
	City       string
	Version    string
	Status     string
	ErrorMsg   []string
	CurrentApp string
	Mux        sync.Mutex
	Ip         string
	Mail       string
}

const (
	metricsUrl = "http://c7n-gateway.staging.saas.hand-china.com/api/v1/metrics"
	ipAddr      = "ns1.dnspod.net:6666"
)

func (m *Metrics) Send() {
	data := m.pack()
	client := http.Client{}
	req, err := http.NewRequest("POST", metricsUrl, bytes.NewReader(data))
	if err != nil {
		log.Error(err)
	}
	m.Ip = GetPublicIP()
	resp, err := client.Do(req)
	if err != nil {
		log.Error(err)
	}
	if resp.StatusCode != 200 {
		log.Errorf("send metrics failed with code: %d", resp.StatusCode)
	}
}

func (m *Metrics) pack() []byte {
	var (
		//v interface{} // value to decode/encode into
		b  []byte
		mh codec.MsgpackHandle
	)

	enc := codec.NewEncoderBytes(&b, &mh)
	err := enc.Encode(m)
	if err != nil {
		fmt.Println(err)
		fmt.Println(b)
	}
	return b
}

func GetPublicIP() string {
	conn, _ := net.Dial("tcp", ipAddr)
	defer conn.Close()

	ip, _ := bufio.NewReader(conn).ReadString('\n')
	return ip
}
