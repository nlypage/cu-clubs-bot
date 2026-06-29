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
			isShadowBanned, err := h.eventParticipantService.IsShadowBanned(context.Background(), c.Sender().ID)
			if err != nil {
				h.logger.Errorf("(user: %d) error while checking shadow ban: %v", c.Sender().ID, err)
				return c.Edit(
					banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
					h.layout.Markup(c, "mainMenu:back"),
				)
			}

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
			var userSubscribed bool
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

			if (event.MaxParticipants == 0 || participantsCount < event.MaxParticipants || isShadowBanned) && registrationActive && roleAllowed && userSubscribed {
				h.logger.Infof("(user: %d) registration approved for event %s (participants: %d/%d, shadow_banned: %t, role: %s, subscribed: %t)",
					c.Sender().ID, eventID, participantsCount, event.MaxParticipants, isShadowBanned, user.Role, userSubscribed)

				_, err = h.eventParticipantService.Register(context.Background(), eventID, c.Sender().ID)
				if err != nil {
					h.logger.Errorf("(user: %d) error while register to event: %v", c.Sender().ID, err)
					return c.Edit(
						banner.Events.Caption(h.layout.Text(c, "technical_issues", err.Error())),
						h.layout.Markup(c, "mainMenu:back"),
					)
				}

				h.logger.Infof("(user: %d) successfully registered to event %s", c.Sender().ID, eventID)

				if !isShadowBanned && participantsCount+1 == event.ExpectedParticipants {
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

				if !isShadowBanned && participantsCount+1 == event.MaxParticipants {
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
				// Детальное логирование причин отказа
				h.logger.Infof("(user: %d) registration denied for event %s - checking conditions:", c.Sender().ID, eventID)
				h.logger.Infof("(user: %d) - registration_active: %t (registration_end: %s, now: %s)",
					c.Sender().ID, registrationActive, event.RegistrationEnd.Format("2006-01-02 15:04:05"), time.Now().In(location.Location()).Format("2006-01-02 15:04:05"))
				h.logger.Infof("(user: %d) - role_allowed: %t (user_role: %s, allowed_roles: %v)",
					c.Sender().ID, roleAllowed, user.Role, event.AllowedRoles)
				h.logger.Infof("(user: %d) - user_subscribed: %t (subscription_required: %t, channel_id: %v)",
					c.Sender().ID, userSubscribed, club.SubscriptionRequired, club.ChannelID)
				h.logger.Infof("(user: %d) - participants_limit: %d/%d (shadow_banned: %t)",
					c.Sender().ID, participantsCount, event.MaxParticipants, isShadowBanned)

				switch {
				case event.RegistrationEnd.Before(time.Now().In(location.Location())):
					h.logger.Infof("(user: %d) registration denied: registration ended (ended: %s, now: %s)",
						c.Sender().ID, event.RegistrationEnd.In(location.Location()).Format("2006-01-02 15:04:05"), time.Now().In(location.Location()).Format("2006-01-02 15:04:05"))
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "registration_ended"),
						ShowAlert: true,
					})
				case !isShadowBanned && event.MaxParticipants > 0 && participantsCount >= event.MaxParticipants:
					h.logger.Infof("(user: %d) registration denied: max participants reached (%d/%d)",
						c.Sender().ID, participantsCount, event.MaxParticipants)
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "max_participants_reached"),
						ShowAlert: true,
					})
				case !roleAllowed:
					h.logger.Infof("(user: %d) registration denied: role not allowed (user_role: %s, allowed_roles: %v)",
						c.Sender().ID, user.Role, event.AllowedRoles)
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "not_allowed_role"),
						ShowAlert: true,
					})
				case !userSubscribed:
					h.logger.Infof("(user: %d) registration denied: not subscribed to channel %v",
						c.Sender().ID, club.ChannelID)
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
				default:
					h.logger.Errorf("(user: %d) registration denied: unknown reason (active: %t, role_ok: %t, subscribed: %t, limit_ok: %t)",
						c.Sender().ID, registrationActive, roleAllowed, userSubscribed,
						(event.MaxParticipants == 0 || participantsCount < event.MaxParticipants || isShadowBanned))
					return c.Respond(&tele.CallbackResponse{
						Text:      h.layout.Text(c, "technical_issues", "Unknown registration error"),
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
			RegistrationEnd:       event.RegistrationEnd.In(location.Location()).Format("02.01.2006 15:04"),
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

func (h Handler) SetupURLEvent(group *tele.Group) {
	group.Handle(h.layout.Callback("user:url:event:register"), h.eventRegister)
}
