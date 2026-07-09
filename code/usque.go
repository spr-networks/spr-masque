package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

var UsquePath = "/usr/bin/usque"

// buildSocksArgs builds the usque argv for SOCKS5 proxy mode. Pure function,
// covered by unit tests. All values are validated before they get here and
// are passed as argv entries (never through a shell).
func buildSocksArgs(s Settings, bindIP string) []string {
	args := []string{
		"-c", WarpConfigFile,
		"socks",
		"-b", bindIP,
		"-p", strconv.Itoa(s.SocksPort),
	}
	if s.ConnectPort != 0 && s.ConnectPort != 443 {
		args = append(args, "-P", strconv.Itoa(s.ConnectPort))
	}
	if s.EndpointVersion == "v6" {
		args = append(args, "-6")
	}
	for _, dns := range s.DNSServers {
		args = append(args, "-d", dns)
	}
	return args
}

// buildRegisterArgs builds the usque argv for the non-interactive
// register/enroll flow.
func buildRegisterArgs(deviceName, jwt string) []string {
	args := []string{
		"-c", WarpConfigFile,
		"register",
		"--accept-tos",
	}
	if deviceName != "" {
		args = append(args, "--name", deviceName)
	}
	if jwt != "" {
		args = append(args, "--jwt", jwt)
	}
	return args
}

// ProxyManager supervises the usque SOCKS child process.
type ProxyManager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	gen       int // bumped on every intentional stop/restart
	running   bool
	startedAt time.Time
	lastErr   string
}

var proxy = &ProxyManager{}

// Start launches usque socks. No-op with error if not registered.
func (pm *ProxyManager) Start(s Settings, bindIP string) error {
	if !warpRegistered() {
		return fmt.Errorf("not registered: run the register flow first")
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.running {
		return nil
	}

	pm.gen++
	gen := pm.gen

	cmd := exec.Command(UsquePath, buildSocksArgs(s, bindIP)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		pm.lastErr = err.Error()
		return err
	}

	pm.cmd = cmd
	pm.running = true
	pm.startedAt = time.Now()
	pm.lastErr = ""
	fmt.Printf("[+] usque socks started (pid %d) on %s:%d\n", cmd.Process.Pid, bindIP, s.SocksPort)

	go pm.wait(cmd, gen, s, bindIP)
	return nil
}

// wait reaps the child; if it died without an intentional Stop/Restart it is
// relaunched after a short delay.
func (pm *ProxyManager) wait(cmd *exec.Cmd, gen int, s Settings, bindIP string) {
	err := cmd.Wait()

	pm.mu.Lock()
	if pm.gen != gen {
		// intentional stop/restart already superseded this child
		pm.mu.Unlock()
		return
	}
	pm.running = false
	if err != nil {
		pm.lastErr = err.Error()
	}
	pm.mu.Unlock()

	fmt.Println("[-] usque exited unexpectedly:", err)
	time.Sleep(5 * time.Second)

	pm.mu.Lock()
	stale := pm.gen != gen
	pm.mu.Unlock()
	if !stale {
		if err := pm.Start(s, bindIP); err != nil {
			fmt.Println("[-] usque relaunch failed:", err)
		}
	}
}

// Stop terminates the child if running.
func (pm *ProxyManager) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.gen++
	if pm.running && pm.cmd != nil && pm.cmd.Process != nil {
		pm.cmd.Process.Kill()
	}
	pm.running = false
}

// Restart stops any running child and starts a fresh one with settings.
func (pm *ProxyManager) Restart(s Settings, bindIP string) error {
	pm.Stop()
	// give the old listener a moment to release the port
	time.Sleep(500 * time.Millisecond)
	return pm.Start(s, bindIP)
}

// Status returns a consistent snapshot.
func (pm *ProxyManager) Status() (running bool, uptime string, lastErr string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.running {
		uptime = time.Since(pm.startedAt).Round(time.Second).String()
	}
	return pm.running, uptime, pm.lastErr
}

// runRegister performs the usque register/enroll flow non-interactively.
// If credentials already exist it refuses unless force is set, in which case
// the old config is moved aside first (usque prompts interactively when a
// config is present, which would hang a daemon).
func runRegister(deviceName, jwt string, force bool) (string, error) {
	if err := validateDeviceName(deviceName); err != nil {
		return "", err
	}
	if err := validateJWT(jwt); err != nil {
		return "", err
	}
	if err := ensureConfigDir(); err != nil {
		return "", err
	}

	if _, err := os.Stat(WarpConfigFile); err == nil {
		if !force {
			return "", fmt.Errorf("already registered; set Force to re-register")
		}
		if err := os.Rename(WarpConfigFile, WarpConfigFile+".bak"); err != nil {
			return "", fmt.Errorf("failed to back up existing config: %v", err)
		}
		os.Chmod(WarpConfigFile+".bak", 0600)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, UsquePath, buildRegisterArgs(deviceName, jwt)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("registration failed: %v", err)
	}

	// usque writes the config with default umask permissions; clamp to 0600.
	if err := os.Chmod(WarpConfigFile, 0600); err != nil {
		return string(out), fmt.Errorf("registration succeeded but chmod failed: %v", err)
	}

	if !warpRegistered() {
		return string(out), fmt.Errorf("registration did not produce valid credentials")
	}

	return string(out), nil
}
