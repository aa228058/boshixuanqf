package domain

import (
	"time"

	"github.com/google/uuid"
)

// AccountGroup represents a user-defined group of accounts.
type AccountGroup struct {
	GroupID    uuid.UUID   `json:"group_id"`
	TenantID   uuid.UUID   `json:"tenant_id"`
	Name       string      `json:"name"`
	Color      string      `json:"color"`
	AccountIDs []uuid.UUID `json:"account_ids"`
	CreatedAt  time.Time   `json:"created_at"`
}
