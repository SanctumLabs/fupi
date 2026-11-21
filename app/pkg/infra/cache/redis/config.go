package redis

const (
	EnvRedisHost       = "REDIS_HOST"
	EnvRedisPort       = "REDIS_PORT"
	EnvRedisUsername   = "REDIS_USERNAME"
	EnvRedisPassword   = "REDIS_PASSWORD"
	EnvRedisMasterName = "REDIS_MASTER_NAME"
	EnvRedisDatabase   = "REDIS_DATABASE"
)

// RedisClientConfig provides parameter options used to create a new redis client
type RedisClientConfig struct {
	Host string `env-description:"Redis Host" env:"REDIS_HOST"`
	Port int    `env-description:"Redis Port" env:"REDIS_PORT"`
	// Address specifies the host:port mapping to use to connect to Redis
	// If the Address parameter provided is a single item in the slice, e.g. [":6379"], a single node client is created
	// If it is more than 2 or more, a ClusterClient is created
	Address  []string `env-description:"Redis Address Database Host" env:"REDIS_ADDRESS"`
	Username string   `env-description:"Redis Username" env:"REDIS_USERNAME"`
	Password string   `env-description:"Redis Password" env:"REDIS_PASSWORD"`
	Database int      `env-description:"Redis Database" env:"REDIS_DATABASE"`

	// MasterName provides the master redis node in a cluster setup. if specified, this will create a redis FailOverClient
	MasterName string `env-description:"Redis Master name" env:"REDIS_MASTER_NAME"`
}
