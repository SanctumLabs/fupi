package outboxdatastore

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sanctumlabs/fupi/app/internal/ports"
)

// DefaultLeaseIdleTimeout is how long the server lets the lease connection sit idle before it drops it. A leader checks
// its lease on every cycle, which takes far less than this, so a healthy holder is never idle that long.
const DefaultLeaseIdleTimeout = 60 * time.Second

var _ ports.Leaser = (*Leaser)(nil)

// Leaser hands out a named leader lease: a session-level advisory lock on a dedicated connection. Each name is a
// separate lock, so the outbox relay and the URL expiry job elect their leaders independently.
type Leaser struct {
	key int64
	// config opens the dedicated connection the lease lives on. It is parsed through the pool's parser so the
	// pool_* parameters of the connection string are removed; a plain connection would send them to the server.
	config  *pgx.ConnConfig
	timeout time.Duration
}

// NewLeaser builds a Leaser for the lock called name. connString is the one the pool was built from, applicationName
// tells the lease connection apart in pg_stat_activity and timeout bounds every statement of the lease.
func NewLeaser(connString, name, applicationName string, timeout, idleTimeout time.Duration) (*Leaser, error) {
	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse the connection string for the %s lease: %w", name, err)
	}
	// The lease connection is housekeeping and must not create a span per statement.
	poolConfig.ConnConfig.Tracer = nil

	// A holder that vanishes without closing its connection (a dead host, a partition) leaves a backend that never notices,
	// and the advisory lock would stay held for as long as the operating system or a proxy keeps the idle socket (hours),
	// with no standby able to take over. Asking the server to drop a session that sits idle bounds that to the timeout.
	// A holder that is alive checks its lease on every cycle, so it is never idle that long; one that is merely slow loses
	// the lease, which is safe: it notices on its next check and asks for it again. Needs PostgreSQL 14 or newer.
	runtimeParams := make(map[string]string, len(poolConfig.ConnConfig.RuntimeParams)+2)
	for key, value := range poolConfig.ConnConfig.RuntimeParams {
		runtimeParams[key] = value
	}
	runtimeParams["application_name"] = applicationName
	runtimeParams["idle_session_timeout"] = strconv.FormatInt(idleTimeout.Milliseconds(), 10)
	poolConfig.ConnConfig.RuntimeParams = runtimeParams

	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return &Leaser{key: int64(h.Sum64()), config: poolConfig.ConnConfig, timeout: timeout}, nil
}

// Acquire takes the lease, or returns ports.ErrNotLeader when another instance holds it. The lock disappears with
// the connection, so a crashed instance, or a failover to a new primary, releases it without anyone cleaning up.
func (l *Leaser) Acquire(ctx context.Context) (ports.Lease, error) {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	conn, err := pgx.ConnectConfig(ctx, l.config)
	if err != nil {
		return nil, fmt.Errorf("open the lease connection: %w", err)
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", l.key).Scan(&acquired); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("take the lease: %w", err)
	}
	if !acquired {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, ports.ErrNotLeader
	}
	return &lease{conn: conn, timeout: l.timeout}, nil
}

type lease struct {
	conn    *pgx.Conn
	timeout time.Duration
}

// Alive runs a trivial statement on the lease's own connection; it fails once the connection, and with it the lock, is gone.
func (l *lease) Alive(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	var one int
	if err := l.conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("the lease connection is not usable: %w", err)
	}
	return nil
}

// Release closes the connection, which releases the lock.
func (l *lease) Release() {
	ctx, cancel := context.WithTimeout(context.Background(), l.timeout)
	defer cancel()
	if err := l.conn.Close(ctx); err != nil {
		slog.Warn("closing the lease connection", "error", err)
	}
}
