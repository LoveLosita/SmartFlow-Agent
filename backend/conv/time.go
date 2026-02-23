package conv

import (
	"errors"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/spf13/viper"
)

// DateFormat 此处定义一个全局常量，确保在整个代码中使用统一的日期格式解析和格式化
const DateFormat = "2006-01-02"

// RealDateToRelativeDate 将绝对日期转换为相对日期（格式: "week-day"）
func RealDateToRelativeDate(realDate string) (int, int, error) {
	SemesterStartDate := viper.GetString("time.semesterStartDate") // 从配置文件中读取学期开始日期
	SemesterEndDate := viper.GetString("time.semesterEndDate")     // 从配置文件中读取学期结束日期
	t, err := time.Parse(DateFormat, realDate)
	if err != nil {
		return 0, 0, err
	}
	start, err := time.Parse(DateFormat, SemesterStartDate)
	if err != nil {
		return 0, 0, err
	}
	end, err := time.Parse(DateFormat, SemesterEndDate)
	if err != nil {
		return 0, 0, err
	}
	// 边界校验：日期必须在学期范围内
	if t.Before(start) || t.After(end) {
		return 0, 0, errors.New("日期超出学期范围")
	}
	// 计算天数差值（注意：24小时为一个基准天）
	days := int(t.Sub(start).Hours() / 24)
	// 计算周数和星期
	// 假设 SemesterStartDate 对应第 1 周，周 1
	week := (days / 7) + 1
	dayOfWeek := (days % 7) + 1
	return week, dayOfWeek, nil
}

// RelativeDateToRealDate 将相对日期转换为绝对日期（输入格式: "week-day"）
func RelativeDateToRealDate(week, dayOfWeek int) (string, error) {
	SemesterStartDate := viper.GetString("time.semesterStartDate") // 从配置文件中读取学期开始日期
	SemesterEndDate := viper.GetString("time.semesterEndDate")     // 从配置文件中读取学期结束日期
	start, _ := time.Parse(DateFormat, SemesterStartDate)
	// 核心转换逻辑：(周-1)*7 + (天-1)
	offsetDays := (week-1)*7 + (dayOfWeek - 1)
	targetDate := start.AddDate(0, 0, offsetDays)
	// 校验计算出的日期是否超出学期结束日期
	end, _ := time.Parse(DateFormat, SemesterEndDate)
	if targetDate.After(end) {
		return "", respond.TimeOutOfRangeOfThisSemester
	}
	return targetDate.Format(DateFormat), nil
}

type SectionTime struct {
	Start string // 第一个开始
	End   string // 第一个结束
}

var sectionTimeMap2 = map[int]SectionTime{
	1:  {Start: "08:00", End: "08:45"},
	2:  {Start: "08:55", End: "09:40"},
	3:  {Start: "10:15", End: "11:00"},
	4:  {Start: "11:10", End: "11:55"},
	5:  {Start: "14:00", End: "14:45"},
	6:  {Start: "14:55", End: "15:40"},
	7:  {Start: "16:15", End: "17:00"},
	8:  {Start: "17:10", End: "17:55"},
	9:  {Start: "19:00", End: "19:45"},
	10: {Start: "19:55", End: "20:40"},
	11: {Start: "20:50", End: "21:35"},
	12: {Start: "21:45", End: "22:30"},
}

func RelativeTimeToRealTime(week, dayOfWeek, startSection, endSection int) (time.Time, time.Time, error) {
	// 1. 安全校验
	if startSection > endSection {
		return time.Time{}, time.Time{}, respond.InvalidSectionRange
	}

	startTimeInfo, okStart := sectionTimeMap2[startSection]
	endTimeInfo, okEnd := sectionTimeMap2[endSection]
	if !okStart || !okEnd {
		return time.Time{}, time.Time{}, respond.InvalidSectionNumber
	}

	if week < 1 || dayOfWeek < 1 || dayOfWeek > 7 {
		return time.Time{}, time.Time{}, respond.InvalidWeekOrDayOfWeek
	}

	// 2. 计算目标日期
	// 偏移天数 = (周数-1)*7 + (周几-1)
	daysOffset := (week-1)*7 + (dayOfWeek - 1)
	TermStartDate := viper.GetString("time.semesterStartDate") // 从配置文件中读取学期开始日期
	baseDate, _ := time.Parse("2006-01-02", TermStartDate)
	targetDate := baseDate.AddDate(0, 0, daysOffset)
	dateStr := targetDate.Format("2006-01-02")

	// 3. 锁定时区 (Asia/Shanghai)
	timeZone := viper.GetString("time.zone") // 从配置文件中读取时区
	loc, _ := time.LoadLocation(timeZone)

	// 拼接：起始节次的 Start 和 结束节次的 End
	startFullStr := fmt.Sprintf("%s %s", dateStr, startTimeInfo.Start)
	endFullStr := fmt.Sprintf("%s %s", dateStr, endTimeInfo.End)

	startTime, err := time.ParseInLocation("2006-01-02 15:04", startFullStr, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	endTime, err := time.ParseInLocation("2006-01-02 15:04", endFullStr, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return startTime, endTime, nil
}

func CalculateFirstDayOfWeek(date time.Time) time.Time {
	// 计算当前日期是周几（0-6，0表示周日）
	weekday := int(date.Weekday())
	if weekday == 0 {
		weekday = 7 // 将周日调整为7，方便计算
	}
	// 计算距离周一的天数偏移
	offset := weekday - 1
	// 计算本周一的日期
	firstDayOfWeek := date.AddDate(0, 0, -offset)
	return firstDayOfWeek
}

func CalculateLastDayOfWeek(date time.Time) time.Time {
	// 计算当前日期是周几（0-6，0表示周日）
	weekday := int(date.Weekday())
	if weekday == 0 {
		weekday = 7 // 将周日调整为7，方便计算
	}
	// 计算距离周日的天数偏移
	offset := 7 - weekday
	// 计算本周日的日期
	lastDayOfWeek := date.AddDate(0, 0, offset)
	return lastDayOfWeek
}
