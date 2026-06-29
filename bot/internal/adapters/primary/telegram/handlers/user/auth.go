package user

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/nlypage/intele/collector"
	"github.com/redis/go-redis/v9"
	tele "gopkg.in/telebot.v3"
	"gorm.io/gorm"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/codes"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/adapters/secondary/redis/emails"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/banner"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/location"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/validator"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/valueobject"
)

func (h Handler) declinePersonalDataAgreement(c tele.Context) error {
	h.logger.Infof("(user: %d) decline personal data agreement", c.Sender().ID)
	return c.Edit(
		banner.Auth.Caption(h.layout.Text(c, "decline_personal_data_agreement_text")),
	)
}

func (h Handler) acceptPersonalDataAgreement(c tele.Context) error {
	h.logger.Infof("(user: %d) accept personal data agreement", c.Sender().ID)
	return c.Edit(
		banner.Auth.Caption(h.layout.Text(c, "auth_menu_text")),
		h.layout.Markup(c, "auth:menu"),
	)
}

func (h Handler) externalUserAuth(c tele.Context) error {
	h.logger.Infof("(user: %d) external user auth", c.Sender().ID)

	inputCollector := collector.New()
	_ = c.Edit(
		banner.Auth.Caption(h.layout.Text(c, "fio_request")),
		h.layout.Markup(c, "auth:backToMenu"),
	)
	inputCollector.Collect(c.Message())

	var (
		fio  string
		done bool
	)
	for {
		response, err := h.input.Get(context.Background(), c.Sender().ID, 0)
		if response.Message != nil {
			inputCollector.Collect(response.Message)
		}
		switch {
		case response.Canceled:
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true, ExcludeLast: true})
			return nil
		case err != nil:
			h.logger.Errorf("(user: %d) error while input fio: %v", c.Sender().ID, err)
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "fio_request"))),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while input fio: %v", c.Sender().ID, err)
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "fio_request"))),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case !validator.Fio(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "invalid_user_fio")),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case validator.Fio(response.Message.Text, nil):
			fio = response.Message.Text
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	fioVO, err := valueobject.NewFIOFromString(fio)
	if err != nil {
		h.logger.Errorf("(user: %d) error creating FIO value object: %v", c.Sender().ID, err)
		return c.Send(
			banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "auth:backToMenu"),
		)
	}

	user := entity.User{
		ID:   c.Sender().ID,
		Role: valueobject.ExternalUser,
		FIO:  fioVO,
	}
	_, err = h.userService.Create(context.Background(), user)
	if err != nil {
		h.logger.Errorf("(user: %d) error while creating new user: %v", c.Sender().ID, err)
		return c.Send(
			banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "auth:backToMenu"),
		)
	}
	h.logger.Infof("(user: %d) new user created(role: %s)", c.Sender().ID, user.Role)

	eventID, err := h.eventsStorage.GetEventID(c.Sender().ID, "before-reg-event-id")
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			h.logger.Errorf("(user: %d) error while getting event ID: %v", c.Sender().ID, err)
			return c.Send(
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		}

		return h.menuHandler.SendMenu(c)
	}

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Send(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	participantsCount, err := h.eventParticipantService.CountByEventID(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get participants count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	var registered bool
	_, errGetParticipant := h.eventParticipantService.Get(context.Background(), eventID, c.Sender().ID)
	if errGetParticipant != nil {
		if !errors.Is(errGetParticipant, gorm.ErrRecordNotFound) {
			h.logger.Errorf("(user: %d) error while get participant: %v", c.Sender().ID, errGetParticipant)
			return c.Send(
				banner.Events.Caption(h.layout.Text(c, "technical_issues", errGetParticipant.Error())),
				h.layout.Markup(c, "mainMenu:back"),
			)
		}
	} else {
		registered = true
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

	_ = c.Send(
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
		h.layout.Markup(c, "user:url:event", struct {
			ID           string
			IsRegistered bool
			IsOver       bool
		}{
			ID:           eventID,
			IsRegistered: registered,
			IsOver:       event.IsOver(0),
		}))
	return nil
}

func (h Handler) isGrantChatMember(c tele.Context) (bool, error) {
	var lastErr error
	checkedChats := 0

	for _, grantChatID := range h.grantChatIDs {
		member, err := c.Bot().ChatMemberOf(&tele.Chat{ID: grantChatID}, &tele.User{ID: c.Sender().ID})
		if err != nil {
			lastErr = err
			h.logger.Errorf("(user: %d, chat: %d) error while verification user's membership in grant chat: %v", c.Sender().ID, grantChatID, err)
			continue
		}

		checkedChats++
		if isActiveChatMember(member.Role) {
			return true, nil
		}
	}

	if checkedChats == 0 && lastErr != nil {
		return false, lastErr
	}

	return false, nil
}

func isActiveChatMember(role tele.MemberStatus) bool {
	return role == tele.Creator || role == tele.Administrator || role == tele.Member
}

func (h Handler) grantUserAuth(c tele.Context) error {
	h.logger.Infof("(user: %d) grant user auth", c.Sender().ID)

	isGrantUser, err := h.isGrantChatMember(c)
	if err != nil {
		h.logger.Errorf("(user: %d) error while verification user's membership in grant chats: %v", c.Sender().ID, err)
		return c.Send(
			banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "core:hide"),
		)
	}

	if !isGrantUser {
		return c.Edit(
			banner.Auth.Caption(h.layout.Text(c, "grant_user_required")),
			h.layout.Markup(c, "auth:backToMenu"),
		)
	}

	inputCollector := collector.New()
	_ = c.Edit(
		banner.Auth.Caption(h.layout.Text(c, "fio_request")),
		h.layout.Markup(c, "auth:backToMenu"),
	)
	inputCollector.Collect(c.Message())

	var (
		fio  string
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
			h.logger.Errorf("(user: %d) error while input fio: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "fio_request"))),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while input fio: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "fio_request"))),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case !validator.Fio(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "invalid_user_fio")),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case validator.Fio(response.Message.Text, nil):
			fio = response.Message.Text
			_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})
			done = true
		}
		if done {
			break
		}
	}

	fioVO, err := valueobject.NewFIOFromString(fio)
	if err != nil {
		h.logger.Errorf("(user: %d) error creating FIO value object: %v", c.Sender().ID, err)
		return c.Send(
			banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "auth:backToMenu"),
		)
	}

	user := entity.User{
		ID:   c.Sender().ID,
		Role: valueobject.GrantUser,
		FIO:  fioVO,
	}
	_, err = h.userService.Create(context.Background(), user)
	if err != nil {
		h.logger.Errorf("(user: %d) error while creating new user: %v", c.Sender().ID, err)
		return c.Send(
			banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "auth:backToMenu"),
		)
	}
	h.logger.Infof("(user: %d) new user created(role: %s)", c.Sender().ID, user.Role)

	eventID, err := h.eventsStorage.GetEventID(c.Sender().ID, "before-reg-event-id")
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			h.logger.Errorf("(user: %d) error while getting event ID: %v", c.Sender().ID, err)
			return c.Send(
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		}

		return h.menuHandler.SendMenu(c)
	}

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Send(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
		return c.Send(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	participantsCount, err := h.eventParticipantService.CountByEventID(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get participants count: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	var registered bool
	_, errGetParticipant := h.eventParticipantService.Get(context.Background(), eventID, c.Sender().ID)
	if errGetParticipant != nil {
		if !errors.Is(errGetParticipant, gorm.ErrRecordNotFound) {
			h.logger.Errorf("(user: %d) error while get participant: %v", c.Sender().ID, errGetParticipant)
			return c.Send(
				banner.Events.Caption(h.layout.Text(c, "technical_issues", errGetParticipant.Error())),
				h.layout.Markup(c, "mainMenu:back"),
			)
		}
	} else {
		registered = true
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

	_ = c.Send(
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
		h.layout.Markup(c, "user:url:event", struct {
			ID           string
			IsRegistered bool
			IsOver       bool
		}{
			ID:           eventID,
			IsRegistered: registered,
			IsOver:       event.IsOver(0),
		}))
	return nil
}

func (h Handler) studentAuth(c tele.Context) error {
	h.logger.Infof("(user: %d) student auth", c.Sender().ID)

	inputCollector := collector.New()
	_ = c.Edit(
		banner.Auth.Caption(h.layout.Text(c, "email_request")),
		h.layout.Markup(c, "auth:backToMenu"),
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
				banner.Auth.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "email_request"))),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while input email: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "email_request"))),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case !validator.Email(response.Message.Text, h.validEmailDomains):
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "invalid_email")),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case validator.Email(response.Message.Text, h.validEmailDomains):
			email = response.Message.Text
			emailVO, err := valueobject.NewEmail(email)
			if err != nil {
				_ = inputCollector.Send(c,
					banner.Auth.Caption(h.layout.Text(c, "invalid_email")),
					h.layout.Markup(c, "auth:backToMenu"),
				)
				continue
			}
			_, err = h.userService.GetByEmail(context.Background(), emailVO)
			if err == nil {
				_ = inputCollector.Send(c,
					banner.Auth.Caption(h.layout.Text(c, "user_with_this_email_already_exists")),
					h.layout.Markup(c, "auth:backToMenu"),
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

	inputCollector = collector.New()
	_ = inputCollector.Send(c,
		banner.Auth.Caption(h.layout.Text(c, "fio_request")),
		h.layout.Markup(c, "auth:backToMenu"),
	)

	var (
		fio     string
		doneFIO bool
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
			h.logger.Errorf("(user: %d) error while input fio %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "fio_request"))),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case response.Message == nil:
			h.logger.Errorf("(user: %d) error while input fio: %v", c.Sender().ID, errGet)
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "input_error", h.layout.Text(c, "fio_request"))),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case !validator.Fio(response.Message.Text, nil):
			_ = inputCollector.Send(c,
				banner.Auth.Caption(h.layout.Text(c, "invalid_user_fio")),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		case validator.Fio(response.Message.Text, nil):
			fio = response.Message.Text
			doneFIO = true
		}
		if doneFIO {
			break
		}
	}
	_ = inputCollector.Clear(c, collector.ClearOptions{IgnoreErrors: true})

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
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
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
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		}
		_ = c.Bot().Delete(loading)

		fioVO, err := valueobject.NewFIOFromString(fio)
		if err != nil {
			h.logger.Errorf("(user: %d) error creating FIO value object: %v", c.Sender().ID, err)
			return c.Send(
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		}

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
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		}

		emailVO, err = valueobject.NewEmail(email)
		if err != nil {
			h.logger.Errorf("(user: %d) error creating email value object: %v", c.Sender().ID, err)
			return c.Send(
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		}

		fioVO, err = valueobject.NewFIOFromString(fio)
		if err != nil {
			h.logger.Errorf("(user: %d) error creating FIO value object: %v", c.Sender().ID, err)
			return c.Send(
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		}

		err = h.codesStorage.Set(
			c.Sender().ID,
			code,
			codes.CodeTypeAuth,
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
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
			)
		}

		h.logger.Infof("(user: %d) auth code sent on %s", c.Sender().ID, email)

		return c.Send(
			banner.Auth.Caption(h.layout.Text(c, "email_auth_link_sent")),
			h.layout.Markup(c, "auth:resendMenu"),
		)
	}

	return c.Send(
		banner.Auth.Caption(h.layout.Text(c,
			"resend_timeout",
			h.resendTTL.Minutes()),
		),
		h.layout.Markup(c, "auth:resendMenu"),
	)
}

func (h Handler) backToAuthMenu(c tele.Context) error {
	return c.Edit(
		banner.Auth.Caption(h.layout.Text(c, "auth_menu_text")),
		h.layout.Markup(c, "auth:menu"),
	)
}

func (h Handler) resendAuthEmailConfirmationCode(c tele.Context) error {
	h.logger.Infof("(user: %d) resend auth code", c.Sender().ID)

	canResend, timeBeforeResend, err := h.codesStorage.GetCanResend(c.Sender().ID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while getting auth code from redis: %v", c.Sender().ID, err)
		return c.Send(
			banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "core:hide"),
		)
	}

	var code string
	var email emails.Email
	if canResend {
		email, err = h.emailsStorage.Get(c.Sender().ID)
		if err != nil && !errors.Is(err, redis.Nil) {
			h.logger.Errorf("(user: %d) error while getting user email from redis: %v", c.Sender().ID, err)
			return c.Send(
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "core:hide"),
			)
		}

		if errors.Is(err, redis.Nil) {
			return c.Send(
				banner.Auth.Caption(h.layout.Text(c, "session_expire")),
				h.layout.Markup(c, "core:hide"),
			)
		}

		loading, _ := c.Bot().Send(c.Chat(), h.layout.Text(c, "loading"))
		emailVO, err := valueobject.NewEmail(email.Email)
		if err != nil {
			_ = c.Bot().Delete(loading)
			h.logger.Errorf("(user: %d) error creating email value object: %v", c.Sender().ID, err)
			return c.Send(
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
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
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "auth:backToMenu"),
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
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "core:hide"),
			)
		}
		err = h.codesStorage.Set(
			c.Sender().ID,
			code,
			codes.CodeTypeAuth,
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
				banner.Auth.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "core:hide"),
			)
		}

		h.logger.Infof("(user: %d) auth code sent on %s", c.Sender().ID, email.Email)

		return c.Edit(
			banner.Auth.Caption(h.layout.Text(c, "email_auth_link_resent")),
			h.layout.Markup(c, "auth:resendMenu"),
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

func (h Handler) AuthSetup(group *tele.Group) {
	group.Handle(h.layout.Callback("auth:personalData:accept"), h.acceptPersonalDataAgreement)
	group.Handle(h.layout.Callback("auth:personalData:decline"), h.declinePersonalDataAgreement)
	group.Handle(h.layout.Callback("auth:external_user"), h.externalUserAuth)
	group.Handle(h.layout.Callback("auth:grant_user"), h.grantUserAuth)
	group.Handle(h.layout.Callback("auth:student"), h.studentAuth)
	group.Handle(h.layout.Callback("auth:resend_email"), h.resendAuthEmailConfirmationCode)
	group.Handle(h.layout.Callback("auth:back_to_menu"), h.backToAuthMenu)
}
