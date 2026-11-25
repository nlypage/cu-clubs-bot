package dto

import (
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
)

// CancelRegistrationAvailability describes whether a user can cancel their
// registration for an event, and why they cannot, if they cannot.
type CancelRegistrationAvailability struct {
	// Allowed is true when the user is free to cancel the registration.
	Allowed bool
	// AlreadyVisited is true when the user has already been marked as visited
	// via QR activation. Cancellation is blocked for such users because
	// deleting the participant record would also erase the attendance mark.
	AlreadyVisited bool
}

type EventUser struct {
	User      entity.User
	UserVisit bool
}

func NewEventUserFromEntity(user entity.User, userVisit bool) EventUser {
	return EventUser{
		User:      user,
		UserVisit: userVisit,
	}
}
