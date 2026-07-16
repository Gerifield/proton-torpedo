package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"proton-torpedo/internal/logic"
)

// Logic .
type Logic struct {
	l *logic.Logic
}

// New .
func New(l *logic.Logic) *Logic {
	return &Logic{
		l: l,
	}
}

func (l *Logic) listServers(w http.ResponseWriter, r *http.Request) {
	servers, err := l.l.ServerList()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(servers)
}

func (l *Logic) ipInfo(w http.ResponseWriter, r *http.Request) {
	info, err := l.l.CheckIP()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(info)
}

func (l *Logic) connectServer(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ServerName string `json:"server_name"`
	}

	var req request
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ServerName == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}

	err = l.l.Connect(req.ServerName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		Success string `json:"success"`
	}{
		Success: "ok",
	})
}

func (l *Logic) statusHandler(w http.ResponseWriter, r *http.Request) {
	serverName, connected := l.l.Status()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Connected  bool   `json:"connected"`
		ServerName string `json:"server_name"`
	}{
		Connected:  connected,
		ServerName: serverName,
	})
}

// logsHandler streams gluetun log lines as Server-Sent Events.
func (l *Logic) logsHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch, history := l.l.SubscribeLogs()
	defer l.l.UnsubscribeLogs(ch)

	// Send historical lines first
	for _, line := range history {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}
	}
}

func (l *Logic) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("./static/")))
	mux.HandleFunc("GET /api/list", l.listServers)
	mux.HandleFunc("GET /api/ip", l.ipInfo)
	mux.HandleFunc("POST /api/connect", l.connectServer)
	mux.HandleFunc("GET /api/status", l.statusHandler)
	mux.HandleFunc("GET /api/logs", l.logsHandler)

	return mux
}

func (l *Logic) ListenAndServer(addr string) error {
	return http.ListenAndServe(addr, l.routes())
}
