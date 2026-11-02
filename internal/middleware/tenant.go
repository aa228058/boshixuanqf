package middleware

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// GetTenantID extracts tenant ID from echo context.
func GetTenantID(c echo.Context) (uuid.UUID, error) {
	v := c.Get(TenantContextKey)
	if v == nil {
		return uuid.Nil, echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
	}
	s, ok := v.(string)
	if !ok {
		return uuid.Nil, echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
	}
	return uuid.Parse(s)
}

// TenantIsolation ensures all requests have a valid tenant.
func TenantIsolation() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if _, err := GetTenantID(c); err != nil {
				return err
			}
			return next(c)
		}
	}
}
