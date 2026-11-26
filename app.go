package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const signalingHealthURL = "http://127.0.0.1:8080/health"

type App struct {
	ctx           context.Context
	signalingCmd  *exec.Cmd
	signalingError string
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// If a local VC Desk server is already running, reuse it rather than
	// starting a second copy and fighting over port 8080.
	if signalingHealthy() {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		a.signalingError = fmt.Sprintf("Could not locate VC Desk: %v", err)
		return
	}
	serverPath := filepath.Join(filepath.Dir(exe), "vcdesk-signaling.exe")
	if _, err := os.Stat(serverPath); err != nil {
		a.signalingError = "The bundled vcdesk-signaling.exe is missing. Keep it beside VCDesk.exe."
		return
	}

	cmd := exec.CommandContext(ctx, serverPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		a.signalingError = fmt.Sprintf("Could not start the signaling server: %v", err)
		return
	}
	a.signalingCmd = cmd
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if signalingHealthy() {
			return
		}
		select {
		case <-ctx.Done():
			a.signalingError = "VC Desk closed while its signaling server was starting."
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
	a.signalingError = "The signaling server did not become ready on 127.0.0.1:8080."
}

func signalingHealthy() bool {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	resp, err := client.Get(signalingHealthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (a *App) shutdown(ctx context.Context) {
	if a.signalingCmd != nil && a.signalingCmd.Process != nil {
		_ = a.signalingCmd.Process.Kill()
	}
}

func (a *App) SignalingStatus() string {
	if a.signalingError != "" {
		return a.signalingError
	}
	if signalingHealthy() {
		return "Signaling server ready."
	}
	return "Signaling server is not responding."
}

func (a *App) AppVersion() string { return "0.2.0" }
