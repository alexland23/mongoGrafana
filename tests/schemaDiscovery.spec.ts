import { test, expect } from '@grafana/plugin-e2e';
import { MongoDataSourceOptions, MongoSecureJsonData } from '../src/types';

test('config editor: enabling schema discovery reveals collection filters', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource<MongoDataSourceOptions, MongoSecureJsonData>({
    fileName: 'datasources.yml',
  });
  await createDataSourceConfigPage({ type: ds.type });

  await expect(page.getByRole('textbox', { name: 'Collection filters' })).not.toBeVisible();
  await page.getByRole('switch', { name: /Enable schema discovery/i }).check({ force: true });
  await expect(page.getByRole('textbox', { name: 'Collection filters' })).toBeVisible();
});

test('query editor: collection field offers seeded collections via autocomplete', async ({
  panelEditPage,
  readProvisionedDataSource,
}) => {
  const ds = await readProvisionedDataSource({
    fileName: 'schema-discovery.yml',
  });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');

  await row.getByLabel('Collection').fill('metrics');
  await expect(row.page().getByRole('option', { name: 'metrics' })).toBeVisible();
  await row.getByLabel('Collection').press('Enter');
  await expect(row.getByLabel('Collection')).toHaveValue('metrics');
});

test('query editor: database field offers discovered databases and can be cleared', async ({
  panelEditPage,
  readProvisionedDataSource,
}) => {
  const ds = await readProvisionedDataSource({
    fileName: 'schema-discovery.yml',
  });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');

  await row.getByLabel('Database').click();
  await row.page().getByRole('option', { name: 'sampledb' }).click();
  await expect(row.getByLabel('Database')).toHaveValue('sampledb');

  await row.page().getByRole('button', { name: 'Clear value' }).click();
  await expect(row.getByLabel('Database')).toHaveValue('');
});

test('query editor: distinct field offers sampled field names via autocomplete', async ({
  panelEditPage,
  readProvisionedDataSource,
}) => {
  const ds = await readProvisionedDataSource({
    fileName: 'schema-discovery.yml',
  });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');

  await row.getByLabel('Query type').click();
  await row.page().getByRole('option', { name: 'Distinct' }).click();
  await row.getByLabel('Collection').fill('metrics');
  await row.getByLabel('Collection').press('Enter');

  await row.getByLabel('Field').click();
  await expect(row.page().getByRole('option')).not.toHaveCount(0);
});
