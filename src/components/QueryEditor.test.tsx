import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { createDataFrame, getDefaultTimeRange, LoadingState, PanelData } from '@grafana/data';
import { QueryEditor } from './QueryEditor';
import { DataSource } from '../datasource';
import { MongoQuery } from '../types';

// CodeEditor (Monaco) isn't under test here and its real "monaco-editor" package pulls in web
// worker / AMD loader machinery jsdom can't run; stub both out so the rest of the query editor
// (the paging fields/controls this file covers) can render. "monaco-editor" must be mocked as
// virtual since @grafana/ui's real CodeEditor requires it eagerly at module-init time, before the
// CodeEditor override below even applies.
jest.mock('monaco-editor', () => ({}), { virtual: true });
jest.mock('@grafana/ui', () => ({
  ...jest.requireActual('@grafana/ui'),
  CodeEditor: () => null,
}));

// schemaDiscoveryEnabled: false keeps Collection/Database/Field as plain <Input> instead of an
// async Combobox, which would otherwise need getDatabases/getCollections/getFields to resolve.
const datasource = {
  schemaDiscoveryEnabled: false,
} as unknown as DataSource;

const baseQuery: MongoQuery = {
  refId: 'A',
  queryType: 'find',
};

// @grafana/ui's Button renders "disabled" as aria-disabled (so a tooltip can still show on
// hover/focus) rather than the native disabled attribute jest-dom's toBeDisabled() checks for.
const expectAriaDisabled = (el: HTMLElement, disabled: boolean) =>
  expect(el).toHaveAttribute('aria-disabled', String(disabled));

function renderEditor(query: MongoQuery, data?: PanelData) {
  const onChange = jest.fn();
  const onRunQuery = jest.fn();
  render(
    <QueryEditor
      query={query}
      onChange={onChange}
      onRunQuery={onRunQuery}
      datasource={datasource}
      data={data}
    />
  );
  return { onChange, onRunQuery };
}

describe('QueryEditor paging fields', () => {
  it('shows Limit/Skip (and Projection/Sort) for "find"', () => {
    renderEditor({ ...baseQuery, queryType: 'find' });
    expect(screen.getByLabelText(/Limit/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Skip/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Projection/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Sort/)).toBeInTheDocument();
  });

  it('shows Limit/Skip but not Projection/Sort for "aggregate"', () => {
    renderEditor({ ...baseQuery, queryType: 'aggregate' });
    expect(screen.getByLabelText(/Limit/)).toBeInTheDocument();
    expect(screen.getByLabelText(/Skip/)).toBeInTheDocument();
    expect(screen.queryByLabelText(/Projection/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/Sort/)).not.toBeInTheDocument();
  });

  it('hides Limit/Skip for "count", "distinct" and "command"', () => {
    for (const queryType of ['count', 'distinct', 'command'] as const) {
      const { unmount } = render(
        <QueryEditor query={{ ...baseQuery, queryType }} onChange={jest.fn()} onRunQuery={jest.fn()} datasource={datasource} />
      );
      expect(screen.queryByLabelText(/^Limit/)).not.toBeInTheDocument();
      unmount();
    }
  });
});

describe('QueryEditor next/previous page controls', () => {
  it('disables both controls when no limit is set', () => {
    renderEditor({ ...baseQuery, queryType: 'aggregate' });
    expectAriaDisabled(screen.getByLabelText('Previous page'), true);
    expectAriaDisabled(screen.getByLabelText('Next page'), true);
  });

  it('disables "Previous page" at skip 0 and enables it once skip is positive', () => {
    renderEditor({ ...baseQuery, queryType: 'aggregate', limit: 10, skip: 0 });
    expectAriaDisabled(screen.getByLabelText('Previous page'), true);

    renderEditor({ ...baseQuery, queryType: 'aggregate', limit: 10, skip: 10 });
    expectAriaDisabled(screen.getAllByLabelText('Previous page').at(-1)!, false);
  });

  it('advances skip by the current limit and reruns the query on "Next page"', () => {
    const { onChange, onRunQuery } = renderEditor({ ...baseQuery, queryType: 'aggregate', limit: 25, skip: 10 });

    fireEvent.click(screen.getByLabelText('Next page'));

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ skip: 35 }));
    expect(onRunQuery).toHaveBeenCalled();
  });

  it('steps skip back by the current limit, floored at 0, on "Previous page"', () => {
    const { onChange } = renderEditor({ ...baseQuery, queryType: 'aggregate', limit: 25, skip: 10 });

    fireEvent.click(screen.getByLabelText('Previous page'));

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ skip: undefined }));
  });

  it('disables "Next page" once the last result came back shorter than the limit', () => {
    const shortFrame = createDataFrame({ refId: 'A', fields: [{ name: 'value', values: [1, 2] }] });
    const data: PanelData = { state: LoadingState.Done, series: [shortFrame], timeRange: getDefaultTimeRange() };

    renderEditor({ ...baseQuery, queryType: 'aggregate', limit: 10, skip: 0 }, data);

    expectAriaDisabled(screen.getByLabelText('Next page'), true);
  });

  it('keeps "Next page" enabled when the last result filled a full page', () => {
    const fullFrame = createDataFrame({
      refId: 'A',
      fields: [{ name: 'value', values: Array.from({ length: 10 }, (_, i) => i) }],
    });
    const data: PanelData = { state: LoadingState.Done, series: [fullFrame], timeRange: getDefaultTimeRange() };

    renderEditor({ ...baseQuery, queryType: 'aggregate', limit: 10, skip: 0 }, data);

    expectAriaDisabled(screen.getByLabelText('Next page'), false);
  });
});
