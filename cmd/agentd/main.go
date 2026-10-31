package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"myagent/internal/api"
	"myagent/internal/app"
)

// version is overridden at build time with:
//
//	go build -ldflags "-X main.version=1.2.3"
var version = "1.0.0"

type runtimeInfo struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
	Data  string `json:"data_dir"`
	UI    string `json:"ui_dir"`
}

func main() {
	// Keep the database, WAL, key files and tool artifacts private by default.
	syscall.Umask(0o077)
	var (
		addr = flag.String(
			"addr",
			envOr("MYAGENT_ADDR", "127.0.0.1:8787"),
			"listen address (loopback only)",
		)
		dataDir   = flag.String("data", os.Getenv("MYAGENT_DATA_DIR"), "data directory")
		workspace = flag.String(
			"workspace",
			os.Getenv("MYAGENT_WORKSPACE"),
			"agent workspace directory",
		)
		uiDir   = flag.String("ui", envOr("MYAGENT_UI_DIR", "ui/dist"), "built UI directory")
		openUI  = flag.Bool("open", true, "open the app in your browser")
		homeDir = flag.String(
			"home",
			os.Getenv("MYAGENT_HOME"),
			"workspace profile registry directory",
		)
		workspaceName = flag.String(
			"workspace-name",
			envOr("MYAGENT_WORKSPACE_NAME", "Personal"),
			"active workspace display name",
		)
		parentPID = flag.Int(
			"parent-pid",
			0,
			"exit when this parent process disappears (used by the native shell)",
		)
	)
	flag.Parse()

	token := os.Getenv("MYAGENT_TOKEN")
	if token == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			log.Fatal(err)
		}
		token = hex.EncodeToString(buf)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := app.New(
		ctx,
		app.Config{
			DataDir:       *dataDir,
			WorkspaceRoot: *workspace,
			WorkspaceName: *workspaceName,
			HomeDir:       *homeDir,
			Address:       *addr,
			Version:       version,
		},
	)
	if err != nil {
		log.Fatalf("start app: %v", err)
	}
	defer a.Close()

	if err := ensureLoopback(*addr); err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	actual := ln.Addr().String()
	url := "http://" + actual
	srv := &http.Server{
		Handler:           api.New(a, token, *uiDir).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       180 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	infoPath := filepath.Join(a.Config().DataDir, "api.json")
	info := runtimeInfo{
		URL:   url,
		Token: token,
		PID:   os.Getpid(),
		Data:  a.Config().DataDir,
		UI:    *uiDir,
	}
	raw, _ := json.MarshalIndent(info, "", "  ")
	if err := os.MkdirAll(filepath.Dir(infoPath), 0o700); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(infoPath, raw, 0o600); err != nil {
		log.Fatal(err)
	}
	defer os.Remove(infoPath)

	log.Printf("MyAgent %s", version)
	log.Printf("workspace: %s", a.Workspace.RootPath)
	log.Printf("data:      %s", a.Config().DataDir)
	log.Printf("secrets:   %s key (%s)", a.KeyKind(), a.KeyDescription())
	log.Printf("listening: %s", url)
	if *openUI {
		openURL := url + "/?token=" + token
		go func() {
			time.Sleep(250 * time.Millisecond)
			_ = exec.Command("/usr/bin/open", openURL).Run()
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	parentGone := make(chan struct{})
	if *parentPID > 0 {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if err := syscall.Kill(*parentPID, 0); err != nil {
					close(parentGone)
					return
				}
			}
		}()
	}
	select {
	case sig := <-sigCh:
		log.Printf("received %s, shutting down", sig)
	case <-parentGone:
		log.Printf("parent process %d exited, shutting down", *parentPID)
	case <-a.RestartCh():
		log.Printf("workspace switch requested, shutting down for restart")
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("server: %v", err)
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	a.Close()
}

func ensureLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", addr, err)
	}
	if host == "" || host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("refusing to listen on non-loopback address %q", host)
	}
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
