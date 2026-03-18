package conf

import (
	"flag"

	"github.com/BurntSushi/toml"
)

var (
	confPath string
	// Conf global config.
	Conf *Config
)

func init() {
	flag.StringVar(&confPath, "conf", "api-example.toml", "default config path")
}

// Init init config.
func Init() (err error) {
	Conf = Default()
	_, err = toml.DecodeFile(confPath, &Conf)
	return
}

// Default returns a config with default values.
func Default() *Config {
	return &Config{
		HTTPServer: &HTTPServer{Addr: ":3200"},
		JWT:        &JWT{Secret: "change-me", ExpireHours: 24},
		ACK:        &ACK{RetryInterval: 5, MaxRetries: 3},
	}
}

// Config is api service config.
type Config struct {
	HTTPServer *HTTPServer
	MySQL      *MySQL
	JWT        *JWT
	Logic      *Logic
	ACK        *ACK
}

// HTTPServer is http server config.
type HTTPServer struct {
	Addr string
}

type ACK struct {
	RetryInterval int // # of seconds
	MaxRetries    int
}

// MySQL is mysql config.
type MySQL struct {
	DSN string
}

// JWT is jwt config.
type JWT struct {
	Secret      string
	ExpireHours int
}

// Logic is logic service address for internal HTTP calls.
type Logic struct {
	Addr string
}
