package service

import (
	"context"
	"strings"
	"time"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/location"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/primary"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/secondary"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap/zapcore"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/layout"

	"github.com/Badsnus/cu-clubs-bot/bot/pkg/logger/types"
)

type NotifyService struct {
	clubOwnerService     primary.ClubOwnerService
	eventRepo            secondary.EventRepository
	notificationRepo     secondary.NotificationRepository
	eventParticipantRepo secondary.EventParticipantRepository

	bot    *tele.Bot
	layout *layout.Layout
	logger *types.Logger

	cron *cron.Cron
}

func NewNotifyService(
	bot *tele.Bot,
	layout *layout.Layout,
	logger *types.Logger,
	clubOwnerService primary.ClubOwnerService,
	eventRepo secondary.EventRepository,
	notificationRepo secondary.NotificationRepository,
	notifyEventParticipantRepo secondary.EventParticipantRepository,
) *NotifyService {
	return &NotifyService{
		clubOwnerService:     clubOwnerService,
		eventRepo:            eventRepo,
		notificationRepo:     notificationRepo,
		eventParticipantRepo: notifyEventParticipantRepo,
		bot:                  bot,
		layout:               layout,
		logger:               logger,
		cron:                 cron.New(cron.WithLocation(location.Location())),
	}
}

// LogHook returns a log hook for the specified channel
//
// Parameters:
//   - channelID is the channel to send the log to
//   - locale is the locale to use for the layout
//   - level is the minimum log level to send
func (s *NotifyService) LogHook(channelID int64, locale string, level zapcore.Level) (types.LogHook, error) {
	chat, err := s.bot.ChatByID(channelID)
	if err != nil {
		return nil, err
	}
	return func(log types.Log) {
		if log.Level >= level {
			_, err = s.bot.Send(chat, s.layout.TextLocale(locale, "log", log))
			if err != nil && !strings.Contains(log.Message, "failed to send log to channel") {
				s.logger.Errorf("failed to send log to channel %d: %v\n", channelID, err)
			}
		}
	}, nil
}

// SendClubWarning sends a warning to club owners if they have enabled notifications
func (s *NotifyService) SendClubWarning(clubID string, what interface{}, opts ...interface{}) error {
	clubOwners, err := s.clubOwnerService.GetByClubID(context.Background(), clubID)
	if err != nil {
		return err
	}

	var errors []error
	for _, owner := range clubOwners {
		if owner.Warnings {
			chat, errGetChat := s.bot.ChatByID(owner.UserID)
			if errGetChat != nil {
				errors = append(errors, errGetChat)
			}
			_, errSend := s.bot.Send(chat, what, opts...)
			if errSend != nil {
				errors = append(errors, errSend)
			}
		}
	}

	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}

func (s *NotifyService) SendEventUpdate(eventID string, what interface{}, opts ...interface{}) error {
	participants, err := s.eventParticipantRepo.GetByEventID(context.Background(), eventID)
	if err != nil {
		return err
	}

	var errors []error
	for _, participant := range participants {
		chat, errGetChat := s.bot.ChatByID(participant.UserID)
		if errGetChat != nil {
			errors = append(errors, errGetChat)
		}
		_, errSend := s.bot.Send(chat, what, opts...)
		if errSend != nil {
			errors = append(errors, errSend)
		}
	}

	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}

// StartNotifyScheduler starts the scheduler for sending notifications
func (s *NotifyService) StartNotifyScheduler() {
	s.logger.Debug("Starting notify scheduler")
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			ctx := context.Background()
			s.checkAndNotify(ctx)
		}
	}()
	s.logger.Info("Notify scheduler started")
}

// StartClubOwnerReminderScheduler starts the scheduler for sending weekly reminder to club owners
func (s *NotifyService) StartClubOwnerReminderScheduler() error {
	s.logger.Debug("Initializing club owner reminder scheduler...")

	// Schedule for every Friday at 16:00
	_, err := s.cron.AddFunc("0 16 * * 5", func() {
		s.logger.Info("=== Club Owner Reminder Scheduler Triggered ===")
		s.sendClubOwnerReminder(context.Background())
	})
	if err != nil {
		return err
	}

	s.logger.Info("Club owner reminder scheduler initialized")
	return nil
}

// StopClubOwnerReminderScheduler stops the club owner reminder scheduler
func (s *NotifyService) StopClubOwnerReminderScheduler() {
	if s.cron != nil {
		s.cron.Stop()
		s.logger.Info("Club owner reminder scheduler stopped")
	}
}

// sendClubOwnerReminder sends weekly reminder to all club owners
func (s *NotifyService) sendClubOwnerReminder(ctx context.Context) {
	s.logger.Info("Sending weekly reminder to club owners")

	clubOwners, err := s.clubOwnerService.GetAllUniqueClubOwners(ctx)
	if err != nil {
		s.logger.Errorf("Failed to get club owners: %v", err)
		return
	}

	s.logger.Infof("Found %d unique club owners to send reminder", len(clubOwners))

	for _, owner := range clubOwners {
		if owner.IsBanned {
			s.logger.Debugf("Skipping banned user %d", owner.UserID)
			continue
		}

		chat, errGetChat := s.bot.ChatByID(owner.UserID)
		if errGetChat != nil {
			s.logger.Errorf("Failed to get chat for user %d: %v", owner.UserID, errGetChat)
			continue
		}

		_, errSend := s.bot.Send(chat,
			s.layout.TextLocale("ru", "club_owner_weekly_reminder"),
			s.layout.MarkupLocale("ru", "core:hide"),
		)
		if errSend != nil {
			s.logger.Errorf("Failed to send mailing to user %d: %v", owner.UserID, errSend)
			continue
		}

		s.logger.Infof("Sent weekly reminder to club owner %d (%s)", owner.UserID, owner.FIO.String())
	}

	s.logger.Info("Weekly reminder to club owners completed")
}

// checkAndNotify checks for events starting in the next 25 hours (to cover both day and hour notifications)
//
// NOTE: localisation is hardcoded for now (ru)
func (s *NotifyService) checkAndNotify(ctx context.Context) {
	s.logger.Debugf("Checking for events starting in the next 25 hours")
	now := time.Now().In(location.Location())

	// Get events starting in the next 25 hours (to cover both day and hour notifications)
	events, err := s.eventRepo.GetUpcomingEvents(ctx, now.Add(25*time.Hour))
	if err != nil {
		s.logger.Errorf("failed to get upcoming events: %v", err)
		return
	}

	for _, event := range events {
		timeUntilStart := event.StartTime.Sub(now)
		s.logger.Debugf("Event %s starts in %s", event.ID, timeUntilStart)

		// Check for day notification (between 23-24 hours before start)
		if timeUntilStart >= 23*time.Hour && timeUntilStart <= 24*time.Hour {
			s.logger.Infof("Sending day notification for event (event_id=%s)", event.ID)
			s.sendNotifications(ctx, event, entity.NotificationTypeDay)
		}

		// Check for hour notification (between 55-60 minutes before start)
		if timeUntilStart >= 55*time.Minute && timeUntilStart <= 60*time.Minute {
			s.logger.Infof("Sending hour notification for event (event_id=%s)", event.ID)
			s.sendNotifications(ctx, event, entity.NotificationTypeHour)
		}
	}
}

// sendNotifications sends notifications to users that have not been notified
func (s *NotifyService) sendNotifications(ctx context.Context, event entity.Event, notificationType entity.NotificationType) {
	// Get users who haven't been notified yet
	participants, err := s.notificationRepo.GetUnnotifiedUsers(ctx, event.ID, notificationType)
	if err != nil {
		s.logger.Errorf("failed to get unnotified users for event %s: %v", event.ID, err)
		return
	}

	for _, participant := range participants {
		s.logger.Infof(
			"Sending %s notification to user (user_id=%d, event_id=%s, notification_type=%s)",
			notificationType,
			participant.UserID,
			event.ID,
			notificationType,
		)

		// Send notification
		chat, errGetChat := s.bot.ChatByID(participant.UserID)
		if errGetChat != nil {
			s.logger.Errorf("failed to get chat for user %d: %v", participant.UserID, errGetChat)
			continue
		}

		var messageKey string
		switch notificationType {
		case entity.NotificationTypeDay:
			messageKey = "event_notification_day"
		case entity.NotificationTypeHour:
			messageKey = "event_notification_hour"
		}

		_, errSend := s.bot.Send(chat,
			s.layout.TextLocale("ru", messageKey, event),
			s.layout.MarkupLocale("ru", "core:hide"),
		)
		if errSend != nil {
			s.logger.Errorf("failed to send notification to user %d: %v", participant.UserID, errSend)
			continue
		}

		// Record that notification was sent
		notification := &entity.EventNotification{
			EventID: event.ID,
			UserID:  participant.UserID,
			Type:    notificationType,
		}

		if err := s.notificationRepo.Create(ctx, notification); err != nil {
			s.logger.Errorf("failed to create notification record: %v", err)
		}
	}
}
