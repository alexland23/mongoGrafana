import React, { ChangeEvent } from 'react';
import { Field, FieldSet, Input, SecretInput, SecretTextArea, Select, Switch, TagsInput } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps, SelectableValue } from '@grafana/data';
import { MongoDataSourceOptions, MongoSecureJsonData } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<MongoDataSourceOptions, MongoSecureJsonData> {}

const READ_PREFERENCE_OPTIONS: Array<SelectableValue<string>> = [
  { label: 'Primary (default)', value: 'primary' },
  { label: 'Primary preferred', value: 'primaryPreferred' },
  { label: 'Secondary', value: 'secondary' },
  { label: 'Secondary preferred', value: 'secondaryPreferred' },
  { label: 'Nearest', value: 'nearest' },
];

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

  const onJsonDataSwitchChange = (key: keyof MongoDataSourceOptions) => (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        [key]: event.target.checked,
      },
    });
  };

  const onCollectionFiltersChange = (tags: string[]) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        collectionFilters: tags,
      },
    });
  };

  const onReadPreferenceChange = (option: SelectableValue<string> | null) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        readPreference: option?.value,
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

  const onSecureTextChange = (key: keyof MongoSecureJsonData) => (event: ChangeEvent<HTMLTextAreaElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: {
        ...secureJsonData,
        [key]: event.target.value,
      },
    });
  };

  const onResetSecureText = (key: keyof MongoSecureJsonData) => () => {
    onOptionsChange({
      ...options,
      secureJsonFields: {
        ...secureJsonFields,
        [key]: false,
      },
      secureJsonData: {
        ...secureJsonData,
        [key]: '',
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

      <FieldSet label="TLS / Security">
        <Field label="Enable TLS" description="Encrypt the connection to MongoDB, independent of any tls URI parameter.">
          <Switch id="config-editor-tls-enabled" value={!!jsonData.tlsEnabled} onChange={onJsonDataSwitchChange('tlsEnabled')} />
        </Field>
        {jsonData.tlsEnabled && (
          <>
            <Field
              label="Skip certificate verification"
              description="Insecure. Only use for self-signed certificates in dev/test environments."
            >
              <Switch
                id="config-editor-tls-skip-verify"
                value={!!jsonData.tlsSkipVerify}
                onChange={onJsonDataSwitchChange('tlsSkipVerify')}
              />
            </Field>
            <Field label="CA certificate" description="PEM-encoded CA certificate used to verify the server. Optional.">
              <SecretTextArea
                id="config-editor-tls-ca-cert"
                isConfigured={!!secureJsonFields.tlsCaCert}
                value={secureJsonData?.tlsCaCert || ''}
                placeholder="-----BEGIN CERTIFICATE-----"
                rows={4}
                cols={60}
                onReset={onResetSecureText('tlsCaCert')}
                onChange={onSecureTextChange('tlsCaCert')}
              />
            </Field>
            <Field
              label="...or CA certificate path"
              description="Path to a PEM file on the machine running the plugin backend. Takes precedence over the pasted certificate above."
            >
              <Input
                id="config-editor-tls-ca-cert-path"
                value={jsonData.tlsCaCertPath || ''}
                placeholder="/etc/grafana/mongo/ca.pem"
                width={60}
                onChange={onJsonDataChange('tlsCaCertPath')}
              />
            </Field>
            <Field label="Client certificate" description="PEM-encoded client certificate for mutual TLS. Optional.">
              <SecretTextArea
                id="config-editor-tls-client-cert"
                isConfigured={!!secureJsonFields.tlsClientCert}
                value={secureJsonData?.tlsClientCert || ''}
                placeholder="-----BEGIN CERTIFICATE-----"
                rows={4}
                cols={60}
                onReset={onResetSecureText('tlsClientCert')}
                onChange={onSecureTextChange('tlsClientCert')}
              />
            </Field>
            <Field
              label="...or client certificate path"
              description="Path to a PEM file on the machine running the plugin backend. Takes precedence over the pasted certificate above."
            >
              <Input
                id="config-editor-tls-client-cert-path"
                value={jsonData.tlsClientCertPath || ''}
                placeholder="/etc/grafana/mongo/client.pem"
                width={60}
                onChange={onJsonDataChange('tlsClientCertPath')}
              />
            </Field>
            <Field label="Client key" description="PEM-encoded client private key for mutual TLS. Optional.">
              <SecretTextArea
                id="config-editor-tls-client-key"
                isConfigured={!!secureJsonFields.tlsClientKey}
                value={secureJsonData?.tlsClientKey || ''}
                placeholder="-----BEGIN PRIVATE KEY-----"
                rows={4}
                cols={60}
                onReset={onResetSecureText('tlsClientKey')}
                onChange={onSecureTextChange('tlsClientKey')}
              />
            </Field>
            <Field
              label="...or client key path"
              description="Path to a PEM file on the machine running the plugin backend. Takes precedence over the pasted key above."
            >
              <Input
                id="config-editor-tls-client-key-path"
                value={jsonData.tlsClientKeyPath || ''}
                placeholder="/etc/grafana/mongo/client-key.pem"
                width={60}
                onChange={onJsonDataChange('tlsClientKeyPath')}
              />
            </Field>
          </>
        )}
      </FieldSet>

      <FieldSet label="Advanced connection options">
        <Field label="Read preference" description="Defaults to primary when unset.">
          <Select
            inputId="config-editor-read-preference"
            options={READ_PREFERENCE_OPTIONS}
            value={READ_PREFERENCE_OPTIONS.find((o) => o.value === jsonData.readPreference) || null}
            isClearable
            width={30}
            onChange={onReadPreferenceChange}
          />
        </Field>
        <Field label="Connect timeout (seconds)" description="Timeout for establishing the initial server connection. Defaults to 10.">
          <Input
            id="config-editor-connect-timeout"
            type="number"
            value={jsonData.connectTimeoutSeconds || ''}
            placeholder="10"
            width={20}
            onChange={onJsonDataChange('connectTimeoutSeconds', true)}
          />
        </Field>
        <Field label="Max pool size" description="Maximum pooled connections per server. Defaults to the driver default (100).">
          <Input
            id="config-editor-max-pool-size"
            type="number"
            value={jsonData.maxPoolSize || ''}
            placeholder="100"
            width={20}
            onChange={onJsonDataChange('maxPoolSize', true)}
          />
        </Field>
      </FieldSet>

      <FieldSet label="Schema discovery">
        <Field
          label="Enable schema discovery"
          description="Lets the query and variable editors offer database/collection/field autocomplete by listing databases, listing collections and sampling documents. Off by default: on large clusters this can be slow, and admins may not want every collection exposed to dashboard authors."
        >
          <Switch
            id="config-editor-schema-discovery-enabled"
            value={!!jsonData.schemaDiscoveryEnabled}
            onChange={onJsonDataSwitchChange('schemaDiscoveryEnabled')}
          />
        </Field>
        {jsonData.schemaDiscoveryEnabled && (
          <Field
            label="Collection filters"
            description='Glob patterns matched against "database.collection", e.g. sampledb.* to allow only that database, or !*.system.* / !*_internal to deny matches. Prefix a pattern with "!" to deny; other patterns allow. No patterns means everything is allowed. Denies always win. Press enter to add each pattern.'
          >
            <TagsInput
              id="config-editor-collection-filters"
              tags={jsonData.collectionFilters || []}
              placeholder="sampledb.*"
              width={60}
              onChange={onCollectionFiltersChange}
            />
          </Field>
        )}
      </FieldSet>
    </>
  );
}
