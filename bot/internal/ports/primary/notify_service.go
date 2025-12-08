package primary

import (
	"go.uber.org/zap/zapcore"

	"github.com/Badsnus/cu-clubs-bot/bot/pkg/logger/types"
)

// NotifyService defines the interface for notification-related use cases
type NotifyService interface {
	LogHook(channelID int64, locale string, level zapcore.Level) (types.LogHook, error)
	SendClubWarning(clubID string, what interface{}, opts ...interface{}) error
	SendEventUpdate(eventID string, what interface{}, opts ...interface{}) error
	StartNotifyScheduler()
	StartClubOwnerReminderScheduler() error
	StopClubOwnerReminderScheduler()
}
