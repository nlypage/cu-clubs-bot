package secondary

import (
	"context"
	"time"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
)

// EventParticipantRepository defines the interface for event participant data access
type EventParticipantRepository interface {
	Create(ctx context.Context, eventParticipant *entity.EventParticipant) (*entity.EventParticipant, error)
	Get(ctx context.Context, eventID string, userID int64) (*entity.EventParticipant, error)
	Update(ctx context.Context, eventParticipant *entity.EventParticipant) (*entity.EventParticipant, error)
	Delete(ctx context.Context, eventID string, userID int64) error
	GetByEventID(ctx context.Context, eventID string) ([]entity.EventParticipant, error)
	CountByEventID(ctx context.Context, eventID string) (int64, error)
	CountVisitedByEventID(ctx context.Context, eventID string) (int64, error)
	GetUserEvents(ctx context.Context, userID int64, limit, offset int) ([]dto.UserEvent, error)
	CountUserEvents(ctx context.Context, userID int64) (int64, error)
	GetVisitedInRange(ctx context.Context, from, to time.Time) ([]dto.Visit, error)
}
