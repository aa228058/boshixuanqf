package domain

import (
	"time"

	"github.com/google/uuid"
)

// ActivationCode represents a redeemable code for plan upgrades.
type ActivationCode struct {
	CodeID     uuid.UUID  `json:"code_id"`
	Code       string     `json:"code"`
	PlanID     string     `json:"plan_id"`
	Status     string     `json:"status"` // unused, used, expired, revoked
	UsedBy     *uuid.UUID `json:"used_by"`
	UsedAt     *time.Time `json:"used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	TimingType string     `json:"timing_type"` // immediate | on_use
	CreatedBy  *uuid.UUID `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	Notes      string     `json:"notes"`
	// UsedByTenantName is populated via LEFT JOIN with tenants table (not stored).
	UsedByTenantName string `json:"used_by_tenant_name" db:"-"`
}
