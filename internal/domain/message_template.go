package domain

import (
	"time"

	"github.com/google/uuid"
)

// MessageTemplate represents a reusable message template.
type MessageTemplate struct {
	TemplateID uuid.UUID `json:"template_id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	Name       string    `json:"name"`
	Content    string    `json:"content"`
	Variables  []string  `json:"variables"`
	CreatedAt  time.Time `json:"created_at"`
}
