package entity

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

type Club struct {
	ID          string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt
	Name        string `gorm:"not null;unique"`
	Description string
	Link        string
	AvatarID    string
	IntroID     string
	ShouldShow  bool `gorm:"default:false"`
	// AllowedRoles - list of roles for which this group can create events
	AllowedRoles pq.StringArray `gorm:"type:text[]"`
	// QrAllowed - true if group can create qr code that can be scanned by users for event registration
	QrAllowed bool
	// SubscriptionRequired - if SubscriptionRequired is true, user should subscribe on club's channel before register
	// on club's event
	SubscriptionRequired bool `gorm:"default:false"`
}
