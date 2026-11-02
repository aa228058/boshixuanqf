package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

type MessageTemplateRepository struct {
	db *pgxpool.Pool
}

func NewMessageTemplateRepository(db *pgxpool.Pool) *MessageTemplateRepository {
	return &MessageTemplateRepository{db: db}
}

func (r *MessageTemplateRepository) List(ctx context.Context, tenantID uuid.UUID) ([]*domain.MessageTemplate, error) {
	rows, err := r.db.Query(ctx, `
		SELECT template_id, tenant_id, name, content, variables, created_at
		FROM message_templates WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()
	var list []*domain.MessageTemplate
	for rows.Next() {
		var t domain.MessageTemplate
		if err := rows.Scan(&t.TemplateID, &t.TenantID, &t.Name, &t.Content, &t.Variables, &t.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, &t)
	}
	return list, rows.Err()
}

func (r *MessageTemplateRepository) Create(ctx context.Context, t *domain.MessageTemplate) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO message_templates (template_id, tenant_id, name, content, variables)
		VALUES ($1, $2, $3, $4, $5)`,
		t.TemplateID, t.TenantID, t.Name, t.Content, t.Variables)
	return err
}

func (r *MessageTemplateRepository) Update(ctx context.Context, tenantID, templateID uuid.UUID, name, content string, variables []string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE message_templates SET name = $3, content = $4, variables = $5
		WHERE template_id = $1 AND tenant_id = $2`,
		templateID, tenantID, name, content, variables)
	return err
}

func (r *MessageTemplateRepository) Delete(ctx context.Context, tenantID, templateID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM message_templates WHERE template_id = $1 AND tenant_id = $2`, templateID, tenantID)
	return err
}
