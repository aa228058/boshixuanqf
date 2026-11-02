package domain

import (
	"time"

	"github.com/google/uuid"
)

// Admin represents a system administrator.
type Admin struct {
	AdminID      uuid.UUID `json:"admin_id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"` // admin, super
	CreatedAt    time.Time `json:"created_at"`
}
