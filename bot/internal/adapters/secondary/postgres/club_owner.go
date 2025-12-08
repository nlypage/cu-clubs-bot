package postgres

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/valueobject"
)

type ClubOwnerRepository struct {
	db *gorm.DB
}

func NewClubOwnerRepository(db *gorm.DB) *ClubOwnerRepository {
	return &ClubOwnerRepository{
		db: db,
	}
}

func (s *ClubOwnerRepository) Create(ctx context.Context, clubOwner *entity.ClubOwner) (*entity.ClubOwner, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Check if club exists
		var clubExists int64
		if err := tx.Model(&entity.Club{}).Where("id = ?", clubOwner.ClubID).Count(&clubExists).Error; err != nil {
			return err
		}
		if clubExists == 0 {
			return fmt.Errorf("club with id %s not found", clubOwner.ClubID)
		}

		// Check if user exists
		var userExists int64
		if err := tx.Model(&entity.User{}).Where("id = ?", clubOwner.UserID).Count(&userExists).Error; err != nil {
			return err
		}
		if userExists == 0 {
			return fmt.Errorf("user with id %d not found", clubOwner.UserID)
		}

		// Create club owner
		return tx.Create(&clubOwner).Error
	})

	return clubOwner, err
}

func (s *ClubOwnerRepository) Delete(ctx context.Context, userID int64, clubID string) error {
	err := s.db.WithContext(ctx).Where("club_id = ? AND user_id = ?", clubID, userID).Delete(&entity.ClubOwner{}).Error
	return err
}

func (s *ClubOwnerRepository) Get(ctx context.Context, clubID string, userID int64) (*entity.ClubOwner, error) {
	var clubOwner entity.ClubOwner
	err := s.db.WithContext(ctx).Where("club_id = ? AND user_id = ?", clubID, userID).First(&clubOwner).Error
	return &clubOwner, err
}

func (s *ClubOwnerRepository) Update(ctx context.Context, clubOwner *entity.ClubOwner) (*entity.ClubOwner, error) {
	err := s.db.WithContext(ctx).Save(&clubOwner).Error
	return clubOwner, err
}

func (s *ClubOwnerRepository) GetByClubID(ctx context.Context, clubID string) ([]dto.ClubOwner, error) {
	type RawClubOwner struct {
		ClubID   string `gorm:"column:club_id"`
		UserID   int64  `gorm:"column:user_id"`
		Username string `gorm:"column:username"`
		Warnings bool   `gorm:"column:warnings"`
		FIO      string `gorm:"column:fio"`
		Email    string `gorm:"column:email"`
		Role     string `gorm:"column:role"`
		IsBanned bool   `gorm:"column:is_banned"`
	}

	var rawResult []RawClubOwner
	err := s.db.WithContext(ctx).
		Table("club_owners").
		Select("club_owners.club_id, club_owners.user_id, users.username, club_owners.warnings, users.fio, users.email, users.role, users.is_banned").
		Joins("LEFT JOIN users ON users.id = club_owners.user_id").
		Where("club_owners.club_id = ?", clubID).
		Scan(&rawResult).Error
	if err != nil {
		return nil, err
	}

	result := make([]dto.ClubOwner, len(rawResult))
	for i, raw := range rawResult {
		var email valueobject.Email
		if raw.Email != "" {
			var err error
			email, err = valueobject.NewEmail(raw.Email)
			if err != nil {
				return nil, fmt.Errorf("invalid email format for user %d: %w", raw.UserID, err)
			}
		}

		fio, err := valueobject.NewFIOFromString(raw.FIO)
		if err != nil {
			return nil, fmt.Errorf("invalid FIO format for user %d: %w", raw.UserID, err)
		}

		role := valueobject.Role(raw.Role)

		result[i] = dto.ClubOwner{
			ClubID:   raw.ClubID,
			UserID:   raw.UserID,
			Username: raw.Username,
			FIO:      fio,
			Email:    email,
			Role:     role,
			IsBanned: raw.IsBanned,
			Warnings: raw.Warnings,
		}
	}

	return result, nil
}

func (s *ClubOwnerRepository) GetByUserID(ctx context.Context, userID int64) ([]dto.ClubOwner, error) {
	type RawClubOwner struct {
		ClubID   string `gorm:"column:club_id"`
		UserID   int64  `gorm:"column:user_id"`
		Username string `gorm:"column:username"`
		Warnings bool   `gorm:"column:warnings"`
		FIO      string `gorm:"column:fio"`
		Email    string `gorm:"column:email"`
		Role     string `gorm:"column:role"`
		IsBanned bool   `gorm:"column:is_banned"`
	}

	var rawResult []RawClubOwner
	err := s.db.WithContext(ctx).
		Table("club_owners").
		Select("club_owners.club_id, club_owners.user_id, users.username, club_owners.warnings, users.fio, users.email, users.role, users.is_banned").
		Joins("LEFT JOIN users ON users.id = club_owners.user_id").
		Where("club_owners.user_id = ?", userID).
		Scan(&rawResult).Error
	if err != nil {
		return nil, err
	}

	result := make([]dto.ClubOwner, len(rawResult))
	for i, raw := range rawResult {
		var email valueobject.Email
		if raw.Email != "" {
			var err error
			email, err = valueobject.NewEmail(raw.Email)
			if err != nil {
				return nil, fmt.Errorf("invalid email format for user %d: %w", raw.UserID, err)
			}
		}

		fio, err := valueobject.NewFIOFromString(raw.FIO)
		if err != nil {
			return nil, fmt.Errorf("invalid FIO format for user %d: %w", raw.UserID, err)
		}

		role := valueobject.Role(raw.Role)

		result[i] = dto.ClubOwner{
			ClubID:   raw.ClubID,
			UserID:   raw.UserID,
			Username: raw.Username,
			FIO:      fio,
			Email:    email,
			Role:     role,
			IsBanned: raw.IsBanned,
			Warnings: raw.Warnings,
		}
	}

	return result, nil
}

func (s *ClubOwnerRepository) GetAllUniqueClubOwners(ctx context.Context) ([]dto.ClubOwner, error) {
	type RawClubOwner struct {
		UserID   int64  `gorm:"column:user_id"`
		Username string `gorm:"column:username"`
		Warnings bool   `gorm:"column:warnings"`
		FIO      string `gorm:"column:fio"`
		Email    string `gorm:"column:email"`
		Role     string `gorm:"column:role"`
		IsBanned bool   `gorm:"column:is_banned"`
	}

	var rawResult []RawClubOwner
	err := s.db.WithContext(ctx).
		Table("club_owners").
		Select("DISTINCT club_owners.user_id, users.username, club_owners.warnings, users.fio, users.email, users.role, users.is_banned").
		Joins("LEFT JOIN users ON users.id = club_owners.user_id").
		Scan(&rawResult).Error
	if err != nil {
		return nil, err
	}

	result := make([]dto.ClubOwner, len(rawResult))
	for i, raw := range rawResult {
		var email valueobject.Email
		if raw.Email != "" {
			var err error
			email, err = valueobject.NewEmail(raw.Email)
			if err != nil {
				return nil, fmt.Errorf("invalid email format for user %d: %w", raw.UserID, err)
			}
		}

		fio, err := valueobject.NewFIOFromString(raw.FIO)
		if err != nil {
			return nil, fmt.Errorf("invalid FIO format for user %d: %w", raw.UserID, err)
		}

		role := valueobject.Role(raw.Role)

		result[i] = dto.ClubOwner{
			UserID:   raw.UserID,
			Username: raw.Username,
			FIO:      fio,
			Email:    email,
			Role:     role,
			IsBanned: raw.IsBanned,
			Warnings: raw.Warnings,
		}
	}

	return result, nil
}
