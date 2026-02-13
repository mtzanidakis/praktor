package scheduler

import (
	"fmt"
	"testing"
	"time"
)

func TestParseScheduleCron(t *testing.T) {
	raw := `{"kind":"cron","cron_expr":"0 9 * * *"}`
	s, err := ParseSchedule(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if s.Kind != "cron" {
		t.Errorf("expected kind 'cron', got '%s'", s.Kind)
	}
	if s.CronExpr != "0 9 * * *" {
		t.Errorf("expected cron expr '0 9 * * *', got '%s'", s.CronExpr)
	}
}

func TestParseScheduleInterval(t *testing.T) {
	raw := `{"kind":"interval","interval_ms":60000}`
	s, err := ParseSchedule(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if s.Kind != "interval" {
		t.Errorf("expected kind 'interval', got '%s'", s.Kind)
	}
	if s.IntervalMs != 60000 {
		t.Errorf("expected interval_ms 60000, got %d", s.IntervalMs)
	}
}

func TestCalculateNextRunCron(t *testing.T) {
	raw := `{"kind":"cron","cron_expr":"* * * * *"}`
	next := CalculateNextRun(raw)
	if next == nil {
		t.Fatal("expected next run time, got nil")
	}
	if next.Before(time.Now()) {
		t.Error("expected next run in the future")
	}
}

func TestCalculateNextRunInterval(t *testing.T) {
	raw := `{"kind":"interval","interval_ms":60000}`
	next := CalculateNextRun(raw)
	if next == nil {
		t.Fatal("expected next run time, got nil")
	}
	expected := time.Now().Add(60 * time.Second)
	diff := next.Sub(expected)
	if diff > time.Second || diff < -time.Second {
		t.Errorf("expected next run ~60s from now, got diff %v", diff)
	}
}

func TestCalculateNextRunOnce(t *testing.T) {
	future := time.Now().Add(1 * time.Hour).UnixMilli()
	raw := fmt.Sprintf(`{"kind":"once","at_ms":%d}`, future)
	next := CalculateNextRun(raw)
	if next == nil {
		t.Fatal("expected next run time, got nil")
	}

	// Past time should return nil
	past := time.Now().Add(-1 * time.Hour).UnixMilli()
	raw = fmt.Sprintf(`{"kind":"once","at_ms":%d}`, past)
	next = CalculateNextRun(raw)
	if next != nil {
		t.Error("expected nil for past once schedule")
	}
}

func TestCalculateNextRunInvalid(t *testing.T) {
	next := CalculateNextRun(`invalid json`)
	if next != nil {
		t.Error("expected nil for invalid schedule")
	}

	next = CalculateNextRun(`{"kind":"unknown"}`)
	if next != nil {
		t.Error("expected nil for unknown kind")
	}
}
