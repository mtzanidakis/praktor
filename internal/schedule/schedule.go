package schedule

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/adhocore/gronx"
)

type Schedule struct {
	Kind       string `json:"kind"`        // "cron", "interval", "once"
	CronExpr   string `json:"cron_expr"`   // Cron expression (if kind=cron)
	IntervalMs int64  `json:"interval_ms"` // Interval in ms (if kind=interval)
	AtMs       int64  `json:"at_ms"`       // Unix ms timestamp (if kind=once)
}

func ParseSchedule(raw string) (*Schedule, error) {
	var s Schedule
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func CalculateNextRun(scheduleJSON string) *time.Time {
	s, err := ParseSchedule(scheduleJSON)
	if err != nil {
		return nil
	}

	var next time.Time
	now := time.Now()

	switch s.Kind {
	case "cron":
		nextTime, err := gronx.NextTick(s.CronExpr, false)
		if err != nil {
			return nil
		}
		next = nextTime
	case "interval":
		next = now.Add(time.Duration(s.IntervalMs) * time.Millisecond)
	case "once":
		t := time.UnixMilli(s.AtMs)
		if t.After(now) {
			next = t
		} else {
			return nil
		}
	default:
		return nil
	}

	return &next
}

// FormatSchedule returns a human-readable description of a schedule JSON string.
func FormatSchedule(scheduleJSON string) string {
	s, err := ParseSchedule(scheduleJSON)
	if err != nil {
		return scheduleJSON
	}

	switch s.Kind {
	case "cron":
		if strings.HasPrefix(s.CronExpr, "@") {
			return s.CronExpr
		}
		fields := strings.Fields(s.CronExpr)
		if len(fields) == 7 {
			return "Every tick: " + s.CronExpr
		}
		if len(fields) == 6 {
			return "Once: " + s.CronExpr
		}
		return s.CronExpr
	case "interval":
		d := time.Duration(s.IntervalMs) * time.Millisecond
		switch {
		case d%time.Hour == 0 && d >= time.Hour:
			h := int(d.Hours())
			if h == 1 {
				return "Every hour"
			}
			return fmt.Sprintf("Every %d hours", h)
		case d%time.Minute == 0:
			m := int(d.Minutes())
			if m == 1 {
				return "Every minute"
			}
			return fmt.Sprintf("Every %d minutes", m)
		default:
			s := int(d.Seconds())
			return fmt.Sprintf("Every %d seconds", s)
		}
	case "once":
		t := time.UnixMilli(s.AtMs)
		return "Once at " + t.Format("Jan 2 15:04")
	default:
		return scheduleJSON
	}
}

// NormalizeSchedule detects plain cron strings and wraps them in JSON format.
// If the input is already valid JSON with a "kind" field, it is passed through.
// Otherwise, it validates as a cron expression and wraps it.
func NormalizeSchedule(raw string) (string, error) {
	raw = strings.TrimSpace(raw)

	// Try parsing as JSON first
	var s Schedule
	if err := json.Unmarshal([]byte(raw), &s); err == nil && s.Kind != "" {
		// Already valid JSON with kind field — validate and pass through
		switch s.Kind {
		case "cron":
			if !gronx.New().IsValid(s.CronExpr) {
				return "", fmt.Errorf("invalid cron expression: %s", s.CronExpr)
			}
		case "interval":
			if s.IntervalMs <= 0 {
				return "", fmt.Errorf("interval_ms must be positive")
			}
		case "once":
			if s.AtMs <= 0 {
				return "", fmt.Errorf("at_ms must be positive")
			}
		default:
			return "", fmt.Errorf("unknown schedule kind: %s", s.Kind)
		}
		return raw, nil
	}

	// Not JSON — try as plain cron expression
	if !gronx.New().IsValid(raw) {
		return "", fmt.Errorf("invalid schedule: not valid JSON or cron expression: %s", raw)
	}

	wrapped := Schedule{Kind: "cron", CronExpr: raw}
	data, err := json.Marshal(wrapped)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
