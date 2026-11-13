package cli

import (
	"encoding/json"
	"strings"

	"github.com/ysk6506/praxium/backend/internal/kernel"
)

// Names of the environment variables the client reads (a flag wins over them).
const (
	EnvEndpoint = "PRAXIUM_ENDPOINT"
	EnvToken    = "PRAXIUM_TOKEN"
	EnvConfig   = "PRAXIUM_CONFIG"
)

// DefaultEndpoint is the address of a server started on the same machine.
const DefaultEndpoint = "http://127.0.0.1:8080"

// Env is what the client may ask of the outside world: an environment lookup
// and a file reader. The composition root fills it with the real ones.
type Env struct {
	Lookup   func(name string) (string, bool)
	ReadFile func(path string) ([]byte, error)
}

// Settings are the resolved endpoint and token.
type Settings struct {
	Endpoint string
	Token    string
}

// fileSettings is the config file: a JSON object.
type fileSettings struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

// Resolve applies the precedence flag, environment, config file, default.
// configPath comes from the --config flag; without it the environment names
// the file. A named file that cannot be read or parsed is an error; no file
// at all is fine.
func Resolve(flagEndpoint, flagToken, configPath string, env Env) (Settings, error) {
	if configPath == "" {
		configPath, _ = env.Lookup(EnvConfig)
	}
	var file fileSettings
	if configPath != "" {
		data, err := env.ReadFile(configPath)
		if err != nil {
			return Settings{}, kernel.Wrap(kernel.CodeInvalidValue, err, "cannot read the config file")
		}
		if err := json.Unmarshal(data, &file); err != nil {
			return Settings{}, kernel.Wrap(kernel.CodeInvalidValue, err, "the config file is not a JSON object with endpoint and token")
		}
	}
	envEndpoint, _ := env.Lookup(EnvEndpoint)
	envToken, _ := env.Lookup(EnvToken)
	out := Settings{
		Endpoint: first(flagEndpoint, envEndpoint, file.Endpoint, DefaultEndpoint),
		Token:    first(flagToken, envToken, file.Token),
	}
	out.Endpoint = strings.TrimRight(out.Endpoint, "/")
	return out, nil
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
