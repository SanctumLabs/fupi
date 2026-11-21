package test

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// KafkaBroker is a single-node Kafka (KRaft) running in a container, set up like the local stack's kafka-single: topics are
// not created automatically. It listens on a host port that stays the same when the broker is stopped and started again.
type KafkaBroker struct {
	// Brokers is the address to give a client.
	Brokers   []string
	container testcontainers.Container
}

// StartKafka starts a broker with the apache/kafka image the local stack uses and removes it when the test ends.
func StartKafka(t *testing.T) *KafkaBroker {
	t.Helper()
	ctx := context.Background()

	// The advertised address must be known before the broker starts, so a free host port is chosen first and bound to the same port inside.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port for kafka: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	hostPort := fmt.Sprintf("%d", port)

	req := testcontainers.ContainerRequest{
		Image:        TEST_KAFKA_IMAGE,
		ExposedPorts: []string{hostPort + "/tcp"},
		Env: map[string]string{
			"CLUSTER_ID":                                     "MkU3OEVBNTcwNTJENDM2Qk",
			"KAFKA_NODE_ID":                                  "1",
			"KAFKA_PROCESS_ROLES":                            "broker,controller",
			"KAFKA_CONTROLLER_QUORUM_VOTERS":                 "1@localhost:9093",
			"KAFKA_LISTENERS":                                "INTERNAL://:9092,CONTROLLER://:9093,EXTERNAL://:" + hostPort,
			"KAFKA_ADVERTISED_LISTENERS":                     "INTERNAL://localhost:9092,EXTERNAL://127.0.0.1:" + hostPort,
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "INTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT,EXTERNAL:PLAINTEXT",
			"KAFKA_INTER_BROKER_LISTENER_NAME":               "INTERNAL",
			"KAFKA_CONTROLLER_LISTENER_NAMES":                "CONTROLLER",
			"KAFKA_AUTO_CREATE_TOPICS_ENABLE":                "false",
			"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":         "0",
			"KAFKA_NUM_PARTITIONS":                           "3",
			"KAFKA_DEFAULT_REPLICATION_FACTOR":               "1",
			"KAFKA_MIN_INSYNC_REPLICAS":                      "1",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
			"KAFKA_HEAP_OPTS":                                "-Xms256m -Xmx256m",
		},
		// The host port is the same as the one inside, so the advertised address works from the host.
		HostConfigModifier: func(hostConfig *container.HostConfig) {
			hostConfig.PortBindings = network.PortMap{
				network.MustParsePort(hostPort + "/tcp"): []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: hostPort}},
			}
		},
		WaitingFor: wait.ForLog("Kafka Server started").WithStartupTimeout(containerReadyTimeout()),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Fatalf("start kafka: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	return &KafkaBroker{Brokers: []string{"127.0.0.1:" + hostPort}, container: c}
}

// Stop stops the broker (a clean shutdown).
func (k *KafkaBroker) Stop(t *testing.T) {
	t.Helper()
	timeout := 30 * time.Second
	if err := k.container.Stop(context.Background(), &timeout); err != nil {
		t.Fatalf("stop kafka: %v", err)
	}
}

// Start starts the stopped broker again on the same address and waits until it serves its partitions. A broker answers
// a ping long before every partition has a leader again (under load that gap is several seconds), and a test that
// produces in that gap fails on a timeout that has nothing to do with what it is testing.
func (k *KafkaBroker) Start(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := k.container.Start(ctx); err != nil {
		t.Fatalf("restart kafka: %v", err)
	}
	client, err := kgo.NewClient(kgo.SeedBrokers(k.Brokers...))
	if err != nil {
		t.Fatalf("connect to kafka: %v", err)
	}
	defer client.Close()
	deadline := time.Now().Add(containerReadyTimeout())
	for {
		requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		pending, err := partitionsWithoutLeader(requestCtx, client)
		cancel()
		if err == nil && pending == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("kafka did not serve its partitions after a restart (%d without a leader): %v", pending, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// partitionsWithoutLeader asks for the metadata of every topic and counts the partitions that have no leader yet.
func partitionsWithoutLeader(ctx context.Context, client *kgo.Client) (int, error) {
	response, err := kmsg.NewPtrMetadataRequest().RequestWith(ctx, client)
	if err != nil {
		return 0, err
	}
	pending := 0
	for _, topic := range response.Topics {
		if topic.ErrorCode != 0 {
			pending++
			continue
		}
		for _, partition := range topic.Partitions {
			if partition.ErrorCode != 0 || partition.Leader < 0 {
				pending++
			}
		}
	}
	return pending, nil
}

// CreateTopic creates a topic with the given number of partitions.
func (k *KafkaBroker) CreateTopic(t *testing.T, name string, partitions int) {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(k.Brokers...))
	if err != nil {
		t.Fatalf("connect to kafka: %v", err)
	}
	defer client.Close()

	topic := kmsg.NewCreateTopicsRequestTopic()
	topic.Topic = name
	topic.NumPartitions = int32(partitions)
	topic.ReplicationFactor = 1
	request := kmsg.NewPtrCreateTopicsRequest()
	request.Topics = append(request.Topics, topic)

	response, err := request.RequestWith(context.Background(), client)
	if err != nil {
		t.Fatalf("create topic %s: %v", name, err)
	}
	for _, result := range response.Topics {
		if result.ErrorCode != 0 {
			t.Fatalf("create topic %s: error code %d: %v", name, result.ErrorCode, result.ErrorMessage)
		}
	}
}

// Consume reads the topic from its start with a fresh client and returns the first n records, failing the test if they
// do not arrive within the timeout. Records are returned in the order of arrival at the client, which for one partition is
// the order in the log.
func (k *KafkaBroker) Consume(t *testing.T, topic string, n int, timeout time.Duration) []*kgo.Record {
	t.Helper()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(k.Brokers...),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		t.Fatalf("connect a consumer: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var records []*kgo.Record
	for len(records) < n {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil {
			t.Fatalf("read %d record(s) from %s: got %d before the timeout", n, topic, len(records))
		}
		fetches.EachRecord(func(r *kgo.Record) { records = append(records, r) })
	}
	return records
}
