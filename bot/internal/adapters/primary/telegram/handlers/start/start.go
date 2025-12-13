package start

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/nlypage/intele"
	"github.com/redis/go-redis/v9"

	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/layout"
	"gorm.io/gorm"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/primary/telegram/handlers/menu"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/callbacks"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/codes"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/emails"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/events"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/banner"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/primary"
	"github.com/Badsnus/cu-clubs-bot/bot/pkg/logger/types"
)

type Handler struct {
	userService             primary.UserService
	clubService             primary.ClubService
	eventService            primary.EventService
	eventParticipantService primary.EventParticipantService
	qrService               primary.QrService
	notificationService     primary.NotifyService

	callbacksStorage callbacks.CallbackStorage

	menuHandler *menu.Handler

	codesStorage  *codes.Storage
	emailsStorage *emails.Storage
	eventsStorage *events.Storage
	layout        *layout.Layout
	logger        *types.Logger
	input         *intele.InputManager
	eventIDTTL    time.Duration
}

func New(
	userSvc primary.UserService,
	clubSvc primary.ClubService,
	eventSvc primary.EventService,
	eventParticipantSvc primary.EventParticipantService,
	qrSvc primary.QrService,
	notifySvc primary.NotifyService,
	callbacksStorage callbacks.CallbackStorage,
	menuHandler *menu.Handler,
	codesStorage *codes.Storage,
	emailsStorage *emails.Storage,
	eventsStorage *events.Storage,
	lt *layout.Layout,
	lg *types.Logger,
	in *intele.InputManager,
	eventIDTTL time.Duration,
) *Handler {
	return &Handler{
		userService:             userSvc,
		clubService:             clubSvc,
		eventService:            eventSvc,
		eventParticipantService: eventParticipantSvc,
		qrService:               qrSvc,
		notificationService:     notifySvc,
		callbacksStorage:        callbacksStorage,
		menuHandler:             menuHandler,
		codesStorage:            codesStorage,
		emailsStorage:           emailsStorage,
		eventsStorage:           eventsStorage,
		layout:                  lt,
		logger:                  lg,
		input:                   in,
		eventIDTTL:              eventIDTTL,
	}
}

func (h Handler) Start(c tele.Context) error {
	h.logger.Infof("(user: %d) enter /start", c.Sender().ID)

	user, err := h.userService.Get(context.Background(), c.Sender().ID)

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		h.logger.Errorf("(user: %d) error while getting user from db: %v", c.Sender().ID, err)
		return c.Send(
			h.layout.Text(c, "technical_issues", err.Error()),
			h.layout.Markup(c, "core:hide"),
		)
	}

	payload := strings.Split(c.Message().Payload, "_")

	if len(payload) < 2 {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Send(
				banner.Auth.Caption(h.layout.Text(c, "personal_data_agreement_text")),
				h.layout.Markup(c, "auth:personalData:agreementMenu"),
			)
		}
		if user.IsBanned {
			return c.Send(
				h.layout.Text(c, "banned"),
				h.layout.Markup(c, "core:hide"),
			)
		}

		// Send weekly digest image
		err = h.sendWeeklyDigest(c)
		if err != nil {
			h.logger.Errorf("(user: %d) error sending weekly digest: %v", c.Sender().ID, err)
		}

		return h.menuHandler.SendMenu(c)
	}

	var payloadType, data string
	if len(payload) == 2 {
		payloadType, data = payload[0], payload[1]
	}

	switch payloadType {
	case "emailCode":
		code, err := h.codesStorage.Get(c.Sender().ID)
		if err != nil && !errors.Is(err, redis.Nil) {
			h.logger.Errorf("(user: %d) error while getting user email from redis: %v", c.Sender().ID, err)
			return c.Send(
				h.layout.Text(c, "wrong_code"),
				h.layout.Markup(c, "core:hide"),
			)
		}
		switch code.Type {
		case codes.CodeTypeAuth:
			return h.auth(c, data)
		case codes.CodeTypeChangingRole:
			return h.changeRole(c, data)
		default:
			h.logger.Errorf("(user: %d) invalid code type: %v", c.Sender().ID, code.Type)
			return c.Send(
				h.layout.Text(c, "something_went_wrong"),
				h.layout.Markup(c, "core:hide"),
			)
		}

	case "userQR":
		return h.userQR(c, data)

	case "eventQR":
		return h.eventQR(c, data)

	case "event":
		return h.eventMenu(c, data)

	default:
		return c.Send(
			h.layout.Text(c, "something_went_wrong"),
			h.layout.Markup(c, "core:hide"),
		)
	}
}

func (h *Handler) sendWeeklyDigest(c tele.Context) error {
	// Get all events
	allEvents, err := h.eventService.GetAll(context.Background())
	if err != nil {
		return err
	}

	// Filter future events for the current week
	now := time.Now()
	startOfWeek := now.AddDate(0, 0, -int(now.Weekday()-time.Monday))
	if now.Weekday() == time.Sunday {
		startOfWeek = now.AddDate(0, 0, -6)
	}
	endOfWeek := startOfWeek.AddDate(0, 0, 7)

	var weeklyEvents []entity.Event
	for _, event := range allEvents {
		if event.StartTime.After(now) && event.StartTime.Before(endOfWeek) {
			weeklyEvents = append(weeklyEvents, event)
		}
	}

	if len(weeklyEvents) == 0 {
		return nil // No events, skip sending
	}

	// Generate digest image
	imageBytes, err := h.eventService.GenerateWeeklyDigestImage(weeklyEvents)
	if err != nil {
		return err
	}

	// Send image
	photo := &tele.Photo{File: tele.FromReader(bytes.NewReader(imageBytes))}
	return c.Send(photo)
}
