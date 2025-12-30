package start

import (
	"context"
	"errors"
	"time"

	tele "gopkg.in/telebot.v3"
	"gorm.io/gorm"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/banner"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/location"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/valueobject"
)

func (h Handler) eventMenu(c tele.Context, eventID string) error {
	_ = c.Delete()
	h.logger.Infof("(user: %d) open event url (event_id=%s)", c.Sender().ID, eventID)

	user, err := h.userService.Get(context.Background(), c.Sender().ID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			h.logger.Errorf("(user: %d) error while get user: %v", c.Sender().ID, err)
			return c.Send(
				banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
				h.layout.Markup(c, "mainMenu:back"),
			)
		}
		h.eventsStorage.SetEventID(c.Sender().ID, "before-reg-event-id", eventID, h.eventIDTTL)
		return c.Send(
			banner.Auth.Caption(h.layout.Text(c, "auth_required")),
			h.layout.Markup(c, "core:hide"),
		)
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

	var registrationActive bool
	if user.Role == valueobject.Student {
		registrationActive = event.RegistrationEnd.After(time.Now().In(location.Location()))
	} else {
		registrationActive = utils.GetMaxRegisteredEndTime(event.StartTime).After(time.Now().In(location.Location())) && event.RegistrationEnd.After(time.Now().In(location.Location()))
	}

	if !registrationActive && !registered {
		return c.Send(
			banner.Events.Caption(h.layout.Text(c, "registration_ended")),
			h.layout.Markup(c, "mainMenu:back"),
		)
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

func (h Handler) eventRegister(c tele.Context) error {
	eventID := c.Callback().Data
	h.logger.Infof("(user: %d) register to event by url (event_id=%s)", c.Sender().ID, eventID)

	event, err := h.eventService.Get(context.Background(), eventID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get event: %v", c.Sender().ID, err)
		return c.Edit(
			banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	club, err := h.clubService.Get(context.Background(), event.ClubID)
	if err != nil {
		h.logger.Errorf("(user: %d) error while get club: %v", c.Sender().ID, err)
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
			return c.Edit(
				banner.Events.Caption(h.layout.Text(c, "technical_issues", errGetParticipant.Error())),
				h.layout.Markup(c, "mainMenu:back"),
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
			h.layout.Markup(c, "mainMenu:back"),
		)
	}

	if c.Callback().Unique == "user_url_event_reg" {
		if !registered {
			var user *entity.User
			user, err = h.userService.Get(context.Background(), c.Sender().ID)
			if err != nil {
				h.logger.Errorf("(user: %d) error while get user: %v", c.Sender().ID, err)
				return c.Edit(
					banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
					h.layout.Markup(c, "mainMenu:back"),
				)
			}

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

			needToSubscribe := make([]int64, 0)
			if club.SubscriptionRequired {
				for _, chatID := range club.ChannelsIDs {
					member, err := c.Bot().ChatMemberOf(&tele.Chat{ID: chatID}, &tele.User{ID: c.Sender().ID})
					if err != nil {
						h.logger.Errorf("(user: %d) error while verification user's membership in the club channel: %v", c.Sender().ID, err)
						return c.Send(
							banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
							h.layout.Markup(c, "core:hide"),
						)
					}

					if member.Role != tele.Creator && member.Role != tele.Administrator && member.Role != tele.Member {
						needToSubscribe = append(needToSubscribe, chatID)
					}
				}
			}

			if (event.MaxParticipants == 0 || participantsCount < event.MaxParticipants) && registrationActive && roleAllowed && len(needToSubscribe) == 0 {
				_, err = h.eventParticipantService.Register(context.Background(), eventID, c.Sender().ID)
				if err != nil {
					h.logger.Errorf("(user: %d) error while register to event: %v", c.Sender().ID, err)
					return c.Edit(
						banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
						h.layout.Markup(c, "mainMenu:back"),
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
				case len(needToSubscribe) > 0:
					channelsNames := make([]string, 0)
					for _, chatID := range needToSubscribe {
						chat, err := c.Bot().ChatByID(chatID)
						if err != nil {
							h.logger.Errorf("(user: %d) error while get chat: %v", c.Sender().ID, err)
							return c.Send(
								banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
								h.layout.Markup(c, "core:hide"),
							)
						}
						channelsNames = append(channelsNames, chat.Username)
					}

					return c.Send(
						banner.Events.Caption(h.layout.Text(c, "user_not_subscribed", struct {
							ChannelsNames []string
						}{
							ChannelsNames: channelsNames,
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
			AfterRegistrationText string
			IsRegistered          bool
		}{
			Name:                  event.Name,
			ClubName:              club.Name,
			Description:           event.Description,
			Location:              event.Location,
			StartTime:             event.StartTime.In(location.Location()).Format("02.01.2006 15:04"),
			EndTime:               endTime,
			RegistrationEnd:       event.RegistrationEnd.In(location.Location()).Format("02.01.2006 15:04"),
			MaxParticipants:       event.MaxParticipants,
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

func (h Handler) SetupURLEvent(group *tele.Group) {
	group.Handle(h.layout.Callback("user:url:event:register"), h.eventRegister)
}
