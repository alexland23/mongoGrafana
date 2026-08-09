import { CoreApp, DataSourceInstanceSettings, DataSourceVariableSupport, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import { DEFAULT_QUERY, MongoDataSourceOptions, MongoQuery } from './types';

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
  constructor(instanceSettings: DataSourceInstanceSettings<MongoDataSourceOptions>) {
    super(instanceSettings);
    this.variables = new MongoVariableSupport();
  }

  getDefaultQuery(_: CoreApp): Partial<MongoQuery> {
    return DEFAULT_QUERY;
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
