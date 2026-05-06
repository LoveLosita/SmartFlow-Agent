package sv

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"gorm.io/gorm"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 50
)

func normalizePage(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = defaultPage
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func formatTimePtr(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func formatPriceText(currency string, amountCent int64) string {
	if amountCent == 0 {
		return "免费"
	}
	if strings.EqualFold(strings.TrimSpace(currency), "CNY") {
		return fmt.Sprintf("¥%.2f", float64(amountCent)/100)
	}
	return fmt.Sprintf("%s %.2f", strings.ToUpper(strings.TrimSpace(currency)), float64(amountCent)/100)
}

func stringPtrFromNonEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "duplicate entry") ||
		strings.Contains(lower, "duplicate key") ||
		strings.Contains(lower, "unique constraint") ||
		strings.Contains(lower, "unique violation") ||
		strings.Contains(lower, "error 1062")
}

func normalizeRecordNotFound(err error, fallback error) error {
	if errorsIsRecordNotFound(err) {
		return fallback
	}
	return err
}

func errorsIsRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

const tokenStoreBadRequestStatus = "40067"

func tokenStoreBadRequest(message string) respond.Response {
	return respond.Response{
		Status: tokenStoreBadRequestStatus,
		Info:   strings.TrimSpace(message),
	}
}
