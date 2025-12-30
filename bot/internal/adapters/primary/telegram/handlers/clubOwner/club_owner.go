package clubowner

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils"

	"github.com/nlypage/intele"
	"github.com/nlypage/intele/collector"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/layout"
	"gorm.io/gorm"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/primary/telegram/handlers/middlewares"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/events"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/common/errorz"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/banner"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/location"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/validator"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/primary"
	"github.com/Badsnus/cu-clubs-bot/bot/pkg/logger/types"
)

type Handler struct {
	bot    *tele.Bot
	layout *layout.Layout
	logger *types.Logger
	input  *intele.InputManager

	eventsStorage *events.Storage

	clubService             primary.ClubService
	clubOwnerService        primary.ClubOwnerService
	userService             primary.UserService
	eventService            primary.EventService
	eventParticipantService primary.EventParticipantService
	qrService               primary.QrService
	notificationService     primary.NotifyService

	mailingChannelID       int64
	avatarChannelID        int64
	introChannelID         int64
	passLocationSubstrings []string
}

func NewHandler(
	b *tele.Bot,
	lt *layout.Layout,
	lg *types.Logger,
	in *intele.InputManager,
	eventsStorage *events.Storage,
	clubSvc primary.ClubService,
	clubOwnerSvc primary.ClubOwnerService,
	userSvc primary.UserService,
	eventSvc primary.EventService,
	eventParticipantSvc primary.EventParticipantService,
	qrSvc primary.QrService,
	notifySvc primary.NotifyService,
	mailingChannelID int64,
	avatarChannelID int64,
	introChannelID int64,
	passLocationSubstrings []string,
) *Handler {
	return &Handler{
		bot:    b,
		layout: lt,
		logger: lg,
		input:  in,

		eventsStorage: eventsStorage,

		clubService:             clubSvc,
		clubOwnerService:        clubOwnerSvc,
		userService:             userSvc,
		eventService:            eventSvc,
		eventParticipantService: eventParticipantSvc,
		qrService:               qrSvc,
		notificationService:     notifySvc,

		mailingChannelID:       mailingChannelID,
		avatarChannelID:        avatarChannelID,
		introChannelID:         introChannelID,
		passLocationSubstrings: passLocationSubstrings,
	}
}

func (h Handler) clubsList(c tele.Context) error {
	h.logger.Infof("(user: %d) edit clubs list", c.Sender().ID)

	var (
		err        error
		clubs      []entity.Club
		rows       []tele.Row
		clubsCount int
	)

	clubs, err = h.clubService.GetByOwnerID(context.Background(), c.Sender().ID)
	if err != nil {
		return err
	}
	clubsCount = len(clubs)

	markup := c.Bot().NewMarkup()
	for _, club := range clubs {
		rows = append(rows, markup.Row(*h.layout.Button(c, "clubOwner:myClubs:club", struct {
			ID   string
			Name string
		}{
			ID:   club.ID,
			Name: club.Name,
		})))
	}

	rows = append(
		rows,
		markup.Row(*h.layout.Button(c, "mainMenu:back")),
	)

	markup.Inline(rows...)

	h.logger.Infof("(user: %d) club owner clubs list", c.Sender().ID)
	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "my_clubs_list", clubsCount)),
		markup,
	)
}

func (h Handler) clubMenu(c tele.Context) error {
	clubID := c.Callback().Data
	if clubID == "" {
		return errorz.ErrInvalidCallbackData
	}

	h.logger.Infof("(user: %d) edit club menu (club_id=%s)", c.Sender().ID, clubID)

	clubOwners, err := h.clubOwnerService.GetByClubID(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club owners: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	clubs, err := h.clubService.GetByOwnerID(context.Background(), c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get clubs: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "clubs", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	menuMarkup := h.layout.Markup(c, "clubOwner:club:menu", struct {
		ID string
	}{
		ID: clubID,
	})

	if len(clubs) > 0 {
		menuMarkup.InlineKeyboard = append(menuMarkup.InlineKeyboard, []tele.InlineButton{*h.layout.Button(c, "clubOwner:myClubs:back").Inline()})
	}

	clubAvatar, err := h.clubService.GetAvatar(context.Background(), club.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get clubs: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "clubs", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	clubIntro, err := h.clubService.GetIntro(context.Background(), club.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get clubs: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "clubs", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	_ = c.Delete()

	if clubIntro != nil {
		videoNote := &tele.VideoNote{
			File: *clubIntro,
		}

		_ = c.Send(videoNote, h.layout.Markup(c, "core:hide"))
	}

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
			Caption: h.layout.Text(c, "club_owner_club_menu_text", struct {
				Club   entity.Club
				Owners []dto.ClubOwner
			}{
				Club:   *club,
				Owners: clubOwners,
			}),
		}

		return c.Send(
			caption,
			menuMarkup,
		)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_owner_club_menu_text", struct {
			Club   entity.Club
			Owners []dto.ClubOwner
		}{
			Club:   *club,
			Owners: clubOwners,
		})),
		menuMarkup,
	)
}

func (h Handler) clubMailing(c tele.Context) error {
	h.logger.Infof("(user: %d) edit club mailing (club_id=%s)", c.Sender().ID, c.Callback().Data)
	clubID := c.Callback().Data

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}
	inputCollector := collector.New()

	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_input_mailing")),
		h.layout.Markup(c, "clubOwner:club:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		message interface{}
		done    bool
	)
	for !done {
		h.logger.Debug("waiting for input")
		response, errGet := h.input.Get(context.Background(), c.Sender().ID, 0)
		if response.Message != nil {
			inputCollector.Collect(response.Message)
		}
		switch {
		case response.Canceled:
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true, ExcludeLast: true})
			return nil
		case errGet != nil:
			h.logger.Errorf("(user: %d) error while get message: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "club_input_mailing"))),
				h.layout.Markup(c, "clubOwner:club:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while get message: message is nil", c.Sender().ID)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "club_input_mailing"))),
				h.layout.Markup(c, "clubOwner:club:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case !validator.MailingText(utils.GetMessageText(response.Message), nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_mailing_text")),
				h.layout.Markup(c, "clubOwner:club:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case validator.MailingText(utils.GetMessageText(response.Message), nil):
			message = utils.ChangeMessageText(
				response.Message,
				h.layout.Text(c, "club_mailing", struct {
					ClubName string
					Text     string
				}{
					ClubName: club.Name,
					Text:     utils.GetMessageText(response.Message),
				}),
			)
			done = true
		}
	}
	_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})

	isCorrectMarkup := h.layout.Markup(c, "clubOwner:isMailingCorrect")
	confirmMessage, err := c.Bot().Send(
		c.Chat(),
		message,
		isCorrectMarkup,
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while copy message: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	response, err := h.input.Get(
		context.Background(),
		c.Sender().ID,
		0,
		h.layout.Callback("clubOwner:confirmMailing"),
		h.layout.Callback("clubOwner:cancelMailing"),
	)
	_ = c.Bot().Delete(confirmMessage)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get message: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "input_error", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}
	if response.Callback == nil {
		h.logger.Errorf("(user: %d) error while get message: callback is nil", c.Sender().ID)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "input_error", "callback is nil")),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	if strings.Contains(response.Callback.Data, "cancel") {
		h.logger.Infof("(user: %d) cancel club mailing (club_id=%s)", c.Sender().ID, club.ID)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "mailing_canceled")),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	h.logger.Infof("(user: %d) sending club mailing (club_id=%s)", c.Sender().ID, club.ID)
	clubUsers, err := h.userService.GetUsersByClubID(context.Background(), club.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club users: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	loading, _ := c.Bot().Send(c.Chat(), h.layout.Text(c, "loading"))
	for _, user := range clubUsers {
		if user.IsMailingAllowed(club.ID) {
			chat, err := c.Bot().ChatByID(user.ID)
			if err != nil {
				continue
			}

			_, _ = c.Bot().Send(
				chat,
				message,
				h.layout.Markup(c, "mailing", struct {
					ClubID  string
					Allowed bool
				}{
					ClubID:  club.ID,
					Allowed: true,
				}),
			)
		}
	}

	h.logger.Infof("(user: %d) club mailing sent (club_id=%s)", c.Sender().ID, club.ID)

	mailingChannel, err := c.Bot().ChatByID(h.mailingChannelID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get mailing channel: %v", c.Sender().ID, err)
	} else {
		_, err = c.Bot().Send(
			mailingChannel,
			message,
		)
		if err != nil {
			h.logger.Errorf("(user: %d) error while send message to mailing channel: %v", c.Sender().ID, err)
		}
	}

	_ = c.Bot().Delete(loading)
	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "mailing_sent")),
		h.layout.Markup(c, "clubOwner:club:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
}

func (h Handler) clubSettings(c tele.Context) error {
	if c.Callback().Data == "" {
		return errorz.ErrInvalidCallbackData
	}
	h.logger.Infof("(user: %d) edit club settings (club_id=%s)", c.Sender().ID, c.Callback().Data)

	club, err := h.clubService.Get(context.Background(), c.Callback().Data)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	clubOwners, err := h.clubOwnerService.GetByClubID(context.Background(), c.Callback().Data)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club owners: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_settings_text", struct {
			Club   entity.Club
			Owners []dto.ClubOwner
		}{
			Club:   *club,
			Owners: clubOwners,
		})),
		h.layout.Markup(c, "clubOwner:club:settings", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
}

func (h Handler) addOwner(c tele.Context) error {
	if c.Callback().Data == "" {
		return errorz.ErrInvalidCallbackData
	}
	clubID := c.Callback().Data

	inputCollector := collector.New()
	inputCollector.Collect(c.Message())
	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	h.logger.Infof("(user: %d) add club owner (club_id=%s)", c.Sender().ID, clubID)
	_ = c.Edit(
		banner.Menu.Caption(h.layout.Text(c, "input_user_id")),
		h.layout.Markup(c, "clubOwner:club:settings:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)

	var (
		user *entity.User
		done bool
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
			_ = inputCollector.Send(c,
				banner.Menu.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_user_id"))),
				h.layout.Markup(c, "clubOwner:club:settings:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case response.Message == nil:
			_ = inputCollector.Send(c,
				banner.Menu.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_user_id"))),
				h.layout.Markup(c, "clubOwner:club:settings:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		default:
			userID, err := strconv.ParseInt(response.Message.Text, 10, 64)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.Menu.Caption(h.layout.Text(c, "input_user_id")),
					h.layout.Markup(c, "clubOwner:club:settings:back", struct {
						ID string
					}{
						ID: club.ID,
					}),
				)
				break
			}

			user, err = h.userService.Get(context.Background(), userID)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.Menu.Caption(h.layout.Text(c, "user_not_found", struct {
						ID   int64
						Text string
					}{
						ID:   userID,
						Text: h.layout.Text(c, "input_user_id"),
					})),
					h.layout.Markup(c, "clubOwner:club:settings:back", struct {
						ID string
					}{
						ID: club.ID,
					}),
				)
				break
			}
			done = true
		}
		if done {
			break
		}
	}

	_, err = h.clubOwnerService.Add(context.Background(), user.ID, club.ID)
	if err != nil {
		_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
		h.logger.Errorf(
			"(user: %d) error while add club owner (club_id=%s, user_id=%d): %v",
			c.Sender().ID,
			clubID,
			user.ID,
			err,
		)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	h.logger.Infof(
		"(user: %d) club owner added (club_id=%s, user_id=%d)",
		c.Sender().ID,
		clubID,
		user.ID,
	)

	_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
	return c.Send(
		banner.Menu.Caption(h.layout.Text(c, "club_owner_added", struct {
			Club entity.Club
			User entity.User
		}{
			Club: *club,
			User: *user,
		})),
		h.layout.Markup(c, "clubOwner:club:settings:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
}

func (h Handler) profile(c tele.Context) error {
	if c.Callback().Data == "" {
		return errorz.ErrInvalidCallbackData
	}
	clubID := c.Callback().Data

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "profile_text")),
		h.layout.Markup(c, "clubOwner:club:settings:profile", struct {
			ID         string
			ShouldShow bool
		}{
			ID:         clubID,
			ShouldShow: club.ShouldShow,
		}),
	)
}

func (h Handler) setClubName(c tele.Context) error {
	h.logger.Infof("(user: %d) set club name", c.Sender().ID)

	club, err := h.clubService.Get(context.Background(), c.Callback().Data)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_club_name")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		clubName string
		done     bool
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
			h.logger.Errorf("(user: %d) error while input club name: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_name"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case response.Message == nil:
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_name"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case !validator.ClubName(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_club_name")),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case validator.ClubName(response.Message.Text, nil):
			clubName = response.Message.Text
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	club.Name = clubName
	_, err = h.clubService.Update(context.Background(), club)
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return c.Send(
				banner.Menu.Caption(h.layout.Text(c, "club_already_exists")),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		}

		h.logger.Errorf("(user: %d) error while update club name: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_name_set")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
}

func (h Handler) setClubDescription(c tele.Context) error {
	h.logger.Infof("(user: %d) set club description", c.Sender().ID)

	club, err := h.clubService.Get(context.Background(), c.Callback().Data)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_club_description")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		clubDescription string
		done            bool
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
			h.logger.Errorf("(user: %d) error while input club description: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_description"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case response.Message == nil:
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_description"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case !validator.ClubDescription(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_club_description")),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case validator.ClubDescription(response.Message.Text, nil):
			clubDescription = response.Message.Text
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	club.Description = clubDescription
	_, err = h.clubService.Update(context.Background(), club)
	if err != nil {
		h.logger.Errorf("(user: %d) error while update club description: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_description_set")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
}

func (h Handler) setClubLink(c tele.Context) error {
	h.logger.Infof("(user: %d) set club link", c.Sender().ID)

	club, err := h.clubService.Get(context.Background(), c.Callback().Data)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_club_link")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		clubLink string
		done     bool
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
			h.logger.Errorf("(user: %d) error while input club link: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_link"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case response.Message == nil:
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_link"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case !validator.ClubLink(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_club_link")),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case validator.ClubLink(response.Message.Text, nil):
			clubLink = response.Message.Text
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	club.Link = clubLink
	_, err = h.clubService.Update(context.Background(), club)
	if err != nil {
		h.logger.Errorf("(user: %d) error while update club link: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_link_set")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
}

func (h Handler) setClubAvatar(c tele.Context) error {
	h.logger.Infof("(user: %d) set club avatar", c.Sender().ID)

	club, err := h.clubService.Get(context.Background(), c.Callback().Data)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_club_avatar")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		clubAvatarID string
		done         bool
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
			h.logger.Errorf("(user: %d) error while input club link: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_avatar"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case response.Message == nil:
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_avatar"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		default:
			if response.Message.Photo == nil {
				_ = inputCollector.Send(c,
					banner.ClubOwner.Caption(h.layout.Text(c, "invalid_club_avatar")),
					h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
						ID string
					}{
						ID: club.ID,
					}),
				)
				continue
			}

			msg, err := h.bot.Send(&tele.Chat{ID: h.avatarChannelID}, response.Message.Photo)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.ClubOwner.Caption(h.layout.Text(c, "invalid_club_avatar")),
					h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
						ID string
					}{
						ID: club.ID,
					}),
				)
				continue
			}

			clubAvatarID = msg.Photo.FileID
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	club.AvatarID = clubAvatarID
	_, err = h.clubService.Update(context.Background(), club)
	if err != nil {
		h.logger.Errorf("(user: %d) error while update club avatar: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_avatar_set")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
}

func (h Handler) setClubIntro(c tele.Context) error {
	h.logger.Infof("(user: %d) set club intro", c.Sender().ID)

	club, err := h.clubService.Get(context.Background(), c.Callback().Data)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_club_intro")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		clubIntroID string
		done        bool
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
			h.logger.Errorf("(user: %d) error while input club intro: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_intro"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case response.Message == nil:
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_intro"))),
				h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		default:
			if response.Message.VideoNote == nil {
				_ = inputCollector.Send(c,
					banner.ClubOwner.Caption(h.layout.Text(c, "invalid_club_intro")),
					h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
						ID string
					}{
						ID: club.ID,
					}),
				)
				continue
			}

			msg, err := h.bot.Send(&tele.Chat{ID: h.introChannelID}, response.Message.VideoNote)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.ClubOwner.Caption(h.layout.Text(c, "invalid_club_intro")),
					h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
						ID string
					}{
						ID: club.ID,
					}),
				)
				continue
			}

			clubIntroID = msg.VideoNote.FileID
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	club.IntroID = clubIntroID
	_, err = h.clubService.Update(context.Background(), club)
	if err != nil {
		h.logger.Errorf("(user: %d) error while update club intro: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_intro_set")),
		h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
}

func (h Handler) shouldShow(c tele.Context) error {
	if c.Callback().Data == "" {
		return errorz.ErrInvalidCallbackData
	}
	clubID := c.Callback().Data

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	club.ShouldShow = !club.ShouldShow
	updatedClub, err := h.clubService.Update(context.Background(), club)
	if err != nil {
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:profile:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "profile_text")),
		h.layout.Markup(c, "clubOwner:club:settings:profile", struct {
			ID         string
			ShouldShow bool
		}{
			ID:         updatedClub.ID,
			ShouldShow: updatedClub.ShouldShow,
		}),
	)
}

func (h Handler) subscriptionAccess(c tele.Context) error {
	clubID := c.Callback().Data

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:back", struct {
				ID string
			}{
				ID: clubID,
			}),
		)
	}

	if c.Callback().Unique == "clOwner_cl_sub_acs_reqr" {
		if club.SubscriptionRequireAllowed {
			club.SubscriptionRequired = !club.SubscriptionRequired
			club, err = h.clubService.Update(context.Background(), club)
			if err != nil {
				h.logger.Errorf("(user: %d) error while update club: %v", c.Sender().ID, err)
				return c.Send(
					banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
					h.layout.Markup(c, "clubOwner:club:settings:back", struct {
						ID string
					}{
						ID: clubID,
					}),
				)
			}
		} else {
			return c.Respond(&tele.CallbackResponse{
				Text:      h.layout.Text(c, "subscription_access_not_allowed"),
				ShowAlert: true,
			})
		}
	}

	channelsNames := make([]string, len(club.ChannelsIDs))
	for i, id := range club.ChannelsIDs {
		chat, err := c.Bot().ChatByID(id)
		if err != nil {
			h.logger.Errorf("(user: %d) error while get channel: %v", c.Sender().ID, err)
			return c.Send(
				banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "clubOwner:club:settings:back", struct {
					ID string
				}{
					ID: clubID,
				}),
			)
		}

		channelsNames[i] = chat.Username
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "subscription_access_text", struct {
			ChannelsNames []string
		}{
			ChannelsNames: channelsNames,
		})),
		h.layout.Markup(c, "clubOwner:club:settings:subscription_access", struct {
			ID                   string
			SubscriptionRequired bool
		}{
			ID:                   clubID,
			SubscriptionRequired: club.SubscriptionRequired,
		}),
	)
}

func (h Handler) addChannelID(c tele.Context) error {
	h.logger.Infof("(user: %d) add club channel id", c.Sender().ID)

	club, err := h.clubService.Get(context.Background(), c.Callback().Data)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_channel_id")),
		h.layout.Markup(c, "clubOwner:club:settings:subscription_access:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		channelID int64
		done      bool
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
			h.logger.Errorf("(user: %d) error while input channel id: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_channel_id"))),
				h.layout.Markup(c, "clubOwner:club:settings:subscription_access:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case response.Message == nil:
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_channel_id"))),
				h.layout.Markup(c, "clubOwner:club:settings:subscription_access:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case !validator.ChannelID(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_channel_id")),
				h.layout.Markup(c, "clubOwner:club:settings:subscription_access:back", struct {
					ID string
				}{
					ID: club.ID,
				}),
			)
		case validator.ChannelID(response.Message.Text, nil):
			channelID, _ = strconv.ParseInt(response.Message.Text, 10, 64)
			_, err := c.Bot().ChatByID(channelID)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.ClubOwner.Caption(h.layout.Text(c, "invalid_channel_id")),
					h.layout.Markup(c, "clubOwner:club:settings:subscription_access:back", struct {
						ID string
					}{
						ID: club.ID,
					}),
				)
				continue
			}

			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	club.ChannelsIDs = append(club.ChannelsIDs, channelID)
	_, err = h.clubService.Update(context.Background(), club)
	if err != nil {
		h.logger.Errorf("(user: %d) error while add channel id: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:subscription_access:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "channel_id_add")),
		h.layout.Markup(c, "clubOwner:club:settings:subscription_access:back", struct {
			ID string
		}{
			ID: club.ID,
		}),
	)
}

func (h Handler) warnings(c tele.Context) error {
	var clubID string
	if c.Callback().Unique == "clubOwner_club_warnings" {
		h.logger.Infof("(user: %d) club warning settings (club_id=%s)", c.Sender().ID, c.Callback().Data)
		if c.Callback().Data == "" {
			return errorz.ErrInvalidCallbackData
		}
		clubID = c.Callback().Data
	}

	if c.Callback().Unique == "cOwner_warnings" {
		h.logger.Infof("(user: %d) club owner edit warning settings (club_id=%s, user_id=%s)", c.Sender().ID, c.Callback().Data, c.Callback().Data)
		callbackData := strings.Split(c.Callback().Data, " ")
		if len(callbackData) != 2 {
			return errorz.ErrInvalidCallbackData
		}
		clubID = callbackData[0]
		userID, err := strconv.ParseInt(callbackData[1], 10, 64)
		if err != nil {
			return errorz.ErrInvalidCallbackData
		}

		clubOwner, err := h.clubOwnerService.Get(context.Background(), clubID, userID)
		if err != nil {
			h.logger.Errorf("(user: %d) error while get club owner: %v", c.Sender().ID, err)
			return c.Edit(
				banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "clubOwner:club:settings:back", struct {
					ID string
				}{
					ID: clubID,
				}),
			)
		}
		clubOwner.Warnings = !clubOwner.Warnings
		_, err = h.clubOwnerService.Update(context.Background(), clubOwner)
		if err != nil {
			h.logger.Errorf(
				"(user: %d) error while update club owner (club_id=%s, user_id=%d): %v",
				c.Sender().ID,
				clubOwner.ClubID,
				clubOwner.UserID,
				err,
			)
			return c.Edit(
				banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "clubOwner:club:settings:back", struct {
					ID string
				}{
					ID: clubID,
				}),
			)
		}
	}

	owners, err := h.clubOwnerService.GetByClubID(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club owners (club_id=%s): %v", c.Sender().ID, clubID, err)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:settings:back", struct {
				ID string
			}{
				ID: clubID,
			}),
		)
	}
	warningsMarkup := h.layout.Markup(c, "clubOwner:club:settings:warnings", struct {
		ID string
	}{
		ID: clubID,
	})

	for _, owner := range owners {
		warningsMarkup.InlineKeyboard = append(
			[][]tele.InlineButton{{*h.layout.Button(c, "clubOwner:club:settings:warnings:user", owner).Inline()}},
			warningsMarkup.InlineKeyboard...,
		)
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "warnings_text")),
		warningsMarkup,
	)
}

func (h Handler) createEvent(c tele.Context) error {
	clubID := c.Callback().Data
	if clubID == "" {
		return errorz.ErrInvalidCallbackData
	}

	h.logger.Infof("(user: %d) create new event request(club=%s)", c.Sender().ID, clubID)

	h.eventsStorage.Clear(c.Sender().ID)

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get clubs: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	inputCollector := collector.New()
	inputCollector.Collect(c.Message())

	isFirst := true

	var steps []struct {
		promptKey   string
		objectFunc  func() interface{}
		errorKey    string
		result      *string
		validator   func(string, map[string]interface{}) bool
		paramsFunc  func(map[string]interface{}) map[string]interface{}
		callbackBtn *tele.Btn
	}

	steps = []struct {
		promptKey   string
		objectFunc  func() interface{}
		errorKey    string
		result      *string
		validator   func(string, map[string]interface{}) bool
		paramsFunc  func(map[string]interface{}) map[string]interface{}
		callbackBtn *tele.Btn
	}{
		{
			promptKey: "input_event_name",
			objectFunc: func() interface{} {
				return struct{}{}
			},
			errorKey:    "invalid_event_name",
			result:      new(string),
			validator:   validator.EventName,
			paramsFunc:  nil,
			callbackBtn: nil,
		},

		{
			promptKey: "input_event_description",
			objectFunc: func() interface{} {
				return struct{}{}
			},
			errorKey:    "invalid_event_description",
			result:      new(string),
			validator:   validator.EventDescription,
			paramsFunc:  nil,
			callbackBtn: h.layout.Button(c, "clubOwner:create_event:description_skip"),
		},

		{
			promptKey: "input_event_location",
			objectFunc: func() interface{} {
				return struct{}{}
			},
			errorKey:    "invalid_event_location",
			result:      new(string),
			validator:   validator.EventLocation,
			paramsFunc:  nil,
			callbackBtn: nil,
		},
		{
			promptKey: "input_event_start_time",
			objectFunc: func() interface{} {
				return struct{}{}
			},
			errorKey:    "invalid_event_start_time",
			result:      new(string),
			validator:   validator.EventStartTime,
			paramsFunc:  nil,
			callbackBtn: nil,
		},
		{
			promptKey: "input_event_end_time",
			objectFunc: func() interface{} {
				return struct{}{}
			},
			errorKey:  "invalid_event_end_time",
			result:    new(string),
			validator: validator.EventEndTime,
			paramsFunc: func(params map[string]interface{}) map[string]interface{} {
				if params == nil {
					params = make(map[string]interface{})
				}
				params["startTime"] = *steps[3].result
				return params
			},
			callbackBtn: h.layout.Button(c, "clubOwner:create_event:end_time_skip"),
		},
		{
			promptKey: "input_event_registered_end_time",
			objectFunc: func() interface{} {
				return struct {
					MaxRegisteredEndTime string
				}{
					MaxRegisteredEndTime: *steps[3].result,
				}
			},
			errorKey:  "invalid_event_registered_end_time",
			result:    new(string),
			validator: validator.EventRegisteredEndTime,
			paramsFunc: func(params map[string]interface{}) map[string]interface{} {
				if params == nil {
					params = make(map[string]interface{})
				}
				params["startTime"] = *steps[3].result
				return params
			},
			callbackBtn: nil,
		},
		{
			promptKey: "input_after_registration_text",
			objectFunc: func() interface{} {
				return struct{}{}
			},
			errorKey:    "invalid_after_registration_text",
			result:      new(string),
			validator:   validator.EventAfterRegistrationText,
			paramsFunc:  nil,
			callbackBtn: h.layout.Button(c, "clubOwner:create_event:after_registration_text_skip"),
		},
		{
			promptKey: "input_max_participants",
			objectFunc: func() interface{} {
				return struct{}{}
			},
			errorKey:    "invalid_max_participants",
			result:      new(string),
			validator:   validator.EventMaxParticipants,
			paramsFunc:  nil,
			callbackBtn: nil,
		},
		{
			promptKey: "input_expected_participants",
			objectFunc: func() interface{} {
				return struct{}{}
			},
			errorKey:    "invalid_expected_participants",
			result:      new(string),
			validator:   validator.EventExpectedParticipants,
			paramsFunc:  nil,
			callbackBtn: nil,
		},
	}

	for _, step := range steps {
		done := false

		var params map[string]interface{}
		if step.paramsFunc != nil {
			params = step.paramsFunc(params)
		}

		markup := h.layout.Markup(c, "clubOwner:club:back", struct {
			ID string
		}{
			ID: club.ID,
		})
		if step.callbackBtn != nil {
			markup.InlineKeyboard = append(
				[][]tele.InlineButton{{*step.callbackBtn.Inline()}},
				markup.InlineKeyboard...,
			)
		}

		if isFirst {
			_ = c.Edit(
				banner.ClubOwner.Caption(h.layout.Text(c, step.promptKey, step.objectFunc())),
				markup,
			)
		} else {
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, step.promptKey, step.objectFunc())),
				markup,
			)
		}
		isFirst = false

		for !done {
			response, errGet := h.input.Get(context.Background(), c.Sender().ID, 0, step.callbackBtn)
			if response.Message != nil {
				inputCollector.Collect(response.Message)
			}
			switch {
			case response.Canceled:
				_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true, ExcludeLast: true})
				return nil
			case errGet != nil:
				h.logger.Errorf("(user: %d) error while input step (%s): %v", c.Sender().ID, step.promptKey, errGet)
				_ = inputCollector.Send(c,
					banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, step.promptKey))),
					h.layout.Markup(c, "clubOwner:club:back", struct {
						ID string
					}{
						ID: club.ID,
					}),
				)
			case response.Callback != nil:
				*step.result = ""
				_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
				done = true
			case !step.validator(response.Message.Text, params):
				_ = inputCollector.Send(c,
					banner.ClubOwner.Caption(h.layout.Text(c, step.errorKey, step.objectFunc())),
					h.layout.Markup(c, "clubOwner:club:back", struct {
						ID string
					}{
						ID: club.ID,
					}),
				)
			case step.validator(response.Message.Text, params):
				*step.result = response.Message.Text
				_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
				done = true
			}
		}
	}

	// Результаты ввода
	const timeLayout = "02.01.2006 15:04"

	var (
		eventDescription             string
		eventStartTime               time.Time
		eventStartTimeStr            string
		eventEndTime                 time.Time
		eventEndTimeStr              string
		eventRegistrationEndTime     time.Time
		eventRegistrationEndTimeStr  string
		eventAfterRegistrationText   string
		eventMaxParticipants         int
		eventMaxExpectedParticipants int
	)

	eventDescription = *steps[1].result
	eventStartTime, _ = time.ParseInLocation(timeLayout, *steps[3].result, location.Location())
	eventStartTimeStr = eventStartTime.Format(timeLayout)

	eventEndTime, err = time.ParseInLocation(timeLayout, *steps[4].result, location.Location())
	eventEndTimeStr = eventEndTime.Format(timeLayout)
	if err != nil {
		eventEndTime = time.Time{}
		eventEndTimeStr = ""
	}

	eventRegistrationEndTime, _ = time.ParseInLocation(timeLayout, *steps[5].result, location.Location())
	eventRegistrationEndTimeStr = eventRegistrationEndTime.Format(timeLayout)

	eventAfterRegistrationText = *steps[6].result
	eventMaxParticipants, _ = strconv.Atoi(*steps[7].result)
	eventMaxExpectedParticipants, _ = strconv.Atoi(*steps[8].result)

	locationSubstrings := h.passLocationSubstrings
	passRequired := len(locationSubstrings) == 0
	if !passRequired {
		for _, substring := range locationSubstrings {
			if strings.Contains(strings.ToLower(*steps[2].result), strings.ToLower(substring)) {
				passRequired = true
				break
			}
		}
	}

	event := entity.Event{
		ClubID:                club.ID,
		Name:                  *steps[0].result,
		Description:           eventDescription,
		Location:              *steps[2].result,
		StartTime:             eventStartTime,
		EndTime:               eventEndTime,
		RegistrationEnd:       eventRegistrationEndTime,
		AfterRegistrationText: eventAfterRegistrationText,
		MaxParticipants:       eventMaxParticipants,
		ExpectedParticipants:  eventMaxExpectedParticipants,
		PassRequired:          passRequired,
	}
	h.eventsStorage.Set(c.Sender().ID, event, 0)

	markup := h.layout.Markup(c, "clubOwner:createClub:confirm", struct {
		ID string
	}{
		ID: clubID,
	})

	var row []tele.InlineButton
	for _, role := range club.AllowedRoles {
		row = append(row, []tele.InlineButton{*h.layout.Button(c, "clubOwner:create_event:role", struct {
			Role     entity.Role
			ID       string
			RoleName string
			Allowed  bool
		}{
			Role:     entity.Role(role),
			ID:       club.ID,
			RoleName: h.layout.Text(c, role),
			Allowed:  slices.Contains(event.AllowedRoles, role),
		}).Inline()}...)
	}

	markup.InlineKeyboard = append(
		[][]tele.InlineButton{row},
		markup.InlineKeyboard...,
	)

	confirmationPayload := struct {
		Name                  string
		Description           string
		Location              string
		StartTime             string
		EndTime               string
		RegistrationEnd       string
		AfterRegistrationText string
		MaxParticipants       int
		ExpectedParticipants  int
	}{
		Name:                  event.Name,
		Description:           event.Description,
		Location:              event.Location,
		StartTime:             eventStartTimeStr,
		EndTime:               eventEndTimeStr,
		RegistrationEnd:       eventRegistrationEndTimeStr,
		AfterRegistrationText: event.AfterRegistrationText,
		MaxParticipants:       event.MaxParticipants,
		ExpectedParticipants:  event.ExpectedParticipants,
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_confirmation", confirmationPayload)),
		markup,
	)
}

func (h Handler) eventAllowedRoles(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	clubID, role := data[0], data[1]

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get clubs: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: club.ID,
			}),
		)
	}

	event, err := h.eventsStorage.Get(c.Sender().ID)
	if err != nil {
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: clubID,
			}),
		)
	}

	var (
		contains bool
		roleI    int
	)
	for i, r := range event.AllowedRoles {
		if r == role {
			contains = true
			roleI = i
			break
		}
	}
	if contains {
		event.AllowedRoles = append(event.AllowedRoles[:roleI], event.AllowedRoles[roleI+1:]...)
	} else {
		event.AllowedRoles = append(event.AllowedRoles, role)
	}

	h.eventsStorage.Set(c.Sender().ID, event, 0)

	markup := h.layout.Markup(c, "clubOwner:createClub:confirm", struct {
		ID string
	}{
		ID: club.ID,
	})

	var row []tele.InlineButton
	for _, role = range club.AllowedRoles {
		row = append(row, []tele.InlineButton{*h.layout.Button(c, "clubOwner:create_event:role", struct {
			Role     entity.Role
			ID       string
			RoleName string
			Allowed  bool
		}{
			Role:     entity.Role(role),
			ID:       club.ID,
			RoleName: h.layout.Text(c, role),
			Allowed:  slices.Contains(event.AllowedRoles, role),
		}).Inline()}...)
	}

	markup.InlineKeyboard = append(
		[][]tele.InlineButton{row},
		markup.InlineKeyboard...,
	)

	const timeLayout = "02.01.2006 15:04"

	eventTimeStr := event.EndTime.Format(timeLayout)
	if event.EndTime.Year() == 1 {
		eventTimeStr = ""
	}

	confirmationPayload := struct {
		Name                  string
		Description           string
		Location              string
		StartTime             string
		EndTime               string
		RegistrationEnd       string
		AfterRegistrationText string
		MaxParticipants       int
		ExpectedParticipants  int
	}{
		Name:                  event.Name,
		Description:           event.Description,
		Location:              event.Location,
		StartTime:             event.StartTime.Format(timeLayout),
		EndTime:               eventTimeStr,
		RegistrationEnd:       event.RegistrationEnd.Format(timeLayout),
		AfterRegistrationText: event.AfterRegistrationText,
		MaxParticipants:       event.MaxParticipants,
		ExpectedParticipants:  event.ExpectedParticipants,
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_confirmation", confirmationPayload)),
		markup,
	)
}

func (h Handler) confirmEventCreation(c tele.Context) error {
	clubID := c.Callback().Data
	if clubID == "" {
		return errorz.ErrInvalidCallbackData
	}

	event, err := h.eventsStorage.Get(c.Sender().ID)
	if err != nil {
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: clubID,
			}),
		)
	}

	if len(event.AllowedRoles) == 0 {
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "event_without_allowed_roles")),
			h.layout.Markup(c, "core:hide"),
		)
	}

	event.StartTime = event.StartTime.UTC()
	event.EndTime = event.EndTime.UTC()
	event.RegistrationEnd = event.RegistrationEnd.UTC()

	_, err = h.eventService.Create(context.Background(), &event)
	if err != nil {
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: clubID,
			}),
		)
	}

	h.eventsStorage.Clear(c.Sender().ID)

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_created", struct {
			Name string
		}{
			Name: event.Name,
		})),
		h.layout.Markup(c, "clubOwner:club:back", struct {
			ID string
		}{
			ID: clubID,
		}))
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
		e           []entity.Event
		rows        []tele.Row
		menuRow     tele.Row
	)

	clubID, p, err := parseEventCallback(c.Callback().Data)
	if err != nil {
		return err
	}

	eventsCount, err = h.eventService.CountByClubID(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get events count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: clubID,
			}),
		)
	}

	e, err = h.eventService.GetByClubID(
		context.Background(),
		eventsOnPage,
		p*eventsOnPage,
		clubID,
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get events: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:club:back", struct {
				ID string
			}{
				ID: clubID,
			}),
		)
	}

	markup := c.Bot().NewMarkup()
	for _, event := range e {
		rows = append(rows, markup.Row(*h.layout.Button(c, "clubOwner:events:event", struct {
			ID     string
			Page   int
			Name   string
			IsOver bool
		}{
			ID:     event.ID,
			Page:   p,
			Name:   event.Name,
			IsOver: event.IsOver(0),
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
		*h.layout.Button(c, "clubOwner:events:prev_page", struct {
			ID   string
			Page int
		}{
			ID:   clubID,
			Page: prevPage,
		}),
		*h.layout.Button(c, "core:page_counter", struct {
			Page       int
			PagesCount int
		}{
			Page:       p + 1,
			PagesCount: pagesCount + 1,
		}),
		*h.layout.Button(c, "clubOwner:events:next_page", struct {
			ID   string
			Page int
		}{
			ID:   clubID,
			Page: nextPage,
		}),
	)

	rows = append(
		rows,
		menuRow,
		markup.Row(*h.layout.Button(c, "clubOwner:club:back", struct {
			ID string
		}{
			ID: clubID,
		})),
	)

	markup.Inline(rows...)

	h.logger.Infof("(user: %d) events list (pages_count=%d, page=%d, club_id=%s events_count=%d, next_page=%d, prev_page=%d)",
		c.Sender().ID,
		pagesCount,
		p,
		clubID,
		eventsCount,
		nextPage,
		prevPage,
	)

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "events_list")),
		markup,
	)
}

func (h Handler) event(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) edit event (event_id=%s)", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:events:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ClubID,
				Page: page,
			}),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:events:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ClubID,
				Page: page,
			}),
		)
	}

	registeredUsersCount, err := h.eventParticipantService.CountByEventID(context.Background(), event.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get registered users count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:events:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ClubID,
				Page: page,
			}),
		)
	}

	visitedUsersCount, err := h.eventParticipantService.CountVisitedByEventID(context.Background(), event.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get visited users count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:events:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ClubID,
				Page: page,
			}),
		)
	}

	eventMarkup := h.layout.Markup(c, "clubOwner:event:menu", struct {
		ID     string
		ClubID string
		Page   string
	}{
		ID:     eventID,
		ClubID: event.ClubID,
		Page:   page,
	})

	if club.QrAllowed {
		eventMarkup.InlineKeyboard = append(
			[][]tele.InlineButton{{*h.layout.Button(c, "clubOwner:event:qr", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}).Inline()}},
			eventMarkup.InlineKeyboard...,
		)
	}

	endTime := event.EndTime.In(location.Location()).Format("02.01.2006 15:04")
	if event.EndTime.Year() == 1 {
		endTime = ""
	}

	return c.Edit(
		banner.Events.Caption(h.layout.Text(c, "club_owner_event_text", struct {
			Name                  string
			Description           string
			Location              string
			StartTime             string
			EndTime               string
			RegistrationEnd       string
			MaxParticipants       int
			VisitedCount          int
			ParticipantsCount     int
			AfterRegistrationText string
			IsRegistered          bool
			Link                  string
		}{
			Name:                  event.Name,
			Description:           event.Description,
			Location:              event.Location,
			StartTime:             event.StartTime.In(location.Location()).Format("02.01.2006 15:04"),
			EndTime:               endTime,
			RegistrationEnd:       event.RegistrationEnd.In(location.Location()).Format("02.01.2006 15:04"),
			MaxParticipants:       event.MaxParticipants,
			ParticipantsCount:     registeredUsersCount,
			VisitedCount:          visitedUsersCount,
			AfterRegistrationText: event.AfterRegistrationText,
			Link:                  event.Link(c.Bot().Me.Username),
		})),
		eventMarkup,
	)
}

func (h Handler) eventSettings(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) edit event settings (event_id=%s)", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	registeredUsersCount, err := h.eventParticipantService.CountByEventID(context.Background(), event.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get registered users count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:events:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ClubID,
				Page: page,
			}),
		)
	}

	visitedUsersCount, err := h.eventParticipantService.CountVisitedByEventID(context.Background(), event.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get visited users count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:events:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ClubID,
				Page: page,
			}),
		)
	}

	endTime := event.EndTime.In(location.Location()).Format("02.01.2006 15:04")
	if event.EndTime.Year() == 1 {
		endTime = ""
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_owner_event_text", struct {
			Name                  string
			Description           string
			Location              string
			StartTime             string
			EndTime               string
			RegistrationEnd       string
			MaxParticipants       int
			ParticipantsCount     int
			VisitedCount          int
			AfterRegistrationText string
			IsRegistered          bool
			Link                  string
		}{
			Name:                  event.Name,
			Description:           event.Description,
			Location:              event.Location,
			StartTime:             event.StartTime.In(location.Location()).Format("02.01.2006 15:04"),
			EndTime:               endTime,
			RegistrationEnd:       event.RegistrationEnd.In(location.Location()).Format("02.01.2006 15:04"),
			MaxParticipants:       event.MaxParticipants,
			ParticipantsCount:     registeredUsersCount,
			VisitedCount:          visitedUsersCount,
			AfterRegistrationText: event.AfterRegistrationText,
			Link:                  event.Link(c.Bot().Me.Username),
		})),
		h.layout.Markup(c, "clubOwner:event:settings", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}))
}

func (h Handler) editEventName(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) edit event name", c.Sender().ID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:settings:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_event_name")),
		h.layout.Markup(c, "clubOwner:event:settings:back", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		eventName string
		done      bool
	)

	oldName := event.Name

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
			h.logger.Errorf("(user: %d) error while input event name: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_event_name"))),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case response.Message == nil:
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_event_name"))),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case !validator.EventName(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_event_name")),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case validator.EventName(response.Message.Text, nil):
			eventName = response.Message.Text
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	event.Name = eventName
	_, err = h.eventService.Update(context.Background(), event)
	if err != nil {
		h.logger.Errorf("(user: %d) error while update event name: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:settings:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	err = h.notificationService.SendEventUpdate(eventID,
		h.layout.Text(c, "event_notification_update", struct {
			Name                  string
			OldName               string
			Description           string
			AfterRegistrationText string
			MaxParticipants       int
		}{
			Name:    event.Name,
			OldName: oldName,
		}),
		h.layout.Markup(c, "core:hide"),
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while send event update notification: %v", c.Sender().ID, err)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_name_changed")),
		h.layout.Markup(c, "clubOwner:event:settings:back", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}),
	)
}

func (h Handler) editEventDescription(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) edit event description", c.Sender().ID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:settings:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	callbackBtn := h.layout.Button(c, "clubOwner:create_event:description_skip")
	markup := h.layout.Markup(c, "clubOwner:event:settings:back", struct {
		ID   string
		Page string
	}{
		ID:   eventID,
		Page: page,
	})
	markup.InlineKeyboard = append(
		[][]tele.InlineButton{{*callbackBtn.Inline()}},
		markup.InlineKeyboard...,
	)

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_event_description")),
		markup,
	)
	inputCollector.Collect(c.Message())

	var (
		eventDescription string
		done             bool
	)
	for {
		response, errGet := h.input.Get(context.Background(), c.Sender().ID, 0, callbackBtn)
		if response.Message != nil {
			inputCollector.Collect(response.Message)
		}
		switch {
		case response.Canceled:
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true, ExcludeLast: true})
			return nil
		case errGet != nil:
			h.logger.Errorf("(user: %d) error while input event name: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_event_description"))),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case response.Callback != nil:
			eventDescription = ""
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		case !validator.EventDescription(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_event_description")),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case validator.EventDescription(response.Message.Text, nil):
			eventDescription = response.Message.Text
			if eventDescription == "skip" {
				eventDescription = ""
			}
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	event.Description = eventDescription
	_, err = h.eventService.Update(context.Background(), event)
	if err != nil {
		h.logger.Errorf("(user: %d) error while update event description: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:settings:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	err = h.notificationService.SendEventUpdate(eventID,
		h.layout.Text(c, "event_notification_update", struct {
			Name                  string
			OldName               string
			Description           string
			AfterRegistrationText string
			MaxParticipants       int
		}{
			Name:        event.Name,
			Description: event.Description,
		}),
		h.layout.Markup(c, "core:hide"),
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while send event update notification: %v", c.Sender().ID, err)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_description_changed")),
		h.layout.Markup(c, "clubOwner:event:settings:back", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}),
	)
}

func (h Handler) editEventAfterRegistrationText(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) edit event after registration text", c.Sender().ID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:settings:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	callbackBtn := h.layout.Button(c, "clubOwner:create_event:after_registration_text_skip")
	markup := h.layout.Markup(c, "clubOwner:event:settings:back", struct {
		ID   string
		Page string
	}{
		ID:   eventID,
		Page: page,
	})
	markup.InlineKeyboard = append(
		[][]tele.InlineButton{{*callbackBtn.Inline()}},
		markup.InlineKeyboard...,
	)

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_after_registration_text")),
		markup,
	)
	inputCollector.Collect(c.Message())

	var (
		eventAfterRegistrationText string
		done                       bool
	)
	for {
		response, errGet := h.input.Get(context.Background(), c.Sender().ID, 0, callbackBtn)
		if response.Message != nil {
			inputCollector.Collect(response.Message)
		}
		switch {
		case response.Canceled:
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true, ExcludeLast: true})
			return nil
		case errGet != nil:
			h.logger.Errorf("(user: %d) error while input event after registration text: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_after_registration_text"))),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case response.Callback != nil:
			eventAfterRegistrationText = ""
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		case !validator.EventAfterRegistrationText(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_after_registration_text")),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case validator.EventAfterRegistrationText(response.Message.Text, nil):
			eventAfterRegistrationText = response.Message.Text
			if eventAfterRegistrationText == "skip" {
				eventAfterRegistrationText = ""
			}
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	event.AfterRegistrationText = eventAfterRegistrationText
	_, err = h.eventService.Update(context.Background(), event)
	if err != nil {
		h.logger.Errorf("(user: %d) error while update event after registration text: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:settings:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	err = h.notificationService.SendEventUpdate(eventID,
		h.layout.Text(c, "event_notification_update", struct {
			Name                  string
			OldName               string
			Description           string
			AfterRegistrationText string
			MaxParticipants       int
		}{
			Name:                  event.Name,
			AfterRegistrationText: event.AfterRegistrationText,
		}),
		h.layout.Markup(c, "core:hide"),
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while send event update notification: %v", c.Sender().ID, err)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_after_registration_text_changed")),
		h.layout.Markup(c, "clubOwner:event:settings:back", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}),
	)
}

func (h Handler) editEventMaxParticipants(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) edit event max count", c.Sender().ID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:settings:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	registeredUsersCount, err := h.eventParticipantService.CountByEventID(context.Background(), event.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get registered users count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:events:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ClubID,
				Page: page,
			}),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "input_edit_max_participants")),
		h.layout.Markup(c, "clubOwner:event:settings:back", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		maxParticipants int
		done            bool
	)
	params := make(map[string]interface{})
	params["previousMaxParticipants"] = event.MaxParticipants
	if event.MaxParticipants == 0 {
		params["previousMaxParticipants"] = registeredUsersCount
	}

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
			h.logger.Errorf("(user: %d) error while input event after registration text: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_edit_max_participants"))),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while input event after registration text: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_edit_max_participants"))),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case !validator.EventEditMaxParticipants(response.Message.Text, params):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_edit_max_participants")),
				h.layout.Markup(c, "clubOwner:event:settings:back", struct {
					ID   string
					Page string
				}{
					ID:   eventID,
					Page: page,
				}),
			)
		case validator.EventEditMaxParticipants(response.Message.Text, params):
			maxParticipants, _ = strconv.Atoi(response.Message.Text)
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	event.MaxParticipants = maxParticipants
	_, err = h.eventService.Update(context.Background(), event)
	if err != nil {
		h.logger.Errorf("(user: %d) error while update event max participants: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:settings:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	err = h.notificationService.SendEventUpdate(eventID,
		h.layout.Text(c, "event_notification_update", struct {
			Name                  string
			OldName               string
			Description           string
			AfterRegistrationText string
			MaxParticipants       int
			ParticipantsChanged   bool
		}{
			Name:                event.Name,
			MaxParticipants:     event.MaxParticipants,
			ParticipantsChanged: true,
		}),
		h.layout.Markup(c, "core:hide"),
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while send event update notification: %v", c.Sender().ID, err)
	}

	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_max_participants_changed")),
		h.layout.Markup(c, "clubOwner:event:settings:back", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}),
	)
}

func (h Handler) eventMailing(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	eventID, page := callbackData[0], callbackData[1]
	h.logger.Infof("(user: %d) edit event mailing (event_id=%s)", c.Sender().ID, eventID)

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "mailing")),
		h.layout.Markup(c, "clubOwner:event:mailing", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}),
	)
}

func (h Handler) mailingRegistered(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	eventID, page := callbackData[0], callbackData[1]
	h.logger.Infof("(user: %d) edit event registered users mailing (event_id=%s)", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_input_registered_mailing")),
		h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
			ID   string
			Page string
		}{
			ID:   event.ID,
			Page: page,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		message interface{}
		done    bool
	)
	for !done {
		h.logger.Debug("waiting for input")
		response, errGet := h.input.Get(context.Background(), c.Sender().ID, 0)
		if response.Message != nil {
			inputCollector.Collect(response.Message)
		}
		switch {
		case response.Canceled:
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true, ExcludeLast: true})
			return nil
		case errGet != nil:
			h.logger.Errorf("(user: %d) error while get message: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "event_input_registered_mailing"))),
				h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
					ID   string
					Page string
				}{
					ID:   event.ID,
					Page: page,
				}),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while get message: message is nil", c.Sender().ID)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "event_input_registered_mailing"))),
				h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
					ID   string
					Page string
				}{
					ID:   event.ID,
					Page: page,
				}),
			)
		case !validator.MailingText(utils.GetMessageText(response.Message), nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_mailing_text")),
				h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
					ID   string
					Page string
				}{
					ID:   event.ID,
					Page: page,
				}),
			)
		case validator.MailingText(utils.GetMessageText(response.Message), nil):
			message = utils.ChangeMessageText(
				response.Message,
				h.layout.Text(c, "event_mailing", struct {
					ClubName  string
					EventName string
					Text      string
				}{
					ClubName:  club.Name,
					EventName: event.Name,
					Text:      utils.GetMessageText(response.Message),
				}),
			)
			done = true
		}
	}
	_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})

	isCorrectMarkup := h.layout.Markup(c, "clubOwner:isMailingCorrect")
	confirmMessage, err := c.Bot().Send(
		c.Chat(),
		message,
		isCorrectMarkup,
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while copy message: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}

	response, err := h.input.Get(
		context.Background(),
		c.Sender().ID,
		0,
		h.layout.Callback("clubOwner:confirmMailing"),
		h.layout.Callback("clubOwner:cancelMailing"),
	)
	_ = c.Bot().Delete(confirmMessage)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get message: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "input_error", err.Error())),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}
	if response.Callback == nil {
		h.logger.Errorf("(user: %d) error while get message: callback is nil", c.Sender().ID)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "input_error", "callback is nil")),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}

	if strings.Contains(response.Callback.Data, "cancel") {
		h.logger.Infof("(user: %d) cancel club mailing (club_id=%s)", c.Sender().ID, club.ID)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "mailing_canceled")),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}

	h.logger.Infof("(user: %d) sending event registered mailing (club_id=%s)", c.Sender().ID, club.ID)
	eventUsers, err := h.userService.GetUsersByEventID(context.Background(), event.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event users: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}

	loading, _ := c.Bot().Send(c.Chat(), h.layout.Text(c, "loading"))
	for _, user := range eventUsers {
		if user.IsMailingAllowed(club.ID) {
			chat, err := c.Bot().ChatByID(user.ID)
			if err != nil {
				continue
			}

			_, _ = c.Bot().Send(
				chat,
				message,
				h.layout.Markup(c, "mailing", struct {
					ClubID  string
					Allowed bool
				}{
					ClubID:  club.ID,
					Allowed: true,
				}),
			)
		}
	}

	h.logger.Infof("(user: %d) event mailing sent (club_id=%s, event_id=%s)", c.Sender().ID, club.ID, event.ID)

	mailingChannel, err := c.Bot().ChatByID(h.mailingChannelID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get mailing channel: %v", c.Sender().ID, err)
	} else {
		_, err = c.Bot().Send(
			mailingChannel,
			message,
		)
		if err != nil {
			h.logger.Errorf("(user: %d) error while send message to mailing channel: %v", c.Sender().ID, err)
		}
	}

	_ = c.Bot().Delete(loading)
	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "mailing_sent")),
		h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
			ID   string
			Page string
		}{
			ID:   event.ID,
			Page: page,
		}),
	)
}

func (h Handler) mailingVisited(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	eventID, page := callbackData[0], callbackData[1]
	h.logger.Infof("(user: %d) edit event visited users mailing (event_id=%s)", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_input_visited_mailing")),
		h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
			ID   string
			Page string
		}{
			ID:   event.ID,
			Page: page,
		}),
	)
	inputCollector.Collect(c.Message())

	var (
		message interface{}
		done    bool
	)
	for !done {
		h.logger.Debug("waiting for input")
		response, errGet := h.input.Get(context.Background(), c.Sender().ID, 0)
		if response.Message != nil {
			inputCollector.Collect(response.Message)
		}
		switch {
		case response.Canceled:
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true, ExcludeLast: true})
			return nil
		case errGet != nil:
			h.logger.Errorf("(user: %d) error while get message: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "event_input_visited_mailing"))),
				h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
					ID   string
					Page string
				}{
					ID:   event.ID,
					Page: page,
				}),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while get message: message is nil", c.Sender().ID)
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "event_input_visited_mailing"))),
				h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
					ID   string
					Page string
				}{
					ID:   event.ID,
					Page: page,
				}),
			)
		case !validator.MailingText(utils.GetMessageText(response.Message), nil):
			_ = inputCollector.Send(c,
				banner.ClubOwner.Caption(h.layout.Text(c, "invalid_mailing_text")),
				h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
					ID   string
					Page string
				}{
					ID:   event.ID,
					Page: page,
				}),
			)
		case validator.MailingText(utils.GetMessageText(response.Message), nil):
			message = utils.ChangeMessageText(
				response.Message,
				h.layout.Text(c, "event_mailing", struct {
					ClubName  string
					EventName string
					Text      string
				}{
					ClubName:  club.Name,
					EventName: event.Name,
					Text:      utils.GetMessageText(response.Message),
				}),
			)
			done = true
		}
	}
	_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})

	isCorrectMarkup := h.layout.Markup(c, "clubOwner:isMailingCorrect")
	confirmMessage, err := c.Bot().Send(
		c.Chat(),
		message,
		isCorrectMarkup,
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while copy message: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}

	response, err := h.input.Get(
		context.Background(),
		c.Sender().ID,
		0,
		h.layout.Callback("clubOwner:confirmMailing"),
		h.layout.Callback("clubOwner:cancelMailing"),
	)
	_ = c.Bot().Delete(confirmMessage)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get message: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "input_error", err.Error())),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}
	if response.Callback == nil {
		h.logger.Errorf("(user: %d) error while get message: callback is nil", c.Sender().ID)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "input_error", "callback is nil")),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}

	if strings.Contains(response.Callback.Data, "cancel") {
		h.logger.Infof("(user: %d) cancel club mailing (club_id=%s)", c.Sender().ID, club.ID)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "mailing_canceled")),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}

	h.logger.Infof("(user: %d) sending event visited mailing (club_id=%s)", c.Sender().ID, club.ID)
	eventUsers, err := h.userService.GetEventUsers(context.Background(), event.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event users: %v", c.Sender().ID, err)
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ID,
				Page: page,
			}),
		)
	}

	loading, _ := c.Bot().Send(c.Chat(), h.layout.Text(c, "loading"))
	for _, user := range eventUsers {
		if user.User.IsMailingAllowed(club.ID) && user.UserVisit {
			chat, err := c.Bot().ChatByID(user.User.ID)
			if err != nil {
				continue
			}

			_, _ = c.Bot().Send(
				chat,
				message,
				h.layout.Markup(c, "mailing", struct {
					ClubID  string
					Allowed bool
				}{
					ClubID:  club.ID,
					Allowed: true,
				}),
			)
		}
	}

	h.logger.Infof("(user: %d) event mailing sent (club_id=%s, event_id=%s)", c.Sender().ID, club.ID, event.ID)

	mailingChannel, err := c.Bot().ChatByID(h.mailingChannelID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get mailing channel: %v", c.Sender().ID, err)
	} else {
		_, err = c.Bot().Send(
			mailingChannel,
			message,
		)
		if err != nil {
			h.logger.Errorf("(user: %d) error while send message to mailing channel: %v", c.Sender().ID, err)
		}
	}

	_ = c.Bot().Delete(loading)
	return c.Send(
		banner.ClubOwner.Caption(h.layout.Text(c, "mailing_sent")),
		h.layout.Markup(c, "clubOwner:event:mailing:back", struct {
			ID   string
			Page string
		}{
			ID:   event.ID,
			Page: page,
		}),
	)
}

func (h Handler) deleteEvent(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) delete event(eventID=%s) request", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "delete_event_text", struct {
			Name string
		}{
			Name: event.Name,
		})),
		h.layout.Markup(c, "clubOwner:event:delete", struct {
			ID   string
			Page string
		}{
			ID:   event.ID,
			Page: page,
		}),
	)
}

func (h Handler) acceptEventDelete(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) delete event(eventID=%s)", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while delete event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:delete:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	err = h.eventService.Delete(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while delete event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:delete:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	err = h.notificationService.SendEventUpdate(eventID,
		h.layout.Text(c, "event_notification_delete", struct {
			Name string
		}{
			Name: event.Name,
		}),
		h.layout.Markup(c, "core:hide"),
	)
	if err != nil {
		h.logger.Errorf("(user: %d) error while send event delete notification: %v", c.Sender().ID, err)
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "event_deleted", struct {
			Name string
		}{
			Name: event.Name,
		})),
		h.layout.Markup(c, "clubOwner:event:delete:back", struct {
			ClubID string
			Page   string
		}{
			ClubID: event.ClubID,
			Page:   page,
		}),
	)
}

func (h Handler) declineEventDelete(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) decline delete event(eventID=%s)", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:delete:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:delete:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	registeredUsersCount, err := h.eventParticipantService.CountByEventID(context.Background(), event.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get registered users count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:events:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ClubID,
				Page: page,
			}),
		)
	}

	visitedUsersCount, err := h.eventParticipantService.CountVisitedByEventID(context.Background(), event.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get visited users count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:events:back", struct {
				ID   string
				Page string
			}{
				ID:   event.ClubID,
				Page: page,
			}),
		)
	}

	eventMarkup := h.layout.Markup(c, "clubOwner:event:menu", struct {
		ID     string
		ClubID string
		Page   string
	}{
		ID:     eventID,
		ClubID: event.ClubID,
		Page:   page,
	})

	if club.QrAllowed {
		eventMarkup.InlineKeyboard = append(
			[][]tele.InlineButton{{*h.layout.Button(c, "clubOwner:event:qr", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}).Inline()}},
			eventMarkup.InlineKeyboard...,
		)
	}

	endTime := event.EndTime.In(location.Location()).Format("02.01.2006 15:04")
	if event.EndTime.Year() == 1 {
		endTime = ""
	}

	return c.Edit(
		banner.ClubOwner.Caption(h.layout.Text(c, "club_owner_event_text", struct {
			Name                  string
			Description           string
			Location              string
			StartTime             string
			EndTime               string
			RegistrationEnd       string
			MaxParticipants       int
			ParticipantsCount     int
			VisitedCount          int
			AfterRegistrationText string
			IsRegistered          bool
			Link                  string
		}{
			Name:                  event.Name,
			Description:           event.Description,
			Location:              event.Location,
			StartTime:             event.StartTime.In(location.Location()).Format("02.01.2006 15:04"),
			EndTime:               endTime,
			RegistrationEnd:       event.RegistrationEnd.In(location.Location()).Format("02.01.2006 15:04"),
			MaxParticipants:       event.MaxParticipants,
			ParticipantsCount:     registeredUsersCount,
			VisitedCount:          visitedUsersCount,
			AfterRegistrationText: event.AfterRegistrationText,
			Link:                  event.Link(c.Bot().Me.Username),
		})),
		eventMarkup,
	)
}

func (h Handler) registeredUsers(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	// page := data[1]
	// h.logger.Infof("(user: %d) get registered users (eventID=%s)", c.Sender().ID, eventID)
	//
	//event, err := h.eventService.Get(context.Background(), eventID)
	//if err != nil {
	//	return c.Send(
	//		banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
	//		h.layout.Markup(c, "core:hide"),
	//	)
	//}

	// club, err := h.clubService.Get(context.Background(), event.ClubID)
	// if err != nil {
	//	return c.Send(
	//		banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
	//		h.layout.Markup(c, "core:hide"),
	//	)
	//}
	//
	//registeredUsersCount, err := h.eventParticipantService.CountByEventID(context.Background(), event.ID)
	//if err != nil {
	//	h.logger.Errorf("(user: %d) error while get registered users count: %v", c.Sender().ID, err)
	//	return c.Edit(
	//		banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
	//		h.layout.Markup(c, "clubOwner:events:back", struct {
	//			ID   string
	//			Page string
	//		}{
	//			ID:   event.ClubID,
	//			Page: page,
	//		}),
	//	)
	//}

	// visitedUsersCount, err := h.eventParticipantService.CountVisitedByEventID(context.Background(), event.ID)
	// if err != nil {
	//	h.logger.Errorf("(user: %d) error while get visited users count: %v", c.Sender().ID, err)
	//	return c.Edit(
	//		banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
	//		h.layout.Markup(c, "clubOwner:events:back", struct {
	//			ID   string
	//			Page string
	//		}{
	//			ID:   event.ClubID,
	//			Page: page,
	//		}),
	//	)
	//}

	users, err := h.userService.GetEventUsers(context.Background(), eventID)
	if err != nil {
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "core:hide"),
		)
	}

	buffer, err := usersToXLSX(users)
	if err != nil {
		return c.Send(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "core:hide"),
		)
	}

	file := &tele.Document{
		File:     tele.FromReader(buffer),
		Caption:  h.layout.Text(c, "registered_users_text"),
		FileName: "users.xlsx",
	}

	return c.Send(
		file,
		h.layout.Markup(c, "core:hide"),
	)
}

func (h Handler) eventQRCode(c tele.Context) error {
	data := strings.Split(c.Callback().Data, " ")
	if len(data) != 2 {
		return errorz.ErrInvalidCallbackData
	}

	eventID := data[0]
	page := data[1]
	h.logger.Infof("(user: %d) getting user Event QR code", c.Sender().ID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	if !club.QrAllowed {
		return c.Edit(
			h.layout.Text(c, "qr_not_allowed"),
			h.layout.Markup(c, "clubOwner:event:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}

	loading, _ := c.Bot().Send(c.Chat(), h.layout.Text(c, "loading"))
	file, err := h.qrService.GetEventQR(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event QR: %v", c.Sender().ID, err)
		return c.Edit(
			banner.ClubOwner.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "clubOwner:event:back", struct {
				ID   string
				Page string
			}{
				ID:   eventID,
				Page: page,
			}),
		)
	}
	_ = c.Bot().Delete(loading)

	return c.Edit(
		&tele.Photo{
			File:    file,
			Caption: h.layout.Text(c, "event_qr_text"),
		},
		h.layout.Markup(c, "clubOwner:event:back", struct {
			ID   string
			Page string
		}{
			ID:   eventID,
			Page: page,
		}),
	)
}

func (h Handler) ClubOwnerSetup(group *tele.Group, middle *middlewares.Handler) {
	group.Use(middle.IsClubOwner)
	group.Handle(h.layout.Callback("clubOwner:my_clubs"), h.clubsList)
	group.Handle(h.layout.Callback("clubOwner:myClubs:back"), h.clubsList)
	group.Handle(h.layout.Callback("clubOwner:myClubs:club"), h.clubMenu)
	group.Handle(h.layout.Callback("clubOwner:club:back"), h.clubMenu)

	group.Handle(h.layout.Callback("clubOwner:club:create_event"), h.createEvent)
	group.Handle(h.layout.Callback("clubOwner:create_event:refill"), h.createEvent)
	group.Handle(h.layout.Callback("clubOwner:create_event:confirm"), h.confirmEventCreation)
	group.Handle(h.layout.Callback("clubOwner:create_event:role"), h.eventAllowedRoles)
	group.Handle(h.layout.Callback("clubOwner:club:back"), h.clubMenu)

	group.Handle(h.layout.Callback("clubOwner:club:events"), h.eventsList)
	group.Handle(h.layout.Callback("clubOwner:events:back"), h.eventsList)
	group.Handle(h.layout.Callback("clubOwner:events:prev_page"), h.eventsList)
	group.Handle(h.layout.Callback("clubOwner:events:next_page"), h.eventsList)
	group.Handle(h.layout.Callback("clubOwner:events:event"), h.event)
	group.Handle(h.layout.Callback("clubOwner:event:back"), h.event)
	group.Handle(h.layout.Callback("clubOwner:event:settings"), h.eventSettings)
	group.Handle(h.layout.Callback("clubOwner:event:settings:back"), h.eventSettings)
	group.Handle(h.layout.Callback("clubOwner:event:settings:edit_name"), h.editEventName)
	group.Handle(h.layout.Callback("clubOwner:event:settings:edit_description"), h.editEventDescription)
	group.Handle(h.layout.Callback("clubOwner:event:settings:edit_after_reg_text"), h.editEventAfterRegistrationText)
	group.Handle(h.layout.Callback("clubOwner:event:settings:edit:max_participants"), h.editEventMaxParticipants)
	group.Handle(h.layout.Callback("clubOwner:event:delete"), h.deleteEvent)
	group.Handle(h.layout.Callback("clubOwner:event:delete:accept"), h.acceptEventDelete)
	group.Handle(h.layout.Callback("clubOwner:event:delete:decline"), h.declineEventDelete)

	group.Handle(h.layout.Callback("clubOwner:event:users"), h.registeredUsers)
	group.Handle(h.layout.Callback("clubOwner:event:qr"), h.eventQRCode)

	group.Handle(h.layout.Callback("clubOwner:event:mailing"), h.eventMailing)
	group.Handle(h.layout.Callback("clubOwner:event:mailing:back"), h.eventMailing)
	group.Handle(h.layout.Callback("clubOwner:event:mailing:registered"), h.mailingRegistered)
	group.Handle(h.layout.Callback("clubOwner:event:mailing:visited"), h.mailingVisited)
	group.Handle(h.layout.Callback("clubOwner:club:mailing"), h.clubMailing)

	group.Handle(h.layout.Callback("clubOwner:club:settings"), h.clubSettings)
	group.Handle(h.layout.Callback("clubOwner:club:settings:back"), h.clubSettings)
	group.Handle(h.layout.Callback("clubOwner:club:settings:add_owner"), h.addOwner)
	group.Handle(h.layout.Callback("clubOwner:club:settings:warnings"), h.warnings)
	group.Handle(h.layout.Callback("clubOwner:club:settings:profile"), h.profile)
	group.Handle(h.layout.Callback("clubOwner:club:settings:profile:set_name"), h.setClubName)
	group.Handle(h.layout.Callback("clubOwner:club:settings:profile:set_description"), h.setClubDescription)
	group.Handle(h.layout.Callback("clubOwner:club:settings:profile:set_link"), h.setClubLink)
	group.Handle(h.layout.Callback("clubOwner:club:settings:profile:set_avatar"), h.setClubAvatar)
	group.Handle(h.layout.Callback("clubOwner:club:settings:profile:set_intro"), h.setClubIntro)
	group.Handle(h.layout.Callback("clubOwner:club:settings:profile:should_show"), h.shouldShow)
	group.Handle(h.layout.Callback("clubOwner:club:settings:subscription_access"), h.subscriptionAccess)
	group.Handle(h.layout.Callback("clubOwner:club:settings:subscription_access:required"), h.subscriptionAccess)
	group.Handle(h.layout.Callback("clubOwner:club:settings:subscription_access:add_channel_id"), h.addChannelID)
	group.Handle(h.layout.Callback("clubOwner:club:settings:subscription_access:back"), h.subscriptionAccess)
	group.Handle(h.layout.Callback("clubOwner:club:settings:profile:back"), h.profile)
	group.Handle(h.layout.Callback("clubOwner:club:settings:warnings:user"), h.warnings)
}

func parseEventCallback(callbackData string) (string, int, error) {
	var (
		p      int
		clubID string
		err    error
	)

	data := strings.Split(callbackData, " ")
	if len(data) == 2 {
		clubID = data[0]
		p, err = strconv.Atoi(data[1])
		if err != nil {
			return "", 0, errorz.ErrInvalidCallbackData
		}
	} else if len(data) == 1 {
		clubID = data[0]
		p = 0
	} else {
		return "", 0, errorz.ErrInvalidCallbackData
	}

	return clubID, p, nil
}

func usersToXLSX(users []dto.EventUser) (*bytes.Buffer, error) {
	f := excelize.NewFile()

	sheet := "Sheet1"
	_ = f.SetCellValue(sheet, "A1", "ID")
	_ = f.SetCellValue(sheet, "B1", "Фамилия")
	_ = f.SetCellValue(sheet, "C1", "Имя")
	_ = f.SetCellValue(sheet, "D1", "Username")
	_ = f.SetCellValue(sheet, "E1", "Посетил")

	for i, user := range users {
		fio := strings.Split(user.User.FIO.String(), " ")

		row := i + 2
		_ = f.SetCellValue(sheet, "A"+strconv.Itoa(row), user.User.ID)
		_ = f.SetCellValue(sheet, "B"+strconv.Itoa(row), fio[0])
		_ = f.SetCellValue(sheet, "C"+strconv.Itoa(row), fio[1])
		_ = f.SetCellValue(sheet, "D"+strconv.Itoa(row), user.User.Username)
		_ = f.SetCellValue(sheet, "e"+strconv.Itoa(row), user.UserVisit)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}

	return &buf, nil
}
