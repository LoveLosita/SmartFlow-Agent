package agent

import (
	"testing"
	"time"
)

func TestParseOptionalDeadlineWithNow_Absolute(t *testing.T) {
	loc := quickNoteLocation()
	now := time.Date(2026, 3, 12, 10, 15, 0, 0, loc)

	deadline, err := parseOptionalDeadlineWithNow("2026-03-20 18:30", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deadline == nil {
		t.Fatalf("deadline should not be nil")
	}

	want := time.Date(2026, 3, 20, 18, 30, 0, 0, loc)
	if !deadline.Equal(want) {
		t.Fatalf("unexpected deadline, got=%s want=%s", deadline.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestParseOptionalDeadlineWithNow_RelativeTomorrowWithoutClock(t *testing.T) {
	loc := quickNoteLocation()
	now := time.Date(2026, 3, 12, 10, 15, 0, 0, loc)

	deadline, err := parseOptionalDeadlineWithNow("明天交计网实验报告", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deadline == nil {
		t.Fatalf("deadline should not be nil")
	}

	want := time.Date(2026, 3, 13, 23, 59, 0, 0, loc)
	if !deadline.Equal(want) {
		t.Fatalf("unexpected deadline, got=%s want=%s", deadline.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestParseOptionalDeadlineWithNow_RelativeTomorrowWithClock(t *testing.T) {
	loc := quickNoteLocation()
	now := time.Date(2026, 3, 12, 10, 15, 0, 0, loc)

	deadline, err := parseOptionalDeadlineWithNow("明天下午3点交计网实验报告", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deadline == nil {
		t.Fatalf("deadline should not be nil")
	}

	want := time.Date(2026, 3, 13, 15, 0, 0, 0, loc)
	if !deadline.Equal(want) {
		t.Fatalf("unexpected deadline, got=%s want=%s", deadline.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestParseOptionalDeadlineWithNow_RelativeWeekday(t *testing.T) {
	loc := quickNoteLocation()
	now := time.Date(2026, 3, 12, 10, 15, 0, 0, loc) // 周四

	deadline, err := parseOptionalDeadlineWithNow("下周一上午9点开组会", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deadline == nil {
		t.Fatalf("deadline should not be nil")
	}

	want := time.Date(2026, 3, 16, 9, 0, 0, 0, loc)
	if !deadline.Equal(want) {
		t.Fatalf("unexpected deadline, got=%s want=%s", deadline.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestParseOptionalDeadlineFromUserInput_NoHint(t *testing.T) {
	loc := quickNoteLocation()
	now := time.Date(2026, 3, 12, 10, 15, 0, 0, loc)

	deadline, hasHint, err := parseOptionalDeadlineFromUserInput("帮我记一下要复习计网", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasHint {
		t.Fatalf("expected no time hint")
	}
	if deadline != nil {
		t.Fatalf("deadline should be nil when no time hint")
	}
}

func TestParseOptionalDeadlineFromUserInput_InvalidDate(t *testing.T) {
	loc := quickNoteLocation()
	now := time.Date(2026, 3, 12, 10, 15, 0, 0, loc)

	deadline, hasHint, err := parseOptionalDeadlineFromUserInput("2026-13-45 25:99 交实验", now)
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if !hasHint {
		t.Fatalf("expected hasHint=true")
	}
	if deadline != nil {
		t.Fatalf("deadline should be nil for invalid date")
	}
}

func TestParseOptionalDeadlineWithNow_Invalid(t *testing.T) {
	loc := quickNoteLocation()
	now := time.Date(2026, 3, 12, 10, 15, 0, 0, loc)

	deadline, err := parseOptionalDeadlineWithNow("记得尽快处理", now)
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if deadline != nil {
		t.Fatalf("deadline should be nil for invalid input")
	}
}
