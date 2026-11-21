package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sanctumlabs/fupi/app/config"
	scandatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/scan"
	urldatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/url"
	redisadapter "github.com/sanctumlabs/fupi/app/internal/adapters/redis"
	"github.com/sanctumlabs/fupi/app/internal/adapters/safebrowsing"
	securityapp "github.com/sanctumlabs/fupi/app/internal/application/security"
	urlapp "github.com/sanctumlabs/fupi/app/internal/application/url"
	"github.com/sanctumlabs/fupi/app/pkg/infra/cache"
	cacheredis "github.com/sanctumlabs/fupi/app/pkg/infra/cache/redis"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
	"github.com/sanctumlabs/fupi/app/pkg/infra/queue/kafka"
	recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
)

const (
	// urlEventsTopic is where the URL context's events are relayed to (the outbox destination of the url datastore).
	urlEventsTopic = "url.events"
	// scannerGroup is the consumer group of the url scanners: its members share the partitions of the topic.
	scannerGroup = "fupi-url-scanner"

	// redisStartupPingTimeout bounds the one ping that only decides which startup line is logged.
	redisStartupPingTimeout = 2 * time.Second
)

// scanning is the url scanner of the worker: a consumer of url.created and what it needs to suspend a flagged url.
type scanning struct {
	consumer *kafka.Consumer
	handler  kafka.Handler
	close    func()
}

// newScanning builds the scanner from cfg, or returns nil when no scanner is configured. Redis is optional here as it is
// in the API (ADR-0015): if it is down, the redirect cache is not evicted and its entries live until their TTL.
func newScanning(ctx context.Context, cfg config.Worker, dbClient database.PostgresDatabaseClient, registry *health.Registry) (*scanning, error) {
	if !cfg.Security.ScanningEnabled() {
		slog.Info("url scanning is disabled: SAFE_BROWSING_API_KEY is not set, created urls are not scanned")
		return nil, nil
	}

	redisClient, err := cacheredis.NewRedisClient(cfg.Redis)
	if err != nil {
		return nil, fmt.Errorf("create redis client: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, redisStartupPingTimeout)
	if pingErr := redisClient.Ping(pingCtx); pingErr != nil {
		slog.Warn("redis is down, continuing without it", "addresses", cfg.Redis.Address, "error", pingErr)
	} else {
		slog.Info("redis is up", "addresses", cfg.Redis.Address)
	}
	cancel()
	registry.Add(health.Check{Name: "redis", Required: false, Fn: redisClient.Ping})

	urlService := urlapp.NewService(
		urldatastore.NewUrlDatastoreAdapter(dbClient, database.Config{
			OperationTimeout: cfg.Database.OperationTimeout,
			RetryConfig:      recoveryutils.DefaultRetryConfig,
		}),
		nil, // only Shorten generates short codes
		urlapp.WithRedirectCache(redisadapter.NewRedirectCache(redisClient), cfg.RedirectCache.TTL, cfg.RedirectCache.AbsentTTL),
	)
	service := securityapp.NewService(
		safebrowsing.NewScanner(cfg.Security.SafeBrowsingAPIKey),
		scandatastore.NewAdapter(dbClient, cfg.Database.OperationTimeout),
		urlService,
	)

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:  cfg.Kafka.Brokers,
		ClientID: cfg.Kafka.ClientID,
		Group:    scannerGroup,
		Topics:   []string{urlEventsTopic},
	})
	if err != nil {
		closeRedis(redisClient)
		return nil, fmt.Errorf("create the kafka consumer: %w", err)
	}
	slog.Info("url scanning is enabled", "provider", safebrowsing.Provider, "topic", urlEventsTopic, "group", scannerGroup)

	return &scanning{
		consumer: consumer,
		handler:  scanHandler(service),
		close: func() {
			consumer.Close()
			closeRedis(redisClient)
		},
	}, nil
}

func closeRedis(client cache.CacheClient) {
	if err := client.Close(); err != nil {
		slog.Warn("closing redis", "error", err)
	}
}

// scanHandler maps a record the outbox relay published onto a Security event: the relay forwards the outbox headers, so
// the event's id and type are headers, and the value is the domain event as JSON.
func scanHandler(service *securityapp.Service) kafka.Handler {
	return func(ctx context.Context, delivery kafka.Delivery) error {
		return service.Handle(ctx, securityapp.Event{
			ID:      delivery.Header("event_id"),
			Type:    delivery.Header("event_type"),
			Payload: delivery.Value,
		})
	}
}
