package scheduler

import (
	"encoding/json"
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
