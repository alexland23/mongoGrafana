import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export type QueryType = 'aggregate' | 'find' | 'count' | 'distinct' | 'command';
export type QueryFormat = 'table' | 'timeseries' | 'long' | 'logs';

/** Code = free-form Monaco editor; Builder = visual pipeline construction (aggregate only). */
export type EditorMode = 'code' | 'builder';

export type FilterOperator = 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte' | 'in' | 'nin' | 'exists' | 'regex';

export interface BuilderFilter {
  field: string;
  operator: FilterOperator;
  /** Raw text value; parsed as JSON when possible (numbers, booleans, quoted strings), else used as a plain string. */
  value: string;
}

export type AggregationOp = 'sum' | 'avg' | 'min' | 'max' | 'count';

export interface BuilderAggregation {
  op: AggregationOp;
  /** Field to aggregate; unused for "count". */
  field?: string;
  /** Output field name. */
  as: string;
}

export interface BuilderSort {
  field: string;
  direction: 'asc' | 'desc';
}

export interface BuilderGroupBy {
  enabled: boolean;
  /** BSON date field bucketed via the $__timeGroup macro. */
  timeField: string;
  /** Bucket size, e.g. "10m"; blank uses the dashboard's suggested interval. */
  interval?: string;
  /** Optional extra field to group by alongside the time bucket, e.g. "host". */
  labelField?: string;
  aggregations: BuilderAggregation[];
}

/** Visual editor state for "Builder" mode; compiled into an aggregation pipeline shown in the code editor. */
export interface QueryBuilderState {
  timeFilterEnabled: boolean;
  /** BSON date field matched via the $__timeFilter macro. */
  timeField: string;
  filters: BuilderFilter[];
  groupBy: BuilderGroupBy;
  sort: BuilderSort[];
  limit?: number;
}

export const DEFAULT_BUILDER_STATE: QueryBuilderState = {
  timeFilterEnabled: true,
  timeField: 'time',
  filters: [],
  groupBy: {
    enabled: false,
    timeField: 'time',
    interval: '',
    labelField: '',
    aggregations: [],
  },
  sort: [],
  limit: 100,
};

export interface MongoQuery extends DataQuery {
  /** aggregate | find | count | distinct | command */
  queryType?: QueryType;
  /** Optional per-query override of the datasource default database */
  database?: string;
  /** Target collection (not used for "command") */
  collection?: string;
  /** Extended JSON: pipeline (aggregate), filter (find/count/distinct) or command document */
  queryText?: string;
  /** Field name for distinct queries */
  field?: string;
  /** Extended JSON projection document (find) */
  projection?: string;
  /** Extended JSON sort document (find) */
  sort?: string;
  /** Max documents to return (find), 0 = unlimited */
  limit?: number;
  /** Documents to skip (find) */
  skip?: number;
  /** How the result should be framed */
  format?: QueryFormat;
  /** code (default) | builder; builder only applies to aggregate queries */
  editorMode?: EditorMode;
  /** Visual editor state, kept alongside queryText so switching back to Builder mode restores it */
  builder?: QueryBuilderState;
  /** Tail the collection over Grafana Live instead of running a one-shot query. Only applies to "logs" format. */
  liveStreaming?: boolean;
  /** Document field to treat as the log line. Only applies to "logs" format; blank keeps the "message" convention. */
  messageField?: string;
  /** Document field to treat as the log level. Only applies to "logs" format; blank keeps the "level" convention. */
  levelField?: string;
  /**
   * Caps how many levels of nested documents get flattened into dot-notation columns; beyond it, a
   * nested document is kept whole as a single JSON-encoded column. Unset/0 means unlimited, i.e.
   * flattening every level (today's default behavior).
   */
  flattenDepth?: number;
}

export const DEFAULT_QUERY: Partial<MongoQuery> = {
  queryType: 'find',
  format: 'table',
  queryText: '{}',
};

/** Pre-fills new annotation queries with an example matching the "Annotations" column contract (see src/README.md). */
export const DEFAULT_ANNOTATION_QUERY: Partial<MongoQuery> = {
  queryType: 'aggregate',
  format: 'table',
  collection: 'logs',
  queryText: `[
  { "$match": { "level": "error" } },
  { "$project": { "time": 1, "title": "$level", "text": "$message", "tags": "$service" } },
  { "$limit": 100 }
]`,
};

/**
 * Options configured for each DataSource instance
 */
export interface MongoDataSourceOptions extends DataSourceJsonData {
  /** MongoDB URI, e.g. mongodb://localhost:27017 or mongodb+srv://cluster.example.net */
  connectionString?: string;
  /** Default database queries run against */
  database?: string;
  /** Username for authentication (password lives in secureJsonData) */
  username?: string;
  /** Per-query timeout in seconds */
  timeoutSeconds?: number;
  /** Enable TLS for the connection, independent of any "tls" URI parameter */
  tlsEnabled?: boolean;
  /** Skip server certificate verification. Insecure; for self-signed certs in dev/test. */
  tlsSkipVerify?: boolean;
  /** Path to a PEM CA certificate file on the plugin backend host. Takes precedence over tlsCaCert. */
  tlsCaCertPath?: string;
  /** Path to a PEM client certificate file on the plugin backend host. Takes precedence over tlsClientCert. */
  tlsClientCertPath?: string;
  /** Path to a PEM client key file on the plugin backend host. Takes precedence over tlsClientKey. */
  tlsClientKeyPath?: string;
  /** primary | primaryPreferred | secondary | secondaryPreferred | nearest */
  readPreference?: string;
  /** Timeout for establishing the initial server connection, in seconds */
  connectTimeoutSeconds?: number;
  /** Maximum pooled connections per server */
  maxPoolSize?: number;
  /** Enable the /databases, /collections and /fields resource endpoints that back autocomplete. Off by default. */
  schemaDiscoveryEnabled?: boolean;
  /**
   * Glob patterns evaluated against "database.collection" (and, for the database list, just the
   * database segment) by schema discovery. Prefix a pattern with "!" to deny matches; other
   * patterns allow them. No patterns means everything is allowed.
   */
  collectionFilters?: string[];
  /**
   * Extra clickable link columns derived from "logs" format results, e.g. pulling a trace ID out of
   * the message field and linking it to a tracing UI.
   */
  derivedFields?: DerivedFieldConfig[];
  /**
   * Caps how many documents a "find" or "aggregate" query can return, injected server-side so a
   * careless find({}) can't pull an entire collection into memory. Zero/unset defaults to 10000 on
   * the backend; a negative value disables the guard.
   */
  maxDocuments?: number;
  /**
   * "" (default) blocks a built-in denylist of destructive/JS-execution operators ($out, $merge,
   * $where, $function, $accumulator) and admin commands (dropDatabase, shutdown, eval); "off"
   * disables the check entirely, e.g. for datasources that intentionally use $merge.
   */
  operatorSafetyMode?: '' | 'off';
  /** Extra pipeline/filter operator keys (e.g. "$lookup") to block beyond the built-in denylist. */
  blockedOperators?: string[];
  /** Extra "command" query type top-level command names to block beyond the built-in denylist. */
  blockedCommands?: string[];
}

/**
 * One derived link field applied to logs results. matcherRegex is evaluated against each row's
 * message text; the first capture group becomes the field's value (or the whole match if the
 * pattern has no capture group). url supports the standard Grafana data link variable
 * `${__value.raw}`.
 */
export interface DerivedFieldConfig {
  matcherRegex: string;
  name: string;
  url: string;
  urlDisplayLabel?: string;
}

/**
 * Values used in the backend but never sent to the frontend
 */
export interface MongoSecureJsonData {
  password?: string;
  /** PEM-encoded CA certificate used to verify the server */
  tlsCaCert?: string;
  /** PEM-encoded client certificate for mutual TLS */
  tlsClientCert?: string;
  /** PEM-encoded client private key for mutual TLS */
  tlsClientKey?: string;
}
