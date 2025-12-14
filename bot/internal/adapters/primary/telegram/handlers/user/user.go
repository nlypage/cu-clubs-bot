package user

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nlypage/intele"
	"github.com/nlypage/intele/collector"
	"github.com/redis/go-redis/v9"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/layout"
	"gorm.io/gorm"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/primary/telegram/handlers/menu"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/callbacks"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/codes"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/emails"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/events"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/common/errorz"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/banner"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/calendar"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/valueobject"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/location"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/validator"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/primary"
	"github.com/Badsnus/cu-clubs-bot/bot/pkg/logger/types"
)

type Handler struct {
	userService             primary.UserService
	eventService            primary.EventService
	clubService             primary.ClubService
	eventParticipantService primary.EventParticipantService
	qrService               primary.QrService
	notificationService     primary.NotifyService

	menuHandler *menu.Handler

	codesStorage     *codes.Storage
	emailsStorage    *emails.Storage
	eventsStorage    *events.Storage
	callbacksStorage callbacks.CallbackStorage
	input            *intele.InputManager
	layout           *layout.Layout
	logger           *types.Logger

	grantChatID       int64
	timezone          string
	validEmailDomains []string
	emailTTL          time.Duration
	authTTL           time.Duration
	resendTTL         time.Duration
}

func New(
	userSvc primary.UserService,
	eventSvc primary.EventService,
	clubSvc primary.ClubService,
	eventParticipantSvc primary.EventParticipantService,
	qrSvc primary.QrService,
	notifySvc primary.NotifyService,
	menuHandler *menu.Handler,
	codesStorage *codes.Storage,
	emailsStorage *emails.Storage,
	eventsStorage *events.Storage,
	callbacksStorage callbacks.CallbackStorage,
	lt *layout.Layout,
	lg *types.Logger,
	in *intele.InputManager,
	grantChatID int64,
	timezone string,
	validEmailDomains []string,
	emailTTL time.Duration,
	authTTL time.Duration,
	resendTTL time.Duration,
) *Handler {
	return &Handler{
		userService:             userSvc,
		eventService:            eventSvc,
		eventParticipantService: eventParticipantSvc,
		clubService:             clubSvc,
		qrService:               qrSvc,
		notificationService:     notifySvc,
		menuHandler:             menuHandler,
		codesStorage:            codesStorage,
		emailsStorage:           emailsStorage,
		eventsStorage:           eventsStorage,
		callbacksStorage:        callbacksStorage,
		layout:                  lt,
		input:                   in,
		logger:                  lg,
		grantChatID:             grantChatID,
		timezone:                timezone,
		validEmailDomains:       validEmailDomains,
		emailTTL:                emailTTL,
		authTTL:                 authTTL,
		resendTTL:               resendTTL,
	}
}

func (h Handler) Hide(c tele.Context) error {
	return c.Delete()
}

func (h Handler) cuClubs(c tele.Context) error {
	const clubsOnPage = 5
	h.logger.Infof("(user: %d) get clubs list", c.Sender().ID)

	var (
		p          int
		prevPage   int
		nextPage   int
		err        error
		clubsCount int64
		clubs      []entity.Club
		rows       []tele.Row
		menuRow    tele.Row
	)
	if c.Callback().Unique != "mainMenu_cuClubs" {
		p, err = strconv.Atoi(c.Callback().Data)
		if err != nil {
			return errorz.ErrInvalidCallbackData
		}
	}

	clubsCount, err = h.clubService.CountByShouldShow(context.Background(), true)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting clubs count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Clubs.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	clubs, err = h.clubService.GetByShouldShowWithPagination(
		context.Background(),
		true,
		clubsOnPage,
		p*clubsOnPage,
		"created_at ASC",
	)
	if err != nil {
		h.logger.Errorf(
			"(user: %d) error while get clubs (offset=%d, limit=%d, order=%s, should_show=%t): %v",
			c.Sender().ID,
			p*clubsOnPage,
			clubsOnPage,
			"created_at ASC",
			true,
			err,
		)
		return c.Edit(
			banner.Clubs.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}
	h.logger.Infof("(user: %d) GetWithPagination returned %d clubs for page %d (limit %d, offset %d)", c.Sender().ID, len(clubs), p, clubsOnPage, p*clubsOnPage)
	h.logger.Debugf("clubs: %+v", clubs)

	markup := c.Bot().NewMarkup()
	for _, club := range clubs {
		h.logger.Debugf("Club: %s (%s)", club.Name, club.ID)
		rows = append(rows, markup.Row(*h.layout.Button(c, "cuClubs:club:intro", struct {
			ID   string
			Name string
			Page int
		}{
			ID:   club.ID,
			Name: club.Name,
			Page: p,
		})))
	}
	h.logger.Debugf("clubs: %+v", rows)

	pagesCount := (int(clubsCount) - 1) / clubsOnPage
	if p == 0 {
		prevPage = pagesCount
	} else {
		prevPage = p - 1
	}

	if p >= pagesCount {
		nextPage = 0
	} else {
		nextPage = p + 1
	}

	menuRow = append(menuRow,
		*h.layout.Button(c, "cuClubs:prev_page", struct {
			Page int
		}{
			Page: prevPage,
		}),
		*h.layout.Button(c, "core:page_counter", struct {
			Page       int
			PagesCount int
		}{
			Page:       p + 1,
			PagesCount: pagesCount + 1,
		}),
		*h.layout.Button(c, "cuClubs:next_page", struct {
			Page int
		}{
			Page: nextPage,
		}),
	)

	rows = append(
		rows,
		menuRow,
		markup.Row(*h.layout.Button(c, "mainMenu:back")),
	)

	markup.Inline(rows...)

	h.logger.Infof(
		"(user: %d) user clubs list (pages_count=%d, page=%d, clubs_count=%d, next_page=%d, prev_page=%d)",
		c.Sender().ID,
		pagesCount,
		p,
		clubsCount,
		nextPage,
		prevPage,
	)

	_ = c.Edit(
		banner.Clubs.Caption(h.layout.Text(c, "cu_clubs_list")),
		markup,
	)
	return nil
}

func (h Handler) clubIntro(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	clubID := callbackData[0]
	pageStr := callbackData[1]
	page, err := strconv.Atoi(pageStr)
	if err != nil {
		return errorz.ErrInvalidCallbackData
	}

	h.logger.Infof("(user: %d) check club intro (club_id=%s)", c.Sender().ID, clubID)

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Clubs.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "cuClubs:back", struct {
				Page int
			}{
				Page: page,
			}),
		)
	}

	clubIntro, err := h.clubService.GetIntro(context.Background(), club.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get clubs: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Clubs.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "cuClubs:back", struct {
				Page int
			}{
				Page: page,
			}),
		)
	}

	if clubIntro != nil {
		videoNote := &tele.VideoNote{
			File: *clubIntro,
		}

		_ = c.Delete()
		err := c.Send(
			videoNote,
			h.layout.Markup(c, "cuClubs:club:intro:menu", struct {
				ID   string
				Page int
			}{
				ID:   club.ID,
				Page: page,
			}),
		)
		if err == nil {
			return nil
		}
	}

	clubAvatar, err := h.clubService.GetAvatar(context.Background(), club.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get clubs: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Clubs.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "cuClubs:back", struct {
				Page int
			}{
				Page: page,
			}),
		)
	}

	menuMarkup := h.layout.Markup(c, "cuClubs:back", struct {
		Page int
	}{
		Page: page,
	})

	if clubAvatar != nil {
		caption := &tele.Photo{
			File: tele.File{
				FileID:     clubAvatar.FileID,
				UniqueID:   clubAvatar.UniqueID,
				FileSize:   clubAvatar.FileSize,
				FilePath:   clubAvatar.FilePath,
				FileLocal:  clubAvatar.FileLocal,
				FileURL:    clubAvatar.FileURL,
				FileReader: clubAvatar.FileReader,
			},
			Caption: h.layout.Text(c, "cu_club_text", struct {
				Club entity.Club
			}{
				Club: *club,
			}),
		}

		return c.Edit(
			caption,
			menuMarkup,
		)
	}

	return c.Edit(
		banner.Clubs.Caption(h.layout.Text(c, "cu_club_text", struct {
			Club entity.Club
		}{
			Club: *club,
		})),
		menuMarkup,
	)
}

func (h Handler) clubAbout(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	clubID := callbackData[0]
	pageStr := callbackData[1]
	page, err := strconv.Atoi(pageStr)
	if err != nil {
		return errorz.ErrInvalidCallbackData
	}

	_ = c.Delete()

	h.logger.Infof("(user: %d) edit club menu (club_id=%s)", c.Sender().ID, clubID)

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Clubs.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "cuClubs:back", struct {
				Page int
			}{
				Page: page,
			}),
		)
	}

	clubAvatar, err := h.clubService.GetAvatar(context.Background(), club.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get clubs: %v", c.Sender().ID, err)
		return c.Send(
			banner.Clubs.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "cuClubs:back", struct {
				Page int
			}{
				Page: page,
			}),
		)
	}

	menuMarkup := h.layout.Markup(c, "cuClubs:back", struct {
		Page int
	}{
		Page: page,
	})

	if clubAvatar != nil {
		caption := &tele.Photo{
			File: tele.File{
				FileID:     clubAvatar.FileID,
				UniqueID:   clubAvatar.UniqueID,
				FileSize:   clubAvatar.FileSize,
				FilePath:   clubAvatar.FilePath,
				FileLocal:  clubAvatar.FileLocal,
				FileURL:    clubAvatar.FileURL,
				FileReader: clubAvatar.FileReader,
			},
			Caption: h.layout.Text(c, "cu_club_text", struct {
				Club entity.Club
			}{
				Club: *club,
			}),
		}

		return c.Send(
			caption,
			menuMarkup,
		)
	}

	return c.Send(
		banner.Clubs.Caption(h.layout.Text(c, "cu_club_text", struct {
			Club entity.Club
		}{
			Club: *club,
		})),
		menuMarkup,
	)
}

func (h Handler) personalAccount(c tele.Context) error {
	h.logger.Infof("(user: %d getting personal account", c.Sender().ID)

	user, err := h.userService.Get(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting user from db: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	fio := user.GetFIO()

	// TODO: refactor
	markup := h.layout.Markup(c, "personalAccount:menu")
	if user.Role != valueobject.Student {
		button := *h.layout.Button(c, "personalAccount:change_role").Inline()
		newRow := []tele.InlineButton{button}

		markup.InlineKeyboard = append(
			markup.InlineKeyboard[:1],
			append([][]tele.InlineButton{newRow}, markup.InlineKeyboard[1:]...)...,
		)
	}
	return c.Edit(
		banner.PersonalAccount.Caption(h.layout.Text(c, "personal_account_text", struct {
			Name string
			Role string
		}{
			Name: fio.Name,
			Role: user.Role.String(),
		})),
		markup,
	)
}

func (h Handler) qrCode(c tele.Context) error {
	h.logger.Infof("(user: %d) requested QR code", c.Sender().ID)

	h.logger.Infof("(user: %d) getting user QR code", c.Sender().ID)
	loading, _ := c.Bot().Send(c.Chat(), h.layout.Text(c, "loading"))
	file, err := h.qrService.GetUserQR(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting user QR code: %v", c.Sender().ID, err)
		_ = c.Bot().Delete(loading)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}
	_ = c.Bot().Delete(loading)

	return c.Edit(
		&tele.Photo{
			File:    file,
			Caption: h.layout.Text(c, "qr_text"),
		},
		h.layout.Markup(c, "mainMenu:back"),
	)
}

func (h Handler) eventsList(c tele.Context) error {
	const eventsOnPage = 5
	h.logger.Infof("(user: %d) edit events list", c.Sender().ID)

	var (
		p           int
		prevPage    int
		nextPage    int
		err         error
		eventsCount int64
		events      []dto.Event
		rows        []tele.Row
		menuRow     tele.Row
	)
	if c.Callback().Unique == "digest_events" {
		p = 0
	} else if c.Callback().Unique != "mainMenu_events" {
		p, err = strconv.Atoi(c.Callback().Data)
		if err != nil {
			return errorz.ErrInvalidCallbackData
		}
	}

	user, err := h.userService.Get(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting user from db: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	eventsCount, err = h.eventService.Count(context.Background(), user.Role)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get events count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}
	h.logger.Infof("(user: %d, role: %s) Event count returned: %d", c.Sender().ID, user.Role, eventsCount) // Added log with role

	events, err = h.eventService.GetWithPagination(
		context.Background(),
		eventsOnPage,
		p*eventsOnPage,
		"start_time ASC",
		user.Role,
		user.ID,
	)
	if err != nil {
		h.logger.Errorf(
			"(user: %d) error while get events (offset=%d, limit=%d, order=%s, role=%s): %v",
			c.Sender().ID,
			p*eventsOnPage,
			eventsOnPage,
			user.Role.String(),
			"start_time ASC",
			err,
		)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}
	h.logger.Infof("(user: %d, role: %s) GetWithPagination returned %d events for page %d (limit %d, offset %d)", c.Sender().ID, user.Role, len(events), p, eventsOnPage, p*eventsOnPage) // Added log with role
	h.logger.Debugf("Events: %+v", events)

	markup := c.Bot().NewMarkup()
	for _, event := range events {
		h.logger.Debugf("Event: %s (%s)", event.Name, event.ID)
		rows = append(rows, markup.Row(*h.layout.Button(c, "user:events:event", struct {
			ID           string
			Name         string
			Page         int
			IsRegistered bool
		}{
			ID:           event.ID,
			Name:         event.Name,
			Page:         p,
			IsRegistered: event.IsRegistered,
		})))
	}
	h.logger.Debugf("Events: %+v", rows)

	pagesCount := (int(eventsCount) - 1) / eventsOnPage
	if p == 0 {
		prevPage = pagesCount
	} else {
		prevPage = p - 1
	}

	if p >= pagesCount {
		nextPage = 0
	} else {
		nextPage = p + 1
	}

	menuRow = append(menuRow,
		*h.layout.Button(c, "user:events:prev_page", struct {
			Page int
		}{
			Page: prevPage,
		}),
		*h.layout.Button(c, "core:page_counter", struct {
			Page       int
			PagesCount int
		}{
			Page:       p + 1,
			PagesCount: pagesCount + 1,
		}),
		*h.layout.Button(c, "user:events:next_page", struct {
			Page int
		}{
			Page: nextPage,
		}),
	)

	rows = append(
		rows,
		menuRow,
		markup.Row(*h.layout.Button(c, "digest:back")),
	)

	markup.Inline(rows...)

	h.logger.Infof(
		"(user: %d) user events list (pages_count=%d, page=%d, events_count=%d, next_page=%d, prev_page=%d)",
		c.Sender().ID,
		pagesCount,
		p,
		eventsCount,
		nextPage,
		prevPage,
	)

	_ = c.Edit(
		banner.Events.Caption(h.layout.Text(c, "events_list")),
		markup,
	)
	return nil
}

func (h Handler) event(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	eventID := callbackData[0]
	page := callbackData[1]
	h.logger.Infof("(user: %d) edit event (event_id=%s)", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	user, err := h.userService.Get(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get user: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	var registered bool
	_, errGetParticipant := h.eventParticipantService.Get(context.Background(), eventID, c.Sender().ID)
	if errGetParticipant != nil {
		if !errors.Is(errGetParticipant, gorm.ErrRecordNotFound) {
			h.logger.Errorf("(user: %d) error while get participant: %v", c.Sender().ID, errGetParticipant)
			return c.Edit(
				banner.Events.Caption(h.layout.Text(c, "technical_issues", errGetParticipant.Error())),
				h.layout.Markup(c, "user:events:back", struct {
					Page string
				}{
					Page: page,
				}),
			)
		}
	} else {
		registered = true
	}

	participantsCount, err := h.eventParticipantService.CountByEventID(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get participants count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	if c.Callback().Unique == "event_register" {
		if !registered {
			var roleAllowed bool
			var registrationActive bool
			var userSubscribed bool
			for _, role := range event.AllowedRoles {
				if role == string(user.Role) {
					roleAllowed = true
				}
			}

			if user.Role == valueobject.Student {
				registrationActive = event.RegistrationEnd.After(time.Now().In(location.Location()))
			} else {
				registrationActive = utils.GetMaxRegisteredEndTime(event.StartTime).After(time.Now().In(location.Location())) && event.RegistrationEnd.After(time.Now().In(location.Location()))
			}

			if club.SubscriptionRequired && club.ChannelID != nil {
				member, err := c.Bot().ChatMemberOf(&tele.Chat{ID: *club.ChannelID}, &tele.User{ID: c.Sender().ID})
				if err != nil {
					h.logger.Errorf("(user: %d) error while verification user's membership in the club channel: %v", c.Sender().ID, err)
					return c.Send(
						banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
						h.layout.Markup(c, "core:hide"),
					)
				}

				if member.Role == tele.Creator || member.Role == tele.Administrator || member.Role == tele.Member {
					userSubscribed = true
				}
			} else {
				userSubscribed = true
			}

			if (event.MaxParticipants == 0 || participantsCount < event.MaxParticipants) && registrationActive && roleAllowed && userSubscribed {
				_, err = h.eventParticipantService.Register(context.Background(), eventID, c.Sender().ID)
				if err != nil {
					h.logger.Errorf("(user: %d) error while register to event: %v", c.Sender().ID, err)
					return c.Edit(
						banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
						h.layout.Markup(c, "user:events:back", struct {
							Page string
						}{
							Page: page,
						}),
					)
				}

				if participantsCount+1 == event.ExpectedParticipants {
					errSendWarning := h.notificationService.SendClubWarning(event.ClubID,
						h.layout.Text(c, "expected_participants_reached_warning", struct {
							Name              string
							ParticipantsCount int
						}{
							Name:              event.Name,
							ParticipantsCount: participantsCount + 1,
						}),
						h.layout.Markup(c, "core:hide"),
					)
					if errSendWarning != nil {
						h.logger.Errorf("(user: %d) error while send expected participants reached warning: %v", c.Sender().ID, errSendWarning)
					}
				}

				if participantsCount+1 == event.MaxParticipants {
					errSendWarning := h.notificationService.SendClubWarning(event.ClubID,
						h.layout.Text(c, "max_participants_reached_warning", struct {
							Name              string
							ParticipantsCount int
						}{
							Name:              event.Name,
							ParticipantsCount: participantsCount + 1,
						}),
						h.layout.Markup(c, "core:hide"),
					)
					if errSendWarning != nil {
						h.logger.Errorf("(user: %d) error while send expected participants reached warning: %v", c.Sender().ID, errSendWarning)
					}
				}
				registered = true
			} else {
				switch {
				case event.RegistrationEnd.Before(time.Now().In(location.Location())):
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "registration_ended"),
						ShowAlert: true,
					})
				case event.MaxParticipants > 0 && participantsCount >= event.MaxParticipants:
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "max_participants_reached"),
						ShowAlert: true,
					})
				case !roleAllowed:
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "not_allowed_role"),
						ShowAlert: true,
					})
				case !userSubscribed:
					chat, err := c.Bot().ChatByID(*club.ChannelID)
					if err != nil {
						h.logger.Errorf("(user: %d) error while get chat: %v", c.Sender().ID, err)
						return c.Send(
							banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
							h.layout.Markup(c, "core:hide"),
						)
					}

					return c.Send(
						banner.Events.Caption(h.layout.Text(c, "user_not_subscribed", struct {
							ChannelName string
						}{
							ChannelName: chat.Username,
						})),
						h.layout.Markup(c, "core:hide"),
					)
				}
			}
		}
	}

	endTime := event.EndTime.In(location.Location()).Format("02.01.2006 15:04")
	if event.EndTime.Year() == 1 {
		endTime = ""
	}

	markup := h.layout.Markup(c, "user:events:event", struct {
		ID   string
		Page string
	}{
		ID:   eventID,
		Page: page,
	})
	if registered {
		markup.InlineKeyboard = append(
			[][]tele.InlineButton{{*h.layout.Button(c, "user:events:event:cancel_registration", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}).Inline()}},
			markup.InlineKeyboard...,
		)
	} else {
		markup.InlineKeyboard = append(
			[][]tele.InlineButton{{*h.layout.Button(c, "user:events:event:register", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}).Inline()}},
			markup.InlineKeyboard...,
		)
	}

	var maxRegistrationEnd time.Time
	if user.Role == valueobject.Student {
		maxRegistrationEnd = event.RegistrationEnd
	} else {
		if event.RegistrationEnd.Before(utils.GetMaxRegisteredEndTime(event.StartTime)) {
			maxRegistrationEnd = event.RegistrationEnd
		} else {
			maxRegistrationEnd = utils.GetMaxRegisteredEndTime(event.StartTime)
		}
	}

	_ = c.Edit(
		banner.Events.Caption(h.layout.Text(c, "event_text", struct {
			Name                  string
			ClubName              string
			Description           string
			Location              string
			StartTime             string
			EndTime               string
			RegistrationEnd       string
			MaxParticipants       int
			ParticipantsCount     int
			AfterRegistrationText string
			IsRegistered          bool
		}{
			Name:                  event.Name,
			ClubName:              club.Name,
			Description:           event.Description,
			Location:              event.Location,
			StartTime:             event.StartTime.In(location.Location()).Format("02.01.2006 15:04"),
			EndTime:               endTime,
			RegistrationEnd:       maxRegistrationEnd.In(location.Location()).Format("02.01.2006 15:04"),
			MaxParticipants:       event.MaxParticipants,
			ParticipantsCount:     participantsCount,
			AfterRegistrationText: event.AfterRegistrationText,
			IsRegistered:          registered,
		})),
		markup,
	)
	return nil
}

func (h Handler) eventCancelRegistration(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	eventID := callbackData[0]
	page := callbackData[1]

	err := h.eventParticipantService.Delete(context.Background(), eventID, c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while delete event participant: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "core:hide"),
		)
	}

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	user, err := h.userService.Get(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get user: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	var registered bool
	_, errGetParticipant := h.eventParticipantService.Get(context.Background(), eventID, c.Sender().ID)
	if errGetParticipant != nil {
		if !errors.Is(errGetParticipant, gorm.ErrRecordNotFound) {
			h.logger.Errorf("(user: %d) error while get participant: %v", c.Sender().ID, errGetParticipant)
			return c.Edit(
				banner.Events.Caption(h.layout.Text(c, "technical_issues", errGetParticipant.Error())),
				h.layout.Markup(c, "user:events:back", struct {
					Page string
				}{
					Page: page,
				}),
			)
		}
	} else {
		registered = true
	}

	participantsCount, err := h.eventParticipantService.CountByEventID(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get participants count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	if c.Callback().Unique == "event_register" {
		if !registered {
			var roleAllowed bool
			var registrationActive bool
			for _, role := range event.AllowedRoles {
				if role == string(user.Role) {
					roleAllowed = true
				}
			}

			if user.Role == valueobject.Student {
				registrationActive = event.RegistrationEnd.After(time.Now().In(location.Location()))
			} else {
				registrationActive = utils.GetMaxRegisteredEndTime(event.StartTime).After(time.Now().In(location.Location())) && event.RegistrationEnd.After(time.Now().In(location.Location()))
			}

			if (event.MaxParticipants == 0 || participantsCount < event.MaxParticipants) && registrationActive && roleAllowed {
				_, err = h.eventParticipantService.Register(context.Background(), eventID, c.Sender().ID)
				if err != nil {
					h.logger.Errorf("(user: %d) error while register to event: %v", c.Sender().ID, err)
					return c.Edit(
						banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
						h.layout.Markup(c, "user:events:back", struct {
							Page string
						}{
							Page: page,
						}),
					)
				}

				if participantsCount+1 == event.ExpectedParticipants {
					errSendWarning := h.notificationService.SendClubWarning(event.ClubID,
						h.layout.Text(c, "expected_participants_reached_warning", struct {
							Name              string
							ParticipantsCount int
						}{
							Name:              event.Name,
							ParticipantsCount: participantsCount + 1,
						}),
						h.layout.Markup(c, "core:hide"),
					)
					if errSendWarning != nil {
						h.logger.Errorf("(user: %d) error while send expected participants reached warning: %v", c.Sender().ID, errSendWarning)
					}
				}

				if participantsCount+1 == event.MaxParticipants {
					errSendWarning := h.notificationService.SendClubWarning(event.ClubID,
						h.layout.Text(c, "max_participants_reached_warning", struct {
							Name              string
							ParticipantsCount int
						}{
							Name:              event.Name,
							ParticipantsCount: participantsCount + 1,
						}),
						h.layout.Markup(c, "core:hide"),
					)
					if errSendWarning != nil {
						h.logger.Errorf("(user: %d) error while send expected participants reached warning: %v", c.Sender().ID, errSendWarning)
					}
				}
				registered = true
			} else {
				switch {
				case event.RegistrationEnd.Before(time.Now().In(location.Location())):
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "registration_ended"),
						ShowAlert: true,
					})
				case event.MaxParticipants > 0 && participantsCount >= event.MaxParticipants:
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "max_participants_reached"),
						ShowAlert: true,
					})
				case !roleAllowed:
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "not_allowed_role"),
						ShowAlert: true,
					})
				}
			}
		}
	}

	endTime := event.EndTime.In(location.Location()).Format("02.01.2006 15:04")
	if event.EndTime.Year() == 1 {
		endTime = ""
	}

	markup := h.layout.Markup(c, "user:events:event", struct {
		ID   string
		Page string
	}{
		ID:   eventID,
		Page: page,
	})
	if registered {
		markup.InlineKeyboard = append(
			[][]tele.InlineButton{{*h.layout.Button(c, "user:events:event:cancel_registration", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}).Inline()}},
			markup.InlineKeyboard...,
		)
	} else {
		markup.InlineKeyboard = append(
			[][]tele.InlineButton{{*h.layout.Button(c, "user:events:event:register", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}).Inline()}},
			markup.InlineKeyboard...,
		)
	}

	var maxRegistrationEnd time.Time
	if user.Role == valueobject.Student {
		maxRegistrationEnd = event.RegistrationEnd
	} else {
		if event.RegistrationEnd.Before(utils.GetMaxRegisteredEndTime(event.StartTime)) {
			maxRegistrationEnd = event.RegistrationEnd
		} else {
			maxRegistrationEnd = utils.GetMaxRegisteredEndTime(event.StartTime)
		}
	}

	_ = c.Edit(
		banner.Events.Caption(h.layout.Text(c, "event_text", struct {
			Name                  string
			ClubName              string
			Description           string
			Location              string
			StartTime             string
			EndTime               string
			RegistrationEnd       string
			MaxParticipants       int
			ParticipantsCount     int
			AfterRegistrationText string
			IsRegistered          bool
		}{
			Name:                  event.Name,
			ClubName:              club.Name,
			Description:           event.Description,
			Location:              event.Location,
			StartTime:             event.StartTime.In(location.Location()).Format("02.01.2006 15:04"),
			EndTime:               endTime,
			RegistrationEnd:       maxRegistrationEnd.In(location.Location()).Format("02.01.2006 15:04"),
			MaxParticipants:       event.MaxParticipants,
			ParticipantsCount:     participantsCount,
			AfterRegistrationText: event.AfterRegistrationText,
			IsRegistered:          registered,
		})),
		markup,
	)
	return nil
}

func (h Handler) eventExportToICS(c tele.Context) error {
	eventID := c.Callback().Data
	h.logger.Infof("(user: %d) export event to ics (event_id=%s)", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "core:hide"),
		)
	}

	ics, err := calendar.ExportEventToICS(*event)
	if err != nil {
		h.logger.Errorf("(user: %d) error while export event to ics: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "core:hide"),
		)
	}

	fileName := fmt.Sprintf("%s.ics", event.Name)
	doc := &tele.Document{
		File: tele.FromReader(bytes.NewReader(ics)),
		Caption: h.layout.Text(c, "event_exported_text", struct {
			FileName string
		}{
			FileName: fileName,
		}),
		FileName: fileName,
	}

	return c.Send(
		doc,
		h.layout.Markup(c, "core:hide"),
	)
}

func (h Handler) myEvents(c tele.Context) error {
	const eventsOnPage = 5
	h.logger.Infof("(user: %d) edit my events list", c.Sender().ID)

	var (
		p           int
		prevPage    int
		nextPage    int
		err         error
		eventsCount int64
		events      []dto.UserEvent
		rows        []tele.Row
		menuRow     tele.Row
	)
	if c.Callback().Unique != "personalAccount_myEvents" {
		p, err = strconv.Atoi(c.Callback().Data)
		if err != nil {
			return errorz.ErrInvalidCallbackData
		}
	}

	user, err := h.userService.Get(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting user from db: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	eventsCount, err = h.userService.CountUserEvents(context.Background(), user.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get events count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	events, err = h.userService.GetUserEvents(context.Background(), user.ID, eventsOnPage, p*eventsOnPage)
	if err != nil {
		h.logger.Errorf(
			"(user: %d) error while get my events (offset=%d, limit=%d): %v",
			c.Sender().ID,
			p*eventsOnPage,
			eventsOnPage,
			err,
		)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	markup := c.Bot().NewMarkup()
	for _, event := range events {
		rows = append(rows, markup.Row(*h.layout.Button(c, "user:myEvents:event", struct {
			ID        string
			Name      string
			Page      int
			IsOver    bool
			IsVisited bool
		}{
			ID:        event.ID,
			Name:      event.Name,
			Page:      p,
			IsOver:    event.IsOver(0),
			IsVisited: event.IsVisited,
		})))
	}

	pagesCount := (int(eventsCount) - 1) / eventsOnPage
	if p == 0 {
		prevPage = pagesCount
	} else {
		prevPage = p - 1
	}

	if p >= pagesCount {
		nextPage = 0
	} else {
		nextPage = p + 1
	}

	menuRow = append(menuRow,
		*h.layout.Button(c, "user:myEvents:prev_page", struct {
			Page int
		}{
			Page: prevPage,
		}),
		*h.layout.Button(c, "core:page_counter", struct {
			Page       int
			PagesCount int
		}{
			Page:       p + 1,
			PagesCount: pagesCount + 1,
		}),
		*h.layout.Button(c, "user:myEvents:next_page", struct {
			Page int
		}{
			Page: nextPage,
		}),
	)

	rows = append(
		rows,
		menuRow,
		markup.Row(*h.layout.Button(c, "personalAccount:back")),
	)

	markup.Inline(rows...)

	h.logger.Infof(
		"(user: %d) user my events list (pages_count=%d, page=%d, events_count=%d, next_page=%d, prev_page=%d)",
		c.Sender().ID,
		pagesCount,
		p,
		eventsCount,
		nextPage,
		prevPage,
	)

	_ = c.Edit(
		banner.Events.Caption(h.layout.Text(c, "my_events_list")),
		markup,
	)
	return nil
}

func (h Handler) myEvent(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	eventID := callbackData[0]
	page := callbackData[1]
	h.logger.Infof("(user: %d) edit my event (event_id=%s)", c.Sender().ID, eventID)

	user, err := h.userService.Get(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get user: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get my event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:myEvents:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:myEvents:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	eventParticipant, err := h.eventParticipantService.Get(context.Background(), eventID, c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting event participant from db: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:myEvents:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	participantsCount, err := h.eventParticipantService.CountByEventID(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get participants count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "user:events:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	endTime := event.EndTime.In(location.Location()).Format("02.01.2006 15:04")
	if event.EndTime.Year() == 1 {
		endTime = ""
	}

	var maxRegistrationEnd time.Time
	if user.Role == valueobject.Student {
		maxRegistrationEnd = event.RegistrationEnd
	} else {
		if event.RegistrationEnd.Before(utils.GetMaxRegisteredEndTime(event.StartTime)) {
			maxRegistrationEnd = event.RegistrationEnd
		} else {
			maxRegistrationEnd = utils.GetMaxRegisteredEndTime(event.StartTime)
		}
	}

	_ = c.Edit(
		banner.Events.Caption(h.layout.Text(c, "my_event_text", struct {
			Name                  string
			ClubName              string
			Description           string
			Location              string
			StartTime             string
			EndTime               string
			RegistrationEnd       string
			MaxParticipants       int
			ParticipantsCount     int
			AfterRegistrationText string
			IsOver                bool
			IsVisited             bool
		}{
			Name:                  event.Name,
			ClubName:              club.Name,
			Description:           event.Description,
			Location:              event.Location,
			StartTime:             event.StartTime.In(location.Location()).Format("02.01.2006 15:04"),
			EndTime:               endTime,
			RegistrationEnd:       maxRegistrationEnd.In(location.Location()).Format("02.01.2006 15:04"),
			MaxParticipants:       event.MaxParticipants,
			ParticipantsCount:     participantsCount,
			AfterRegistrationText: event.AfterRegistrationText,
			IsOver:                event.IsOver(0),
			IsVisited:             eventParticipant.IsEventQr || eventParticipant.IsUserQr,
		})),
		h.layout.Markup(c, "user:myEvents:event", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}))
	return nil
}

func (h Handler) myEventCancelRegistration(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	eventID := callbackData[0]
	// page := callbackData[1]

	err := h.eventParticipantService.Delete(context.Background(), eventID, c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while delete event participant: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "core:hide"),
		)
	}

	return h.menuHandler.EditMenu(c)
}

func (h Handler) mailingSwitch(c tele.Context) error {
	h.logger.Infof("(user: %d) mailing switch (club_id=%s)", c.Sender().ID, c.Callback().Data)
	clubID := c.Callback().Data

	allowed, err := h.userService.IgnoreMailing(context.Background(), c.Sender().ID, clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while switching ignore mailing: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	return c.Edit(
		utils.ChangeMessageText(c.Message(), utils.GetMessageText(c.Message())),
		h.layout.Markup(c, "mailing", struct {
			ClubID  string
			Allowed bool
		}{
			ClubID:  clubID,
			Allowed: allowed,
		}),
	)
}

func (h Handler) changeRole(c tele.Context) error {
	_ = c.Respond()

	user, err := h.userService.Get(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting user from db: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues")),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	confirmationMessage, err := c.Bot().Send(
		c.Chat(),
		h.layout.Text(c, "change_role_confirmation"),
		h.layout.Markup(c, "personalAccount:change_role:confirmation"),
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while edit message: %v", c.Sender().ID, err)
		return c.Send(
			banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	response, err := h.input.Get(
		context.Background(),
		c.Sender().ID,
		0,
		h.layout.Callback("changeRole:confirm"),
		h.layout.Callback("changeRole:cancel"),
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get message: %v", c.Sender().ID, err)
		return c.Send(
			banner.PersonalAccount.Caption(h.layout.Text(c, "input_error", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}
	if response.Callback == nil {
		if response.Message != nil {
			_ = c.Bot().Delete(response.Message)
		}
		if confirmationMessage != nil {
			_ = c.Bot().Delete(confirmationMessage)
		}
		h.logger.Errorf("(user: %d) error while get message: callback is nil", c.Sender().ID)
		return c.Edit(
			banner.PersonalAccount.Caption(h.layout.Text(c, "input_should_be_callback")),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	if confirmationMessage != nil {
		_ = c.Bot().Delete(confirmationMessage)
	}

	if strings.Contains(response.Callback.Data, "cancel") {
		return nil
	}

	// TODO: refactor
	markup := h.layout.Markup(c, "changeRole:choose_new_role")
	if user.Role == valueobject.GrantUser {
		markup.InlineKeyboard = markup.InlineKeyboard[1:] // убираем возможность перехода к роли абитуриента
	}

	err = c.Edit(
		banner.PersonalAccount.Caption(h.layout.Text(c, "change_role_text")),
		markup,
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while edit message: %v", c.Sender().ID, err)
		return c.Send(
			banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	response, err = h.input.Get(
		context.Background(),
		c.Sender().ID,
		0,
		h.layout.Callback("changeRole:grant_user"),
		h.layout.Callback("changeRole:student"),
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get message: %v", c.Sender().ID, err)
		return c.Send(
			banner.PersonalAccount.Caption(h.layout.Text(c, "input_error", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}
	if response.Callback == nil {
		if response.Message != nil {
			_ = c.Bot().Delete(response.Message)
		}
		h.logger.Errorf("(user: %d) error while get message: callback is nil", c.Sender().ID)
		return c.Edit(
			banner.PersonalAccount.Caption(h.layout.Text(c, "input_should_be_callback")),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	switch {
	case strings.Contains(response.Callback.Data, "grant_user"):
		return h.changeRoleGrantUser(c)
	case strings.Contains(response.Callback.Data, "student"):
		return h.changeRoleStudent(c)
	default:
		return c.Edit(
			banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues")),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}
}

func (h Handler) changeRoleGrantUser(c tele.Context) error {
	grantChatID := h.grantChatID
	member, err := c.Bot().ChatMemberOf(&tele.Chat{ID: grantChatID}, &tele.User{ID: c.Sender().ID})
	if err != nil {
		h.logger.Errorf("(user: %d) error while verification user's membership in the grant chat: %v", c.Sender().ID, err)
		return c.Send(
			banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	if member.Role != tele.Creator && member.Role != tele.Administrator && member.Role != tele.Member {
		return c.Edit(
			banner.PersonalAccount.Caption(h.layout.Text(c, "grant_user_required")),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	tempEmail, err := valueobject.NewEmail("temp@example.com")
	if err != nil {
		h.logger.Errorf("(user: %d) error creating temp email: %v", c.Sender().ID, err)
		return c.Edit(
			banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues")),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}
	err = h.userService.ChangeRole(context.Background(), c.Sender().ID, valueobject.GrantUser, tempEmail)
	if err != nil {
		h.logger.Errorf("(user: %d) error while change role: %v", c.Sender().ID, err)
		return c.Edit(
			banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues")),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}
	return h.personalAccount(c)
}

func (h Handler) changeRoleStudent(c tele.Context) error {
	inputCollector := collector.New()
	_ = c.Edit(
		banner.PersonalAccount.Caption(h.layout.Text(c, "email_request")),
		h.layout.Markup(c, "personalAccount:back"),
	)
	inputCollector.Collect(c.Message())

	var (
		email     string
		doneEmail bool
	)
	for {
		response, errGet := h.input.Get(context.Background(), c.Sender().ID, 0)
		if response.Message != nil {
			inputCollector.Collect(response.Message)
		}
		switch {
		case response.Canceled:
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true, ExcludeLast: true})
			return nil
		case errGet != nil:
			h.logger.Errorf("(user: %d) error while input email: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.PersonalAccount.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "email_request"))),
				h.layout.Markup(c, "personalAccount:back"),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while input email: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.PersonalAccount.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "email_request"))),
				h.layout.Markup(c, "personalAccount:back"),
			)
		case !validator.Email(response.Message.Text, h.validEmailDomains):
			_ = inputCollector.Send(c,
				banner.PersonalAccount.Caption(h.layout.Text(c, "invalid_email")),
				h.layout.Markup(c, "personalAccount:back"),
			)
		case validator.Email(response.Message.Text, h.validEmailDomains):
			email = response.Message.Text
			emailVO, err := valueobject.NewEmail(email)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.PersonalAccount.Caption(h.layout.Text(c, "invalid_email")),
					h.layout.Markup(c, "personalAccount:back"),
				)
				continue
			}
			_, err = h.userService.GetByEmail(context.Background(), emailVO)
			if err == nil {
				_ = inputCollector.Send(c,
					banner.PersonalAccount.Caption(h.layout.Text(c, "user_with_this_email_already_exists")),
					h.layout.Markup(c, "personalAccount:back"),
				)
				continue
			}
			doneEmail = true
		}
		if doneEmail {
			break
		}
	}
	_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})

	user, err := h.userService.Get(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get user: %v", c.Sender().ID, err)
		return c.Send(
			banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	canResend, _, err := h.codesStorage.GetCanResend(c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting auth code from redis: %v", c.Sender().ID, err)
		return c.Send(
			banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
		)
	}
	var code string
	if canResend {
		loading, _ := c.Bot().Send(c.Chat(), h.layout.Text(c, "loading"))
		emailVO, err := valueobject.NewEmail(email)
		if err != nil {
			h.logger.Errorf("(user: %d) error creating email value object: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}

		code, err = h.userService.SendAuthCode(
			context.Background(),
			emailVO,
			c.Bot().Me.Username,
		)
		if err != nil {
			_ = c.Bot().Delete(loading)
			h.logger.Errorf("(user: %d) error while sending auth code: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}
		_ = c.Bot().Delete(loading)

		fioVO := user.GetFIO()
		err = h.emailsStorage.Set(
			c.Sender().ID,
			email,
			emails.EmailContext{
				FIO: fioVO,
			},
			h.emailTTL,
		)
		if err != nil {
			h.logger.Errorf("(user: %d) error while saving email to redis: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}

		emailVO, err = valueobject.NewEmail(email)
		if err != nil {
			h.logger.Errorf("(user: %d) error creating email value object: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}

		fioVO = user.GetFIO()
		err = h.codesStorage.Set(
			c.Sender().ID,
			code,
			codes.CodeTypeChangingRole,
			codes.CodeContext{
				Email: emailVO,
				FIO:   fioVO,
			},
			h.authTTL,
			h.resendTTL,
		)
		if err != nil {
			h.logger.Errorf("(user: %d) error while saving auth code to redis: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}

		h.logger.Infof("(user: %d) auth code sent on %s", c.Sender().ID, email)

		return c.Send(
			banner.PersonalAccount.Caption(h.layout.Text(c, "email_auth_link_sent")),
			h.layout.Markup(c, "changeRole:student:resendMenu"),
		)
	}

	return c.Send(
		banner.Auth.Caption(h.layout.Text(c,
			"resend_timeout",
			h.resendTTL.Minutes()),
		),
		h.layout.Markup(c, "changeRole:student:resendMenu"),
	)
}

func (h Handler) resendChangeRoleEmailConfirmationCode(c tele.Context) error {
	h.logger.Infof("(user: %d) resend auth code", c.Sender().ID)

	canResend, timeBeforeResend, err := h.codesStorage.GetCanResend(c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting auth code from redis: %v", c.Sender().ID, err)
		return c.Send(
			banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "personalAccount:back"),
		)
	}

	var code string
	var email emails.Email
	if canResend {
		email, err = h.emailsStorage.Get(c.Sender().ID)
		if err != nil && !errors.Is(err, redis.Nil) {
			h.logger.Errorf("(user: %d) error while getting user email from redis: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}

		if errors.Is(err, redis.Nil) {
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "session_expire")),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}

		loading, _ := c.Bot().Send(c.Chat(), h.layout.Text(c, "loading"))
		emailVO, err := valueobject.NewEmail(email.Email)
		if err != nil {
			_ = c.Bot().Delete(loading)
			h.logger.Errorf("(user: %d) error creating email value object: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}

		code, err = h.userService.SendAuthCode(
			context.Background(),
			emailVO,
			c.Bot().Me.Username,
		)
		if err != nil {
			_ = c.Bot().Delete(loading)
			h.logger.Errorf("(user: %d) error while sending auth code: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}
		_ = c.Bot().Delete(loading)

		err = h.emailsStorage.Set(
			c.Sender().ID,
			email.Email,
			email.EmailContext,
			h.emailTTL,
		)
		if err != nil {
			h.logger.Errorf("(user: %d) error while saving user email to redis: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}
		err = h.codesStorage.Set(
			c.Sender().ID,
			code,
			codes.CodeTypeChangingRole,
			codes.CodeContext{
				Email: emailVO,
				FIO:   email.EmailContext.FIO,
			},
			h.authTTL,
			h.resendTTL,
		)
		if err != nil {
			h.logger.Errorf("(user: %d) error while saving auth code to redis: %v", c.Sender().ID, err)
			return c.Send(
				banner.PersonalAccount.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "personalAccount:back"),
			)
		}

		h.logger.Infof("(user: %d) auth code sent on %s", c.Sender().ID, email.Email)

		return c.Edit(
			banner.PersonalAccount.Caption(h.layout.Text(c, "email_auth_link_resent")),
			h.layout.Markup(c, "changeRole:student:resendMenu"),
		)
	}

	return c.Respond(&tele.CallbackResponse{
		Text: h.layout.Text(c,
			"resend_timeout_with_time_before_resend",
			math.Round(timeBeforeResend.Minutes()),
		),
		ShowAlert: true,
	})
}

func (h Handler) UserSetup(group *tele.Group) {
	group.Handle(h.layout.Callback("mainMenu:cuClubs"), h.cuClubs)
	group.Handle(h.layout.Callback("cuClubs:prev_page"), h.cuClubs)
	group.Handle(h.layout.Callback("cuClubs:next_page"), h.cuClubs)
	group.Handle(h.layout.Callback("cuClubs:back"), h.cuClubs)
	group.Handle(h.layout.Callback("cuClubs:club:intro"), h.clubIntro)
	group.Handle(h.layout.Callback("cuClubs:club:intro:back"), h.clubIntro)
	group.Handle(h.layout.Callback("cuClubs:club:about"), h.clubAbout)

	group.Handle(h.layout.Callback("mainMenu:events"), h.digest)
	group.Handle(h.layout.Callback("digest:events"), h.eventsList)
	group.Handle(h.layout.Callback("digest:back"), h.digest)
	group.Handle(h.layout.Callback("user:events:prev_page"), h.eventsList)
	group.Handle(h.layout.Callback("user:events:next_page"), h.eventsList)
	group.Handle(h.layout.Callback("user:events:back"), h.digest)
	group.Handle(h.layout.Callback("user:events:event"), h.event)
	group.Handle(h.layout.Callback("user:events:event:cancel_registration"), h.eventCancelRegistration)
	group.Handle(h.layout.Callback("user:myEvents:event:export"), h.eventExportToICS)
	group.Handle(h.layout.Callback("user:myEvents:event:cancel_registration"), h.myEventCancelRegistration)
	group.Handle(h.layout.Callback("user:events:event:register"), h.event)

	group.Handle(h.layout.Callback("mainMenu:personalAccount"), h.personalAccount)
	group.Handle(h.layout.Callback("personalAccount:my_events"), h.myEvents)
	group.Handle(h.layout.Callback("user:myEvents:prev_page"), h.myEvents)
	group.Handle(h.layout.Callback("user:myEvents:next_page"), h.myEvents)
	group.Handle(h.layout.Callback("user:myEvents:event"), h.myEvent)
	group.Handle(h.layout.Callback("user:myEvents:back"), h.myEvents)
	group.Handle(h.layout.Callback("personalAccount:change_role"), h.changeRole)
	group.Handle(h.layout.Callback("changeRole:student:resend_email"), h.resendChangeRoleEmailConfirmationCode)
	group.Handle(h.layout.Callback("personalAccount:back"), h.personalAccount)

	group.Handle(h.layout.Callback("mainMenu:qr"), h.qrCode)

	group.Handle(h.layout.Callback("mailing:switch"), h.mailingSwitch)
}

func (h Handler) digest(c tele.Context) error {
	h.logger.Infof("(user: %d) send digest", c.Sender().ID)
	_ = c.Delete()
	loading, _ := c.Bot().Send(c.Chat(), h.layout.Text(c, "loading"))

	botUsername := c.Bot().Me.Username

	// Get events for the week
	weeklyEvents, err := h.eventService.GetWeeklyEvents(context.Background())
	if err != nil {
		h.logger.Errorf("(user: %d) error getting all events: %v", c.Sender().ID, err)
		return c.Send(h.layout.Text(c, "technical_issues", err.Error()))
	}

	if len(weeklyEvents) == 0 {
		return c.Send("На этой неделе мероприятий нет.", h.layout.Markup(c, "digest:menu"))
	}

	// Generate digest images
	images, err := h.eventService.GenerateWeeklyDigestImage(weeklyEvents)
	if err != nil {
		h.logger.Errorf("(user: %d) error generating digest: %v", c.Sender().ID, err)
		return c.Send(h.layout.Text(c, "technical_issues", err.Error()))
	}

	// Edit message to single image with caption and buttons
	imageBytes := images[0]
	photo := &tele.Photo{
		File:    tele.FromReader(bytes.NewReader(imageBytes)),
		Caption: h.generateDigestText(weeklyEvents, botUsername),
	}
	markup := h.layout.Markup(c, "digest:menu")

	_ = c.Bot().Delete(loading)
	return c.Send(photo, markup)
}

func (h Handler) generateDigestText(events []entity.Event, botUsername string) string {
	// Group events by day
	eventsByDay := make(map[time.Time][]entity.Event)
	for _, event := range events {
		day := event.StartTime.In(location.Location()).Truncate(24 * time.Hour)
		eventsByDay[day] = append(eventsByDay[day], event)
	}

	// Always use current week starting from Monday
	now := time.Now().In(location.Location())
	startOfWeek := now.AddDate(0, 0, -int(now.Weekday()-time.Monday))
	if now.Weekday() == time.Sunday {
		startOfWeek = now.AddDate(0, 0, -6)
	}
	startOfWeek = startOfWeek.Truncate(24 * time.Hour)

	// Generate 7 days
	var days []time.Time
	for i := 0; i < 7; i++ {
		days = append(days, startOfWeek.AddDate(0, 0, i))
	}

	text := "<b>Дайджест мероприятий на неделю</b>\n\n"

	for _, day := range days {
		weekday := day.Weekday()
		weekdayName := map[time.Weekday]string{
			time.Monday:    "Понедельник",
			time.Tuesday:   "Вторник",
			time.Wednesday: "Среда",
			time.Thursday:  "Четверг",
			time.Friday:    "Пятница",
			time.Saturday:  "Суббота",
			time.Sunday:    "Воскресенье",
		}[weekday]

		text += fmt.Sprintf("<b>%s (%d %s):</b>\n\n", weekdayName, day.Day(), getMonthName(day.Month()))

		dayEvents := eventsByDay[day]
		if len(dayEvents) == 0 {
			text += "<i>В этот день нет мероприятий</i>\n\n"
		} else {
			for _, event := range dayEvents {
				if time.Now().In(location.Location()).After(event.RegistrationEnd) {
					text += fmt.Sprintf("➡️ %s\n", event.Name)
				} else {
					link := fmt.Sprintf("https://t.me/%s?start=event_%s", botUsername, event.ID)
					text += fmt.Sprintf("➡️ <a href=\"%s\">%s</a>\n", link, event.Name)
				}
			}
			text += "\n"
		}
	}

	return text
}

func getMonthName(month time.Month) string {
	months := map[time.Month]string{
		time.January:   "января",
		time.February:  "февраля",
		time.March:     "марта",
		time.April:     "апреля",
		time.May:       "мая",
		time.June:      "июня",
		time.July:      "июля",
		time.August:    "августа",
		time.September: "сентября",
		time.October:   "октября",
		time.November:  "ноября",
		time.December:  "декабря",
	}
	return months[month]
}
