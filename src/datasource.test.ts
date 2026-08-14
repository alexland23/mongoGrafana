import { throwError } from 'rxjs';

import { DataQueryRequest, DataSourceInstanceSettings, LoadingState, PluginType } from '@grafana/data';

import { DataSource } from './datasource';
import { MongoDataSourceOptions, MongoQuery } from './types';

const getDataStream = jest.fn();

jest.mock('@grafana/runtime', () => {
  const actual = jest.requireActual('@grafana/runtime');
  return {
    ...actual,
    getGrafanaLiveSrv: () => ({ getDataStream }),
    getTemplateSrv: () => ({ replace: (text: string) => text }),
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

const baseTarget: MongoQuery = {
  refId: 'A',
  queryType: 'find',
  collection: 'logs',
  format: 'logs',
  liveStreaming: true,
};

const baseRequest: DataQueryRequest<MongoQuery> = {
  requestId: 'req-1',
  targets: [baseTarget],
  scopedVars: {},
} as DataQueryRequest<MongoQuery>;

describe('DataSource live streaming', () => {
  beforeEach(() => {
    getDataStream.mockReset();
  });

  it('surfaces getDataStream failures as a populated DataQueryResponse error instead of an unhandled observable error', async () => {
    getDataStream.mockReturnValue(throwError(() => new Error('subscribe failed: not found')));

    const ds = new DataSource(instanceSettings);
    const responses: Array<{ data: unknown[]; state?: LoadingState; error?: { message?: string; refId?: string } }> =
      [];

    await new Promise<void>((resolve, reject) => {
      ds.query(baseRequest).subscribe({
        next: (rsp) => responses.push(rsp),
        error: reject,
        complete: resolve,
      });
    });

    expect(responses).toHaveLength(1);
    expect(responses[0].state).toBe(LoadingState.Error);
    expect(responses[0].error?.message).toContain('subscribe failed: not found');
    expect(responses[0].error?.refId).toBe('A');
  });

  it('resolves distinct live channels for query texts that collide under a 32-bit hash', () => {
    getDataStream.mockReturnValue(throwError(() => new Error('unused')));

    // These two strings collide under the plain djb2 32-bit hash previously used for
    // hashChannelSegment (both hash to "1ghqtjy"), demonstrating the ~64k-input birthday
    // bound that made cross-wired live subscriptions (PR #29 review, finding 7) reachable
    // in practice. The widened 64-bit digest must resolve them to different channels.
    const queryA: MongoQuery = { ...baseTarget, queryText: '902662a29bf29504721fe991' };
    const queryB: MongoQuery = { ...baseTarget, queryText: 'da191659f798e5ed2e216824' };

    const ds = new DataSource(instanceSettings);
    ds.query({ ...baseRequest, targets: [queryA] }).subscribe();
    ds.query({ ...baseRequest, targets: [queryB] }).subscribe();

    expect(getDataStream).toHaveBeenCalledTimes(2);
    const pathA = getDataStream.mock.calls[0][0].addr.path;
    const pathB = getDataStream.mock.calls[1][0].addr.path;
    expect(pathA).not.toEqual(pathB);
  });
});
