package domain

import (
	"time"

	"github.com/google/uuid"
)

// TenantSubscription tracks plan subscriptions.
type TenantSubscription struct {
	SubscriptionID uuid.UUID  `json:"subscription_id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	PlanID         string     `json:"plan_id"`
	StartedAt      time.Time  `json:"started_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Status         string     `json:"status"` // active, expired, cancelled
	CreatedAt      time.Time  `json:"created_at"`
}
