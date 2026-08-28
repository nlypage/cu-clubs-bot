package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/dto"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
)

type EventParticipantRepository struct {
	db *gorm.DB
}

func NewEventParticipantRepository(db *gorm.DB) *EventParticipantRepository {
	return &EventParticipantRepository{
		db: db,
	}
}

func (s *EventParticipantRepository) Create(ctx context.Context, eventParticipant *entity.EventParticipant) (*entity.EventParticipant, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Check if event exists
		var eventExists int64
		if err := tx.Model(&entity.Event{}).Where("id = ?", eventParticipant.EventID).Count(&eventExists).Error; err != nil {
			return err
		}
		if eventExists == 0 {
			return fmt.Errorf("event with id %s not found", eventParticipant.EventID)
		}

		// Check if user exists
		var userExists int64
		if err := tx.Model(&entity.User{}).Where("id = ?", eventParticipant.UserID).Count(&userExists).Error; err != nil {
			return err
		}
		if userExists == 0 {
			return fmt.Errorf("user with id %d not found", eventParticipant.UserID)
		}

		// Create participant
		return tx.Create(&eventParticipant).Error
	})

	return eventParticipant, err
}

func (s *EventParticipantRepository) Get(ctx context.Context, eventID string, userID int64) (*entity.EventParticipant, error) {
	var eventParticipant entity.EventParticipant
	err := s.db.WithContext(ctx).Where("event_id = ? AND user_id = ?", eventID, userID).First(&eventParticipant).Error
	return &eventParticipant, err
}

func (s *EventParticipantRepository) Update(ctx context.Context, eventParticipant *entity.EventParticipant) (*entity.EventParticipant, error) {
	err := s.db.WithContext(ctx).Save(&eventParticipant).Error
	return eventParticipant, err
}

func (s *EventParticipantRepository) Delete(ctx context.Context, eventID string, userID int64) error {
	err := s.db.WithContext(ctx).Where("event_id = ? AND user_id = ?", eventID, userID).Delete(&entity.EventParticipant{}).Error
	return err
}

func (s *EventParticipantRepository) GetByEventID(ctx context.Context, eventID string) ([]entity.EventParticipant, error) {
	var eventParticipants []entity.EventParticipant
	err := s.db.WithContext(ctx).Where("event_id = ?", eventID).Find(&eventParticipants).Error
	return eventParticipants, err
}

func (s *EventParticipantRepository) CountByEventID(ctx context.Context, eventID string) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&entity.EventParticipant{}).Where("event_id = ?", eventID).Count(&count).Error
	return count, err
}

func (s *EventParticipantRepository) CountVisitedByEventID(ctx context.Context, eventID string) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&entity.EventParticipant{}).Where("event_id = ? AND (is_event_qr = true OR is_user_qr = true)", eventID).Count(&count).Error
	return count, err
}

// GetUserEvents returns events that user with given id has registered on, with pagination.
// It returns events in the order of start_time (upcoming first, then past).
// If user has registered on more events than limit, it returns only first limit events.
// If user has registered on fewer events than limit, it returns all events.
// If user has registered on no events, it returns empty list.
// If error occurs during the process, it returns error.
func (s *EventParticipantRepository) GetUserEvents(ctx context.Context, userID int64, limit, offset int) ([]dto.UserEvent, error) {
	type eventWithQR struct {
		entity.Event
		IsUserQr  bool
		IsEventQr bool
	}

	var events []eventWithQR
	currentTime := time.Now()

	// Count total upcoming events for this user
	var upcomingCount int64
	if err := s.db.WithContext(ctx).
		Model(&entity.Event{}).
		Joins("JOIN event_participants ON events.id = event_participants.event_id").
		Where("event_participants.user_id = ? AND events.start_time > ?", userID, currentTime).
		Count(&upcomingCount).Error; err != nil {
		return nil, err
	}

	// If offset is within upcoming events, get upcoming events
	if offset < int(upcomingCount) {
		if err := s.db.WithContext(ctx).
			Table("events").
			Select("events.*, event_participants.is_user_qr, event_participants.is_event_qr").
			Joins("JOIN event_participants ON event_participants.event_id = events.id").
			Where("event_participants.user_id = ? AND events.start_time > ?", userID, currentTime).
			Order("events.start_time ASC").
			Limit(limit).
			Offset(offset).
			Find(&events).Error; err != nil {
			return nil, err
		}
	}

	// If we haven't filled the limit, and there might be past events to show
	remainingLimit := limit - len(events)
	if remainingLimit > 0 {
		pastOffset := max(0, offset-int(upcomingCount)) // Adjust offset for past events
		var pastEvents []eventWithQR
		if err := s.db.WithContext(ctx).
			Table("events").
			Select("events.*, event_participants.is_user_qr, event_participants.is_event_qr").
			Joins("JOIN event_participants ON event_participants.event_id = events.id").
			Where("event_participants.user_id = ? AND events.start_time <= ?", userID, currentTime).
			Order("events.start_time DESC").
			Limit(remainingLimit).
			Offset(pastOffset).
			Find(&pastEvents).Error; err != nil {
			return nil, err
		}
		events = append(events, pastEvents...)
	}

	// Convert to DTOs
	result := make([]dto.UserEvent, len(events))
	for i, event := range events {
		result[i] = dto.NewUserEventFromEntity(event.Event, event.IsUserQr || event.IsEventQr)
	}

	return result, nil
}

func (s *EventParticipantRepository) CountUserEvents(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&entity.EventParticipant{}).
		Joins("JOIN events ON event_participants.event_id = events.id").
		Where("event_participants.user_id = ? AND events.deleted_at IS NULL", userID).
		Count(&count).Error
	return count, err
}

// GetVisitedInRange returns all visits (participants marked by user or event QR)
// across all clubs in the given time range, ordered by club name and visit time.
// The visit time is the moment the QR was scanned, stored in updated_at
// (the only update path for participants is QR activation).
func (s *EventParticipantRepository) GetVisitedInRange(ctx context.Context, from, to time.Time) ([]dto.Visit, error) {
	type visitRow struct {
		ClubName  string
		Email     string
		Username  string
		VisitedAt time.Time
	}

	var rows []visitRow
	err := s.db.WithContext(ctx).
		Table("event_participants").
		Select("clubs.name as club_name, users.email as email, users.username as username, event_participants.updated_at as visited_at").
		Joins("JOIN events ON events.id = event_participants.event_id AND events.deleted_at IS NULL").
		Joins("JOIN clubs ON clubs.id = events.club_id").
		Joins("JOIN users ON users.id = event_participants.user_id").
		Where("event_participants.updated_at >= ? AND event_participants.updated_at <= ? AND (event_participants.is_user_qr = true OR event_participants.is_event_qr = true)", from, to).
		Order("club_name ASC, visited_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	visits := make([]dto.Visit, len(rows))
	for i, row := range rows {
		login, _, _ := strings.Cut(row.Email, "@")
		visits[i] = dto.Visit{
			ClubName:  row.ClubName,
			Login:     login,
			Username:  row.Username,
			VisitedAt: row.VisitedAt,
		}
	}

	return visits, nil
}
