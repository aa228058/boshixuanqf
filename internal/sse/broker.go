package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"tg-sender/internal/config"
	"tg-sender/internal/middleware"
)

// Broker manages SSE clients and broadcasts events.
type Broker struct {
	cfg     *config.Config
	mu      sync.RWMutex
	clients map[string]map[uuid.UUID]chan Event // topic -> clientID -> channel
	closed  bool
	done    chan struct{}
}

// Event represents an SSE event.
type Event struct {
	Topic   string `json:"topic"`
	Payload any    `json:"payload"`
}

// NewBroker creates a new SSE broker.
func NewBroker(cfg *config.Config) *Broker {
	return &Broker{
		cfg:     cfg,
		clients: make(map[string]map[uuid.UUID]chan Event),
		done:    make(chan struct{}),
	}
}

// Subscribe adds a client to a topic.
func (b *Broker) Subscribe(topic string) (uuid.UUID, chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := uuid.New()
	ch := make(chan Event, b.cfg.SSEChannelBuffer)

	if b.clients[topic] == nil {
		b.clients[topic] = make(map[uuid.UUID]chan Event)
	}
	b.clients[topic][id] = ch
	return id, ch
}

// Unsubscribe removes a client from a topic.
// Does NOT close the channel to avoid panics from concurrent Broadcast sends.
func (b *Broker) Unsubscribe(topic string, id uuid.UUID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if clients, ok := b.clients[topic]; ok {
		delete(clients, id)
		if len(clients) == 0 {
			delete(b.clients, topic)
		}
	}
}

// Close gracefully shuts down the broker, closing all client channels.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	close(b.done)

	for topic, clients := range b.clients {
		for id, ch := range clients {
			close(ch)
			delete(clients, id)
		}
		delete(b.clients, topic)
	}
}

// Broadcast sends an event to all clients of a topic.
// Non-blocking: if a client's channel is full, the event is dropped.
// Uses recover() to guard against sends to closed channels during shutdown.
func (b *Broker) Broadcast(topic string, payload any) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}
	clients := make(map[uuid.UUID]chan Event)
	for id, ch := range b.clients[topic] {
		clients[id] = ch
	}
	b.mu.RUnlock()

	event := Event{Topic: topic, Payload: payload}
	dropped := 0
	for _, ch := range clients {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Channel was closed during shutdown, ignore
				}
			}()
			select {
			case ch <- event:
			default:
				// Channel full, drop event to avoid blocking
				dropped++
			}
		}()
	}
	if dropped > 0 {
		// Log dropped events at debug level (avoid flooding logs)
		// dropped message, available for metrics if needed
	}
}

// BroadcastToTenant sends an event to a specific tenant's topic.
// Also broadcasts to the tenant's unified "events" topic so SSE clients
// subscribed via Handler("events") receive all events.
func (b *Broker) BroadcastToTenant(tenantID uuid.UUID, eventType string, payload any) {
	topic := fmt.Sprintf("tenant:%s:%s", tenantID.String(), eventType)
	unifiedTopic := fmt.Sprintf("tenant:%s:events", tenantID.String())
	if topic == unifiedTopic {
		// eventType 本身就是 "events"（如进度推送）：两个 topic 相同，
		// 只发包装格式一份，避免同一 topic 收到裸 payload + 包装 payload 双份事件。
		b.Broadcast(unifiedTopic, map[string]any{
			"type":    eventType,
			"payload": payload,
		})
		return
	}
	b.Broadcast(topic, payload)
	// Also broadcast to unified events topic for SSE clients
	b.Broadcast(unifiedTopic, map[string]any{
		"type":    eventType,
		"payload": payload,
	})
}

// globalTopic is the broadcast topic every SSE client receives regardless of
// tenant. Used for system-wide alerts (resource pressure, adaptive mode).
const globalTopic = "events"

// BroadcastGlobal publishes a wrapped event to the global topic so ALL
// connected SSE clients receive it. Only for system-wide alerts; per-tenant
// events must use BroadcastToTenant to preserve tenant isolation.
func (b *Broker) BroadcastGlobal(eventType string, payload any) {
	b.Broadcast(globalTopic, map[string]any{
		"type":    eventType,
		"payload": payload,
	})
}

// Publish sends an event to a specific task topic.
func (b *Broker) Publish(taskID uuid.UUID, payload any) {
	topic := fmt.Sprintf("task:%s", taskID.String())
	b.Broadcast(topic, payload)
}

// Handler returns an echo handler for SSE connections with tenant isolation.
// Supports token via query parameter (for EventSource which cannot set custom headers).
func (b *Broker) Handler(topic string) echo.HandlerFunc {
	return func(c echo.Context) error {
		var tenantID uuid.UUID

		// Try to extract tenant ID from JWT context first (set by JWTAuth middleware)
		var err error
		tenantID, err = middleware.GetTenantID(c)
		if err != nil {
			// Fallback: parse token from query parameter for EventSource
			tokenStr := c.QueryParam("token")
			if tokenStr == "" {
				return c.NoContent(http.StatusUnauthorized)
			}
			token, err := jwt.ParseWithClaims(tokenStr, &middleware.JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
				return []byte(b.cfg.JWTSecret), nil
			}, jwt.WithValidMethods([]string{"HS256"}),
			jwt.WithIssuer(middleware.TokenIssuer), jwt.WithAudience(middleware.TokenAudience))
			if err != nil || !token.Valid {
				return c.NoContent(http.StatusUnauthorized)
			}
			claims, ok := token.Claims.(*middleware.JWTClaims)
			if !ok || claims.TokenType == "refresh" {
				return c.NoContent(http.StatusUnauthorized)
			}
			parsedID, err := uuid.Parse(claims.TenantID)
			if err != nil {
				return c.NoContent(http.StatusUnauthorized)
			}
			tenantID = parsedID
			c.Set(middleware.TenantContextKey, claims.TenantID)
			// Reject revoked tokens even when passed via query param (memory-only check).
			if claims.ID != "" && middleware.IsTokenRevoked(nil, claims.ID) {
				return c.NoContent(http.StatusUnauthorized)
			}
		}

		// Tenant-isolated topic
		isolatedTopic := fmt.Sprintf("tenant:%s:%s", tenantID.String(), topic)

		c.Response().Header().Set("Content-Type", "text/event-stream")
		c.Response().Header().Set("Cache-Control", "no-cache")
		c.Response().Header().Set("Connection", "keep-alive")
		c.Response().Header().Set("X-Accel-Buffering", "no")
		c.Response().WriteHeader(200)

		// Subscribe to tenant-isolated topic AND global topic, so
		// system-wide alerts (resource_alert / adaptive_mode) reach every client.
		idIso, chIso := b.Subscribe(isolatedTopic)
		defer b.Unsubscribe(isolatedTopic, idIso)
		idGlob, chGlob := b.Subscribe(globalTopic)
		defer b.Unsubscribe(globalTopic, idGlob)

		flusher, ok := c.Response().Writer.(interface{ Flush() })
		if !ok {
			return fmt.Errorf("streaming unsupported")
		}

		writeEvent := func(event Event) {
			data, _ := json.Marshal(event.Payload)
			// Escape newlines in payload to comply with SSE protocol:
			// multi-line data must be split into multiple "data: " fields
			payloadStr := strings.ReplaceAll(string(data), "\n", "\ndata: ")
			fmt.Fprintf(c.Response(), "event: %s\ndata: %s\n\n", event.Topic, payloadStr)
			flusher.Flush()
		}

		// Send initial connection event
		fmt.Fprintf(c.Response(), "event: connected\ndata: %s\n\n", `{"status":"connected"}`)
		flusher.Flush()

		// Heartbeat ticker to keep connection alive (Nginx defaults to 60s timeout)
		heartbeat := time.NewTicker(b.cfg.SSEHeartbeat)
		defer heartbeat.Stop()

		for {
			select {
			case event, ok := <-chIso:
				if !ok {
					return nil
				}
				writeEvent(event)
			case event, ok := <-chGlob:
				if !ok {
					return nil
				}
				writeEvent(event)
			case <-heartbeat.C:
				// SSE comment heartbeat (ignored by EventSource but keeps TCP alive)
				fmt.Fprint(c.Response(), ":heartbeat\n\n")
				flusher.Flush()
			case <-c.Request().Context().Done():
				return nil
			}
		}
	}
}
