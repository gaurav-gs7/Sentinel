package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type onboardRequest struct {
	Name               string  `json:"name"`
	Team               string  `json:"team"`
	Owner              string  `json:"owner"`
	Tier               string  `json:"tier"`
	Language           string  `json:"language"`
	Namespace          string  `json:"namespace,omitempty"`
	Environment        string  `json:"environment"`
	SLO                string  `json:"slo"`
	SLOAvailability    float64 `json:"sloAvailability,omitempty"`
	SLOLatencyP95      string  `json:"sloLatencyP95,omitempty"`
	SLOErrorRate       float64 `json:"sloErrorRate,omitempty"`
	DeploymentStrategy string  `json:"deploymentStrategy"`
	RepoURL            string  `json:"repoUrl,omitempty"`
	Repository         string  `json:"repository,omitempty"`
	Pager              string  `json:"pager,omitempty"`
	RunbookURL         string  `json:"runbookUrl,omitempty"`
	DashboardURL       string  `json:"dashboardUrl,omitempty"`
}

type healthGateRequest struct {
	P99LatencyMs float64 `json:"p99LatencyMs"`
	ErrorRate    float64 `json:"errorRate"`
	SuccessCount int     `json:"successCount"`
}

type incidentRequest struct {
	Service    string  `json:"service"`
	Severity   string  `json:"severity"`
	AlertName  string  `json:"alertName"`
	Title      string  `json:"title"`
	ErrorRate  float64 `json:"errorRate"`
	LatencyP95 float64 `json:"latencyP95Ms"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "service":
		if err := service(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "onboard":
		if err := onboard(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "list":
		if err := get("/api/v1/services"); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := serviceGet(os.Args[2:], "status", "/api/v1/services/%s/status"); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "deployments":
		if err := serviceGet(os.Args[2:], "deployments", "/api/v1/services/%s/deployments"); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "rollback":
		if err := servicePost(os.Args[2:], "rollback", "/api/v1/services/%s/rollback", nil); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "health-gate":
		if err := healthGate(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "check":
		if err := serviceGetByArg(os.Args[2:], "check", "/api/v1/services/%s/readiness"); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "score":
		if err := serviceGetByArg(os.Args[2:], "score", "/api/v1/services/%s/score"); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "slo":
		if err := slo(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "rollout":
		if err := rollout(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "incident":
		if err := incident(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	case "workflow", "workflows":
		if err := workflow(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: %v\n", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func service(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("service subcommand is required")
	}
	switch args[0] {
	case "init":
		return onboardInit(args[1:])
	case "register":
		return fmt.Errorf("service register is intentionally file-driven in GitOps; use service init for this local MVP")
	case "list":
		return get("/api/v1/services")
	case "describe":
		return serviceGetByArg(args[1:], "service describe", "/api/v1/services/%s")
	default:
		return fmt.Errorf("unknown service subcommand %q", args[0])
	}
}

func onboard(args []string) error {
	fs := flag.NewFlagSet("onboard", flag.ExitOnError)
	req := onboardRequest{}
	fs.StringVar(&req.Name, "name", "", "service name")
	fs.StringVar(&req.Language, "language", "go", "service language: go or python")
	fs.StringVar(&req.Team, "team", "", "owning team")
	fs.StringVar(&req.Owner, "owner", "", "service owner")
	fs.StringVar(&req.Tier, "tier", "medium", "service tier: critical, high, medium, low")
	fs.StringVar(&req.Namespace, "namespace", "", "kubernetes namespace")
	fs.StringVar(&req.Environment, "env", "staging", "environment")
	fs.StringVar(&req.SLO, "slo", "99.9", "availability SLO target")
	fs.StringVar(&req.SLOLatencyP95, "slo-latency-p95", "300ms", "p95 latency SLO")
	fs.Float64Var(&req.SLOErrorRate, "slo-error-rate", 1, "max error-rate percentage")
	fs.StringVar(&req.DeploymentStrategy, "strategy", "canary", "deployment strategy")
	fs.StringVar(&req.RepoURL, "repo-url", "", "service repository URL")
	fs.StringVar(&req.Repository, "repository", "", "service repository URL or owner/repo")
	fs.StringVar(&req.Pager, "pager", "", "pager/on-call rotation")
	fs.StringVar(&req.RunbookURL, "runbook", "", "runbook URL")
	fs.StringVar(&req.DashboardURL, "dashboard", "", "dashboard URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if req.Name == "" || req.Owner == "" {
		return fmt.Errorf("--name and --owner are required")
	}
	if req.Team == "" {
		req.Team = req.Owner
	}

	return post("/api/v1/services", req)
}

func onboardInit(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("service name is required")
	}
	name := args[0]
	reqArgs := append([]string{"--name", name}, args[1:]...)
	normalized := make([]string, 0, len(reqArgs))
	for i := 0; i < len(reqArgs); i++ {
		switch reqArgs[i] {
		case "--slo-availability":
			normalized = append(normalized, "--slo")
		case "--deployment":
			normalized = append(normalized, "--strategy")
		default:
			normalized = append(normalized, reqArgs[i])
		}
	}
	return onboard(normalized)
}

func serviceGet(args []string, command, path string) error {
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	name := fs.String("name", "", "service name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("--name is required")
	}
	return get(fmt.Sprintf(path, *name))
}

func serviceGetByArg(args []string, command, path string) error {
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	nameFlag := fs.String("name", "", "service name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := *nameFlag
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		return fmt.Errorf("service name is required")
	}
	return get(fmt.Sprintf(path, name))
}

func servicePost(args []string, command, path string, payload any) error {
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	name := fs.String("name", "", "service name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("--name is required")
	}
	return post(fmt.Sprintf(path, *name), payload)
}

func healthGate(args []string) error {
	fs := flag.NewFlagSet("health-gate", flag.ExitOnError)
	name := fs.String("name", "", "service name")
	req := healthGateRequest{}
	fs.Float64Var(&req.P99LatencyMs, "p99-latency-ms", 0, "observed p99 latency in milliseconds")
	fs.Float64Var(&req.ErrorRate, "error-rate", 0, "observed error rate percentage")
	fs.IntVar(&req.SuccessCount, "success-count", 100, "successful request sample count")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("--name is required")
	}
	return post(fmt.Sprintf("/api/v1/services/%s/health-gate", *name), req)
}

func slo(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("slo subcommand is required")
	}
	switch args[0] {
	case "status":
		return serviceGetByArg(args[1:], "slo status", "/api/v1/services/%s/slos")
	default:
		return fmt.Errorf("unknown slo subcommand %q", args[0])
	}
}

func rollout(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("rollout subcommand is required")
	}
	switch args[0] {
	case "status":
		return serviceGetByArg(args[1:], "rollout status", "/api/v1/services/%s/status")
	case "rollback":
		return servicePostByArg(args[1:], "rollout rollback", "/api/v1/services/%s/rollback", nil)
	case "start", "pause":
		return fmt.Errorf("rollout %s is represented by the local CI/CD runner in this MVP", args[0])
	default:
		return fmt.Errorf("unknown rollout subcommand %q", args[0])
	}
}

func incident(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("incident subcommand is required")
	}
	switch args[0] {
	case "create":
		return incidentCreate(args[1:])
	case "list":
		return get("/api/v1/incidents")
	case "show":
		id, err := firstArg(args[1:], "incident id")
		if err != nil {
			return err
		}
		return get("/api/v1/incidents/" + id)
	case "timeline":
		id, err := firstArg(args[1:], "incident id")
		if err != nil {
			return err
		}
		return get("/api/v1/incidents/" + id + "/timeline")
	case "postmortem":
		id, err := firstArg(args[1:], "incident id")
		if err != nil {
			return err
		}
		return get("/api/v1/incidents/" + id + "/postmortem")
	default:
		return fmt.Errorf("unknown incident subcommand %q", args[0])
	}
}

func incidentCreate(args []string) error {
	fs := flag.NewFlagSet("incident create", flag.ExitOnError)
	req := incidentRequest{}
	fs.StringVar(&req.Service, "service", "", "service name")
	fs.StringVar(&req.Severity, "severity", "critical", "incident severity")
	fs.StringVar(&req.AlertName, "alert", "HighErrorRate", "alert name")
	fs.StringVar(&req.Title, "title", "High error rate during canary", "incident title")
	fs.Float64Var(&req.ErrorRate, "error-rate", 8.2, "observed error rate percentage")
	fs.Float64Var(&req.LatencyP95, "latency-p95-ms", 850, "observed p95 latency in milliseconds")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if req.Service == "" && fs.NArg() > 0 {
		req.Service = fs.Arg(0)
	}
	if req.Service == "" {
		return fmt.Errorf("service is required")
	}
	return post("/api/v1/incidents", req)
}

func workflow(args []string) error {
	if len(args) == 0 {
		return get("/api/v1/workflows")
	}
	switch args[0] {
	case "list":
		return get("/api/v1/workflows")
	case "show":
		id, err := firstArg(args[1:], "workflow id")
		if err != nil {
			return err
		}
		return get("/api/v1/workflows/" + id)
	default:
		return fmt.Errorf("unknown workflow subcommand %q", args[0])
	}
}

func servicePostByArg(args []string, command, path string, payload any) error {
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	nameFlag := fs.String("name", "", "service name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := *nameFlag
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		return fmt.Errorf("service name is required")
	}
	return post(fmt.Sprintf(path, name), payload)
}

func firstArg(args []string, label string) (string, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return args[0], nil
}

func get(path string) error {
	return call(http.MethodGet, path, nil)
}

func post(path string, payload any) error {
	return call(http.MethodPost, path, payload)
}

func call(method, path string, payload any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, strings.TrimRight(baseURL(), "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := os.Getenv("SENTINEL_API_TOKEN"); token != "" {
		req.Header.Set("X-Sentinel-Token", token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("api returned %s: %s", resp.Status, string(responseBody))
	}
	printJSON(responseBody)
	return nil
}

func printJSON(raw []byte) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		fmt.Println(string(raw))
		return
	}
	formatted, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Println(string(raw))
		return
	}
	fmt.Println(string(formatted))
}

func baseURL() string {
	value := os.Getenv("SENTINEL_API_URL")
	if value == "" {
		return "http://127.0.0.1:8080"
	}
	return value
}

func usage() {
	fmt.Fprintf(os.Stderr, `Sentinel CLI

Usage:
  sentinel service init payments-api --owner payments-platform --team platform --tier critical --language go --slo-availability 99.9 --slo-latency-p95 300ms --deployment canary --pager payments-oncall
  sentinel onboard --name payments-api --language go --team platform --owner gaurav --env staging --slo 99.9 --strategy canary
  sentinel service list
  sentinel service describe payments-api
  sentinel check payments-api
  sentinel score payments-api
  sentinel slo status payments-api
  sentinel rollout status payments-api
  sentinel rollout rollback payments-api
  sentinel incident create --service payments-api --alert HighErrorRate --error-rate 8.2 --latency-p95-ms 850
  sentinel incident list
  sentinel incident show inc-20260516103000
  sentinel workflows list
  sentinel workflows show wf-...
  sentinel list
  sentinel status --name payments-api
  sentinel health-gate --name payments-api --p99-latency-ms 450 --error-rate 0.2 --success-count 100
  sentinel rollback --name payments-api
  sentinel deployments --name payments-api
`)
}
