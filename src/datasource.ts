import { Observable, of } from 'rxjs';

import {
  AnnotationQuery,
  AnnotationSupport,
  CoreApp,
  DataQueryRequest,
  DataQueryResponse,
  DataSourceInstanceSettings,
  DataSourceVariableSupport,
  ScopedVars,
} from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import { DEFAULT_ANNOTATION_QUERY, DEFAULT_QUERY, MongoDataSourceOptions, MongoQuery } from './types';

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

  query(request: DataQueryRequest<MongoQuery>): Observable<DataQueryResponse> {
    const targets = request.targets.filter((t) => !t.hide && this.filterQuery(t));
    if (targets.length === 0) {
      return of({ data: [] });
    }
    return super.query({ ...request, targets });
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

  /**
   * Runs MongoDB's explain command for an aggregate or find query and returns the raw query plan.
   * Macros ($__timeFrom etc.) are not interpolated -- there's no dashboard time range at this point
   * -- so a query relying on them should be explained with literal values substituted.
   */
  explainQuery(query: MongoQuery): Promise<Record<string, unknown>> {
    return this.postResource<Record<string, unknown>>('explain', {
      queryType: query.queryType ?? 'aggregate',
      database: query.database,
      collection: query.collection,
      queryText: query.queryText,
    });
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
