package urlapp

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/domain/url"
	"github.com/sanctumlabs/fupi/app/internal/ports"
)

const (
	deviceMobile  = "mobile"
	deviceDesktop = "desktop"
	deviceBot     = "bot"

	accessPublishTimeout = 5 * time.Second
	// dropLogEvery keeps an overload from flooding the log: one line per this many dropped events.
	dropLogEvery = 1000
)

var (
	botMarkers    = []string{"bot", "crawler", "spider", "slurp", "curl", "wget", "python-requests", "headless", "preview"}
	mobileMarkers = []string{"mobile", "android", "iphone", "ipad"}
)

// ClassifyDevice says whether a user agent belongs to a bot, a mobile device or a desktop. Bots are classified rather
// than dropped, so analytics can tell them apart. An empty or unrecognised agent counts as desktop.
func ClassifyDevice(userAgent string) string {
	agent := strings.ToLower(userAgent)
	for _, marker := range botMarkers {
		if strings.Contains(agent, marker) {
			return deviceBot
		}
	}
	for _, marker := range mobileMarkers {
		if strings.Contains(agent, marker) {
			return deviceMobile
		}
	}
	return deviceDesktop
}

// Access is one visit to a short url, as the HTTP layer saw it.
type Access struct {
	ShortCode string
	// IP is used to resolve the country and goes no further.
	IP        string
	UserAgent string
	Referer   string
}

// accessedPayload is the JSON of a published URLAccessed. It has no IP address.
type accessedPayload struct {
	EventID     string    `json:"event_id"`
	EventType   string    `json:"event_type"`
	OccurredAt  time.Time `json:"occurred_at"`
	ShortCode   string    `json:"short_code"`
	CountryCode string    `json:"country_code"`
	DeviceType  string    `json:"device_type"`
	UserAgent   string    `json:"user_agent"`
	Referer     string    `json:"referer"`
}

// AccessRecorder publishes URLAccessed events best-effort and off the request path: Record queues the visit on a
// buffered channel and returns at once, and one goroutine resolves the country, drops the IP and publishes. When the
// buffer is full the visit is dropped. It deliberately bypasses the transactional outbox (ADR-0011): a redirect must not
// wait for, or write to, the database to report itself, and losing a few analytics events is acceptable.
type AccessRecorder struct {
	geo       ports.GeoLocator
	publisher ports.EventPublisher
	topic     string
	queue     chan Access
	done      chan struct{}
	finished  sync.WaitGroup
	closeOnce sync.Once
	dropped   atomic.Int64
}

// NewAccessRecorder starts a recorder that buffers up to buffer visits and publishes them to topic.
func NewAccessRecorder(geo ports.GeoLocator, publisher ports.EventPublisher, topic string, buffer int) *AccessRecorder {
	r := &AccessRecorder{
		geo:       geo,
		publisher: publisher,
		topic:     topic,
		queue:     make(chan Access, buffer),
		done:      make(chan struct{}),
	}
	r.finished.Add(1)
	go r.run()
	return r
}

// Record queues a visit without blocking. A nil recorder ignores it.
func (r *AccessRecorder) Record(access Access) {
	if r == nil {
		return
	}
	select {
	case r.queue <- access:
	default:
		if n := r.dropped.Add(1); n%dropLogEvery == 1 {
			slog.Warn("access analytics buffer is full, dropping events", "dropped_total", n)
		}
	}
}

// Dropped is how many visits were dropped because the buffer was full.
func (r *AccessRecorder) Dropped() int64 { return r.dropped.Load() }

// Close publishes what is still queued and stops the recorder.
func (r *AccessRecorder) Close() {
	r.closeOnce.Do(func() { close(r.done) })
	r.finished.Wait()
}

func (r *AccessRecorder) run() {
	defer r.finished.Done()
	for {
		select {
		case access := <-r.queue:
			r.publish(access)
		case <-r.done:
			for {
				select {
				case access := <-r.queue:
					r.publish(access)
				default:
					return
				}
			}
		}
	}
}

func (r *AccessRecorder) publish(access Access) {
	ctx, cancel := context.WithTimeout(context.Background(), accessPublishTimeout)
	defer cancel()

	country, err := r.geo.Country(ctx, access.IP)
	if err != nil {
		slog.WarnContext(ctx, "resolving the visitor's country failed", "error", err)
		country = ""
	}

	// The IP is not copied into the event: it ends here.
	event := url.NewURLAccessed(access.ShortCode, access.UserAgent, access.Referer, country, ClassifyDevice(access.UserAgent))
	value, err := json.Marshal(accessedPayload{
		EventID:     event.ID(),
		EventType:   event.EventType(),
		OccurredAt:  event.OccurredAt(),
		ShortCode:   event.ShortCode,
		CountryCode: event.CountryCode,
		DeviceType:  event.DeviceType,
		UserAgent:   event.UserAgent,
		Referer:     event.Referer,
	})
	if err != nil {
		slog.ErrorContext(ctx, "encoding URLAccessed failed", "error", err)
		return
	}

	results := r.publisher.Publish(ctx, []ports.Message{{
		Topic: r.topic,
		Key:   []byte(access.ShortCode),
		Value: value,
		Headers: []ports.Header{
			{Key: "content-type", Value: "application/json"},
			{Key: "event_id", Value: event.ID()},
			{Key: "event_type", Value: event.EventType()},
		},
	}})
	if len(results) > 0 && results[0].Err != nil {
		slog.WarnContext(ctx, "publishing URLAccessed failed, dropping it", "error", results[0].Err)
	}
}
