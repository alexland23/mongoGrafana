import { DataSourceInstanceSettings, PluginType, ScopedVars } from '@grafana/data';

import { DataSource } from './datasource';
import { MongoDataSourceOptions, MongoQuery } from './types';

const replaceMock = jest.fn(
  (text: string, _scopedVars?: ScopedVars, _format?: (value: string | string[]) => string) => text
);

jest.mock('@grafana/runtime', () => {
  const actual = jest.requireActual('@grafana/runtime');
  return {
    ...actual,
    getTemplateSrv: () => ({ replace: replaceMock }),
  };
});

const instanceSettings: DataSourceInstanceSettings<MongoDataSourceOptions> = {
  id: 1,
  uid: 'mongo-uid',
  type: 'alandave-mongodb-datasource',
  name: 'MongoDB',
  meta: { id: 'alandave-mongodb-datasource', type: PluginType.datasource } as DataSourceInstanceSettings['meta'],
  jsonData: {},
  access: 'proxy',
  readOnly: false,
};

describe('DataSource.filterQuery', () => {
  const ds = new DataSource(instanceSettings);

  it('drops a command query with no queryText', () => {
    expect(ds.filterQuery({ refId: 'A', queryType: 'command', queryText: '' })).toBe(false);
  });

  it('runs a command query once queryText is set', () => {
    expect(ds.filterQuery({ refId: 'A', queryType: 'command', queryText: '{"ping": 1}' })).toBe(true);
  });

  it('drops any non-command query with no collection', () => {
    expect(ds.filterQuery({ refId: 'A', queryType: 'find' })).toBe(false);
    expect(ds.filterQuery({ refId: 'A', queryType: 'aggregate', queryText: '[]' })).toBe(false);
  });

  it('requires queryText for aggregate queries', () => {
    expect(ds.filterQuery({ refId: 'A', queryType: 'aggregate', collection: 'logs', queryText: '' })).toBe(false);
    expect(ds.filterQuery({ refId: 'A', queryType: 'aggregate', collection: 'logs', queryText: '[]' })).toBe(true);
  });

  it('requires a field for distinct queries', () => {
    expect(ds.filterQuery({ refId: 'A', queryType: 'distinct', collection: 'logs', field: '' })).toBe(false);
    expect(ds.filterQuery({ refId: 'A', queryType: 'distinct', collection: 'logs', field: 'host' })).toBe(true);
  });

  it('allows find and count queries with an empty filter', () => {
    expect(ds.filterQuery({ refId: 'A', queryType: 'find', collection: 'logs', queryText: '' })).toBe(true);
    expect(ds.filterQuery({ refId: 'A', queryType: 'count', collection: 'logs', queryText: '' })).toBe(true);
  });

  it('defaults to aggregate semantics when queryType is unset', () => {
    expect(ds.filterQuery({ refId: 'A', collection: 'logs', queryText: '' })).toBe(false);
    expect(ds.filterQuery({ refId: 'A', collection: 'logs', queryText: '[]' })).toBe(true);
  });
});

describe('DataSource.applyTemplateVariables', () => {
  beforeEach(() => {
    replaceMock.mockReset();
    replaceMock.mockImplementation((text: string) => text);
  });

  it('replaces database, collection, queryText, field, projection and sort', () => {
    const ds = new DataSource(instanceSettings);
    replaceMock.mockImplementation((text: string) => `[${text}]`);

    const query: MongoQuery = {
      refId: 'A',
      database: 'db',
      collection: 'coll',
      queryText: '{}',
      field: 'host',
      projection: '{"_id": 0}',
      sort: '{"time": 1}',
    };

    const resolved = ds.applyTemplateVariables(query, {});

    expect(resolved.database).toBe('[db]');
    expect(resolved.collection).toBe('[coll]');
    expect(resolved.queryText).toBe('[{}]');
    expect(resolved.field).toBe('[host]');
    expect(resolved.projection).toBe('[{"_id": 0}]');
    expect(resolved.sort).toBe('[{"time": 1}]');
  });

  it('leaves undefined fields undefined instead of coercing to a string', () => {
    const ds = new DataSource(instanceSettings);

    const query: MongoQuery = { refId: 'A', queryType: 'find', collection: 'logs' };
    const resolved = ds.applyTemplateVariables(query, {});

    expect(resolved.database).toBeUndefined();
    expect(resolved.queryText).toBeUndefined();
    expect(resolved.field).toBeUndefined();
    expect(resolved.projection).toBeUndefined();
    expect(resolved.sort).toBeUndefined();
    // Fields outside the replaced set pass through untouched.
    expect(resolved.queryType).toBe('find');
    expect(resolved.collection).toBe('logs');
  });

  it('passes scopedVars through to templateSrv.replace for each replaced field', () => {
    const ds = new DataSource(instanceSettings);
    const scopedVars = { host: { text: 'a', value: 'a' } };

    ds.applyTemplateVariables({ refId: 'A', queryText: '$host' }, scopedVars);

    expect(replaceMock).toHaveBeenCalledWith('$host', scopedVars, expect.any(Function));
  });

  it('formats a multi-value variable as a JSON array via the format callback', () => {
    const ds = new DataSource(instanceSettings);
    replaceMock.mockImplementation((text: string, _scopedVars, format?: (value: string | string[]) => string) =>
      format ? format(['a', 'b']) : text
    );

    const resolved = ds.applyTemplateVariables({ refId: 'A', queryText: '$hosts' }, {});

    expect(resolved.queryText).toBe('["a","b"]');
  });
});
