import { DEFAULT_BUILDER_STATE, QueryBuilderState } from '../../types';
import { compileBuilderPipeline } from './compile';

const emptyState: QueryBuilderState = {
  timeFilterEnabled: false,
  timeField: 'time',
  filters: [],
  groupBy: { enabled: false, timeField: 'time', interval: '', labelField: '', aggregations: [] },
  sort: [],
};

describe('compileBuilderPipeline', () => {
  it('compiles an empty state to an empty pipeline', () => {
    expect(JSON.parse(compileBuilderPipeline(emptyState))).toEqual([]);
  });

  it('emits an unquoted $__timeFilter macro as the whole $match value', () => {
    const pipeline = compileBuilderPipeline({ ...emptyState, timeFilterEnabled: true, timeField: 'time' });
    expect(pipeline).toContain('"$match": $__timeFilter(time)');
    // The macro must not be JSON-string-quoted, or the backend would never expand it.
    expect(pipeline).not.toContain('"$__timeFilter(time)"');
  });

  it('combines a time filter with match filters under $and', () => {
    const pipeline = compileBuilderPipeline({
      ...emptyState,
      timeFilterEnabled: true,
      filters: [{ field: 'status', operator: 'eq', value: 'ok' }],
    });
    const parsedPortion = pipeline.replace('$__timeFilter(time)', '"__TF__"');
    const [match] = JSON.parse(parsedPortion);
    expect(match.$match.$and).toEqual(['__TF__', { status: 'ok' }]);
  });

  it.each([
    ['eq', '5', { count: 5 }],
    ['ne', 'ok', { count: { $ne: 'ok' } }],
    ['gt', '5', { count: { $gt: 5 } }],
    ['gte', '5', { count: { $gte: 5 } }],
    ['lt', '5', { count: { $lt: 5 } }],
    ['lte', '5', { count: { $lte: 5 } }],
    ['in', 'a, b, 3', { count: { $in: ['a', 'b', 3] } }],
    ['nin', 'a, b', { count: { $nin: ['a', 'b'] } }],
    ['regex', '^prefix', { count: { $regex: '^prefix' } }],
  ] as const)('compiles a single %s filter into $match', (operator, value, expected) => {
    const [match] = JSON.parse(
      compileBuilderPipeline({ ...emptyState, filters: [{ field: 'count', operator, value }] })
    );
    expect(match).toEqual({ $match: expected });
  });

  it('compiles an exists filter with the boolean value, not the string', () => {
    const [match] = JSON.parse(
      compileBuilderPipeline({ ...emptyState, filters: [{ field: 'count', operator: 'exists', value: 'false' }] })
    );
    expect(match).toEqual({ $match: { count: { $exists: false } } });
  });

  it('skips filters with no field name', () => {
    expect(JSON.parse(compileBuilderPipeline({ ...emptyState, filters: [{ field: '', operator: 'eq', value: '1' }] }))).toEqual(
      []
    );
  });

  it('emits an unquoted $__timeGroup macro and projects the bucket plus aggregations', () => {
    const pipeline = compileBuilderPipeline({
      ...emptyState,
      groupBy: {
        enabled: true,
        timeField: 'time',
        interval: '10m',
        labelField: 'host',
        aggregations: [
          { op: 'avg', field: 'cpu', as: 'avg_cpu' },
          { op: 'count', as: 'n' },
        ],
      },
    });
    expect(pipeline).toContain('"t": $__timeGroup(time, "10m")');

    const withoutMacro = pipeline.replace('$__timeGroup(time, "10m")', '"__TG__"');
    const [group, project] = JSON.parse(withoutMacro);
    expect(group.$group).toEqual({
      _id: { t: '__TG__', host: '$host' },
      avg_cpu: { $avg: '$cpu' },
      n: { $sum: 1 },
    });
    expect(project.$project).toEqual({ _id: 0, time: '$_id.t', host: '$_id.host', avg_cpu: 1, n: 1 });
  });

  it('omits an aggregation missing a required field or output name', () => {
    const pipeline = compileBuilderPipeline({
      ...emptyState,
      groupBy: {
        enabled: true,
        timeField: 'time',
        aggregations: [
          { op: 'avg', field: '', as: 'missing_field' },
          { op: 'sum', field: 'cpu', as: '' },
        ],
      },
    });
    const [group] = JSON.parse(pipeline.replace(/\$__timeGroup\([^)]*\)/, '"__TG__"'));
    expect(group.$group).toEqual({ _id: { t: '__TG__' } });
  });

  it('does not group when disabled or missing a time field', () => {
    expect(
      compileBuilderPipeline({ ...emptyState, groupBy: { ...emptyState.groupBy, enabled: true, timeField: '' } })
    ).toBe('[]');
  });

  it('compiles sort direction and drops sort rows with no field', () => {
    const [sort] = JSON.parse(
      compileBuilderPipeline({
        ...emptyState,
        sort: [
          { field: 'time', direction: 'asc' },
          { field: '', direction: 'desc' },
          { field: 'host', direction: 'desc' },
        ],
      })
    );
    expect(sort).toEqual({ $sort: { time: 1, host: -1 } });
  });

  it('appends a $limit stage only when limit is a positive number', () => {
    expect(compileBuilderPipeline({ ...emptyState, limit: 0 })).toBe('[]');
    expect(JSON.parse(compileBuilderPipeline({ ...emptyState, limit: 50 }))).toEqual([{ $limit: 50 }]);
  });

  it('produces the documented example pipeline end-to-end', () => {
    const pipeline = compileBuilderPipeline({
      timeFilterEnabled: true,
      timeField: 'time',
      filters: [],
      groupBy: {
        enabled: true,
        timeField: 'time',
        interval: '10m',
        labelField: 'host',
        aggregations: [{ op: 'avg', field: 'cpu', as: 'avg_cpu' }],
      },
      sort: [{ field: 'time', direction: 'asc' }],
      limit: 100,
    });

    const normalized = pipeline.replace('$__timeFilter(time)', '"__TF__"').replace('$__timeGroup(time, "10m")', '"__TG__"');
    expect(JSON.parse(normalized)).toEqual([
      { $match: '__TF__' },
      { $group: { _id: { t: '__TG__', host: '$host' }, avg_cpu: { $avg: '$cpu' } } },
      { $project: { _id: 0, time: '$_id.t', host: '$_id.host', avg_cpu: 1 } },
      { $sort: { time: 1 } },
      { $limit: 100 },
    ]);
  });

  it('the default builder state compiles to valid JSON', () => {
    expect(() => JSON.parse(compileBuilderPipeline(DEFAULT_BUILDER_STATE).replace(/\$__timeFilter\([^)]*\)/, '"tf"'))).not.toThrow();
  });
});
