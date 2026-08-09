import React, { ChangeEvent } from 'react';
import { Field, FieldSet, Input, SecretInput } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { MongoDataSourceOptions, MongoSecureJsonData } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<MongoDataSourceOptions, MongoSecureJsonData> {}

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData, secureJsonFields, secureJsonData } = options;

  const onJsonDataChange =
    (key: keyof MongoDataSourceOptions, isNumber = false) =>
    (event: ChangeEvent<HTMLInputElement>) => {
      onOptionsChange({
        ...options,
        jsonData: {
          ...jsonData,
          [key]: isNumber ? Number(event.target.value) : event.target.value,
        },
      });
    };

  const onPasswordChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: {
        ...secureJsonData,
        password: event.target.value,
      },
    });
  };

  const onResetPassword = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: {
        ...secureJsonFields,
        password: false,
      },
      secureJsonData: {
        ...secureJsonData,
        password: '',
      },
    });
  };

  return (
    <>
      <FieldSet label="Connection">
        <Field
          label="Connection string"
          description="MongoDB URI. Options like tls, replicaSet and authSource can be set as URI parameters."
          required
        >
          <Input
            id="config-editor-connection-string"
            value={jsonData.connectionString || ''}
            placeholder="mongodb://localhost:27017"
            width={60}
            onChange={onJsonDataChange('connectionString')}
          />
        </Field>
        <Field label="Default database" description="Database queries run against unless overridden per query.">
          <Input
            id="config-editor-database"
            value={jsonData.database || ''}
            placeholder="mydb"
            width={40}
            onChange={onJsonDataChange('database')}
          />
        </Field>
        <Field label="Query timeout (seconds)" description="Maximum execution time per query. Defaults to 30.">
          <Input
            id="config-editor-timeout"
            type="number"
            value={jsonData.timeoutSeconds || ''}
            placeholder="30"
            width={20}
            onChange={onJsonDataChange('timeoutSeconds', true)}
          />
        </Field>
      </FieldSet>

      <FieldSet label="Authentication">
        <Field label="Username" description="Leave empty to connect without authentication.">
          <Input
            id="config-editor-username"
            value={jsonData.username || ''}
            placeholder="username"
            width={40}
            onChange={onJsonDataChange('username')}
          />
        </Field>
        <Field label="Password" description="Stored encrypted and only sent to the plugin backend.">
          <SecretInput
            id="config-editor-password"
            isConfigured={!!secureJsonFields.password}
            value={secureJsonData?.password || ''}
            placeholder="password"
            width={40}
            onReset={onResetPassword}
            onChange={onPasswordChange}
          />
        </Field>
      </FieldSet>
    </>
  );
}
