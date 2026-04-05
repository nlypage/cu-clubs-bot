package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/xuri/excelize/v2"
	tele "gopkg.in/telebot.v3"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/secondary"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/location"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/shadowban"
	"github.com/Badsnus/cu-clubs-bot/bot/pkg/logger/types"
)

/*
PassService - сервис управления пропусками для событий.

Основные принципы работы:
- Пропуски создаются автоматически при регистрации пользователя на событие, которое требует пропуск
- Планировщик отправляет уже созданные пропуски согласно расписанию
- Поддерживается отправка через email и Telegram
- Гибкая система конфигураций для разных типов отправки (будни/выходные)
*/

type PassConfig struct {
	Name            string
	EmailRecipients []string
	TelegramChatID  int64
	IsActive        bool
	CronSchedule    string
}

type EventWithPasses struct {
	Event  entity.Event
	Passes []entity.Pass
}

type PassService struct {
	bot    *tele.Bot
	logger *types.Logger

	passRepo   secondary.PassRepository
	eventRepo  secondary.EventRepository
	userRepo   secondary.UserRepository
	clubRepo   secondary.ClubRepository
	smtpClient secondary.SMTPClient

	cron             *cron.Cron
	configs          map[string]*PassConfig
	schedulerStarted bool
	shadowMatcher    *shadowban.Matcher
}

func NewPassService(
	bot *tele.Bot,
	logger *types.Logger,
	passRepo secondary.PassRepository,
	eventRepo secondary.EventRepository,
	userRepo secondary.UserRepository,
	clubRepo secondary.ClubRepository,
	smtpClient secondary.SMTPClient,
	passEmails []string,
	telegramChatID int64,
	shadowBanNameSurnames []string,
) *PassService {
	ps := &PassService{
		bot:              bot,
		logger:           logger,
		passRepo:         passRepo,
		eventRepo:        eventRepo,
		userRepo:         userRepo,
		clubRepo:         clubRepo,
		smtpClient:       smtpClient,
		cron:             cron.New(cron.WithLocation(location.Location())),
		configs:          make(map[string]*PassConfig),
		schedulerStarted: false,
		shadowMatcher:    shadowban.NewMatcher(shadowBanNameSurnames),
	}

	weekdayConfig := &PassConfig{
		Name:            "weekday",
		EmailRecipients: passEmails,
		TelegramChatID:  telegramChatID,
		IsActive:        true,
		CronSchedule:    "0 16 * * 1-5",
	}
	ps.configs["weekday"] = weekdayConfig

	weekendConfig := &PassConfig{
		Name:            "weekend",
		EmailRecipients: passEmails,
		TelegramChatID:  telegramChatID,
		IsActive:        true,
		CronSchedule:    "0 12 * * 6",
	}
	ps.configs["weekend"] = weekendConfig

	//testConfig := &PassConfig{
	//	Name:            "test",
	//	EmailRecipients: passEmails,
	//	TelegramChatID:  telegramChatID,
	//	IsActive:        true,
	//	CronSchedule:    "* * * * *",
	//}
	//ps.configs["test"] = testConfig

	return ps
}

func (s *PassService) getConfig(name string) *PassConfig {
	if config, exists := s.configs[name]; exists {
		return config
	}
	return nil
}

// CreatePassForUser создает пропуск для пользователя с проверкой на дублирование
func (s *PassService) CreatePassForUser(
	ctx context.Context,
	eventID string,
	userID int64,
	requesterType entity.PassRequesterType,
	requesterID any,
	passType entity.PassType,
	reason string,
	scheduledAt time.Time,
) (*entity.Pass, error) {
	hasActive, err := s.passRepo.HasActivePass(ctx, eventID, userID)
	if err != nil {
		s.logger.Errorf("Failed to check active pass for user %d, event %s: %v", userID, eventID, err)
		return nil, fmt.Errorf("failed to check active pass: %w", err)
	}

	if hasActive {
		s.logger.Debugf("Active pass already exists for user %d, event %s", userID, eventID)
		return nil, fmt.Errorf("active pass already exists for this user and event")
	}

	pass := &entity.Pass{
		EventID:     eventID,
		UserID:      userID,
		Type:        passType,
		Status:      entity.PassStatusPending,
		Reason:      reason,
		ScheduledAt: scheduledAt,
	}
	pass.SetRequester(requesterType, requesterID)

	created, err := s.passRepo.CreatePass(ctx, pass)
	if err != nil {
		s.logger.Errorf("Failed to create pass for user %d, event %s: %v", userID, eventID, err)
		return nil, fmt.Errorf("failed to create pass: %w", err)
	}

	s.logger.Debugf("Created pass %s for user %d, event %s (type: %s, requester: %s)", created.ID, userID, eventID, passType, requesterType)
	return created, nil
}

// CreatePassByClub создает пропуск от имени клуба через API
func (s *PassService) CreatePassByClub(
	ctx context.Context,
	eventID string,
	userID int64,
	clubID string,
	reason string,
	scheduledAt time.Time,
) (*entity.Pass, error) {
	return s.CreatePassForUser(
		ctx,
		eventID,
		userID,
		entity.PassRequesterTypeClub,
		clubID,
		entity.PassTypeApi,
		reason,
		scheduledAt,
	)
}

// CreatePassesByClub создает пропуски для нескольких пользователей от имени клуба
func (s *PassService) CreatePassesByClub(
	ctx context.Context,
	eventID string,
	userIDs []int64,
	clubID string,
	reason string,
	scheduledAt time.Time,
) ([]entity.Pass, []error) {
	var passes []entity.Pass
	var errors []error

	for _, userID := range userIDs {
		pass, err := s.CreatePassByClub(ctx, eventID, userID, clubID, reason, scheduledAt)
		if err != nil {
			errors = append(errors, fmt.Errorf("user %d: %w", userID, err))
			continue
		}
		passes = append(passes, *pass)
	}

	return passes, errors
}

func (s *PassService) StartScheduler() error {
	s.logger.Debug("Initializing pass scheduler...")

	for _, config := range s.configs {
		if !config.IsActive || config.CronSchedule == "" {
			s.logger.Debugf("Skipping config %s (active: %v, schedule: %s)", config.Name, config.IsActive, config.CronSchedule)
			continue
		}

		configName := config.Name
		s.logger.Debugf("Adding cron job for config %s with schedule: %s", configName, config.CronSchedule)

		_, err := s.cron.AddFunc(config.CronSchedule, func() {
			s.logger.Debugf("=== CRON TRIGGERED for %s ===", configName)
			s.processPendingPasses(context.Background(), configName)
		})
		if err != nil {
			return fmt.Errorf("failed to add cron job for config %s: %w", config.Name, err)
		}
		s.logger.Debugf("Successfully added cron job for config %s", configName)
	}

	s.cron.Start()
	s.schedulerStarted = true
	entries := s.cron.Entries()
	s.logger.Infof("Pass scheduler started with %d jobs", len(entries))
	for i, entry := range entries {
		s.logger.Debugf("Job #%d: next run at %s", i+1, entry.Next.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func (s *PassService) StopScheduler() {
	if s.cron != nil {
		s.cron.Stop()
		s.schedulerStarted = false
		s.logger.Info("Pass scheduler stopped")
	}
}

func (s *PassService) processPendingPasses(ctx context.Context, configName string) {
	s.logger.Debugf("Processing pending passes for config: %s", configName)

	config := s.getConfig(configName)
	if config == nil || !config.IsActive {
		s.logger.Debugf("Config %s not found or inactive", configName)
		return
	}

	now := time.Now().In(location.Location())

	s.logger.Debugf("=== Pass Scheduler ===")
	s.logger.Debugf("Current time (local): %s", now.Format("2006-01-02 15:04:05"))
	s.logger.Debugf("Looking for pending passes with scheduled_at <= %s", now.Format("2006-01-02 15:04:05"))

	pendingPasses, err := s.passRepo.GetPendingPassesForSchedule(ctx, now)
	if err != nil {
		s.logger.Error("Failed to get pending passes", "error", err)
		return
	}

	s.logger.Debugf("Found %d pending passes", len(pendingPasses))

	// Log each pass details for diagnosis
	for i, pass := range pendingPasses {
		s.logger.Debugf("Pass #%d: ID=%s, EventID=%s, UserID=%d, ScheduledAt=%s",
			i+1, pass.ID, pass.EventID, pass.UserID, pass.ScheduledAt.In(location.Location()).Format("2006-01-02 15:04:05"))
	}

	var eventsWithPasses []EventWithPasses
	if len(pendingPasses) > 0 {
		eventsWithPasses = s.groupPassesByEvent(ctx, pendingPasses)
	}

	telegramSent, emailSent, err := s.sendConsolidatedPassNotification(ctx, eventsWithPasses, config)
	if err != nil {
		s.logger.Error("Failed to send consolidated notification", "error", err)
		return
	}

	if len(pendingPasses) > 0 {
		sentAt := time.Now()
		var passIDs []string
		for _, eventWithPasses := range eventsWithPasses {
			for _, pass := range eventWithPasses.Passes {
				passIDs = append(passIDs, pass.ID)
			}
		}
		if len(passIDs) > 0 {
			if err := s.passRepo.MarkPassesAsSent(ctx, passIDs, sentAt, emailSent, telegramSent); err != nil {
				s.logger.Error("Failed to mark passes as sent", "error", err)
			}
		}
	}

	s.logger.Infow("Processed pending passes",
		"events", len(eventsWithPasses),
		"totalPasses", len(pendingPasses),
		"config", configName)
}

func (s *PassService) groupPassesByEvent(ctx context.Context, passes []entity.Pass) []EventWithPasses {
	eventPassesMap := make(map[string][]entity.Pass)
	eventMap := make(map[string]entity.Event)

	for _, pass := range passes {
		eventPassesMap[pass.EventID] = append(eventPassesMap[pass.EventID], pass)

		if _, exists := eventMap[pass.EventID]; !exists {
			event, err := s.eventRepo.GetEventByID(ctx, pass.EventID)
			if err != nil {
				s.logger.Error("Failed to get event", "eventID", pass.EventID, "error", err)
				continue
			}
			eventMap[pass.EventID] = *event
		}
	}

	var result []EventWithPasses
	for eventID, eventPasses := range eventPassesMap {
		if event, exists := eventMap[eventID]; exists {
			result = append(result, EventWithPasses{
				Event:  event,
				Passes: eventPasses,
			})
		}
	}

	return result
}

func (s *PassService) sendConsolidatedPassNotification(ctx context.Context, eventsWithPasses []EventWithPasses, config *PassConfig) (telegramSent bool, emailSent bool, err error) {
	totalPasses := 0
	for _, eventWithPasses := range eventsWithPasses {
		totalPasses += len(eventWithPasses.Passes)
	}

	message := s.formatConsolidatedPassMessage(ctx, eventsWithPasses, totalPasses)

	var consolidatedExcel *bytes.Buffer
	if totalPasses > 0 {
		consolidatedExcel, err = s.generateConsolidatedPassExcel(ctx, eventsWithPasses)
		if err != nil {
			s.logger.Errorw("Failed to generate consolidated Excel file", "error", err)
			return false, false, err
		}
	} else {
		consolidatedExcel, err = s.generateEmptyPassExcel()
		if err != nil {
			s.logger.Errorw("Failed to generate empty Excel file", "error", err)
			return false, false, err
		}
	}

	if config.TelegramChatID != 0 {
		buf := bytes.NewBuffer(consolidatedExcel.Bytes())
		if sendErr := s.sendTelegramNotification(config.TelegramChatID, message, buf); sendErr != nil {
			s.logger.Errorw("Failed to send consolidated Telegram notification", "error", sendErr)
			telegramSent = false
		} else {
			telegramSent = true
			s.logger.Info("Consolidated Telegram notification sent")
		}
	}

	if len(config.EmailRecipients) > 0 {
		subject := fmt.Sprintf("Сводка пропусков - %d событий (%d пропусков)",
			len(eventsWithPasses), totalPasses)

		emailSent = false
		for _, email := range config.EmailRecipients {
			buf := bytes.NewBuffer(consolidatedExcel.Bytes())
			if sendErr := s.smtpClient.Send(email, "", "", subject, buf); sendErr != nil {
				s.logger.Errorw("Failed to send email", "email", email, "error", sendErr)
			} else {
				emailSent = true
			}
		}
	}

	s.logger.Infow("Notification send results", "telegramSent", telegramSent, "emailSent", emailSent)

	return telegramSent, emailSent, nil
}

func (s *PassService) formatConsolidatedPassMessage(ctx context.Context, eventsWithPasses []EventWithPasses, totalPasses int) string {
	var message strings.Builder
	totalShadowBanned := 0

	message.WriteString("📋 <b>Сводка пропусков</b>\n\n")

	if totalPasses == 0 {
		message.WriteString("✅ <b>Нет пропусков для отправки</b>\n\n")
		return message.String()
	}

	_, _ = fmt.Fprintf(&message, "📊 <b>Всего событий:</b> %d\n", len(eventsWithPasses))
	_, _ = fmt.Fprintf(&message, "👥 <b>Всего пропусков:</b> %d\n\n", totalPasses)

	for i, eventWithPasses := range eventsWithPasses {
		event := eventWithPasses.Event
		passes := eventWithPasses.Passes
		shadowBannedCount := s.countShadowBannedPasses(ctx, passes)
		totalShadowBanned += shadowBannedCount

		_, _ = fmt.Fprintf(&message, "<b>%d. %s</b>\n", i+1, event.Name)
		_, _ = fmt.Fprintf(&message, "📅 %s\n", event.StartTime.In(location.Location()).Format("02.01.2006 15:04"))
		_, _ = fmt.Fprintf(&message, "📍 %s\n", event.Location)
		_, _ = fmt.Fprintf(&message, "👥 Пропусков: %d\n\n", len(passes))
		if shadowBannedCount > 0 {
			_, _ = fmt.Fprintf(&message, "⛔ Не пускать: %d\n\n", shadowBannedCount)
		}
	}

	if totalShadowBanned > 0 {
		return strings.Replace(
			message.String(),
			fmt.Sprintf("👥 <b>Всего пропусков:</b> %d\n\n", totalPasses),
			fmt.Sprintf("👥 <b>Всего пропусков:</b> %d\n⛔ <b>Не пускать:</b> %d\n\n", totalPasses, totalShadowBanned),
			1,
		)
	}

	return message.String()
}

func (s *PassService) generateConsolidatedPassExcel(ctx context.Context, eventsWithPasses []EventWithPasses) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			s.logger.Errorf("Failed to close Excel file: %v", err)
		}
	}()

	sheetName := "Пропуски"
	if err := f.SetSheetName("Sheet1", sheetName); err != nil {
		return nil, fmt.Errorf("failed to set sheet name: %w", err)
	}

	headers := []string{"Событие", "Дата", "Время", "Место", "ФИО", "Роль"}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheetName, cell, header); err != nil {
			return nil, fmt.Errorf("failed to set header cell: %w", err)
		}
	}

	row := 2
	for _, eventWithPasses := range eventsWithPasses {
		event := eventWithPasses.Event
		passes := eventWithPasses.Passes

		var userIDs []int64
		for _, pass := range passes {
			userIDs = append(userIDs, pass.UserID)
		}

		users, err := s.userRepo.GetMany(ctx, userIDs)
		if err != nil {
			s.logger.Error("Failed to get users for Excel", "error", err)
			continue
		}

		userMap := make(map[int64]entity.User)
		for _, user := range users {
			userMap[user.ID] = user
		}

		for _, pass := range passes {
			user, exists := userMap[pass.UserID]
			if !exists {
				continue
			}

			data := []any{
				event.Name,
				event.StartTime.In(location.Location()).Format("02.01.2006"),
				event.StartTime.In(location.Location()).Format("15:04"),
				event.Location,
				s.formatPassFIO(user),
				user.Role,
			}

			for i, value := range data {
				cell, _ := excelize.CoordinatesToCellName(i+1, row)
				if err := f.SetCellValue(sheetName, cell, value); err != nil {
					s.logger.Errorf("Failed to set cell value: %v", err)
					continue
				}
			}
			row++
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}

	return &buf, nil
}

func (s *PassService) countShadowBannedPasses(ctx context.Context, passes []entity.Pass) int {
	if s.shadowMatcher == nil || len(passes) == 0 {
		return 0
	}

	userIDs := make([]int64, 0, len(passes))
	for _, pass := range passes {
		userIDs = append(userIDs, pass.UserID)
	}

	users, err := s.userRepo.GetMany(ctx, userIDs)
	if err != nil {
		s.logger.Errorw("Failed to get users for pass summary", "error", err)
		return 0
	}

	userMap := make(map[int64]entity.User, len(users))
	for _, user := range users {
		userMap[user.ID] = user
	}

	count := 0
	for _, pass := range passes {
		user, exists := userMap[pass.UserID]
		if !exists {
			continue
		}
		if s.shadowMatcher.MatchUser(user) {
			count++
		}
	}

	return count
}

func (s *PassService) formatPassFIO(user entity.User) string {
	if s.shadowMatcher != nil && s.shadowMatcher.MatchUser(user) {
		return user.FIO.String() + " (НЕ ПУСКАТЬ)"
	}

	return user.FIO.String()
}

func (s *PassService) sendTelegramNotification(chatID int64, message string, file *bytes.Buffer) error {
	if file != nil && file.Len() > 0 {
		document := &tele.Document{
			File:     tele.FromReader(file),
			FileName: fmt.Sprintf("passes_%s.xlsx", time.Now().Format("2006-01-02")),
		}

		document.Caption = message
		_, err := s.bot.Send(&tele.Chat{ID: chatID}, document, &tele.SendOptions{ParseMode: tele.ModeHTML})
		return err
	}

	_, err := s.bot.Send(&tele.Chat{ID: chatID}, message, &tele.SendOptions{ParseMode: tele.ModeHTML})
	return err
}

func (s *PassService) generateEmptyPassExcel() (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			s.logger.Errorf("Failed to close Excel file: %v", err)
		}
	}()

	sheetName := "Пропуски"
	if err := f.SetSheetName("Sheet1", sheetName); err != nil {
		return nil, fmt.Errorf("failed to set sheet name: %w", err)
	}

	headers := []string{"Событие", "Дата", "Время", "Место", "ФИО", "Роль"}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheetName, cell, header); err != nil {
			return nil, fmt.Errorf("failed to set header cell: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}

	return &buf, nil
}
