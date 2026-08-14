import { merge, Observable, of } from 'rxjs';
import { catchError } from 'rxjs/operators';

import {
  AnnotationQuery,
  AnnotationSupport,
  CoreApp,
  DataQueryRequest,
  DataQueryResponse,
  DataSourceInstanceSettings,
  DataSourceVariableSupport,
  LiveChannelScope,
  LoadingState,
  ScopedVars,
} from '@grafana/data';
import { DataSourceWithBackend, getGrafanaLiveSrv, getTemplateSrv, toDataQueryError } from '@grafana/runtime';

import { DEFAULT_ANNOTATION_QUERY, DEFAULT_QUERY, MongoDataSourceOptions, MongoQuery } from './types';

const isLiveTarget = (query: MongoQuery): boolean => query.format === 'logs' && !!query.liveStreaming;

/** Grafana Live channel paths may only contain [A-z0-9_-/=.]; anything else makes the subscription
 * fail with "invalid channel ID". */
const sanitizeChannelSegment = (value: string): string => value.replace(/[^A-Za-z0-9_\-.]/g, '_');

/** Cheap (non-cryptographic) hash so free-form filter text can distinguish live channels without
 * blowing past Grafana Live's 160-character channel ID limit or violating its character allowlist. */
const hashChannelSegment = (value: string): string => {
  let hash = 5381;
  for (let i = 0; i < value.length; i++) {
    hash = (hash * 33) ^ value.charCodeAt(i);
  }
  return (hash >>> 0).toString(36);
};

/** Distinguishing query params baked into the live channel path so editing the filter, collection or
 * database re-subscribes to a fresh channel instead of reusing a stale RunStream. */
const liveChannelPath = (query: MongoQuery): string => {
  const parts = [
    sanitizeChannelSegment(query.refId),
    sanitizeChannelSegment(query.database ?? ''),
    sanitizeChannelSegment(query.collection ?? ''),
    hashChannelSegment(query.queryText ?? ''),
  ];
  return `logs/${parts.join('/')}`;
};

/**
 * Lets dashboard variables be defined with the regular query editor; the
 * first column of the result provides the variable values.
 */
class MongoVariableSupport extends DataSourceVariableSupport<DataSource, MongoQuery, MongoDataSourceOptions> {}

/** Renders multi-value variables as JSON arrays so they stay valid inside queries. */
const jsonAwareFormat = (value: string | string[]) => {
  if (Array.isArray(value)) {
    return JSON.stringify(value);
  }
  return value;
};

export class DataSource extends DataSourceWithBackend<MongoQuery, MongoDataSourceOptions> {
  /** Whether the /databases, /collections and /fields resource endpoints are enabled. */
  schemaDiscoveryEnabled: boolean;

  /** Grafana's standard frame > event mapping reads `time`/`timeEnd`/`title`/`text`/`tags` columns; see src/README.md. */
  annotations: AnnotationSupport<MongoQuery> = {
    getDefaultQuery: (): Partial<MongoQuery> => DEFAULT_ANNOTATION_QUERY,
    prepareQuery: (anno: AnnotationQuery<MongoQuery>): MongoQuery | undefined => anno.target,
  };

  constructor(instanceSettings: DataSourceInstanceSettings<MongoDataSourceOptions>) {
    super(instanceSettings);
    this.variables = new MongoVariableSupport();
    this.schemaDiscoveryEnabled = !!instanceSettings.jsonData.schemaDiscoveryEnabled;
  }

  getDefaultQuery(_: CoreApp): Partial<MongoQuery> {
    return DEFAULT_QUERY;
  }

  /**
   * Splits off "logs" format queries with live tailing enabled and streams
   * them over Grafana Live (backed by the backend's RunStream, see
   * pkg/plugin/stream.go), merging them with the regular one-shot response
   * for the rest of the targets.
   */
  query(request: DataQueryRequest<MongoQuery>): Observable<DataQueryResponse> {
    const targets = request.targets.filter((t) => !t.hide && this.filterQuery(t));
    const liveTargets = targets.filter(isLiveTarget);
    const normalTargets = targets.filter((t) => !isLiveTarget(t));

    const responses: Array<Observable<DataQueryResponse>> = liveTargets.map((target) => {
      const resolved = this.applyTemplateVariables(target, request.scopedVars);
      return getGrafanaLiveSrv()
        .getDataStream({
          key: `${request.requestId}-${resolved.refId}`,
          addr: {
            scope: LiveChannelScope.DataSource,
            stream: this.uid,
            path: liveChannelPath(resolved),
            data: resolved,
          },
        })
        .pipe(
          catchError((err) => {
            // Native Error objects hold `message` as a non-enumerable own property, so it must be
            // read explicitly rather than spread into the response's error object.
            const error = toDataQueryError(err);
            error.refId = resolved.refId;
            return of({ data: [], state: LoadingState.Error, error });
          })
        );
    });

    if (normalTargets.length > 0) {
      responses.push(super.query({ ...request, targets: normalTargets }));
    }

    return responses.length > 0 ? merge(...responses) : of({ data: [] });
  }

  /** Lists databases visible to schema discovery. */
  getDatabases(): Promise<string[]> {
    return this.getResource<string[]>('databases');
  }

  /** Lists collections in `db` (or the datasource default database) visible to schema discovery. */
  getCollections(db?: string): Promise<string[]> {
    return this.getResource<string[]>('collections', db ? { db } : undefined);
  }

  /** Lists field names discovered by sampling `collection` in `db` (or the datasource default database). */
  getFields(db: string | undefined, collection: string): Promise<string[]> {
    return this.getResource<string[]>('fields', db ? { db, collection } : { collection });
  }

  applyTemplateVariables(query: MongoQuery, scopedVars: ScopedVars): MongoQuery {
    const templateSrv = getTemplateSrv();
    const replace = (text?: string) => (text ? templateSrv.replace(text, scopedVars, jsonAwareFormat) : text);

    return {
      ...query,
      database: replace(query.database),
      collection: replace(query.collection),
      queryText: replace(query.queryText),
      field: replace(query.field),
      projection: replace(query.projection),
      sort: replace(query.sort),
    };
  }

  filterQuery(query: MongoQuery): boolean {
    const queryType = query.queryType ?? 'aggregate';
    if (queryType === 'command') {
      return !!query.queryText;
    }
    if (!query.collection) {
      return false;
    }
    if (queryType === 'aggregate') {
      return !!query.queryText;
    }
    if (queryType === 'distinct') {
      return !!query.field;
    }
    // find and count are valid with an empty filter
    return true;
  }
}
