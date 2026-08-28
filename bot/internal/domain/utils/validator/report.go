package validator

import (
	"time"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/location"
)

// ReportDate checks that the date is in the DD.MM.YYYY format
func ReportDate(date string, _ map[string]interface{}) bool {
	const layout = "02.01.2006"

	_, err := time.ParseInLocation(layout, date, location.Location())
	return err == nil
}

// ReportEndDate checks that the end date is in the DD.MM.YYYY format
// and is not earlier than the start date passed in params["fromDate"]
func ReportEndDate(end string, params map[string]interface{}) bool {
	const layout = "02.01.2006"

	fromDateStr, ok := params["fromDate"].(string)
	if !ok {
		return false
	}
	fromDate, err := time.ParseInLocation(layout, fromDateStr, location.Location())
	if err != nil {
		return false
	}

	endDate, err := time.ParseInLocation(layout, end, location.Location())
	if err != nil {
		return false
	}

	return !endDate.Before(fromDate)
}
