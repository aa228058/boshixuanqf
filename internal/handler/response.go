package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// Response represents a unified API response.
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ListResponse represents a paginated list response.
type ListResponse struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	Data     any    `json:"data"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// Success returns a success response with data.
func Success(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "success",
		Data:    data,
	})
}

// SuccessList returns a paginated list response.
func SuccessList(c echo.Context, data any, total, page, pageSize int) error {
	return c.JSON(http.StatusOK, ListResponse{
		Code:     200,
		Message:  "success",
		Data:     data,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// Created returns a created response.
func Created(c echo.Context, data any) error {
	return c.JSON(http.StatusCreated, Response{
		Code:    201,
		Message: "created",
		Data:    data,
	})
}

// NoContent returns a 204 no content response.
func NoContent(c echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}

// NewError returns a unified error response.
// P2-6 审计清理: NewError 死代码已删——全项目无调用点, 且语义有争议(map 塞 message)。