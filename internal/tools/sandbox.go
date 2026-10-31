package tools

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type SandboxSpec struct {
	Command    string
	Args       []string
	Dir        string
	Workspace  string
	ToolDir    string
	OutDir     string
	AllowNet   bool
	AllowWrite bool
	AllowExec  []string
	DenyRead   []string
	Env        []string
	Stdin      string
	OnLog      func(stream, text string)
}

type SandboxResult struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	TimedOut  bool
	Sandboxed bool
	Err       error
}

func sandboxExecPath() string {
	for _, p := range []string{"/usr/bin/sandbox-exec", "/bin/sandbox-exec"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func RunSandboxed(ctx context.Context, spec SandboxSpec) SandboxResult {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if spec.Dir == "" {
		spec.Dir = spec.Workspace
	}
	stdout := &lineBuffer{onLog: spec.OnLog, stream: "stdout"}
	stderr := &lineBuffer{onLog: spec.OnLog, stream: "stderr"}
	var cmd *exec.Cmd
	profile := sandboxProfile(spec)
	sbx := sandboxExecPath()
	if sbx != "" && os.Getenv("MYAGENT_NO_SANDBOX") != "1" {
		args := []string{"-p", profile, spec.Command}
		args = append(args, spec.Args...)
		cmd = exec.CommandContext(ctx, sbx, args...)
		cmd.Env = spec.Env
		cmd.Dir = spec.Dir
	} else {
		cmd = exec.CommandContext(ctx, spec.Command, spec.Args...)
		cmd.Env = spec.Env
		cmd.Dir = spec.Dir
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	res := SandboxResult{
		ExitCode:  0,
		Sandboxed: sbx != "" && os.Getenv("MYAGENT_NO_SANDBOX") != "1",
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}
	if err != nil {
		res.Err = err
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
		}
	}
	stdout.flush()
	stderr.flush()
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	return res
}

func sandboxProfile(spec SandboxSpec) string {
	quote := func(p string) string {
		p = sandboxPath(p)
		p = strings.ReplaceAll(p, `\`, `\\`)
		p = strings.ReplaceAll(p, `"`, `\"`)
		return `"` + p + `"`
	}
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n")
	b.WriteString("(allow process*)\n")
	b.WriteString("(allow signal (target self))\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow ipc-posix-shm*)\n")
	b.WriteString("(allow ipc-posix-sem*)\n")
	b.WriteString("(allow file-read*)\n")
	// Broad reads are needed by language runtimes. Re-deny the app data and
	// common credential locations, then re-allow only the paths a tool needs.
	for _, p := range spec.DenyRead {
		if p != "" {
			b.WriteString("(deny file-read* (subpath " + quote(p) + "))\n")
		}
	}
	for _, p := range []string{spec.Workspace, spec.ToolDir, spec.OutDir, os.Getenv("TMPDIR")} {
		if p != "" {
			b.WriteString("(allow file-read* (subpath " + quote(p) + "))\n")
		}
	}
	b.WriteString("(allow file-write*")
	if spec.OutDir != "" {
		b.WriteString(" (subpath " + quote(spec.OutDir) + ")")
	}
	if spec.AllowWrite && spec.Workspace != "" {
		b.WriteString(" (subpath " + quote(spec.Workspace) + ")")
	}
	b.WriteString(
		" (literal \"/dev/null\") (literal \"/dev/urandom\") (literal \"/dev/stdout\") (literal \"/dev/stderr\"))\n",
	)
	if spec.AllowNet {
		b.WriteString("(allow network*)\n")
	}
	return b.String()
}

func sandboxPath(p string) string {
	if p == "" {
		return ""
	}
	if real, err := realPath(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

type lineBuffer struct {
	mu     sync.Mutex
	buf    strings.Builder
	onLog  func(stream, text string)
	stream string
	limit  int
}

func (w *lineBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.limit == 0 {
		w.limit = 512 * 1024
	}
	if w.buf.Len() < w.limit {
		remain := w.limit - w.buf.Len()
		if len(p) > remain {
			w.buf.Write(p[:remain])
		} else {
			w.buf.Write(p)
		}
	}
	if w.onLog != nil {
		sc := bufio.NewScanner(strings.NewReader(string(p)))
		sc.Buffer(make([]byte, 4096), 1024*1024)
		for sc.Scan() {
			w.onLog(w.stream, sc.Text())
		}
	}
	return len(p), nil
}

func (w *lineBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *lineBuffer) flush() {}
