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

	// TLSEnabled turns on TLS for the connection independent of any "tls"
	// URI parameter.
	TLSEnabled bool `json:"tlsEnabled"`
	// TLSSkipVerify disables server certificate verification. Insecure;
	// intended for self-signed certs in dev/test environments.
	TLSSkipVerify bool `json:"tlsSkipVerify"`
	// ReadPreference is one of primary, primaryPreferred, secondary,
	// secondaryPreferred, nearest. Empty means driver default (primary).
	ReadPreference string `json:"readPreference"`
	// TLSCACertPath, TLSClientCertPath and TLSClientKeyPath are paths to
	// PEM files on the machine running the plugin backend. When set, each
	// takes precedence over the corresponding pasted certificate content.
	TLSCACertPath     string `json:"tlsCaCertPath"`
	TLSClientCertPath string `json:"tlsClientCertPath"`
	TLSClientKeyPath  string `json:"tlsClientKeyPath"`
	// ConnectTimeoutSeconds bounds initial server connection time. Defaults to 10.
	ConnectTimeoutSeconds int `json:"connectTimeoutSeconds"`
	// MaxPoolSize caps the number of pooled connections per server. Defaults
	// to the driver default (100) when zero.
	MaxPoolSize uint64 `json:"maxPoolSize"`

	// SchemaDiscoveryEnabled turns on the /databases, /collections and
	// /fields resource endpoints backing autocomplete in the query and
	// variable editors. Off by default: listing databases/collections or
	// sampling for field discovery can be slow on large clusters, and
	// admins may not want every collection exposed to dashboard authors.
	SchemaDiscoveryEnabled bool `json:"schemaDiscoveryEnabled"`
	// CollectionFilters is an ordered list of glob patterns evaluated
	// against "database.collection" (and, for the database list, just the
	// database segment of each pattern) by the schema discovery resource
	// handlers. A pattern prefixed with "!" denies matches; other patterns
	// allow them. No patterns means everything is allowed; a deny match
	// always wins over an allow match.
	CollectionFilters []string `json:"collectionFilters"`

	// DerivedFields extracts extra clickable link columns out of "logs"
	// format results, e.g. pulling a trace ID out of the message field and
	// linking it to a tracing UI. Matched against the message column
	// (see queryModel.MessageField in pkg/plugin).
	DerivedFields []DerivedFieldConfig `json:"derivedFields"`

	// MaxDocuments bounds how many documents a "find" or "aggregate" query
	// can return, injected server-side (SetLimit / a trailing $limit stage)
	// so a careless find({}) can't pull an entire collection into memory.
	// Zero/unset defaults to 10000; a negative value disables the guard.
	MaxDocuments int64 `json:"maxDocuments"`

	// OperatorSafetyMode controls the operator/command denylist applied to
	// query text before it reaches MongoDB. Empty (the default) blocks
	// defaultBlockedOperators/defaultBlockedCommands plus BlockedOperators/
	// BlockedCommands below; "off" disables the check for datasources that
	// intentionally need e.g. $merge for materialized views.
	OperatorSafetyMode string `json:"operatorSafetyMode"`
	// BlockedOperators adds extra pipeline/filter operator keys (e.g.
	// "$lookup") to the built-in denylist.
	BlockedOperators []string `json:"blockedOperators"`
	// BlockedCommands adds extra "command" query type top-level command
	// names (e.g. "collMod") to the built-in denylist.
	BlockedCommands []string `json:"blockedCommands"`

	Secrets *SecretPluginSettings `json:"-"`
}

// DerivedFieldConfig describes one derived link field applied to logs
// results. MatcherRegex is evaluated against each row's message text; the
// first capture group is used as the field's value, or the whole match if
// the pattern has no capture group. URL supports the standard Grafana data
// link variable ${__value.raw}.
type DerivedFieldConfig struct {
	MatcherRegex    string `json:"matcherRegex"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	URLDisplayLabel string `json:"urlDisplayLabel"`
}

type SecretPluginSettings struct {
	Password string `json:"password"`
	// TLSCACert, TLSClientCert and TLSClientKey are PEM-encoded and only
	// used when TLSEnabled is set.
	TLSCACert     string `json:"tlsCaCert"`
	TLSClientCert string `json:"tlsClientCert"`
	TLSClientKey  string `json:"tlsClientKey"`
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
	if settings.ConnectTimeoutSeconds <= 0 {
		settings.ConnectTimeoutSeconds = 10
	}
	if settings.MaxDocuments == 0 {
		settings.MaxDocuments = 10000
	}

	settings.Secrets = loadSecretPluginSettings(source.DecryptedSecureJSONData)

	return &settings, nil
}

func loadSecretPluginSettings(source map[string]string) *SecretPluginSettings {
	return &SecretPluginSettings{
		Password:      source["password"],
		TLSCACert:     source["tlsCaCert"],
		TLSClientCert: source["tlsClientCert"],
		TLSClientKey:  source["tlsClientKey"],
	}
}
