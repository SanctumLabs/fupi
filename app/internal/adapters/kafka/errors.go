package kafkaadapter

import "errors"

// errNoResult marks a message the producer returned no answer for; it is treated as a transient failure.
var errNoResult = errors.New("kafka: no result for the message")
