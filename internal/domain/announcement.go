package domain

import (
	"time"

	"github.com/google/uuid"
)

// Announcement represents a system announcement.
type Announcement struct {
	AnnouncementID uuid.UUID  `json:"announcement_id"`
	Title          string     `json:"title"`
	Content        string     `json:"content"`
	Priority       string     `json:"priority"`    // normal, urgent
	TargetType     string     `json:"target_type"` // all, specific_plan
	TargetPlans    []string   `json:"target_plans"`
	StartAt        time.Time  `json:"start_at"`
	EndAt          *time.Time `json:"end_at"`
	IsActive       bool       `json:"is_active"`
	CreatedAt      time.Time  `json:"created_at"`
}
