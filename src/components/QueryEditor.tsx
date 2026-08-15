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
import { DEFAULT_BUILDER_STATE, EditorMode, MongoDataSourceOptions, MongoQuery, QueryFormat, QueryType } from '../types';
import { QueryBuilder } from './QueryBuilder';
import { compileBuilderState } from './QueryBuilder/compile';

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
  { label: 'Long', value: 'long', description: 'Time-sorted long rows (time + value + labels), no wide conversion' },
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

// The visual Builder compiles to an aggregation pipeline ("aggregate") or a bare filter document
// ("find"/"count"/"distinct"); "command" takes an arbitrary command document Builder can't produce.
const isBuilderEligible = (queryType: QueryType): boolean => queryType !== 'command';

// Only "find" and "aggregate" have a skip/limit concept: runFind sets them as cursor options,
// and runAggregate appends them as trailing $skip/$limit pipeline stages (pkg/plugin/safety.go's
// applyAggregatePaging). "count"/"distinct"/"command" don't return a page-able result set.
const isPagingEligible = (queryType: QueryType): boolean => queryType === 'find' || queryType === 'aggregate';

const EDITOR_MODES: Array<SelectableValue<EditorMode>> = [
  { label: 'Code', value: 'code' },
  { label: 'Builder', value: 'builder' },
];

export function QueryEditor({ query, onChange, onRunQuery, datasource, data }: Props) {
  const queryType = query.queryType ?? 'find';
  const editorMode = query.editorMode ?? 'code';
  const discoveryEnabled = datasource.schemaDiscoveryEnabled;

  // Document count of this query's own result, used to disable "Next page" once the last page
  // came back shorter than the configured limit (i.e. there's nothing left to page into). Read
  // from frame.meta.custom.documentCount (set by the backend's buildDocsFrame) rather than the
  // frame's row count directly: for format="timeseries" LongToWide pivots same-timestamp rows
  // from multiple series into a single row, so the frame's row count can undercount documents.
  const lastResultFrame = data?.series.find((frame) => frame.refId === query.refId);
  const lastResultRowCount: number | undefined =
    (lastResultFrame?.meta?.custom?.documentCount as number | undefined) ?? lastResultFrame?.length;
  const pageSize = query.limit ?? 0;
  const pageSkip = query.skip ?? 0;
  const onPageChange = (direction: 1 | -1) => {
    const nextSkip = Math.max(0, pageSkip + direction * pageSize);
    onChange({ ...query, skip: nextSkip || undefined });
    onRunQuery();
  };

  const onEditorModeChange = (mode: EditorMode) => {
    onChange({ ...query, editorMode: mode });
    onRunQuery();
  };

  const onTextInput =
    (key: 'collection' | 'database' | 'field' | 'projection' | 'sort' | 'messageField' | 'levelField') =>
    (event: ChangeEvent<HTMLInputElement>) => {
      onChange({ ...query, [key]: event.target.value });
    };

  const onNumberInput = (key: 'limit' | 'skip' | 'flattenDepth') => (event: ChangeEvent<HTMLInputElement>) => {
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

  // Some selections here (query type, format) add or remove whole
  // InlineFieldRows -- e.g. switching off "find" drops the Projection/Sort/
  // Limit/Skip row, switching to "logs" adds the Message/Level row. Applying
  // that layout change synchronously, inside the Combobox's own
  // onSelectedItemChange, races with the popover's closing transition and
  // its floating-ui reference tracking, which in Explore's query-row host
  // manifests as a runaway re-render loop (React error #185). Deferring the
  // query update to the next tick -- same trick as deferredRunQuery above --
  // lets the popover finish closing first.
  const deferredOnChange = (next: MongoQuery) => setTimeout(() => onChange(next), 0);

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
              // "command" has no Builder support; leaving it for a builder-eligible type is fine
              // as-is, but moving *to* it while in Builder mode must drop back to Code.
              const nextEditorMode = isBuilderEligible(v.value) ? editorMode : 'code';
              deferredOnChange({
                ...query,
                queryType: v.value,
                editorMode: nextEditorMode,
                // Builder compiles to a pipeline for "aggregate" and a bare filter document
                // otherwise; re-compile so the query type change doesn't leave queryText in the
                // wrong shape for the new type.
                queryText: nextEditorMode === 'builder' ? compileBuilderState(v.value, query.builder ?? DEFAULT_BUILDER_STATE) : query.queryText,
                liveStreaming: isLiveEligible(v.value) ? query.liveStreaming : false,
              });
            }}
          />
        </InlineField>
        {isBuilderEligible(queryType) && (
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
              deferredOnChange({ ...query, format: v.value, liveStreaming: v.value === 'logs' ? query.liveStreaming : false });
              deferredRunQuery();
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
        <InlineField label="Database" labelWidth={16} tooltip="Optional override of the datasource default database.">
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
        <InlineField
          label="Flatten depth"
          labelWidth={18}
          tooltip='Levels of nested documents to flatten into dot-notation columns before keeping the rest as a single raw JSON column. Blank flattens every level.'
        >
          <Input
            id="query-editor-flatten-depth"
            type="number"
            value={query.flattenDepth ?? ''}
            placeholder="∞"
            width={12}
            onChange={onNumberInput('flattenDepth')}
            onBlur={onRunQuery}
          />
        </InlineField>
      </InlineFieldRow>

      {query.format === 'logs' && (
        <InlineFieldRow>
          <InlineField
            label="Message field"
            labelWidth={18}
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
            labelWidth={16}
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

      {isPagingEligible(queryType) && (
        <InlineFieldRow>
          {queryType === 'find' && (
            <>
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
            </>
          )}
          <InlineField
            label="Limit"
            labelWidth={8}
            tooltip={
              queryType === 'aggregate' ? 'Documents to return, appended as a trailing $limit stage.' : undefined
            }
          >
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
          <InlineField
            label="Skip"
            labelWidth={8}
            tooltip={
              queryType === 'aggregate'
                ? 'Documents to skip, appended as a leading $skip stage before the limit.'
                : undefined
            }
          >
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
          <InlineField label="Page" labelWidth={6} tooltip="Step skip backward/forward by the current limit.">
            <>
              <Button
                id="query-editor-prev-page"
                icon="angle-left"
                variant="secondary"
                size="md"
                disabled={!pageSize || pageSkip <= 0}
                tooltip="Previous page"
                onClick={() => onPageChange(-1)}
              />
              <Button
                id="query-editor-next-page"
                icon="angle-right"
                variant="secondary"
                size="md"
                disabled={!pageSize || (lastResultRowCount !== undefined && lastResultRowCount < pageSize)}
                tooltip="Next page"
                onClick={() => onPageChange(1)}
              />
            </>
          </InlineField>
        </InlineFieldRow>
      )}

      {isBuilderEligible(queryType) && editorMode === 'builder' ? (
        <QueryBuilder query={query} onChange={onChange} onRunQuery={onRunQuery} />
      ) : (
        <InlineFieldRow>
          <InlineField
            label={EDITOR_LABEL[queryType]}
            labelWidth={34}
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
