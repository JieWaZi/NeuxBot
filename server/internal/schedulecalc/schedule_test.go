package schedulecalc

import (
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func testTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestStructuredRulesAndTraditionalCron(t *testing.T) {
	cases := []struct {
		// name 是断言场景名称。
		name string
		// input 是保存的当地规则。
		input models.AutomaticTaskSchedule
		// after 是查询起点的 UTC 时间。
		after string
		// expected 是该规则的下一次 UTC 机会。
		expected string
	}{
		{"hourly", models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindHourly, Timezone: "Asia/Shanghai", Minute: 15}, "2026-09-30T00:15:00Z", "2026-09-30T01:15:00Z"},
		{"daily", models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindDaily, Timezone: "Asia/Shanghai", Hour: 9}, "2026-09-30T00:00:00Z", "2026-09-30T01:00:00Z"},
		{"weekly", models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindWeekly, Timezone: "Asia/Shanghai", Hour: 9, Weekday: 0}, "2026-09-30T00:00:00Z", "2026-10-04T01:00:00Z"},
		{"monthly skips September", models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindMonthly, Timezone: "Asia/Shanghai", Hour: 9, DayOfMonth: 31}, "2026-09-01T00:00:00Z", "2026-10-31T01:00:00Z"},
		{"every minute", models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "* * * * *"}, "2026-09-30T00:00:01Z", "2026-09-30T00:01:00Z"},
		{"day and weekday use OR", models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "0 9 1 * 1"}, "2026-09-29T00:00:00Z", "2026-10-01T09:00:00Z"},
		{"spring missing time skipped", models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindDaily, Timezone: "America/New_York", Hour: 2, Minute: 30}, "2026-03-08T00:00:00Z", "2026-03-09T06:30:00Z"},
		{"fall duplicate time skipped", models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindDaily, Timezone: "America/New_York", Hour: 1, Minute: 30}, "2026-11-01T05:30:00Z", "2026-11-02T06:30:00Z"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			rule, err := Compile(item.input)
			if err != nil {
				t.Fatal(err)
			}
			if actual := rule.Next(testTime(t, item.after)); !actual.Equal(testTime(t, item.expected)) {
				t.Fatalf("next = %s, expected %s", actual, item.expected)
			}
		})
	}
}

func TestOncePreviewAndMissedAggregation(t *testing.T) {
	rule, err := Compile(models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindOnce, Timezone: "Asia/Shanghai", OnceAt: "2026-10-01T09:00"})
	if err != nil {
		t.Fatal(err)
	}
	after := testTime(t, "2026-09-30T00:00:00Z")
	preview := rule.Preview(after, 3)
	if len(preview) != 1 || preview[0] != testTime(t, "2026-10-01T01:00:00Z").UnixMilli() {
		t.Fatalf("preview = %v", preview)
	}
	count, last, next := rule.Missed(time.UnixMilli(preview[0]), testTime(t, "2026-10-02T01:00:00Z"))
	if count != 1 || last.UnixMilli() != preview[0] || !next.IsZero() {
		t.Fatalf("missed = %d, %s, %s", count, last, next)
	}
	minute, err := Compile(models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "* * * * *"})
	if err != nil {
		t.Fatal(err)
	}
	count, last, next = minute.Missed(after, after.Add(5*time.Minute))
	if count != 6 || !last.Equal(after.Add(5*time.Minute)) || !next.Equal(after.Add(6*time.Minute)) {
		t.Fatalf("minute missed = %d, %s, %s", count, last, next)
	}
}

func TestInvalidAndNonexistentRules(t *testing.T) {
	inputs := []models.AutomaticTaskSchedule{
		{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "@reboot"},
		{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "0 * * * * *"},
		{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "CRON_TZ=Asia/Shanghai * * * * *"},
		{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "*/0 * * * *"},
		{Kind: constants.AutomaticScheduleKindDaily, Timezone: "Local", Hour: 9},
		{Kind: constants.AutomaticScheduleKindDaily, Timezone: "Invalid/Timezone", Hour: 9},
		{Kind: constants.AutomaticScheduleKindDaily, Timezone: "UTC", Hour: 24},
		{Kind: constants.AutomaticScheduleKindMonthly, Timezone: "UTC", DayOfMonth: 0},
		{Kind: constants.AutomaticScheduleKindOnce, Timezone: "America/New_York", OnceAt: "2026-03-08T02:30"},
	}
	for _, input := range inputs {
		if _, err := Compile(input); err != status_error.AutomaticScheduleInvalid {
			t.Fatalf("input %+v error = %v", input, err)
		}
	}
	rule, err := Compile(models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "0 0 30 2 *"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.Preview(testTime(t, "2026-01-01T00:00:00Z"), 3)) != 0 {
		t.Fatal("impossible date has a next occurrence")
	}
}
