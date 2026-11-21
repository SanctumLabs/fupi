package prometheusmetrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotalCounterVec = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint"},
	)

	ResponseDurationHistogramVec = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_response_duration_seconds",
			Help:    "Histogram of response durations for HTTP requests",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	BidsCreatedCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bids_created",
			Help: "The total number of bids created",
		},
		[]string{"bid_type"},
	)

	IndividualBidCreationDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "bid_creation_duration_seconds",
			Help: "Bid creation duration in seconds.",
		},
		[]string{"bid_type"},
	)

	SendLeadRequestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "send_lead_request_duration_seconds",
			Help: "Send Lead request duration in seconds.",
		},
		[]string{"bid_type", "listing_id"},
	)

	SendNotificationRequestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "send_notification_request_duration_seconds",
			Help: "Send Notification request duration in seconds.",
		},
		[]string{"notification_type", "channel"},
	)

	SendBidPaymentRequestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "send_bid_payment_request_duration_seconds",
			Help: "Send Bid Payment request duration in seconds.",
		},
		[]string{"payment_type"},
	)

	RetrieveBidPaymentRequestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "retrieve_bid_payment_request_duration_seconds",
			Help: "Retrieve Bid Payment request duration in seconds.",
		},
		[]string{},
	)

	GetUserByIdRequestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "get_user_by_id_request_duration_seconds",
			Help: "Get User By ID request duration in seconds.",
		},
		[]string{},
	)

	GetListingInfoByIdRequestDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "get_listing_by_id_request_duration_seconds",
			Help: "Get Listing By ID request duration in seconds.",
		},
		[]string{},
	)

	IndividualOutboxEventDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "outbox_event_processing_duration_seconds",
			Help: "Outbox Event processing duration in seconds.",
		},
		[]string{"destination", "event_type", "group_id"},
	)

	OutboxEventBatchDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "outbox_event_batch_processing_duration_seconds",
			Help: "Outbox Event Batch processing duration in seconds.",
		},
		[]string{"destination", "event_type", "group_id"},
	)

	OutboxEventJobDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "outbox_event_total_duration_seconds",
			Help: "Outbox Event total duration in seconds.",
		},
		[]string{"destination", "event_type", "group_id"},
	)

	OutboxEventProcessedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "outbox_event_processed",
			Help: "Outbox Event Processed",
		},
		[]string{"destination", "event_type", "group_id"},
	)

	OutboxEventBatchCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "outbox_event_batch_count",
			Help: "Outbox Event Batch count",
		},
		[]string{"destination", "event_type", "group_id"},
	)
)
