package conv

import (
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/spf13/viper"
)

// DateFormat 此处定义基准学期的开始和结束日期
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
