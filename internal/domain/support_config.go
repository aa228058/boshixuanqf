package domain

import "time"

// SupportConfig holds customer service contact information.
type SupportConfig struct {
	ID           int       `json:"id"`
	ContactType  string    `json:"contact_type"`
	ContactValue string    `json:"contact_value"`
	DisplayName  string    `json:"display_name"`
	QRCodeURL    *string   `json:"qr_code_url"`
	IsActive     bool      `json:"is_active"`
	UpdatedAt    time.Time `json:"updated_at"`
}
