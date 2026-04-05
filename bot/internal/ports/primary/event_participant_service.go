package primary

import (
	"context"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
)

// EventParticipantService defines the interface for event participant-related use cases
type EventParticipantService interface {
	Register(ctx context.Context, eventID string, userID int64) (*entity.EventParticipant, error)
	Get(ctx context.Context, eventID string, userID int64) (*entity.EventParticipant, error)
	Update(ctx context.Context, eventParticipant *entity.EventParticipant) (*entity.EventParticipant, error)
	Delete(ctx context.Context, eventID string, userID int64) error
	GetByEventID(ctx context.Context, eventID string) ([]entity.EventParticipant, error)
	CountByEventID(ctx context.Context, eventID string) (int, error)
	CountVisitedByEventID(ctx context.Context, eventID string) (int, error)
	GetUserEvents(ctx context.Context, userID int64, limit, offset int) ([]dto.UserEvent, error)
	CountUserEvents(ctx context.Context, userID int64) (int64, error)
	MarkAsVisited(ctx context.Context, eventID string, userID int64, isUserQR, isEventQR bool) error
	IsUserRegistered(ctx context.Context, eventID string, userID int64) (bool, error)
	IsShadowBanned(ctx context.Context, userID int64) (bool, error)
	CanCancelRegistration(ctx context.Context, eventID string) (bool, error)
	BulkRegister(ctx context.Context, eventID string, userIDs []int64) ([]entity.EventParticipant, error)
	GetVisitedParticipants(ctx context.Context, eventID string) ([]entity.EventParticipant, error)
	GetNotVisitedParticipants(ctx context.Context, eventID string) ([]entity.EventParticipant, error)
}
