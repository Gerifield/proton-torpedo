package logic

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Logic .
type Logic struct {
	logger              *slog.Logger
	serverListCacheLock sync.Mutex
	serverListCache     []Server

	runningProcess *exec.Cmd
}

// New .
func New(logger *slog.Logger) *Logic {
	return &Logic{
		logger:          logger,
		serverListCache: make([]Server, 0),
	}
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

type IPInfo struct {
	IP       string `json:"ip"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Loc      string `json:"loc"`
	Org      string `json:"org"`
	Postal   string `json:"postal"`
	Timezone string `json:"timezone"`
	Readme   string `json:"readme"`
}

func (l *Logic) ServerList() ([]Server, error) {
	cmd := exec.Command("./gluetun-entrypoint", "format-servers", "-protonvpn", "-format", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var srvList []Server
	err = json.Unmarshal(out, &srvList)

	// Filter and keep only where VPN is "wireguard"
	var filteredList []Server
	for _, srv := range srvList {
		if srv.VPN == "wireguard" {
			filteredList = append(filteredList, srv)
		}
	}

	// Cache the list
	l.serverListCacheLock.Lock()
	l.serverListCache = filteredList
	l.serverListCacheLock.Unlock()
	l.logger.Info("Server list loaded", slog.Int("num_servers", len(filteredList)))

	return filteredList, err
}

func (l *Logic) Connect(serverName string) error {
	l.serverListCacheLock.Lock()
	cacheLen := len(l.serverListCache)
	l.serverListCacheLock.Unlock()

	// Minimal race condition here is fine
	if cacheLen == 0 {
		l.logger.Info("updating server list before connecting")
		// Load the server list if not already loaded
		_, err := l.ServerList()
		if err != nil {
			return err
		}
	}

	// Get details based on the server name
	var selectedServer Server
	l.serverListCacheLock.Lock()
	for _, srv := range l.serverListCache {
		if srv.ServerName == serverName {
			selectedServer = srv
			break
		}
	}
	l.serverListCacheLock.Unlock()

	if selectedServer.ServerName == "" {
		return errors.New("server not found")
	}

	// If there's a running process, kill it first
	if l.runningProcess != nil && l.runningProcess.Process != nil {
		l.logger.Info("killing existing process before starting a new one")
		err := l.runningProcess.Process.Signal(os.Interrupt)
		if err != nil {
			return fmt.Errorf("failed to kill existing process: %w", err)
		}

		l.logger.Info("wait for process to exit")
		_, err = l.runningProcess.Process.Wait()

		if err != nil {
			return fmt.Errorf("failed to wait for existing process to exit: %w", err)
		}

		l.logger.Info("process exited, starting new one")
		l.runningProcess = nil
	}

	cmd := exec.Command("./gluetun-entrypoint")
	// Set the output to the same as the current process
	cmd.Stdout = &logWrapper{logger: l.logger}

	// Copy env variable, but skip the `SERVER_HOSTNAMES` since we'll use our own
	localEnvs := os.Environ()
	var filteredEnvs []string
	for _, env := range localEnvs {
		if !strings.HasPrefix(env, "SERVER_HOSTNAMES=") {
			filteredEnvs = append(filteredEnvs, env)
		}
	}

	// Add our own SERVER_HOSTNAMES
	filteredEnvs = append(filteredEnvs, fmt.Sprintf("SERVER_HOSTNAMES=%s", selectedServer.Hostname))
	// Set the envs
	cmd.Env = filteredEnvs

	err := cmd.Start()
	if err != nil {
		return err
	}

	l.logger.Info("new process starting", slog.Int("pid", cmd.Process.Pid), slog.String("server", selectedServer.ServerName))
	l.runningProcess = cmd

	return nil
}

func (l *Logic) CheckIP() (IPInfo, error) {
	resp, err := http.Get("http://ipinfo.io")
	if err != nil {
		return IPInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var ipInfo IPInfo
	err = json.NewDecoder(resp.Body).Decode(&ipInfo)

	return ipInfo, err
}

type logWrapper struct {
	logger *slog.Logger
}

func (l *logWrapper) Write(p []byte) (n int, err error) {
	payload := strings.TrimSpace(string(p))
	l.logger.Info("[gluetun] process log", slog.String("msg", payload))

	return len(p), nil
}
