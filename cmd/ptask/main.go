package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

type ipcRequest struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

type ipcResponse struct {
	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
	ID    string `json:"id,omitempty"`
	Tasks []task `json:"tasks,omitempty"`
}

type task struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Prompt   string `json:"prompt"`
	Status   string `json:"status"`
}

func sendIPC(natsURL, groupID, reqType string, payload map[string]any) (*ipcResponse, error) {
	conn, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}
	defer conn.Close()

	topic := fmt.Sprintf("host.ipc.%s", groupID)
	data, err := json.Marshal(ipcRequest{Type: reqType, Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	msg, err := conn.Request(topic, data, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("ipc request: %w", err)
	}

	var resp ipcResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}

func parseArgs(args []string) map[string]string {
	result := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if len(args[i]) > 2 && args[i][:2] == "--" && i+1 < len(args) {
			result[args[i][2:]] = args[i+1]
			i++
		}
	}
	return result
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, `  ptask create --name "..." --schedule "..." --prompt "..."`)
	fmt.Fprintln(os.Stderr, "  ptask list")
	fmt.Fprintln(os.Stderr, `  ptask delete --id "..."`)
	os.Exit(1)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	groupID := os.Getenv("GROUP_ID")
	if groupID == "" {
		groupID = "default"
	}

	if len(os.Args) < 2 {
		usage()
	}

	command := os.Args[1]
	rest := os.Args[2:]

	switch command {
	case "create":
		args := parseArgs(rest)
		if args["name"] == "" || args["schedule"] == "" || args["prompt"] == "" {
			fatal("--name, --schedule, and --prompt are required")
		}
		resp, err := sendIPC(natsURL, groupID, "create_task", map[string]any{
			"name":     args["name"],
			"schedule": args["schedule"],
			"prompt":   args["prompt"],
		})
		if err != nil {
			fatal("%v", err)
		}
		if resp.Error != "" {
			fatal("%s", resp.Error)
		}
		fmt.Printf("Task created: %s\n", resp.ID)

	case "list":
		resp, err := sendIPC(natsURL, groupID, "list_tasks", map[string]any{})
		if err != nil {
			fatal("%v", err)
		}
		if resp.Error != "" {
			fatal("%s", resp.Error)
		}
		if len(resp.Tasks) == 0 {
			fmt.Println("No tasks found.")
		} else {
			for _, t := range resp.Tasks {
				fmt.Printf("  %s  %s  %s  [%s]\n", t.ID, t.Status, t.Name, t.Schedule)
			}
		}

	case "delete":
		args := parseArgs(rest)
		if args["id"] == "" {
			fatal("--id is required")
		}
		resp, err := sendIPC(natsURL, groupID, "delete_task", map[string]any{
			"id": args["id"],
		})
		if err != nil {
			fatal("%v", err)
		}
		if resp.Error != "" {
			fatal("%s", resp.Error)
		}
		fmt.Println("Task deleted.")

	default:
		fatal("unknown command: %s", command)
	}
}
