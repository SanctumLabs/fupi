package entity

import "time"

type (
	DomainEvent interface {
		// Identity returns the identity of the event i.e. the name of the event
		Identity() string

		// CorrelationId returns the correlation id of the event i.e. a unique ID for the event
		CorrelationId() string

		// Destination returns the destination of the event i.e. where it is to be sent
		Destination() string

		// Destinations returns the destinations of the event i.e. where it is to be sent
		Destinations() []string

		// Headers returns the headers of the event i.e. additional information about the event

		Headers() map[string]any

		// CreatedAt returns the created at time of the event
		CreatedAt() time.Time

		Metadata() map[string]any

		// Data returns the data of the event
		Data() any

		Marshaler
	}

	DomainEventParams struct {
		// Additional metadata that is useful for this event
		Metadata map[string]any

		// CorrelationId is the correlation ID of the event i.e. a unique ID for the event
		CorrelationId string

		// Destination is the destination for this event, i.e. where it is to be sent
		Destination string

		// Destinations is the destinations for this event, i.e. which topics/services that will receive this event
		Destinations []string

		CreatedAt time.Time
	}
)
