import React, { ChangeEvent, useCallback, useEffect, useRef, useState } from 'react';
import {
  Alert,
  Button,
  CodeEditor,
  Combobox,
  ComboboxOption,
  InlineField,
  InlineFieldRow,
  InlineSwitch,
  Input,
  Modal,
  RadioButtonGroup,
} from '@grafana/ui';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import { DataSource } from '../datasource';
import { EditorMode, MongoDataSourceOptions, MongoQuery, QueryFormat, QueryType } from '../types';
import { QueryBuilder } from './QueryBuilder';

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

const toOptions = (values: string[]): Array<ComboboxOption<string>> => values.map((v) => ({ label: v, value: v }));

// Live tailing reuses parseDocument on the backend, which only understands a
// plain filter document — not an aggregation pipeline ("aggregate") or an
// arbitrary command document ("command").
const isLiveEligible = (queryType: QueryType): boolean =>
  queryType === 'find' || queryType === 'count' || queryType === 'distinct';

// Explain reuses the backend's parseDocument/parsePipeline, which only understand a plain filter
// document ("find") or an aggregation pipeline ("aggregate") -- not "count"/"distinct" (bare
// filters with no query plan of their own) or "command" (arbitrary, not necessarily explainable).
const isExplainEligible = (queryType: QueryType): boolean => queryType === 'aggregate' || queryType === 'find';

const EDITOR_MODES: Array<SelectableValue<EditorMode>> = [
  { label: 'Code', value: 'code' },
  { label: 'Builder', value: 'builder' },
];

export function QueryEditor({ query, onChange, onRunQuery, datasource }: Props) {
  const queryType = query.queryType ?? 'aggregate';
  const editorMode = query.editorMode ?? 'code';
  const discoveryEnabled = datasource.schemaDiscoveryEnabled;

  const onEditorModeChange = (mode: EditorMode) => {
    // Builder state only compiles to an aggregation pipeline; switching in forces that query type,
    // which isn't live-eligible, so drop any stale Live toggle along with it.
    const nextQueryType = mode === 'builder' ? 'aggregate' : query.queryType;
    onChange({
      ...query,
      editorMode: mode,
      queryType: nextQueryType,
      liveStreaming: isLiveEligible(nextQueryType ?? 'aggregate') ? query.liveStreaming : false,
    });
    onRunQuery();
  };

  const onTextInput =
    (key: 'collection' | 'database' | 'field' | 'projection' | 'sort' | 'messageField' | 'levelField') =>
    (event: ChangeEvent<HTMLInputElement>) => {
      onChange({ ...query, [key]: event.target.value });
    };

  const onNumberInput = (key: 'limit' | 'skip') => (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, [key]: event.target.value === '' ? undefined : Number(event.target.value) });
  };

  const onQueryTextChange = (value: string) => {
    onChange({ ...query, queryText: value });
    onRunQuery();
  };

  const [explainOpen, setExplainOpen] = useState(false);
  const [explainLoading, setExplainLoading] = useState(false);
  const [explainResult, setExplainResult] = useState<string | null>(null);
  const [explainError, setExplainError] = useState<string | null>(null);

  const onExplain = async () => {
    setExplainOpen(true);
    setExplainLoading(true);
    setExplainError(null);
    setExplainResult(null);
    try {
      const plan = await datasource.explainQuery(query);
      setExplainResult(JSON.stringify(plan, null, 2));
    } catch (e) {
      setExplainError(e instanceof Error ? e.message : String(e));
    } finally {
      setExplainLoading(false);
    }
  };

  // Async Comboboxes (unlike the static ones above) run their own state
  // transition inside onSelectedItemChange; triggering onRunQuery
  // synchronously from within that handler races with it. Deferring to the
  // next tick lets the selection settle first.
  const deferredRunQuery = () => setTimeout(onRunQuery, 0);

  // Combobox re-fetches whenever the async `options` function it's given
  // changes identity, so these must stay referentially stable across
  // renders (an unstable reference can otherwise feed back into a render
  // loop). Reading current values from a ref keeps a single stable
  // function while always seeing the latest datasource/query.
  const latest = useRef({ datasource, database: query.database, collection: query.collection });
  useEffect(() => {
    latest.current = { datasource, database: query.database, collection: query.collection };
  });

  const loadDatabases = useCallback(async () => {
    try {
      return toOptions(await latest.current.datasource.getDatabases());
    } catch {
      return [];
    }
  }, []);

  const loadCollections = useCallback(async () => {
    try {
      return toOptions(await latest.current.datasource.getCollections(latest.current.database));
    } catch {
      return [];
    }
  }, []);

  const loadFields = useCallback(async () => {
    const { datasource, database, collection } = latest.current;
    if (!collection) {
      return [];
    }
    try {
      return toOptions(await datasource.getFields(database, collection));
    } catch {
      return [];
    }
  }, []);

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
              // Builder state only compiles to an aggregation pipeline; leaving "aggregate" drops back to Code.
              onChange({
                ...query,
                queryType: v.value,
                editorMode: v.value === 'aggregate' ? query.editorMode : 'code',
                liveStreaming: isLiveEligible(v.value) ? query.liveStreaming : false,
              });
            }}
          />
        </InlineField>
        {queryType === 'aggregate' && (
          <InlineField label="Editor" labelWidth={10}>
            <RadioButtonGroup options={EDITOR_MODES} value={editorMode} onChange={(v) => onEditorModeChange(v ?? 'code')} />
          </InlineField>
        )}
        {queryType !== 'command' && (
          <InlineField label="Collection" labelWidth={12} tooltip="Collection to query. Supports dashboard variables.">
            {discoveryEnabled ? (
              <Combobox
                id="query-editor-collection"
                options={loadCollections}
                value={query.collection || null}
                placeholder="collection"
                width={24}
                createCustomValue
                onChange={(v) => {
                  onChange({ ...query, collection: v.value });
                  deferredRunQuery();
                }}
              />
            ) : (
              <Input
                id="query-editor-collection"
                value={query.collection || ''}
                placeholder="collection"
                width={24}
                onChange={onTextInput('collection')}
                onBlur={onRunQuery}
              />
            )}
          </InlineField>
        )}
        <InlineField label="Format" labelWidth={10}>
          <Combobox
            id="query-editor-format"
            options={FORMATS}
            value={query.format ?? 'table'}
            width={20}
            onChange={(v) => {
              // Live tailing only makes sense for the logs visualization.
              onChange({ ...query, format: v.value, liveStreaming: v.value === 'logs' ? query.liveStreaming : false });
              onRunQuery();
            }}
          />
        </InlineField>
        {query.format === 'logs' && isLiveEligible(queryType) && (
          <InlineField label="Live" labelWidth={8} tooltip="Tail the collection for new matching documents instead of running a one-shot query.">
            <InlineSwitch
              id="query-editor-live-streaming"
              value={!!query.liveStreaming}
              onChange={(e) => {
                onChange({ ...query, liveStreaming: e.currentTarget.checked });
                onRunQuery();
              }}
            />
          </InlineField>
        )}
        <InlineField label="Database" labelWidth={12} tooltip="Optional override of the datasource default database.">
          {discoveryEnabled ? (
            <Combobox
              id="query-editor-database"
              options={loadDatabases}
              value={query.database || null}
              placeholder="(default)"
              width={20}
              createCustomValue
              isClearable
              onChange={(v) => {
                onChange({ ...query, database: v?.value });
                deferredRunQuery();
              }}
            />
          ) : (
            <Input
              id="query-editor-database"
              value={query.database || ''}
              placeholder="(default)"
              width={20}
              onChange={onTextInput('database')}
              onBlur={onRunQuery}
            />
          )}
        </InlineField>
      </InlineFieldRow>

      {query.format === 'logs' && (
        <InlineFieldRow>
          <InlineField
            label="Message field"
            labelWidth={14}
            tooltip='Document field to treat as the log line. Blank uses the "message" convention.'
          >
            <Input
              id="query-editor-message-field"
              value={query.messageField || ''}
              placeholder="message"
              width={20}
              onChange={onTextInput('messageField')}
              onBlur={onRunQuery}
            />
          </InlineField>
          <InlineField
            label="Level field"
            labelWidth={12}
            tooltip='Document field to treat as the log level. Blank uses the "level" convention.'
          >
            <Input
              id="query-editor-level-field"
              value={query.levelField || ''}
              placeholder="level"
              width={20}
              onChange={onTextInput('levelField')}
              onBlur={onRunQuery}
            />
          </InlineField>
        </InlineFieldRow>
      )}

      {queryType === 'distinct' && (
        <InlineFieldRow>
          <InlineField label="Field" labelWidth={14} tooltip="Field to collect distinct values of.">
            {discoveryEnabled ? (
              <Combobox
                id="query-editor-field"
                options={loadFields}
                value={query.field || null}
                placeholder="field.name"
                width={30}
                createCustomValue
                onChange={(v) => {
                  onChange({ ...query, field: v.value });
                  deferredRunQuery();
                }}
              />
            ) : (
              <Input
                id="query-editor-field"
                value={query.field || ''}
                placeholder="field.name"
                width={30}
                onChange={onTextInput('field')}
                onBlur={onRunQuery}
              />
            )}
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

      {queryType === 'aggregate' && editorMode === 'builder' ? (
        <QueryBuilder query={query} onChange={onChange} onRunQuery={onRunQuery} />
      ) : (
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
      )}

      {isExplainEligible(queryType) && editorMode !== 'builder' && (
        <InlineFieldRow>
          <Button
            icon="info-circle"
            variant="secondary"
            size="sm"
            onClick={onExplain}
            tooltip="Show the MongoDB query plan for this query. Dashboard time macros are not substituted."
          >
            Explain
          </Button>
        </InlineFieldRow>
      )}

      <Modal title="Query plan" isOpen={explainOpen} onDismiss={() => setExplainOpen(false)}>
        {explainLoading && <div>Running explain…</div>}
        {explainError && (
          <Alert title="Explain failed" severity="error">
            {explainError}
          </Alert>
        )}
        {explainResult && (
          <CodeEditor
            language="json"
            value={explainResult}
            height={400}
            showMiniMap={false}
            showLineNumbers
            readOnly
            monacoOptions={{ fontSize: 12, scrollBeyondLastLine: false }}
          />
        )}
      </Modal>
    </>
  );
}
