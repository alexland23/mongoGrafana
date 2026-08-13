import React, { ChangeEvent } from 'react';
import { Button, CodeEditor, InlineField, InlineFieldRow, InlineSwitch, Input } from '@grafana/ui';
import { BuilderFilter, BuilderSort, DEFAULT_BUILDER_STATE, MongoQuery, QueryBuilderState } from '../../types';
import { compileBuilderPipeline } from './compile';
import { FilterRow } from './FilterRow';
import { SortRow } from './SortRow';
import { GroupByEditor } from './GroupByEditor';

interface Props {
  query: MongoQuery;
  onChange: (query: MongoQuery) => void;
  onRunQuery: () => void;
}

export function QueryBuilder({ query, onChange, onRunQuery }: Props) {
  const state = query.builder ?? DEFAULT_BUILDER_STATE;

  // A blur fired by clicking a button (e.g. "Add aggregation") reaches this
  // handler before that click's own onChange commits, so running
  // synchronously here would submit the pipeline from just before the
  // click. Deferring to the next tick lets the state update settle first
  // (same race the Combobox fields in QueryEditor.tsx work around).
  const deferredRunQuery = () => setTimeout(onRunQuery, 0);

  const update = (patch: Partial<QueryBuilderState>) => {
    const next = { ...state, ...patch };
    onChange({ ...query, builder: next, queryText: compileBuilderPipeline(next) });
  };

  const addFilter = () => update({ filters: [...state.filters, { field: '', operator: 'eq', value: '' }] });
  const setFilter = (index: number, filter: BuilderFilter) => {
    update({ filters: state.filters.map((f, i) => (i === index ? filter : f)) });
  };
  const removeFilter = (index: number) => update({ filters: state.filters.filter((_, i) => i !== index) });

  const addSort = () => update({ sort: [...state.sort, { field: '', direction: 'asc' }] });
  const setSort = (index: number, sort: BuilderSort) => {
    update({ sort: state.sort.map((s, i) => (i === index ? sort : s)) });
  };
  const removeSort = (index: number) => update({ sort: state.sort.filter((_, i) => i !== index) });

  return (
    <div onBlur={deferredRunQuery}>
      <InlineFieldRow>
        <InlineField
          label="Time filter"
          labelWidth={14}
          tooltip="Restricts results to the dashboard time range via $__timeFilter."
        >
          <InlineSwitch
            showLabel
            value={state.timeFilterEnabled}
            onChange={(e: ChangeEvent<HTMLInputElement>) => {
              update({ timeFilterEnabled: e.currentTarget.checked });
              deferredRunQuery();
            }}
          />
        </InlineField>
        {state.timeFilterEnabled && (
          <InlineField label="Time field" labelWidth={12}>
            <Input
              aria-label="Time field"
              placeholder="time"
              width={18}
              value={state.timeField}
              onChange={(e: ChangeEvent<HTMLInputElement>) => update({ timeField: e.target.value })}
              onBlur={onRunQuery}
            />
          </InlineField>
        )}
      </InlineFieldRow>

      <InlineField label="Match filters" labelWidth={14} grow>
        <div style={{ width: '100%' }}>
          {state.filters.map((f, i) => (
            <FilterRow key={i} filter={f} onChange={(filter) => setFilter(i, filter)} onRemove={() => removeFilter(i)} />
          ))}
          <Button icon="plus" variant="secondary" size="sm" onClick={addFilter}>
            Add filter
          </Button>
        </div>
      </InlineField>

      <InlineField label="Group by" labelWidth={14} grow>
        <GroupByEditor groupBy={state.groupBy} onChange={(groupBy) => update({ groupBy })} />
      </InlineField>

      <InlineField label="Sort" labelWidth={14} grow>
        <div style={{ width: '100%' }}>
          {state.sort.map((s, i) => (
            <SortRow key={i} sort={s} onChange={(sort) => setSort(i, sort)} onRemove={() => removeSort(i)} />
          ))}
          <Button icon="plus" variant="secondary" size="sm" onClick={addSort}>
            Add sort
          </Button>
        </div>
      </InlineField>

      <InlineFieldRow>
        <InlineField label="Limit" labelWidth={14}>
          <Input
            aria-label="Limit"
            type="number"
            width={12}
            placeholder="∞"
            value={state.limit ?? ''}
            onChange={(e: ChangeEvent<HTMLInputElement>) =>
              update({ limit: e.target.value === '' ? undefined : Number(e.target.value) })
            }
            onBlur={onRunQuery}
          />
        </InlineField>
      </InlineFieldRow>

      <InlineField label="Compiled pipeline" labelWidth={20} grow>
        <CodeEditor
          language="json"
          value={query.queryText || ''}
          height={160}
          showMiniMap={false}
          showLineNumbers
          readOnly
          monacoOptions={{ fontSize: 13, scrollBeyondLastLine: false }}
        />
      </InlineField>
    </div>
  );
}
