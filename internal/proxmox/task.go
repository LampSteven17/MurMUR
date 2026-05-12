package proxmox

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TaskStatus is /nodes/{node}/tasks/{upid}/status.
type TaskStatus struct {
	Status     string `json:"status"`     // running | stopped
	ExitStatus string `json:"exitstatus"` // "OK" on success; otherwise the error string
	PID        int    `json:"pid,omitempty"`
	PStart     int64  `json:"pstart,omitempty"`
	StartTime  int64  `json:"starttime,omitempty"`
	Type       string `json:"type,omitempty"`
	User       string `json:"user,omitempty"`
	UPID       string `json:"upid,omitempty"`
	Node       string `json:"node,omitempty"`
	ID         string `json:"id,omitempty"`
}

// Done reports whether the task has finished executing.
func (t TaskStatus) Done() bool { return t.Status == "stopped" }

// OK reports whether the task finished successfully.
func (t TaskStatus) OK() bool { return t.Done() && t.ExitStatus == "OK" }

// parseUPIDNode extracts the node name from a UPID string.
// UPID format: "UPID:<node>:<pid_hex>:<pstart_hex>:<starttime_hex>:<dtype>:<id>:<user>:".
func parseUPIDNode(upid string) (string, error) {
	parts := strings.SplitN(upid, ":", 3)
	if len(parts) < 3 || parts[0] != "UPID" || parts[1] == "" {
		return "", fmt.Errorf("proxmox: malformed UPID %q", upid)
	}
	return parts[1], nil
}

// GetTaskStatus returns the current state of a task. Pass the UPID as
// returned by any mutating endpoint; the node is parsed from it.
func (c *Client) GetTaskStatus(ctx context.Context, upid string) (TaskStatus, error) {
	node, err := parseUPIDNode(upid)
	if err != nil {
		return TaskStatus{}, err
	}
	var s TaskStatus
	if err := c.GetJSON(ctx, "/nodes/"+node+"/tasks/"+upid+"/status", &s); err != nil {
		return TaskStatus{}, err
	}
	return s, nil
}

// WaitForTask polls a task until it finishes or the context is cancelled.
// On success returns the final TaskStatus. On non-OK exit returns a TaskError
// so callers can distinguish task failure from transport failure.
func (c *Client) WaitForTask(ctx context.Context, upid string, poll time.Duration) (TaskStatus, error) {
	if poll <= 0 {
		poll = 2 * time.Second
	}
	for {
		s, err := c.GetTaskStatus(ctx, upid)
		if err != nil {
			return s, err
		}
		if s.Done() {
			if !s.OK() {
				return s, &TaskError{UPID: upid, ExitStatus: s.ExitStatus}
			}
			return s, nil
		}
		select {
		case <-ctx.Done():
			return s, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// TaskError is returned by WaitForTask when the ProxMox task itself fails.
type TaskError struct {
	UPID       string
	ExitStatus string
}

func (e *TaskError) Error() string {
	return fmt.Sprintf("proxmox: task %s failed: %s", e.UPID, e.ExitStatus)
}
