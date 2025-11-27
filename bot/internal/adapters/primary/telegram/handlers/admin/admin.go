package admin

import (
	"context"
	"errors"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/layout"
	"gorm.io/gorm"

	"github.com/nlypage/intele"
	"github.com/nlypage/intele/collector"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/common/errorz"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/banner"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/validator"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/valueobject"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/primary"
	"github.com/Badsnus/cu-clubs-bot/bot/pkg/logger/types"
)

type Handler struct {
	layout *layout.Layout
	logger *types.Logger
	bot    *tele.Bot
	input  *intele.InputManager

	adminUserService primary.UserService
	clubService      primary.ClubService
	clubOwnerService primary.ClubOwnerService
}

func New(
	userSvc primary.UserService,
	clubSvc primary.ClubService,
	clubOwnerSvc primary.ClubOwnerService,
	b *tele.Bot,
	lt *layout.Layout,
	lg *types.Logger,
	in *intele.InputManager,
) *Handler {
	return &Handler{
		layout:           lt,
		logger:           lg,
		bot:              b,
		input:            in,
		adminUserService: userSvc,
		clubService:      clubSvc,
		clubOwnerService: clubOwnerSvc,
	}
}

func (h Handler) adminMenu(c tele.Context) error {
	h.logger.Infof("(user: %d) edit admin menu", c.Sender().ID)
	commands := h.layout.Commands()
	errSetCommands := c.Bot().SetCommands(commands)
	if errSetCommands != nil {
		h.logger.Errorf("(user: %d) error while set admin commands: %v", c.Sender().ID, errSetCommands)
	}

	return c.Edit(
		banner.Menu.Caption(h.layout.Text(c, "admin_menu_text", c.Sender().Username)),
		h.layout.Markup(c, "admin:menu"),
	)
}

func (h Handler) createClub(c tele.Context) error {
	h.logger.Infof("(user: %d) create new club request", c.Sender().ID)
	inputCollector := collector.New()
	_ = c.Edit(
		banner.Menu.Caption(h.layout.Text(c, "input_club_name")),
		h.layout.Markup(c, "admin:backToMenu"),
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
				banner.Menu.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_name"))),
				h.layout.Markup(c, "admin:backToMenu"),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while input club name: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.Menu.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_club_name"))),
				h.layout.Markup(c, "admin:backToMenu"),
			)
		case !validator.ClubName(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.Menu.Caption(h.layout.Text(c, "invalid_club_name")),
				h.layout.Markup(c, "admin:backToMenu"),
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

	club, err := h.clubService.Create(context.Background(), &entity.Club{
		Name: clubName,
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return c.Send(
				banner.Menu.Caption(h.layout.Text(c, "club_already_exists")),
				h.layout.Markup(c, "admin:backToMenu"),
			)
		}

		h.logger.Errorf("(user: %d) error while create new club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:backToMenu"),
		)
	}

	h.logger.Infof("(user: %d) new club created: %s", c.Sender().ID, club.Name)
	return c.Send(
		banner.Menu.Caption(h.layout.Text(c, "club_created", club)),
		h.layout.Markup(c, "admin:backToMenu"),
	)
}

func (h Handler) clubsList(c tele.Context) error {
	const clubsOnPage = 5
	h.logger.Infof("(user: %d) edit clubs list", c.Sender().ID)

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
	if c.Callback().Unique != "admin_clubs" {
		p, err = strconv.Atoi(c.Callback().Data)
		if err != nil {
			return errorz.ErrInvalidCallbackData
		}
	}

	clubsCount, err = h.clubService.Count(context.Background())
	if err != nil {
		h.logger.Errorf("(user: %d) error while get clubs count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:backToMenu"),
		)
	}

	clubs, err = h.clubService.GetWithPagination(context.Background(), clubsOnPage, p*clubsOnPage, "created_at DESC")
	if err != nil {
		h.logger.Errorf(
			"(user: %d) error while get clubs (offset=%d, limit=%d, order_by=%s): %v",
			c.Sender().ID,
			p*clubsOnPage,
			clubsOnPage,
			"created_at DESC",
			err,
		)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:backToMenu"),
		)
	}

	markup := c.Bot().NewMarkup()
	for _, club := range clubs {
		rows = append(rows, markup.Row(*h.layout.Button(c, "admin:clubs:club", struct {
			ID   string
			Name string
			Page int
		}{
			ID:   club.ID,
			Name: club.Name,
			Page: p,
		})))
	}
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
		*h.layout.Button(c, "admin:clubs:prev_page", struct {
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
		*h.layout.Button(c, "admin:clubs:next_page", struct {
			Page int
		}{
			Page: nextPage,
		}),
	)

	rows = append(
		rows,
		menuRow,
		markup.Row(*h.layout.Button(c, "admin:back_to_menu")),
	)

	markup.Inline(rows...)

	h.logger.Infof("(user: %d) clubs list (pages_count=%d, page=%d, clubs_count=%d, next_page=%d, prev_page=%d)",
		c.Sender().ID,
		pagesCount,
		p,
		clubsCount,
		nextPage,
		prevPage,
	)

	_ = c.Edit(
		banner.Menu.Caption(h.layout.Text(c, "clubs_list", clubsCount)),
		markup,
	)
	return nil
}

func (h Handler) clubMenu(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	clubID, page := callbackData[0], callbackData[1]

	h.logger.Infof("(user: %d) edit club menu (club_id=%s)", c.Sender().ID, clubID)

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:clubs:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	if c.Callback().Unique == "admin_club_qr" {
		club.QrAllowed = !club.QrAllowed
		club, err = h.clubService.Update(context.Background(), club)
		if err != nil {
			h.logger.Errorf("(user: %d) error while update club: %v", c.Sender().ID, err)
			return c.Send(
				banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "admin:clubs:back", struct {
					Page string
				}{
					Page: page,
				}),
			)
		}
	} else if c.Callback().Unique == "admin_club_sub_required" {
		club.SubscriptionRequired = !club.SubscriptionRequired
		club, err = h.clubService.Update(context.Background(), club)
		if err != nil {
			h.logger.Errorf("(user: %d) error while update club: %v", c.Sender().ID, err)
			return c.Send(
				banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "admin:clubs:back", struct {
					Page string
				}{
					Page: page,
				}),
			)
		}
	}

	clubOwners, err := h.clubOwnerService.GetByClubID(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club owners: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:clubs:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	return c.Edit(
		banner.Menu.Caption(h.layout.Text(c, "admin_club_menu_text", struct {
			Club   entity.Club
			Owners []dto.ClubOwner
		}{
			Club:   *club,
			Owners: clubOwners,
		})),
		h.layout.Markup(c, "admin:club:menu", struct {
			ID                   string
			Page                 string
			QrAllowed            bool
			SubscriptionRequired bool
		}{
			ID:                   clubID,
			Page:                 page,
			QrAllowed:            club.QrAllowed,
			SubscriptionRequired: club.SubscriptionRequired,
		}),
	)
}

func (h Handler) addClubOwner(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	clubID := callbackData[0]
	page := callbackData[1]

	h.logger.Infof("(user: %d) add club owner (club_id=%s)", c.Sender().ID, clubID)
	inputCollector := collector.New()
	inputCollector.Collect(c.Message())

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:clubs:back", struct {
				ID   string
				Page string
			}{
				ID:   clubID,
				Page: page,
			}),
		)
	}

	_ = c.Edit(
		banner.Menu.Caption(h.layout.Text(c, "input_user_id")),
		h.layout.Markup(c, "admin:club:back", struct {
			ID   string
			Page string
		}{
			ID:   club.ID,
			Page: page,
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
				h.layout.Markup(c, "admin:club:back", struct {
					ID   string
					Page string
				}{
					ID:   club.ID,
					Page: page,
				}),
			)
		case response.Message == nil:
			_ = inputCollector.Send(c,
				banner.Menu.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "input_user_id"))),
				h.layout.Markup(c, "admin:club:back", struct {
					ID   string
					Page string
				}{
					ID:   clubID,
					Page: page,
				}),
			)
		default:
			userID, err := strconv.ParseInt(response.Message.Text, 10, 64)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.Menu.Caption(h.layout.Text(c, "input_user_id")),
					h.layout.Markup(c, "admin:club:back", struct {
						ID   string
						Page string
					}{
						ID:   club.ID,
						Page: page,
					}),
				)
				break
			}

			user, err = h.adminUserService.Get(context.Background(), userID)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.Menu.Caption(h.layout.Text(c, "user_not_found", struct {
						ID   int64
						Text string
					}{
						ID:   userID,
						Text: h.layout.Text(c, "input_user_id"),
					})),
					h.layout.Markup(c, "admin:club:back", struct {
						ID   string
						Page string
					}{
						ID:   club.ID,
						Page: page,
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
			club.ID,
			user.ID,
			err,
		)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:club:back", struct {
				ID   string
				Page string
			}{
				ID:   club.ID,
				Page: page,
			}),
		)
	}

	h.logger.Infof(
		"(user: %d) club owner added (club_id=%s, user_id=%d)",
		c.Sender().ID,
		club.ID,
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
		h.layout.Markup(c, "admin:club:back", struct {
			ID   string
			Page string
		}{
			ID:   club.ID,
			Page: page,
		}),
	)
}

func (h Handler) removeClubOwner(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	clubID := callbackData[0]
	page := callbackData[1]

	h.logger.Infof("(user: %d) remove club owner (club_id=%s)", c.Sender().ID, clubID)
	inputCollector := collector.New()
	inputCollector.Collect(c.Message())
	_ = c.Edit(
		banner.Menu.Caption(h.layout.Text(c, "input_user_id")),
		h.layout.Markup(c, "admin:club:back", struct {
			ID   string
			Page string
		}{
			ID:   clubID,
			Page: page,
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
				h.layout.Markup(c, "admin:club:back", struct {
					ID   string
					Page string
				}{
					ID:   clubID,
					Page: page,
				}),
			)
		default:
			userID, err := strconv.ParseInt(response.Message.Text, 10, 64)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.Menu.Caption(h.layout.Text(c, "input_user_id")),
					h.layout.Markup(c, "admin:club:back", struct {
						ID   string
						Page string
					}{
						ID:   clubID,
						Page: page,
					}),
				)
				break
			}

			user, err = h.adminUserService.Get(context.Background(), userID)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.Menu.Caption(h.layout.Text(c, "user_not_found", struct {
						ID   int64
						Text string
					}{
						ID:   userID,
						Text: h.layout.Text(c, "input_user_id"),
					})),
					h.layout.Markup(c, "admin:club:back", struct {
						ID   string
						Page string
					}{
						ID:   clubID,
						Page: page,
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

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:club:back", struct {
				ID   string
				Page string
			}{
				ID:   clubID,
				Page: page,
			}),
		)
	}

	err = h.clubOwnerService.Remove(context.Background(), user.ID, club.ID)
	if err != nil {
		_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
		h.logger.Errorf(
			"(user: %d) error while remove club owner (club_id=%s, user_id=%d): %v",
			c.Sender().ID,
			clubID,
			user.ID,
			err,
		)
		return c.Send(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:club:back", struct {
				ID   string
				Page string
			}{
				ID:   clubID,
				Page: page,
			}),
		)
	}

	h.logger.Infof(
		"(user: %d) club owner removed (club_id=%s, user_id=%d)",
		c.Sender().ID,
		clubID,
		user.ID,
	)

	_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
	return c.Send(
		banner.Menu.Caption(h.layout.Text(c, "club_owner_removed", struct {
			Club entity.Club
			User entity.User
		}{
			Club: *club,
			User: *user,
		})),
		h.layout.Markup(c, "admin:club:back", struct {
			ID   string
			Page string
		}{
			ID:   clubID,
			Page: page,
		}),
	)
}

func (h Handler) manageRoles(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) < 2 {
		return errorz.ErrInvalidCallbackData
	}
	clubID := callbackData[0]
	page := callbackData[1]

	h.logger.Infof("(user: %d) manage roles (club_id=%s)", c.Sender().ID, clubID)

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club (club_id=%s): %v", c.Sender().ID, clubID, err)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:club:back", struct {
				ID   string
				Page string
			}{
				ID:   clubID,
				Page: page,
			}),
		)
	}

	if c.Callback().Unique == "admin_role" {
		h.logger.Infof("(user: %d) toggle role (club_id=%s, role=%s)", c.Sender().ID, clubID, callbackData[2])
		if len(callbackData) != 3 {
			return errorz.ErrInvalidCallbackData
		}
		role := callbackData[2]

		var (
			contains bool
			roleI    int
		)
		for i, r := range club.AllowedRoles {
			if r == role {
				contains = true
				roleI = i
				break
			}
		}
		if contains {
			club.AllowedRoles = append(club.AllowedRoles[:roleI], club.AllowedRoles[roleI+1:]...)
		} else {
			club.AllowedRoles = append(club.AllowedRoles, role)
		}

		club, err = h.clubService.Update(context.Background(), club)
		if err != nil {
			h.logger.Errorf("(user: %d) error while update club (club_id=%s): %v", c.Sender().ID, clubID, err)
			return c.Edit(
				banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "admin:club:back", struct {
					ID   string
					Page string
				}{
					ID:   clubID,
					Page: page,
				}),
			)
		}
	}

	rolesMarkup := h.layout.Markup(c, "admin:club:roles", struct {
		ID   string
		Page string
	}{
		ID:   club.ID,
		Page: page,
	})

	for _, role := range valueobject.AllRoles() {
		var on bool
		for _, allowedRole := range club.AllowedRoles {
			if allowedRole == role.String() {
				on = true
				break
			}
		}
		rolesMarkup.InlineKeyboard = append(
			[][]tele.InlineButton{{*h.layout.Button(c, "admin:club:roles:role", struct {
				Role     string
				Page     string
				ID       string
				RoleText string
				On       bool
			}{
				Role:     string(role),
				Page:     page,
				ID:       club.ID,
				RoleText: h.layout.Text(c, string(role)),
				On:       on,
			}).Inline()}},
			rolesMarkup.InlineKeyboard...,
		)
	}

	return c.Edit(
		banner.Menu.Caption(h.layout.Text(c, "manage_roles")),
		rolesMarkup,
	)
}

func (h Handler) deleteClub(c tele.Context) error {
	callbackData := strings.Split(c.Callback().Data, " ")
	if len(callbackData) != 2 {
		return errorz.ErrInvalidCallbackData
	}
	clubID := callbackData[0]
	page := callbackData[1]

	h.logger.Infof("(user: %d) delete club (club_id=%s)", c.Sender().ID, clubID)

	club, err := h.clubService.Get(context.Background(), clubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:clubs:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}
	err = h.clubService.Delete(context.Background(), club.ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while delete club: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Menu.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "admin:clubs:back", struct {
				Page string
			}{
				Page: page,
			}),
		)
	}

	h.logger.Infof("(user: %d) club deleted (club_id=%s)", c.Sender().ID, clubID)
	return c.Edit(
		banner.Menu.Caption(h.layout.Text(c, "club_deleted", club)),
		h.layout.Markup(c, "admin:clubs:back", struct {
			Page string
		}{
			Page: page,
		}),
	)
}

func (h Handler) banUser(c tele.Context) error {
	_ = c.Delete()
	if c.Message() == nil || c.Message().Payload == "" {
		return c.Send(
			h.layout.Text(c, "invalid_ban_data"),
			h.layout.Markup(c, "core:hide"),
		)
	}
	userID, err := strconv.ParseInt(c.Message().Payload, 10, 64)
	if err != nil {
		return c.Send(
			h.layout.Text(c, "invalid_ban_data"),
			h.layout.Markup(c, "core:hide"),
		)
	}

	h.logger.Infof("(user: %d) attempt ban user: %d", c.Sender().ID, userID)
	if userID == c.Sender().ID {
		return c.Send(
			h.layout.Text(c, "attempt_to_ban_self"),
			h.layout.Markup(c, "core:hide"),
		)
	}
	user, err := h.adminUserService.Ban(context.Background(), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Send(
				h.layout.Text(c, "user_not_found", struct {
					ID int64
				}{
					ID: userID,
				}),
				h.layout.Markup(c, "core:hide"),
			)
		}
		h.logger.Errorf("(user: %d) error while ban user: %v", c.Sender().ID, err)
		return c.Send(
			h.layout.Text(c, "technical_issues", err.Error()),
			h.layout.Markup(c, "core:hide"),
		)
	}

	if user.IsBanned {
		h.logger.Infof("(user: %d) user banned: %d", c.Sender().ID, userID)
		return c.Send(
			h.layout.Text(c, "user_banned", struct {
				FIO string
				ID  int64
			}{
				FIO: user.FIO.String(),
				ID:  user.ID,
			}),
			h.layout.Markup(c, "core:hide"),
		)
	}

	h.logger.Infof("(user: %d) user unbanned: %d", c.Sender().ID, userID)
	return c.Send(
		h.layout.Text(c, "user_unbanned", struct {
			FIO string
			ID  int64
		}{
			FIO: user.FIO.String(),
			ID:  user.ID,
		}),
		h.layout.Markup(c, "core:hide"),
	)
}

func (h Handler) AdminSetup(group *tele.Group) {
	group.Handle(h.layout.Callback("mainMenu:admin_menu"), h.adminMenu)
	group.Handle(h.layout.Callback("admin:back_to_menu"), h.adminMenu)
	group.Handle(h.layout.Callback("admin:create_club"), h.createClub)
	group.Handle(h.layout.Callback("admin:clubs"), h.clubsList)
	group.Handle(h.layout.Callback("admin:clubs:prev_page"), h.clubsList)
	group.Handle(h.layout.Callback("admin:clubs:next_page"), h.clubsList)
	group.Handle(h.layout.Callback("admin:clubs:back"), h.clubsList)
	group.Handle(h.layout.Callback("admin:clubs:club"), h.clubMenu)
	group.Handle(h.layout.Callback("admin:club:qr_allowed"), h.clubMenu)
	group.Handle(h.layout.Callback("admin:club:subscription_required"), h.clubMenu)
	group.Handle(h.layout.Callback("admin:club:back"), h.clubMenu)
	group.Handle(h.layout.Callback("admin:club:add_owner"), h.addClubOwner)
	group.Handle(h.layout.Callback("admin:club:del_owner"), h.removeClubOwner)
	group.Handle(h.layout.Callback("admin:club:roles"), h.manageRoles)
	group.Handle(h.layout.Callback("admin:club:roles:role"), h.manageRoles)
	group.Handle(h.layout.Callback("admin:club:delete"), h.deleteClub)
	group.Handle("/ban", h.banUser)
}
