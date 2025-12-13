package primary

import (
	"context"
	"time"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/valueobject"
)

// EventService defines the interface for event-related use cases
type EventService interface {
	Create(ctx context.Context, event *entity.Event) (*entity.Event, error)
	Get(ctx context.Context, id string) (*entity.Event, error)
	GetByQRCodeID(ctx context.Context, qrCodeID string) (*entity.Event, error)
	GetMany(ctx context.Context, ids []string) ([]entity.Event, error)
	GetAll(ctx context.Context) ([]entity.Event, error)
	GetByClubID(ctx context.Context, limit, offset int, clubID string) ([]entity.Event, error)
	CountByClubID(ctx context.Context, clubID string) (int64, error)
	GetFutureByClubID(ctx context.Context, limit, offset int, order string, clubID string, additionalTime time.Duration) ([]entity.Event, error)
	Update(ctx context.Context, event *entity.Event) (*entity.Event, error)
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context, role valueobject.Role) (int64, error)
	GetWithPagination(ctx context.Context, limit, offset int, order string, role valueobject.Role, userID int64) ([]dto.Event, error)
	GenerateWeeklyDigestImage(events []entity.Event) ([]byte, error)
}
