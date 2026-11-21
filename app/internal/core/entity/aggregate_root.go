package entity

type (
	// AggregateRoot represents an Aggregate Entity in the system
	AggregateRoot struct {
		Entity
		domainEvents []DomainEvent
	}

	// AggregateRootParams are the parameters/arguments to create a new aggregate root
	AggregateRootParams struct {
		EntityParams
		DomainEvents []DomainEvent `json:"domainEvents"`
	}
)

// NewAggregateRoot creates a new Aggregate root with a given entity and the events
func NewAggregateRoot(params AggregateRootParams) (AggregateRoot, error) {
	entity, err := NewEntity(params.EntityParams)
	if err != nil {
		return AggregateRoot{}, err
	}

	return AggregateRoot{
		Entity:       entity,
		domainEvents: params.DomainEvents,
	}, nil
}

// ApplyDomain applies a domain event to an aggregate root
func (ar *AggregateRoot) ApplyDomain(e DomainEvent) {
	ar.domainEvents = append(ar.domainEvents, e)
}

// DomainEvents retrieves the domain events
func (ar *AggregateRoot) DomainEvents() []DomainEvent {
	return ar.domainEvents
}
