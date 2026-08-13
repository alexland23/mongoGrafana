import React, { ChangeEvent } from 'react';
import { IconButton, Input, RadioButtonGroup } from '@grafana/ui';
import { SelectableValue } from '@grafana/data';
import { BuilderSort } from '../../types';

const DIRECTIONS: Array<SelectableValue<'asc' | 'desc'>> = [
  { label: 'Ascending', value: 'asc' },
  { label: 'Descending', value: 'desc' },
];

interface Props {
  sort: BuilderSort;
  onChange: (sort: BuilderSort) => void;
  onRemove: () => void;
}

export function SortRow({ sort, onChange, onRemove }: Props) {
  const onFieldChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...sort, field: event.target.value });
  };

  return (
    <div style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
      <Input aria-label="Sort field" placeholder="field.name" width={22} value={sort.field} onChange={onFieldChange} />
      <RadioButtonGroup
        options={DIRECTIONS}
        value={sort.direction}
        onChange={(v) => onChange({ ...sort, direction: v ?? 'asc' })}
      />
      <IconButton name="trash-alt" tooltip="Remove sort" onClick={onRemove} />
    </div>
  );
}
