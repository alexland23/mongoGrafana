import React, { ChangeEvent } from 'react';
import { Button, Combobox, ComboboxOption, IconButton, InlineSwitch, Input } from '@grafana/ui';
import { AggregationOp, BuilderAggregation, BuilderGroupBy } from '../../types';

const AGG_OPS: Array<ComboboxOption<AggregationOp>> = [
  { label: 'sum', value: 'sum' },
  { label: 'avg', value: 'avg' },
  { label: 'min', value: 'min' },
  { label: 'max', value: 'max' },
  { label: 'count', value: 'count' },
];

interface Props {
  groupBy: BuilderGroupBy;
  onChange: (groupBy: BuilderGroupBy) => void;
}

export function GroupByEditor({ groupBy, onChange }: Props) {
  const set = (patch: Partial<BuilderGroupBy>) => onChange({ ...groupBy, ...patch });

  const setAgg = (index: number, patch: Partial<BuilderAggregation>) => {
    const aggregations = groupBy.aggregations.map((a, i) => (i === index ? { ...a, ...patch } : a));
    set({ aggregations });
  };

  const addAgg = () => set({ aggregations: [...groupBy.aggregations, { op: 'avg', field: '', as: '' }] });
  const removeAgg = (index: number) => set({ aggregations: groupBy.aggregations.filter((_, i) => i !== index) });

  return (
    <div>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
        <InlineSwitch
          showLabel
          label="Group by time bucket"
          value={groupBy.enabled}
          onChange={(e) => set({ enabled: e.currentTarget.checked })}
        />
      </div>
      {groupBy.enabled && (
        <>
          <div style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
            <Input
              aria-label="Group by time field"
              placeholder="time"
              width={18}
              value={groupBy.timeField}
              onChange={(e: ChangeEvent<HTMLInputElement>) => set({ timeField: e.target.value })}
            />
            <Input
              aria-label="Bucket interval"
              placeholder="$__interval (default)"
              width={22}
              value={groupBy.interval}
              onChange={(e: ChangeEvent<HTMLInputElement>) => set({ interval: e.target.value })}
            />
            <Input
              aria-label="Extra group-by field"
              placeholder="label field (optional), e.g. host"
              width={28}
              value={groupBy.labelField}
              onChange={(e: ChangeEvent<HTMLInputElement>) => set({ labelField: e.target.value })}
            />
          </div>
          {groupBy.aggregations.map((agg, i) => (
            <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
              <Combobox
                aria-label="Aggregation function"
                width={12}
                options={AGG_OPS}
                value={agg.op}
                onChange={(o) => setAgg(i, { op: o.value ?? 'avg' })}
              />
              {agg.op !== 'count' && (
                <Input
                  aria-label="Aggregation field"
                  placeholder="field.name"
                  width={20}
                  value={agg.field}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => setAgg(i, { field: e.target.value })}
                />
              )}
              <Input
                aria-label="Output field name"
                placeholder="as"
                width={16}
                value={agg.as}
                onChange={(e: ChangeEvent<HTMLInputElement>) => setAgg(i, { as: e.target.value })}
              />
              <IconButton name="trash-alt" tooltip="Remove aggregation" onClick={() => removeAgg(i)} />
            </div>
          ))}
          <Button icon="plus" variant="secondary" size="sm" onClick={addAgg}>
            Add aggregation
          </Button>
        </>
      )}
    </div>
  );
}
