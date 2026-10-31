package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type runtimeInfo struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	Data  string `json:"data_dir"`
}

func main() {
	dataDir := flag.String("data", os.Getenv("MYAGENT_DATA_DIR"), "data directory")
	flag.Parse()
	if *dataDir == "" {
		home, _ := os.UserHomeDir()
		*dataDir = filepath.Join(home, "Library", "Application Support", "MyAgent")
	}
	info, err := loadRuntime(*dataDir)
	if err != nil {
		fail("cannot find a running MyAgent: %v", err)
	}
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}
	cli := &client{base: info.URL, token: info.Token}
	switch args[0] {
	case "run":
		cmdRun(cli, args[1:])
	case "workflows", "ls":
		cmdWorkflows(cli)
	case "runs":
		cmdRuns(cli, args[1:])
	case "status":
		cmdStatus(cli, args[1:])
	case "cancel":
		cmdCancel(cli, args[1:])
	case "tools":
		cmdTools(cli)
	case "open":
		fmt.Println(info.URL)
	default:
		usage()
	}
}

type client struct {
	base  string
	token string
}

func (c *client) do(method, path string, body any) (map[string]any, error) {
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	hc := &http.Client{Timeout: 120 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if e, ok := out["error"].(string); ok {
			msg = e
		}
		return out, fmt.Errorf("%s", msg)
	}
	return out, nil
}

func cmdRun(c *client, args []string) {
	if len(args) == 0 {
		fail("usage: agentctl run <workflow> [--input k=v ...]")
	}
	name := args[0]
	inputs := map[string]any{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--input" && i+1 < len(args) {
			i++
			parts := strings.SplitN(args[i], "=", 2)
			if len(parts) == 2 {
				inputs[parts[0]] = coerce(parts[1])
			}
			continue
		}
		if strings.HasPrefix(a, "--input=") {
			parts := strings.SplitN(strings.TrimPrefix(a, "--input="), "=", 2)
			if len(parts) == 2 {
				inputs[parts[0]] = coerce(parts[1])
			}
		}
	}
	wf, err := findWorkflow(c, name)
	if err != nil {
		fail("%v", err)
	}
	res, err := c.do(
		"POST",
		"/api/workflows/"+str(wf["id"])+"/run",
		map[string]any{"inputs": inputs, "source": "cli"},
	)
	if err != nil {
		fail("%v", err)
	}
	run, _ := res["run"].(map[string]any)
	fmt.Printf("run %s (%s)\n", str(run["id"]), str(run["status"]))
	fmt.Printf("open %s/#/runs/%s\n", c.base, str(run["id"]))
}

func cmdWorkflows(c *client) {
	res, err := c.do("GET", "/api/workflows", nil)
	if err != nil {
		fail("%v", err)
	}
	list, _ := res["workflows"].([]any)
	fmt.Printf("%-28s %-8s %s\n", "NAME", "VERSION", "STATUS")
	for _, item := range list {
		w, _ := item.(map[string]any)
		ver, _ := w["version"].(map[string]any)
		fmt.Printf("%-28s %-8v %s\n", str(w["name"]), ver["version"], str(ver["status"]))
	}
}

func cmdRuns(c *client, args []string) {
	limit := "20"
	for i := 0; i < len(args); i++ {
		if args[i] == "--limit" && i+1 < len(args) {
			limit = args[i+1]
			i++
		}
	}
	res, err := c.do("GET", "/api/runs?limit="+limit, nil)
	if err != nil {
		fail("%v", err)
	}
	runs, _ := res["runs"].([]any)
	fmt.Printf("%-38s %-20s %-12s %-10s %s\n", "RUN", "TARGET", "STATUS", "SOURCE", "COST")
	for _, item := range runs {
		r, _ := item.(map[string]any)
		fmt.Printf(
			"%-38s %-20s %-12s %-10s $%.4f\n",
			str(r["id"]),
			str(r["target_name"]),
			str(r["status"]),
			str(r["source"]),
			num(r["cost_micro"])/1e6,
		)
	}
}

func cmdStatus(c *client, args []string) {
	if len(args) == 0 {
		fail("usage: agentctl status <run-id>")
	}
	res, err := c.do("GET", "/api/runs/"+args[0], nil)
	if err != nil {
		fail("%v", err)
	}
	run, _ := res["run"].(map[string]any)
	fmt.Printf("%s  %s  %s\n", str(run["id"]), str(run["target_name"]), str(run["status"]))
	if e := str(run["error"]); e != "" {
		fmt.Println("error:", e)
	}
	steps, _ := run["steps"].([]any)
	for _, s := range steps {
		sm, _ := s.(map[string]any)
		fmt.Printf(
			"  %-20s attempt=%v %-12s %s\n",
			str(sm["step_id"]),
			sm["attempt"],
			str(sm["status"]),
			str(sm["error"]),
		)
	}
}

func cmdCancel(c *client, args []string) {
	if len(args) == 0 {
		fail("usage: agentctl cancel <run-id>")
	}
	_, err := c.do("POST", "/api/runs/"+args[0]+"/cancel", nil)
	if err != nil {
		fail("%v", err)
	}
	fmt.Println("cancelled")
}

func cmdTools(c *client) {
	res, err := c.do("GET", "/api/tools", nil)
	if err != nil {
		fail("%v", err)
	}
	list, _ := res["tools"].([]any)
	fmt.Printf("%-28s %-10s %-12s %-10s %s\n", "NAME", "KIND", "VERSION", "STATUS", "SIDE EFFECT")
	for _, item := range list {
		t, _ := item.(map[string]any)
		ver, _ := t["version"].(map[string]any)
		fmt.Printf(
			"%-28s %-10s %-12v %-10s %s\n",
			str(t["name"]),
			str(t["kind"]),
			ver["version"],
			str(ver["status"]),
			str(t["side_effect"]),
		)
	}
}

func findWorkflow(c *client, name string) (map[string]any, error) {
	res, err := c.do("GET", "/api/workflows", nil)
	if err != nil {
		return nil, err
	}
	list, _ := res["workflows"].([]any)
	for _, item := range list {
		w, _ := item.(map[string]any)
		if str(w["name"]) == name || str(w["id"]) == name {
			return w, nil
		}
	}
	return nil, fmt.Errorf("workflow %q not found", name)
}

func loadRuntime(dataDir string) (runtimeInfo, error) {
	var info runtimeInfo
	raw, err := os.ReadFile(filepath.Join(dataDir, "api.json"))
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(raw, &info)
	return info, err
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func num(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func coerce(s string) any {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		switch v.(type) {
		case float64, bool, map[string]any, []any:
			return v
		}
	}
	return s
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Println(`agentctl — control the local MyAgent app

Usage:
  agentctl run <workflow> [--input key=value ...]
  agentctl workflows
  agentctl runs [--limit N]
  agentctl status <run-id>
  agentctl cancel <run-id>
  agentctl tools
  agentctl open`)
	os.Exit(1)
}
