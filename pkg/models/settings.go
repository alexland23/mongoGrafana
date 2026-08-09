package models

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type PluginSettings struct {
	// ConnectionString is a MongoDB URI, e.g. mongodb://localhost:27017 or
	// mongodb+srv://cluster0.example.net. Options such as tls, replicaSet and
	// authSource can be passed as URI parameters.
	ConnectionString string `json:"connectionString"`
	// Database is the default database queries run against. It can be
	// overridden per query.
	Database string `json:"database"`
	// Username enables authentication when set; the password comes from
	// secure JSON data.
	Username string `json:"username"`
	// TimeoutSeconds bounds each query's execution time. Defaults to 30.
	TimeoutSeconds int `json:"timeoutSeconds"`

	Secrets *SecretPluginSettings `json:"-"`
}

type SecretPluginSettings struct {
	Password string `json:"password"`
}

func LoadPluginSettings(source backend.DataSourceInstanceSettings) (*PluginSettings, error) {
	settings := PluginSettings{}
	err := json.Unmarshal(source.JSONData, &settings)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal PluginSettings json: %w", err)
	}

	if settings.ConnectionString == "" {
		return nil, errors.New("connection string is required")
	}
	if settings.TimeoutSeconds <= 0 {
		settings.TimeoutSeconds = 30
	}

	settings.Secrets = loadSecretPluginSettings(source.DecryptedSecureJSONData)

	return &settings, nil
}

func loadSecretPluginSettings(source map[string]string) *SecretPluginSettings {
	return &SecretPluginSettings{
		Password: source["password"],
	}
}
