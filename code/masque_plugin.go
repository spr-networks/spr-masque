package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var UNIX_PLUGIN_LISTENER = "/state/plugins/spr-masque/socket"

// getContainerIP returns the container's address on the spr-masque bridge
// (eth0). The SOCKS listener binds here so it is only reachable from the
// plugin's own docker network — SPR groups/policies gate device access.
func getContainerIP() string {
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

func proxyBindIP() string {
	if ip := getContainerIP(); ip != "" {
		return ip
	}
	// fall back to all interfaces inside the container netns; there are no
	// published ports, so this is still bridge-local.
	return "0.0.0.0"
}

func httpError(w http.ResponseWriter, msg string, code int) {
	fmt.Println("[-]", msg)
	http.Error(w, msg, code)
}

// POST /register {"DeviceName": "...", "JWT": "...", "Force": false}
func handleRegister(w http.ResponseWriter, r *http.Request) {
	req := struct {
		DeviceName string
		JWT        string
		Force      bool
	}{}
	if r.Body != nil {
		defer r.Body.Close()
		// tolerate an empty body: register with defaults
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			httpError(w, err.Error(), 400)
			return
		}
	}

	settings := loadSettings()
	name := req.DeviceName
	if name == "" {
		name = settings.DeviceName
	}

	out, err := runRegister(name, req.JWT, req.Force)
	if err != nil {
		code := 500
		if strings.Contains(err.Error(), "already registered") {
			code = 409
		} else if strings.Contains(err.Error(), "invalid") {
			code = 400
		}
		httpError(w, err.Error(), code)
		return
	}
	fmt.Println("[+] registration complete")

	// bring the proxy up with the fresh credentials
	if err := proxy.Restart(settings, proxyBindIP()); err != nil {
		fmt.Println("[-] proxy start after registration failed:", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"Registered": warpRegistered(),
		"Output":     out,
	})
}

// GET /status
func handleStatus(w http.ResponseWriter, r *http.Request) {
	settings := loadSettings()
	running, uptime, lastErr := proxy.Status()
	containerIP := getContainerIP()

	status := map[string]interface{}{
		"Registered":      warpRegistered(),
		"ProxyRunning":    running,
		"Uptime":          uptime,
		"LastError":       lastErr,
		"ContainerIP":     containerIP,
		"SocksPort":       settings.SocksPort,
		"BindAddress":     net.JoinHostPort(containerIP, fmt.Sprintf("%d", settings.SocksPort)),
		"EndpointVersion": settings.EndpointVersion,
	}

	if wc, err := loadWarpConfig(); err == nil {
		for k, v := range redactWarpConfig(wc) {
			status[k] = v
		}
		endpoint := wc.EndpointV4
		if settings.EndpointVersion == "v6" {
			endpoint = wc.EndpointV6
		}
		if endpoint != "" {
			status["Endpoint"] = fmt.Sprintf("%s (udp/%d)", endpoint, settings.ConnectPort)
		}
	}

	connectivity := map[string]interface{}{"OK": false}
	if running {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		trace, err := fetchTrace(ctx, net.JoinHostPort(containerIP, fmt.Sprintf("%d", settings.SocksPort)))
		if err != nil {
			connectivity["Error"] = err.Error()
		} else {
			fields := parseTrace(trace)
			connectivity["OK"] = fields["warp"] == "on" || fields["warp"] == "plus"
			connectivity["Warp"] = fields["warp"]
			connectivity["Colo"] = fields["colo"]
			connectivity["IP"] = fields["ip"]
		}
	}
	status["Connectivity"] = connectivity

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// GET /config, PUT /config
func handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loadSettings())
		return
	}

	defer r.Body.Close()
	s := defaultSettings()
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		httpError(w, err.Error(), 400)
		return
	}
	if s.DNSServers == nil {
		s.DNSServers = []string{}
	}
	if err := validateSettings(s); err != nil {
		httpError(w, err.Error(), 400)
		return
	}
	if err := saveSettings(s); err != nil {
		httpError(w, err.Error(), 500)
		return
	}

	if warpRegistered() {
		if err := proxy.Restart(s, proxyBindIP()); err != nil {
			fmt.Println("[-] proxy restart after config change failed:", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

// POST /restart
func handleRestart(w http.ResponseWriter, r *http.Request) {
	if !warpRegistered() {
		httpError(w, "not registered", 400)
		return
	}
	if err := proxy.Restart(loadSettings(), proxyBindIP()); err != nil {
		httpError(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"Restarted": true})
}

// GET /trace — raw cloudflare trace text fetched through the proxy
func handleTrace(w http.ResponseWriter, r *http.Request) {
	running, _, _ := proxy.Status()
	if !running {
		httpError(w, "proxy not running", 400)
		return
	}
	settings := loadSettings()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	trace, err := fetchTrace(ctx, net.JoinHostPort(getContainerIP(), fmt.Sprintf("%d", settings.SocksPort)))
	if err != nil {
		httpError(w, err.Error(), 502)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, trace)
}

type spaHandler struct {
	staticPath string
	indexPath  string
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path = filepath.Join(h.staticPath, path)
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

func logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
}

func main() {
	if err := ensureConfigDir(); err != nil {
		fmt.Println("[-] failed to create config dir:", err)
	}

	// keep credentials clamped even if written by an older usque run
	if _, err := os.Stat(WarpConfigFile); err == nil {
		os.Chmod(WarpConfigFile, 0600)
	}

	// autostart the proxy if we already have credentials
	if warpRegistered() {
		if err := proxy.Start(loadSettings(), proxyBindIP()); err != nil {
			fmt.Println("[-] proxy autostart failed:", err)
		}
	} else {
		fmt.Println("[ ] no WARP credentials yet; waiting for /register")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", handleRegister)
	mux.HandleFunc("GET /status", handleStatus)
	mux.HandleFunc("GET /config", handleConfig)
	mux.HandleFunc("PUT /config", handleConfig)
	mux.HandleFunc("POST /restart", handleRestart)
	mux.HandleFunc("GET /trace", handleTrace)
	mux.Handle("/", spaHandler{staticPath: "/ui", indexPath: "index.html"})

	os.Remove(UNIX_PLUGIN_LISTENER)
	os.MkdirAll(filepath.Dir(UNIX_PLUGIN_LISTENER), 0755)
	listener, err := net.Listen("unix", UNIX_PLUGIN_LISTENER)
	if err != nil {
		panic(err)
	}
	if err := os.Chmod(UNIX_PLUGIN_LISTENER, 0770); err != nil {
		panic(err)
	}

	server := http.Server{Handler: logRequest(mux)}
	server.Serve(listener)
}
