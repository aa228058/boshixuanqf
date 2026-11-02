package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// RequireRole restricts an admin endpoint to the given roles. It must be
// applied AFTER AdminAuth, which sets the "admin_role" context value from the
// JWT claims. F-06: the admin plane previously had no role gradient at all.
func RequireRole(roles ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role, _ := c.Get("admin_role").(string)
			for _, r := range roles {
				if role == r {
					return next(c)
				}
			}
			return echo.NewHTTPError(http.StatusForbidden, "权限不足: 该操作仅限超级管理员")
		}
	}
}
