import React, { ChangeEvent } from 'react';
import { CodeEditor, Combobox, ComboboxOption, InlineField, InlineFieldRow, Input } from '@grafana/ui';
import { QueryEditorProps } from '@grafana/data';
import { DataSource } from '../datasource';
import { MongoDataSourceOptions, MongoQuery, QueryFormat, QueryType } from '../types';

type Props = QueryEditorProps<DataSource, MongoQuery, MongoDataSourceOptions>;

const QUERY_TYPES: Array<ComboboxOption<QueryType>> = [
  { label: 'Aggregate', value: 'aggregate', description: 'Run an aggregation pipeline (recommended)' },
  { label: 'Find', value: 'find', description: 'Find documents matching a filter' },
  { label: 'Count', value: 'count', description: 'Count documents matching a filter' },
  { label: 'Distinct', value: 'distinct', description: 'Distinct values of a field' },
  { label: 'Command', value: 'command', description: 'Run a raw database command' },
];

const FORMATS: Array<ComboboxOption<QueryFormat>> = [
  { label: 'Table', value: 'table', description: 'Return rows as-is' },
  { label: 'Time series', value: 'timeseries', description: 'Convert time + value (+ label) rows into series' },
  { label: 'Logs', value: 'logs', description: 'Render results in the logs visualization' },
];

const EDITOR_LABEL: Record<QueryType, string> = {
  aggregate: 'Pipeline (extended JSON array)',
  find: 'Filter (extended JSON)',
  count: 'Filter (extended JSON)',
  distinct: 'Filter (extended JSON)',
  command: 'Command document (extended JSON)',
};

export function QueryEditor({ query, onChange, onRunQuery }: Props) {
  const queryType = query.queryType ?? 'aggregate';

  const onTextInput =
    (key: 'collection' | 'database' | 'field' | 'projection' | 'sort') => (event: ChangeEvent<HTMLInputElement>) => {
      onChange({ ...query, [key]: event.target.value });
    };

  const onNumberInput = (key: 'limit' | 'skip') => (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, [key]: event.target.value === '' ? undefined : Number(event.target.value) });
  };

  const onQueryTextChange = (value: string) => {
    onChange({ ...query, queryText: value });
    onRunQuery();
  };

  return (
    <>
      <InlineFieldRow>
        <InlineField label="Query type" labelWidth={14}>
          <Combobox
            id="query-editor-query-type"
            options={QUERY_TYPES}
            value={queryType}
            width={24}
            onChange={(v) => {
              onChange({ ...query, queryType: v.value });
            }}
          />
        </InlineField>
        {queryType !== 'command' && (
          <InlineField label="Collection" labelWidth={12} tooltip="Collection to query. Supports dashboard variables.">
            <Input
              id="query-editor-collection"
              value={query.collection || ''}
              placeholder="collection"
              width={24}
              onChange={onTextInput('collection')}
              onBlur={onRunQuery}
            />
          </InlineField>
        )}
        <InlineField label="Format" labelWidth={10}>
          <Combobox
            id="query-editor-format"
            options={FORMATS}
            value={query.format ?? 'table'}
            width={20}
            onChange={(v) => {
              onChange({ ...query, format: v.value });
              onRunQuery();
            }}
          />
        </InlineField>
        <InlineField label="Database" labelWidth={12} tooltip="Optional override of the datasource default database.">
          <Input
            id="query-editor-database"
            value={query.database || ''}
            placeholder="(default)"
            width={20}
            onChange={onTextInput('database')}
            onBlur={onRunQuery}
          />
        </InlineField>
      </InlineFieldRow>

      {queryType === 'distinct' && (
        <InlineFieldRow>
          <InlineField label="Field" labelWidth={14} tooltip="Field to collect distinct values of.">
            <Input
              id="query-editor-field"
              value={query.field || ''}
              placeholder="field.name"
              width={30}
              onChange={onTextInput('field')}
              onBlur={onRunQuery}
            />
          </InlineField>
        </InlineFieldRow>
      )}

      {queryType === 'find' && (
        <InlineFieldRow>
          <InlineField label="Projection" labelWidth={14} tooltip='e.g. {"name": 1, "_id": 0}'>
            <Input
              id="query-editor-projection"
              value={query.projection || ''}
              placeholder="{}"
              width={26}
              onChange={onTextInput('projection')}
              onBlur={onRunQuery}
            />
          </InlineField>
          <InlineField label="Sort" labelWidth={8} tooltip='e.g. {"time": 1}'>
            <Input
              id="query-editor-sort"
              value={query.sort || ''}
              placeholder="{}"
              width={22}
              onChange={onTextInput('sort')}
              onBlur={onRunQuery}
            />
          </InlineField>
          <InlineField label="Limit" labelWidth={8}>
            <Input
              id="query-editor-limit"
              type="number"
              value={query.limit ?? ''}
              placeholder="∞"
              width={12}
              onChange={onNumberInput('limit')}
              onBlur={onRunQuery}
            />
          </InlineField>
          <InlineField label="Skip" labelWidth={8}>
            <Input
              id="query-editor-skip"
              type="number"
              value={query.skip ?? ''}
              placeholder="0"
              width={12}
              onChange={onNumberInput('skip')}
              onBlur={onRunQuery}
            />
          </InlineField>
        </InlineFieldRow>
      )}

      <InlineFieldRow>
        <InlineField
          label={EDITOR_LABEL[queryType]}
          labelWidth={30}
          grow
          tooltip="MongoDB extended JSON. Macros: $__timeFrom, $__timeTo, $__timeFrom_ms, $__timeTo_ms, $__interval_ms, $__maxDataPoints. Dashboard variables like $variable are supported. Press Cmd/Ctrl+S or blur the editor to run."
        >
          <CodeEditor
            language="json"
            value={query.queryText || ''}
            height={200}
            showMiniMap={false}
            showLineNumbers
            onBlur={onQueryTextChange}
            onSave={onQueryTextChange}
            monacoOptions={{ fontSize: 13, scrollBeyondLastLine: false }}
          />
        </InlineField>
      </InlineFieldRow>
    </>
  );
}
