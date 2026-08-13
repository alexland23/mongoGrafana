import React, { ChangeEvent } from 'react';
import { Combobox, ComboboxOption, IconButton, InlineField, InlineFieldRow, Input, RadioButtonGroup } from '@grafana/ui';
import { SelectableValue } from '@grafana/data';
import { BuilderFilter, FilterOperator } from '../../types';

const OPERATORS: Array<ComboboxOption<FilterOperator>> = [
  { label: '=', value: 'eq', description: 'Equals' },
  { label: '!=', value: 'ne', description: 'Not equals' },
  { label: '>', value: 'gt', description: 'Greater than' },
  { label: '>=', value: 'gte', description: 'Greater than or equal' },
  { label: '<', value: 'lt', description: 'Less than' },
  { label: '<=', value: 'lte', description: 'Less than or equal' },
  { label: 'in', value: 'in', description: 'Comma-separated list of allowed values' },
  { label: 'not in', value: 'nin', description: 'Comma-separated list of disallowed values' },
  { label: 'exists', value: 'exists', description: 'Field is present' },
  { label: 'regex', value: 'regex', description: 'Matches a regular expression' },
];

const EXISTS_OPTIONS: Array<SelectableValue<string>> = [
  { label: 'Yes', value: 'true' },
  { label: 'No', value: 'false' },
];

interface Props {
  filter: BuilderFilter;
  onChange: (filter: BuilderFilter) => void;
  onRemove: () => void;
}

export function FilterRow({ filter, onChange, onRemove }: Props) {
  const onFieldChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...filter, field: event.target.value });
  };

  const onOperatorChange = (option: ComboboxOption<FilterOperator>) => {
    onChange({ ...filter, operator: option.value ?? 'eq' });
  };

  const onValueChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...filter, value: event.target.value });
  };

  return (
    <InlineFieldRow>
      <InlineField label="Field" labelWidth={10}>
        <Input aria-label="Filter field" placeholder="field.name" width={22} value={filter.field} onChange={onFieldChange} />
      </InlineField>
      <InlineField label="Operator" labelWidth={12}>
        <Combobox aria-label="Filter operator" width={14} options={OPERATORS} value={filter.operator} onChange={onOperatorChange} />
      </InlineField>
      <InlineField label="Value" labelWidth={8} grow>
        {filter.operator === 'exists' ? (
          <RadioButtonGroup
            options={EXISTS_OPTIONS}
            value={filter.value !== 'false' ? 'true' : 'false'}
            onChange={(v) => onChange({ ...filter, value: v ?? 'true' })}
          />
        ) : (
          <Input
            aria-label="Filter value"
            placeholder={
              filter.operator === 'in' || filter.operator === 'nin' ? 'val1, val2, val3' : filter.operator === 'regex' ? '^prefix' : 'value'
            }
            width={26}
            value={filter.value}
            onChange={onValueChange}
          />
        )}
      </InlineField>
      <IconButton name="trash-alt" tooltip="Remove filter" onClick={onRemove} />
    </InlineFieldRow>
  );
}
