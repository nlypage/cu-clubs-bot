package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/utils/shadowban"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/valueobject"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/ports/secondary"

	tele "gopkg.in/telebot.v3"
)

type UserService struct {
	userRepo             secondary.UserRepository
	eventParticipantRepo secondary.EventParticipantRepository
	smtpClient           secondary.SMTPClient

	emailHTMLFilePath string
	shadowMatcher     *shadowban.Matcher
}

func NewUserService(
	userRepo secondary.UserRepository,
	eventParticipantRepo secondary.EventParticipantRepository,
	smtpClient secondary.SMTPClient,
	emailHTMLFilePath string,
	shadowBanNameSurnames []string,
) *UserService {
	return &UserService{
		userRepo:             userRepo,
		eventParticipantRepo: eventParticipantRepo,
		smtpClient:           smtpClient,

		emailHTMLFilePath: emailHTMLFilePath,
		shadowMatcher:     shadowban.NewMatcher(shadowBanNameSurnames),
	}
}

func (s *UserService) Create(ctx context.Context, user entity.User) (*entity.User, error) {
	return s.userRepo.Create(ctx, &user)
}

func (s *UserService) Get(ctx context.Context, userID int64) (*entity.User, error) {
	return s.userRepo.Get(ctx, uint(userID))
}

func (s *UserService) GetByEmail(ctx context.Context, email valueobject.Email) (*entity.User, error) {
	return s.userRepo.GetByEmail(ctx, email)
}

func (s *UserService) GetByQRCodeID(ctx context.Context, qrCodeID string) (*entity.User, error) {
	return s.userRepo.GetByQRCodeID(ctx, qrCodeID)
}

func (s *UserService) GetAll(ctx context.Context) ([]entity.User, error) {
	return s.userRepo.GetAll(ctx)
}

func (s *UserService) Update(ctx context.Context, user *entity.User) (*entity.User, error) {
	return s.userRepo.Update(ctx, user)
}

func (s *UserService) UpdateData(ctx context.Context, c tele.Context) (*entity.User, error) {
	user, err := s.Get(ctx, c.Sender().ID)
	if err != nil {
		return nil, err
	}
	user.ID = c.Sender().ID

	return s.userRepo.Update(ctx, user)
}

func (s *UserService) Count(ctx context.Context) (int64, error) {
	return s.userRepo.Count(ctx)
}

func (s *UserService) GetWithPagination(ctx context.Context, limit int, offset int, order string) ([]entity.User, error) {
	return s.userRepo.GetWithPagination(ctx, limit, offset, order)
}

func (s *UserService) Ban(ctx context.Context, userID int64) (*entity.User, error) {
	user, err := s.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	user.IsBanned = true
	return s.Update(ctx, user)
}

func (s *UserService) GetUsersByEventID(ctx context.Context, eventID string) ([]entity.User, error) {
	users, err := s.userRepo.GetUsersByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}

	return s.filterUsers(users), nil
}

func (s *UserService) GetEventUsers(ctx context.Context, eventID string) ([]dto.EventUser, error) {
	users, err := s.userRepo.GetEventUsers(ctx, eventID)
	if err != nil {
		return nil, err
	}

	return s.filterEventUsers(users), nil
}

func (s *UserService) GetUsersByClubID(ctx context.Context, clubID string) ([]entity.User, error) {
	users, err := s.userRepo.GetUsersByClubID(ctx, clubID)
	if err != nil {
		return nil, err
	}

	return s.filterUsers(users), nil
}

func (s *UserService) GetUserEvents(ctx context.Context, userID int64, limit, offset int) ([]dto.UserEvent, error) {
	return s.eventParticipantRepo.GetUserEvents(ctx, userID, limit, offset)
}

func (s *UserService) CountUserEvents(ctx context.Context, userID int64) (int64, error) {
	return s.eventParticipantRepo.CountUserEvents(ctx, userID)
}

func (s *UserService) SendAuthCode(_ context.Context, email valueobject.Email, botUserName string) (code string, err error) {
	link, code, err := generateAuthLink(12, botUserName)
	if err != nil {
		return "", err
	}

	message, err := s.smtpClient.GenerateEmailConfirmationMessage(s.emailHTMLFilePath, map[string]string{
		"AuthLink": link,
	})
	if err != nil {
		return "", err
	}

	if err := s.smtpClient.Send(email.String(), "Email confirmation", message, "Email confirmation", nil); err != nil {
		return "", fmt.Errorf("failed to send email: %w", err)
	}

	return code, nil
}

// IgnoreMailing is a function that allows or disallows mailing for a user (returns error and new state)
func (s *UserService) IgnoreMailing(ctx context.Context, userID int64, clubID string) (bool, error) {
	return s.userRepo.IgnoreMailing(ctx, userID, clubID)
}

func (s *UserService) ChangeRole(
	ctx context.Context,
	userID int64,
	role entity.Role,
	email valueobject.Email,
) error {
	user, err := s.Get(ctx, userID)
	if err != nil {
		return err
	}
	user.Role = role
	user.Email = email
	_, err = s.Update(ctx, user)
	return err
}

func generateAuthLink(codeLength int, botUserName string) (link string, code string, err error) {
	code, err = generateRandomCode(codeLength)
	if err != nil {
		return link, code, err
	}

	return fmt.Sprintf("https://t.me/%s?start=emailCode_%s", botUserName, code), code, err
}

func generateRandomCode(length int) (string, error) {
	bts := make([]byte, length)
	if _, err := rand.Read(bts); err != nil {
		return "", err
	}
	return hex.EncodeToString(bts)[:length], nil
}

func (s *UserService) filterUsers(users []entity.User) []entity.User {
	if s.shadowMatcher == nil || len(users) == 0 {
		return users
	}

	filtered := make([]entity.User, 0, len(users))
	for _, user := range users {
		if s.shadowMatcher.MatchUser(user) {
			continue
		}
		filtered = append(filtered, user)
	}

	return filtered
}

func (s *UserService) filterEventUsers(users []dto.EventUser) []dto.EventUser {
	if s.shadowMatcher == nil || len(users) == 0 {
		return users
	}

	filtered := make([]dto.EventUser, 0, len(users))
	for _, user := range users {
		if s.shadowMatcher.MatchUser(user.User) {
			continue
		}
		filtered = append(filtered, user)
	}

	return filtered
}
