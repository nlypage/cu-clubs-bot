package primary

import (
	"context"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
)

// ClubOwnerService defines the interface for club owner-related use cases
type ClubOwnerService interface {
	Add(ctx context.Context, userID int64, clubID string) (*entity.ClubOwner, error)
	Remove(ctx context.Context, userID int64, clubID string) error
	Get(ctx context.Context, clubID string, userID int64) (*entity.ClubOwner, error)
	Update(ctx context.Context, clubOwner *entity.ClubOwner) (*entity.ClubOwner, error)
	GetByClubID(ctx context.Context, clubID string) ([]dto.ClubOwner, error)
	GetByUserID(ctx context.Context, userID int64) ([]dto.ClubOwner, error)
	GetAllUniqueClubOwners(ctx context.Context) ([]dto.ClubOwner, error)
}
