package urlapp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeGeo struct{ country string }

func (g fakeGeo) Country(context.Context, string) (string, error) { return g.country, nil }

// fakePublisher records what it is given; when block is set, Publish waits on it.
type fakePublisher struct {
	mu       sync.Mutex
	messages []ports.Message
	block    chan struct{}
}

func (p *fakePublisher) Publish(_ context.Context, messages []ports.Message) []ports.PublishResult {
	if p.block != nil {
		<-p.block
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, messages...)
	return make([]ports.PublishResult, len(messages))
}
func (p *fakePublisher) Ping(context.Context) error { return nil }
func (p *fakePublisher) Close()                     {}

func (p *fakePublisher) published() []ports.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ports.Message(nil), p.messages...)
}

func TestClassifyDevice(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit Mobile/15E148": "mobile",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit Chrome Mobile Safari":        "mobile",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit Chrome Safari":              "desktop",
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":         "bot",
		"curl/8.4.0": "bot",
		"":           "desktop",
	}
	for userAgent, want := range cases {
		assert.Equal(t, want, ClassifyDevice(userAgent), userAgent)
	}
}

func TestRecordPublishesURLAccessedWithoutTheRawIP(t *testing.T) {
	publisher := &fakePublisher{}
	recorder := NewAccessRecorder(fakeGeo{country: "KE"}, publisher, "url.accessed", 8)

	recorder.Record(Access{ShortCode: "abc123", IP: "203.0.113.77", UserAgent: "Googlebot/2.1", Referer: "https://example.com"})
	recorder.Close()

	messages := publisher.published()
	require.Len(t, messages, 1)
	assert.Equal(t, "url.accessed", messages[0].Topic)
	assert.NotContains(t, string(messages[0].Value), "203.0.113.77", "no raw IP may leave the process")
	for _, header := range messages[0].Headers {
		assert.NotContains(t, header.Value, "203.0.113.77")
	}

	var payload map[string]any
	require.NoError(t, json.Unmarshal(messages[0].Value, &payload))
	assert.Equal(t, "abc123", payload["short_code"])
	assert.Equal(t, "KE", payload["country_code"])
	assert.Equal(t, "bot", payload["device_type"], "bots are classified, not dropped")
	assert.Equal(t, "https://example.com", payload["referer"])
	assert.Equal(t, "url.accessed", payload["event_type"])
	assert.NotEmpty(t, payload["event_id"])
}

func TestRecordDropsWhenTheBufferIsFullAndNeverBlocks(t *testing.T) {
	publisher := &fakePublisher{block: make(chan struct{})}
	recorder := NewAccessRecorder(fakeGeo{}, publisher, "url.accessed", 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			recorder.Record(Access{ShortCode: "abc123"})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Record blocked the caller while the publisher was stuck")
	}

	assert.Positive(t, recorder.Dropped())
	close(publisher.block)
	recorder.Close()
}

func TestRecordOnANilRecorderIsANoop(t *testing.T) {
	var recorder *AccessRecorder
	assert.NotPanics(t, func() { recorder.Record(Access{ShortCode: "abc123"}) })
}
