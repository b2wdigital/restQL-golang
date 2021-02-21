package conf

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v6"
	"gopkg.in/yaml.v2"
)

const configFileName = "restql.yml"

type requestIDConf struct {
	Header   string `yaml:"header"`
	Strategy string `yaml:"strategy"`
}

type compressionConf struct{}

type timeoutConf struct {
	Duration string `yaml:"duration"`
}

type cacheConf struct{}

type corsConf struct {
	AllowOrigin      string `yaml:"allowOrigin" env:"RESTQL_CORS_ALLOW_ORIGIN"`
	AllowMethods     string `yaml:"allowMethods" env:"RESTQL_CORS_ALLOW_METHODS"`
	AllowHeaders     string `yaml:"allowHeaders" env:"RESTQL_CORS_ALLOW_HEADERS"`
	ExposeHeaders    string `yaml:"exposeHeaders" env:"RESTQL_CORS_EXPOSE_HEADERS"`
	MaxAge           int    `yaml:"maxAge" env:"RESTQL_CORS_MAX_AGE"`
	AllowCredentials bool   `yaml:"allowCredentials" env:"RESTQL_CORS_ALLOW_CREDENTIALS"`
}

type requestCancellationConf struct {
	Enabled       bool          `yaml:"enabled"`
	WatchInterval time.Duration `yaml:"watchInterval"`
}

// Config represents all parameters allowed in restQL runtime.
type Config struct {
	HTTP struct {
		ForwardPrefix        string        `yaml:"forwardPrefix" env:"RESTQL_FORWARD_PREFIX"`
		GlobalQueryTimeout   time.Duration `env:"RESTQL_QUERY_GLOBAL_TIMEOUT" envDefault:"30s"`
		QueryResourceTimeout time.Duration `env:"RESTQL_QUERY_RESOURCE_TIMEOUT" envDefault:"5s"`

		Server struct {
			APIAddr         string `env:"RESTQL_PORT,required"`
			APIHealthAddr   string `env:"RESTQL_HEALTH_PORT,required"`
			PropfAddr       string `env:"RESTQL_PPROF_PORT"`
			EnablePprof     bool   `env:"RESTQL_ENABLE_PPROF"`
			EnableFullPprof bool   `env:"RESTQL_ENABLE_FULL_PPROF"`
			Admin           struct {
				Enable            bool   `yaml:"enable" env:"RESTQL_ADMIN_ENABLE"`
				AuthorizationCode string `yaml:"authorizationCode" env:"RESTQL_ADMIN_AUTHORIZATION_CODE"`
			} `yaml:"admin"`

			GracefulShutdownTimeout time.Duration `yaml:"gracefulShutdownTimeout"`
			ReadTimeout             time.Duration `yaml:"readTimeout"`
			IdleTimeout             time.Duration `yaml:"idleTimeout"`

			Middlewares struct {
				RequestID           *requestIDConf           `yaml:"requestId"`
				Compression         *compressionConf         `yaml:"compression"`
				Timeout             *timeoutConf             `yaml:"timeout"`
				Cors                *corsConf                `yaml:"cors"`
				Cache               *cacheConf               `yaml:"cache"`
				RequestCancellation *requestCancellationConf `yaml:"requestCancellation"`
			} `yaml:"middlewares"`
		} `yaml:"server"`

		Client struct {
			DnsRefreshInterval  time.Duration `yaml:"dnsRefreshInterval"`
			ConnTimeout         time.Duration `yaml:"connectionTimeout"`
			MaxRequestTimeout   time.Duration `yaml:"maxRequestTimeout"`
			MaxConnsPerHost     int           `yaml:"maxConnectionsPerHost"`
			MaxIdleConns        int           `yaml:"maxIdleConnections"`
			MaxIdleConnsPerHost int           `yaml:"maxIdleConnectionsPerHost"`
			MaxIdleConnDuration time.Duration `yaml:"maxIdleConnectionDuration"`
		} `yaml:"client"`
	} `yaml:"http"`

	Logging struct {
		Enable               bool   `yaml:"enable" env:"RESTQL_LOGGING_ENABLE"`
		TimestampFieldName   string `yaml:"timestampFieldName"`
		TimestampFieldFormat string `yaml:"timestampFieldFormat"`
		Level                string `yaml:"level" env:"RESTQL_LOGGING_LEVEL"`
		Format               string `yaml:"format"`
	} `yaml:"logging"`

	Cache struct {
		Mappings struct {
			MaxSize            int           `yaml:"maxSize" env:"RESTQL_CACHE_MAPPINGS_MAX_SIZE"`
			Expiration         time.Duration `yaml:"expiration" env:"RESTQL_CACHE_MAPPINGS_EXPIRATION"`
			RefreshInterval    time.Duration `yaml:"refreshInterval" env:"RESTQL_CACHE_MAPPINGS_REFRESH_INTERVAL"`
			RefreshQueueLength int           `yaml:"refreshQueueLength" env:"RESTQL_CACHE_MAPPINGS_REFRESH_QUEUE_LENGTH"`
		} `yaml:"mappings"`
		Query struct {
			MaxSize int `yaml:"maxSize" env:"RESTQL_CACHE_QUERY_MAX_SIZE"`
		} `yaml:"query"`
		Parser struct {
			MaxSize int `yaml:"maxSize" env:"RESTQL_CACHE_PARSER_MAX_SIZE"`
		} `yaml:"parser"`
	} `yaml:"cache"`

	Plugins struct {
		DisableDatabase bool `yaml:"disableDatabase" env:"RESTQL_PLUGINS_DATABASE_DISABLE"`
	} `yaml:"plugins"`

	Tenant string `env:"RESTQL_TENANT"`

	Mappings map[string]string `yaml:"mappings"`

	TenantMappings map[string]map[string]string `yaml:"tenants"`

	Queries map[string]map[string][]string `yaml:"queries"`

	Env EnvSource

	Build string
}

// Load returns a Config build from the
// defaults, YAML configuration file and
// environment variables.
func Load(build string) (*Config, error) {
	cfg := Config{}
	readDefaults(&cfg)

	err := yaml.Unmarshal(readConfigFile(), &cfg)
	if err != nil {
		return nil, err
	}

	err = env.Parse(&cfg)
	if err != nil {
		return nil, err
	}

	cfg.Build = build
	cfg.Env = EnvSource{}

	return &cfg, nil
}

func readConfigFile() []byte {
	path := getConfigFilepath()
	if path == "" {
		log.Printf("[WARN] no config file present")
		return nil
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Printf("[ERROR] could not load file at %s", path)
		return nil
	}

	return data
}

func getConfigFilepath() string {
	envConfigFilepath := os.Getenv("RESTQL_CONFIG")
	if envConfigFilepath != "" {
		fileAtCustom, err := filepath.Abs(envConfigFilepath)
		if err != nil {
			log.Printf("[DEBUG] failed to find directory: %v", err)
		}

		return fileAtCustom
	}

	fileAtRoot := filepath.Join(".", configFileName)
	if doesFileExist(fileAtRoot) {
		return fileAtRoot
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("[DEBUG] failed to find home directory: %v", err)
		return ""
	}

	fileAtHome := filepath.Join(homeDir, configFileName)
	if doesFileExist(fileAtHome) {
		return fileAtHome
	}

	return ""
}

func doesFileExist(filepath string) bool {
	info, err := os.Stat(filepath)
	if os.IsNotExist(err) {
		return false
	}

	return !info.IsDir()
}

// EnvSource allows access to environment variables.
type EnvSource struct{}

// GetString returns the environment variable value
// for the given key.
func (e EnvSource) GetString(key string) string {
	return os.Getenv("RESTQL_" + key)
}

// GetAll return all the environment variables.
func (e EnvSource) GetAll() map[string]string {
	result := make(map[string]string)
	for _, envVar := range os.Environ() {
		pair := strings.SplitN(envVar, "=", 2)
		if strings.HasPrefix(pair[0], "RESTQL_") {
			result[pair[0]] = pair[1]
		}
	}

	return result
}
