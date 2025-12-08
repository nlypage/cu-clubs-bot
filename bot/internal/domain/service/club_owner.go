package service

import (
	"context"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/secondary"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
)

type ClubOwnerService struct {
	repo     secondary.ClubOwnerRepository
	userRepo secondary.UserRepository
}

func NewClubOwnerService(storage secondary.ClubOwnerRepository, userStorage secondary.UserRepository) *ClubOwnerService {
	return &ClubOwnerService{
		repo:     storage,
		userRepo: userStorage,
	}
}

func (s *ClubOwnerService) Add(ctx context.Context, userID int64, clubID string) (*entity.ClubOwner, error) {
	return s.repo.Create(ctx, &entity.ClubOwner{UserID: userID, ClubID: clubID})
}

func (s *ClubOwnerService) Remove(ctx context.Context, userID int64, clubID string) error {
	return s.repo.Delete(ctx, userID, clubID)
}

func (s *ClubOwnerService) Get(ctx context.Context, clubID string, userID int64) (*entity.ClubOwner, error) {
	return s.repo.Get(ctx, clubID, userID)
}

func (s *ClubOwnerService) Update(ctx context.Context, clubOwner *entity.ClubOwner) (*entity.ClubOwner, error) {
	return s.repo.Update(ctx, clubOwner)
}

func (s *ClubOwnerService) GetByClubID(ctx context.Context, clubID string) ([]dto.ClubOwner, error) {
	return s.repo.GetByClubID(ctx, clubID)
}

func (s *ClubOwnerService) GetByUserID(ctx context.Context, userID int64) ([]dto.ClubOwner, error) {
	return s.repo.GetByUserID(ctx, userID)
}

func (s *ClubOwnerService) GetAllUniqueClubOwners(ctx context.Context) ([]dto.ClubOwner, error) {
	return s.repo.GetAllUniqueClubOwners(ctx)
}
