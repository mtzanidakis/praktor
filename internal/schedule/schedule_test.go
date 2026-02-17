package schedule

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

func TestNormalizeSchedulePlainCron(t *testing.T) {
	result, err := NormalizeSchedule("0 9 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, err := ParseSchedule(result)
	if err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if s.Kind != "cron" {
		t.Errorf("expected kind 'cron', got '%s'", s.Kind)
	}
	if s.CronExpr != "0 9 * * *" {
		t.Errorf("expected cron_expr '0 9 * * *', got '%s'", s.CronExpr)
	}
}

func TestNormalizeScheduleEveryMinute(t *testing.T) {
	result, err := NormalizeSchedule("* * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, err := ParseSchedule(result)
	if err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if s.Kind != "cron" || s.CronExpr != "* * * * *" {
		t.Errorf("unexpected result: %+v", s)
	}
}

func TestNormalizeSchedulePassthroughJSON(t *testing.T) {
	input := `{"kind":"cron","cron_expr":"0 9 * * *"}`
	result, err := NormalizeSchedule(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != input {
		t.Errorf("expected passthrough, got '%s'", result)
	}
}

func TestNormalizeScheduleIntervalJSON(t *testing.T) {
	input := `{"kind":"interval","interval_ms":300000}`
	result, err := NormalizeSchedule(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != input {
		t.Errorf("expected passthrough, got '%s'", result)
	}
}

func TestNormalizeScheduleInvalid(t *testing.T) {
	_, err := NormalizeSchedule("not a cron")
	if err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestNormalizeScheduleInvalidCronInJSON(t *testing.T) {
	_, err := NormalizeSchedule(`{"kind":"cron","cron_expr":"bad"}`)
	if err == nil {
		t.Error("expected error for invalid cron in JSON")
	}
}

func TestNormalizeScheduleUnknownKind(t *testing.T) {
	_, err := NormalizeSchedule(`{"kind":"bogus"}`)
	if err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestNormalizeScheduleTag(t *testing.T) {
	tags := []string{"@daily", "@hourly", "@weekly", "@monthly", "@yearly", "@5minutes", "@10minutes", "@15minutes", "@30minutes"}
	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			result, err := NormalizeSchedule(tag)
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tag, err)
			}
			s, err := ParseSchedule(result)
			if err != nil {
				t.Fatalf("result not valid JSON: %v", err)
			}
			if s.Kind != "cron" || s.CronExpr != tag {
				t.Errorf("expected cron with expr %s, got %+v", tag, s)
			}
		})
	}
}

func TestNormalizeScheduleWithYear(t *testing.T) {
	result, err := NormalizeSchedule("20 10 17 2 * 2026")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, err := ParseSchedule(result)
	if err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if s.Kind != "cron" || s.CronExpr != "20 10 17 2 * 2026" {
		t.Errorf("expected 6-field cron, got %+v", s)
	}
}

func TestNormalizeScheduleWithAbbreviations(t *testing.T) {
	cases := []string{"0 9 * JAN MON", "0 9 * * MON-FRI", "0 0 1 JAN *"}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			result, err := NormalizeSchedule(expr)
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", expr, err)
			}
			s, err := ParseSchedule(result)
			if err != nil {
				t.Fatalf("result not valid JSON: %v", err)
			}
			if s.Kind != "cron" || s.CronExpr != expr {
				t.Errorf("expected cron with expr %s, got %+v", expr, s)
			}
		})
	}
}

func TestCalculateNextRunWithYear(t *testing.T) {
	// Far future year — should have a next run
	futureExpr := fmt.Sprintf(`{"kind":"cron","cron_expr":"0 0 1 1 * %d"}`, time.Now().Year()+2)
	next := CalculateNextRun(futureExpr)
	if next == nil {
		t.Error("expected next run for future year expression, got nil")
	}

	// Past year — should return nil
	pastExpr := fmt.Sprintf(`{"kind":"cron","cron_expr":"0 0 1 1 * %d"}`, time.Now().Year()-1)
	next = CalculateNextRun(pastExpr)
	if next != nil {
		t.Error("expected nil for past year expression")
	}
}

func TestFormatScheduleTag(t *testing.T) {
	raw := `{"kind":"cron","cron_expr":"@daily"}`
	result := FormatSchedule(raw)
	if result != "@daily" {
		t.Errorf("expected '@daily', got '%s'", result)
	}
}

func TestFormatScheduleWithYear(t *testing.T) {
	raw := `{"kind":"cron","cron_expr":"20 10 17 2 * 2026"}`
	result := FormatSchedule(raw)
	if result != "Once: 20 10 17 2 * 2026" {
		t.Errorf("expected 'Once: 20 10 17 2 * 2026', got '%s'", result)
	}
}

func TestNormalizeScheduleWithWhitespace(t *testing.T) {
	result, err := NormalizeSchedule("  */5 * * * *  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, err := ParseSchedule(result)
	if err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if s.CronExpr != "*/5 * * * *" {
		t.Errorf("expected trimmed cron, got '%s'", s.CronExpr)
	}
}
