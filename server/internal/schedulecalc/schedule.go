// Package schedulecalc 纯计算自动任务的当地时间规则、预览及错过机会。
package schedulecalc

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/robfig/cron/v3"
)

// AutomaticTaskSchedule 保存可预览与持久化的当地定时规则。
type AutomaticTaskSchedule struct {
	// Kind 是一次性、常用周期或自定义 Cron 的规则类型。
	Kind constants.AutomaticScheduleKind `json:"kind"`
	// Timezone 是保存与计算规则使用的 IANA 时区。
	Timezone string `json:"timezone" validate:"max=120"`
	// OnceAt 是一次性计划的当地日期时间，格式为 YYYY-MM-DDTHH:mm。
	OnceAt string `json:"onceAt" validate:"max=16"`
	// Minute 是每小时或固定时间规则的分钟。
	Minute int `json:"minute"`
	// Hour 是每天、每周或每月规则的小时。
	Hour int `json:"hour"`
	// Weekday 是每周规则的星期，星期日为 0。
	Weekday int `json:"weekday"`
	// DayOfMonth 是每月规则的日期，不存在的日期跳过该月。
	DayOfMonth int `json:"dayOfMonth"`
	// Cron 是仅包含分、时、日、月、周的自定义表达式。
	Cron string `json:"cron" validate:"max=120"`
}

// Rule 是已校验并固定时区的规则计算器。
type Rule struct {
	// location 是持久规则的时区，与桌面当前时区无关。
	location *time.Location
	// recurring 是五字段 Cron 编译结果，一次性计划为空。
	recurring cron.Schedule
	// once 是一次性计划固定的 UTC 时间。
	once time.Time
}

// Compile 校验结构化规则并建立与前端预览共用的分钟计算器。
func Compile(input AutomaticTaskSchedule) (Rule, error) {
	location, err := time.LoadLocation(strings.TrimSpace(input.Timezone))
	if err != nil || input.Timezone == "" || input.Timezone == "Local" {
		return Rule{}, status_error.AutomaticScheduleInvalid
	}
	rule := Rule{location: location}
	if input.Kind == constants.AutomaticScheduleKindOnce {
		value, parseErr := time.ParseInLocation(constants.AutomaticTaskLocalTimeLayout, input.OnceAt, location)
		if parseErr != nil || value.Format(constants.AutomaticTaskLocalTimeLayout) != input.OnceAt {
			return Rule{}, status_error.AutomaticScheduleInvalid
		}
		rule.once = firstWallMinute(value, location).UTC()
		return rule, nil
	}
	if input.Kind != constants.AutomaticScheduleKindCron &&
		(input.Minute < 0 || input.Minute > 59 || input.Hour < 0 || input.Hour > 23) {
		return Rule{}, status_error.AutomaticScheduleInvalid
	}
	var expression string
	switch input.Kind {
	case constants.AutomaticScheduleKindHourly:
		expression = fmt.Sprintf("%d * * * *", input.Minute)
	case constants.AutomaticScheduleKindDaily:
		expression = fmt.Sprintf("%d %d * * *", input.Minute, input.Hour)
	case constants.AutomaticScheduleKindWeekly:
		if input.Weekday < 0 || input.Weekday > 6 {
			return Rule{}, status_error.AutomaticScheduleInvalid
		}
		expression = fmt.Sprintf("%d %d * * %d", input.Minute, input.Hour, input.Weekday)
	case constants.AutomaticScheduleKindMonthly:
		if input.DayOfMonth < 1 || input.DayOfMonth > 31 {
			return Rule{}, status_error.AutomaticScheduleInvalid
		}
		expression = fmt.Sprintf("%d %d %d * *", input.Minute, input.Hour, input.DayOfMonth)
	case constants.AutomaticScheduleKindCron:
		expression = strings.TrimSpace(input.Cron)
		if len(strings.Fields(expression)) != 5 {
			return Rule{}, status_error.AutomaticScheduleInvalid
		}
	default:
		return Rule{}, status_error.AutomaticScheduleInvalid
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	rule.recurring, err = parser.Parse(expression)
	if err != nil {
		return Rule{}, status_error.AutomaticScheduleInvalid
	}
	return rule, nil
}

// Next 返回严格晚于输入时间的下一次 UTC 机会；重复当地分钟只运行第一次。
func (r Rule) Next(after time.Time) time.Time {
	if !r.once.IsZero() {
		if r.once.After(after) {
			return r.once
		}
		return time.Time{}
	}
	if r.recurring == nil {
		return time.Time{}
	}
	for {
		next := r.recurring.Next(after.In(r.location))
		if next.IsZero() {
			return next
		}
		if firstWallMinute(next, r.location).Equal(next) {
			return next.UTC()
		}
		after = next
	}
}

// Preview 返回最多指定次数的未来机会，一次性计划只有一个。
func (r Rule) Preview(after time.Time, count int) []int64 {
	values := make([]int64, 0, count)
	for range count {
		next := r.Next(after)
		if next.IsZero() {
			break
		}
		values = append(values, next.UnixMilli())
		after = next
	}
	return values
}

// Missed 聚合从首个机会到截止时间的错过数量、最近机会与下次时间。
func (r Rule) Missed(first time.Time, through time.Time) (int64, time.Time, time.Time) {
	var count int64
	var last time.Time
	next := first
	for !next.IsZero() && !next.After(through) {
		count++
		last = next
		next = r.Next(next)
	}
	return count, last, next
}

// firstWallMinute 找到时区回拨时同一当地分钟最早的 UTC 身份。
func firstWallMinute(value time.Time, location *time.Location) time.Time {
	wall := value.In(location)
	_, offset := wall.Zone()
	_, previousOffset := wall.Add(-24 * time.Hour).Zone()
	if previousOffset > offset {
		earlier := wall.Add(time.Duration(offset-previousOffset) * time.Second)
		if earlier.In(location).Format(constants.AutomaticTaskLocalTimeLayout) == wall.Format(constants.AutomaticTaskLocalTimeLayout) {
			return earlier
		}
	}
	return value
}
