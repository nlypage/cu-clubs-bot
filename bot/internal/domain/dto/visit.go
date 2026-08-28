package dto

import (
	"time"
)

// Visit represents a single attendance record (QR scan) for admin reports
type Visit struct {
	ClubName  string
	Login     string    // local part of the email (e.g. v.trunov), empty if the user has no email
	Username  string    // telegram username
	VisitedAt time.Time // time of the QR scan
}
