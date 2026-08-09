import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export type QueryType = 'aggregate' | 'find' | 'count' | 'distinct' | 'command';
export type QueryFormat = 'table' | 'timeseries' | 'logs';

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
}

export const DEFAULT_QUERY: Partial<MongoQuery> = {
  queryType: 'aggregate',
  format: 'table',
  queryText: `[
  { "$match": { } },
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
}

/**
 * Values used in the backend but never sent to the frontend
 */
export interface MongoSecureJsonData {
  password?: string;
}
