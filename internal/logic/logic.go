package logic

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ipClient never reuses connections and has a short timeout. Reusing a keep-
// alive that was established over the (now-gone) VPN interface causes the next
// request to hang until an OS-level TCP timeout, so keepalives are disabled.
var ipClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: -1,
		}).DialContext,
	},
}

// Logic .
type Logic struct {
	logger              *slog.Logger
	serverListCacheLock sync.Mutex
	serverListCache     []Server

	processMu        sync.Mutex
	runningProcess   *exec.Cmd
	activeServerName string

	broadcaster LogBroadcaster
	stateFile   string
}

// New .
func New(logger *slog.Logger) *Logic {
	stateFile := os.Getenv("STATE_FILE")
	if stateFile == "" {
		stateFile = "/data/torpedo-state.json"
	}
	return &Logic{
		logger:          logger,
		serverListCache: make([]Server, 0),
		stateFile:       stateFile,
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

type savedState struct {
	ServerName string `json:"server_name"`
}

func (l *Logic) saveState(serverName string) {
	dir := filepath.Dir(l.stateFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		l.logger.Warn("failed to create state directory", "err", err)
		return
	}
	data, err := json.Marshal(savedState{ServerName: serverName})
	if err != nil {
		l.logger.Warn("failed to marshal state", "err", err)
		return
	}
	if err := os.WriteFile(l.stateFile, data, 0600); err != nil {
		l.logger.Warn("failed to save state file", "err", err, "path", l.stateFile)
	}
}

// Restore reconnects to the last active server saved before a restart.
func (l *Logic) Restore() error {
	data, err := os.ReadFile(l.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var s savedState
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s.ServerName == "" {
		return nil
	}
	l.broadcaster.write(fmt.Sprintf("Restoring last active connection: %s", s.ServerName))
	return l.Connect(s.ServerName)
}

// Status returns the active server name and whether a process is running.
func (l *Logic) Status() (serverName string, connected bool) {
	l.processMu.Lock()
	defer l.processMu.Unlock()
	return l.activeServerName, l.runningProcess != nil && l.runningProcess.Process != nil
}

// Disconnect stops the running VPN process and clears the persisted state so a
// restart does not reconnect.
func (l *Logic) Disconnect() error {
	l.processMu.Lock()
	defer l.processMu.Unlock()

	if l.runningProcess == nil || l.runningProcess.Process == nil {
		return nil
	}

	l.broadcaster.write("Disconnecting...")
	l.logger.Info("stopping VPN process")
	if err := l.runningProcess.Process.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("failed to signal process: %w", err)
	}
	if _, err := l.runningProcess.Process.Wait(); err != nil {
		return fmt.Errorf("failed to wait for process: %w", err)
	}
	l.runningProcess = nil
	l.activeServerName = ""

	// gluetun installs iptables DROP rules for anything not going through the
	// VPN interface. Its SIGINT shutdown does not reset them, so once the VPN
	// process is gone all outbound traffic (e.g. /api/ip -> ipinfo.io) is
	// blocked until the next Connect() reinitializes the firewall. Flush the
	// rules ourselves to restore normal networking after a disconnect.
	l.resetFirewall()

	// gluetun also rewrites /etc/resolv.conf to point at its local DNS at
	// 127.0.0.1:53. That DNS is gone with the process, so name resolution
	// stops working until we point resolv.conf at a public resolver.
	l.resetResolvConf()

	l.clearState()
	l.broadcaster.write("Disconnected.")
	return nil
}

// resetFirewall undoes gluetun's kill-switch after it exits. Gluetun installs
// DROP policies AND explicit DROP rules on INPUT/FORWARD/OUTPUT plus NAT
// entries, and its SIGINT shutdown does not restore any of them — so all
// outbound traffic stays blocked until we reset.
//
// Safe scope here: Tailscale runs in userspace mode (TS_USERSPACE=true) so it
// does not add iptables rules in this netns, and no other rules exist beyond
// what gluetun installed. Flushing filter + nat therefore removes only
// gluetun's rules.
func (l *Logic) resetFirewall() {
	commands := [][]string{
		{"-P", "INPUT", "ACCEPT"},
		{"-P", "FORWARD", "ACCEPT"},
		{"-P", "OUTPUT", "ACCEPT"},
		{"-F"},
		{"-X"},
		{"-t", "nat", "-F"},
		{"-t", "nat", "-X"},
	}
	for _, args := range commands {
		out, err := exec.Command("iptables", args...).CombinedOutput()
		if err != nil {
			l.logger.Warn("iptables reset failed",
				"args", strings.Join(args, " "),
				"err", err,
				"output", strings.TrimSpace(string(out)))
		}
	}
	l.broadcaster.write("Firewall rules cleared.")
}

// resetResolvConf overwrites /etc/resolv.conf with public DNS servers.
// Gluetun replaces the file so requests go through its local DNS (127.0.0.1),
// which stops resolving once gluetun exits.
func (l *Logic) resetResolvConf() {
	const contents = "nameserver 1.1.1.1\nnameserver 8.8.8.8\n"
	if err := os.WriteFile("/etc/resolv.conf", []byte(contents), 0644); err != nil {
		l.logger.Warn("failed to reset /etc/resolv.conf", "err", err)
		return
	}
	l.broadcaster.write("DNS restored (1.1.1.1, 8.8.8.8).")
}

func (l *Logic) clearState() {
	if err := os.Remove(l.stateFile); err != nil && !os.IsNotExist(err) {
		l.logger.Warn("failed to remove state file", "err", err, "path", l.stateFile)
	}
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

	l.processMu.Lock()
	defer l.processMu.Unlock()

	// If there's a running process, kill it first
	if l.runningProcess != nil && l.runningProcess.Process != nil {
		l.broadcaster.write("Stopping current connection...")
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

	l.broadcaster.write(fmt.Sprintf("Connecting to %s (%s)...", selectedServer.ServerName, selectedServer.Hostname))

	cmd := exec.Command("./gluetun-entrypoint")
	cmd.Stdout = &logWrapper{logger: l.logger, broadcaster: &l.broadcaster}

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
	l.activeServerName = selectedServer.ServerName

	l.saveState(selectedServer.ServerName)

	return nil
}

type ipWhoIsResponse struct {
	Success       bool    `json:"success"`
	Message       string  `json:"message"`
	IP            string  `json:"ip"`
	City          string  `json:"city"`
	Region        string  `json:"region"`
	Country       string  `json:"country"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	Postal        string  `json:"postal"`
	Connection    struct {
		Org string `json:"org"`
		ISP string `json:"isp"`
	} `json:"connection"`
	Timezone struct {
		ID string `json:"id"`
	} `json:"timezone"`
}

func (l *Logic) CheckIP() (IPInfo, error) {
	resp, err := ipClient.Get("https://ipwho.is/")
	if err != nil {
		return IPInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var raw ipWhoIsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return IPInfo{}, err
	}
	if !raw.Success {
		return IPInfo{}, fmt.Errorf("ipwho.is error: %s", raw.Message)
	}

	org := raw.Connection.Org
	if org == "" {
		org = raw.Connection.ISP
	}
	return IPInfo{
		IP:       raw.IP,
		City:     raw.City,
		Region:   raw.Region,
		Country:  raw.Country,
		Loc:      fmt.Sprintf("%f,%f", raw.Latitude, raw.Longitude),
		Org:      org,
		Postal:   raw.Postal,
		Timezone: raw.Timezone.ID,
	}, nil
}

// SubscribeLogs returns a channel of incoming log lines and the recent history.
func (l *Logic) SubscribeLogs() (chan string, []string) {
	return l.broadcaster.Subscribe()
}

// UnsubscribeLogs removes the subscriber and closes its channel.
func (l *Logic) UnsubscribeLogs(ch chan string) {
	l.broadcaster.Unsubscribe(ch)
}

// LogBroadcaster fans out log lines to SSE subscribers with a history buffer.
type LogBroadcaster struct {
	mu          sync.Mutex
	history     []string
	subscribers []chan string
}

const logHistorySize = 200

func (b *LogBroadcaster) write(line string) {
	b.mu.Lock()
	b.history = append(b.history, line)
	if len(b.history) > logHistorySize {
		b.history = b.history[len(b.history)-logHistorySize:]
	}
	subs := make([]chan string, len(b.subscribers))
	copy(subs, b.subscribers)
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- line:
		default: // drop if subscriber is slow
		}
	}
}

func (b *LogBroadcaster) Subscribe() (chan string, []string) {
	ch := make(chan string, 64)
	b.mu.Lock()
	hist := make([]string, len(b.history))
	copy(hist, b.history)
	b.subscribers = append(b.subscribers, ch)
	b.mu.Unlock()
	return ch, hist
}

func (b *LogBroadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	for i, sub := range b.subscribers {
		if sub == ch {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			break
		}
	}
	b.mu.Unlock()
	close(ch)
}

type logWrapper struct {
	logger      *slog.Logger
	broadcaster *LogBroadcaster
}

func (l *logWrapper) Write(p []byte) (n int, err error) {
	payload := strings.TrimSpace(string(p))
	if payload != "" {
		l.logger.Info("[gluetun] process log", slog.String("msg", payload))
		l.broadcaster.write(payload)
	}

	return len(p), nil
}
