package logic

import (
	"encoding/json"
	"os/exec"
)

// Logic .
type Logic struct{}

// New .
func New() *Logic {
	return &Logic{}
}

type Server struct {
	VPN         string   `json:"vpn"` // openvpn/wireguard
	Country     string   `json:"country"`
	City        string   `json:"city"`
	ServerName  string   `json:"server_name"`
	Hostname    string   `json:"hostname"`
	TCP         bool     `json:"tcp"`
	UDP         bool     `json:"udp"`
	Stream      bool     `json:"stream"`
	PortForward bool     `json:"port_forward"`
	Ips         []string `json:"ips"`
}

func (l *Logic) ServerList() ([]Server, error) {
	cmd := exec.Command("./gluetun-entrypoint", "format-servers", "-protonvpn", "-format", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var srvList []Server
	err = json.Unmarshal(out, &srvList)

	return srvList, err
}
