import { test, expect } from '@grafana/plugin-e2e';
import type { Locator } from '@playwright/test';

// Monaco's visible container isn't itself focusable -- the actual keyboard
// target is its inner "inputarea" textarea. Clicking the container can land
// outside that textarea, leaving keystrokes to fall through to Grafana's
// global keyboard shortcuts instead of the editor.
async function setPipelineText(editor: Locator, text: string) {
  const input = editor.locator('textarea');
  await input.click();
  await input.press('ControlOrMeta+A');
  await input.pressSequentially(text, { delay: 10 });
}

test('smoke: should render query editor', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  await expect(panelEditPage.getQueryEditorRow('A').getByLabel('Collection')).toBeVisible();
});

test('count query should return number of seeded documents', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');
  await row.getByLabel('Query type').click();
  await row.page().getByRole('option', { name: 'Count' }).click();
  await row.getByLabel('Collection').fill('metrics');
  await panelEditPage.setVisualization('Table');
  await expect(panelEditPage.refreshPanel()).toBeOK();
  await expect(panelEditPage.panel.fieldNames).toContainText(['count']);
});

test('aggregate query using a blocked operator is rejected', async ({
  panelEditPage,
  readProvisionedDataSource,
  selectors,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');
  await row.getByLabel('Query type').click();
  await row.page().getByRole('option', { name: 'Aggregate' }).click();
  await row.getByLabel('Collection').fill('metrics');
  const editor = panelEditPage.getByGrafanaSelector(selectors.components.CodeEditor.container);
  await setPipelineText(editor, '[{"$out": "pwned"}]');
  await panelEditPage.setVisualization('Table');
  await expect(panelEditPage.refreshPanel()).not.toBeOK();
  await expect(row.page().getByText('is not permitted by this datasource\'s safety settings')).toBeVisible();
});

test('builder mode compiles a bare filter document for a count query', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');
  await row.getByLabel('Query type').click();
  await row.page().getByRole('option', { name: 'Count' }).click();
  await row.getByRole('radio', { name: 'Builder' }).click({ force: true });
  // "Group by" only applies to aggregate pipelines, not the bare filter document count/find/distinct compile to.
  await expect(row.getByText('Group by')).not.toBeVisible();
  await row.getByLabel('Collection').fill('metrics');
  await row.getByRole('button', { name: 'Add filter' }).click();
  await row.getByLabel('Filter field').fill('host');
  await row.getByLabel('Filter value').fill('web-01');
  await panelEditPage.setVisualization('Table');
  await expect(panelEditPage.refreshPanel()).toBeOK();
  await expect(panelEditPage.panel.fieldNames).toContainText(['count']);
});

test('explain shows a query plan for an aggregate query', async ({ panelEditPage, readProvisionedDataSource, selectors }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');
  await row.getByLabel('Query type').click();
  await row.page().getByRole('option', { name: 'Aggregate' }).click();
  await row.getByLabel('Collection').fill('metrics');
  const editor = panelEditPage.getByGrafanaSelector(selectors.components.CodeEditor.container);
  await setPipelineText(editor, '[{"$match": {}}]');
  await row.getByRole('button', { name: 'Explain' }).click();
  await expect(row.page().getByRole('dialog', { name: 'Query plan' })).toBeVisible();
  await expect(row.page().getByText('"queryPlanner"')).toBeVisible();
});

// Regression test for https://github.com/alexland23/mongoGrafana/issues/46 -- switching to a
// query type or format that drops/adds a whole InlineFieldRow used to crash Explore with React
// error #185 (infinite update loop) because the resulting layout shift raced with the "Query
// type"/"Format" Combobox's own closing transition. Only reproduced in Explore, not panel edit,
// so this needs explorePage rather than panelEditPage.
test('switching query type in Explore does not crash the page', async ({ explorePage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await explorePage.goto();
  await explorePage.datasource.set(ds.name);
  const row = explorePage.getQueryEditorRow('A');

  await row.getByLabel('Query type').click();
  await row.page().getByRole('option', { name: 'Distinct' }).click();
  await expect(row.getByLabel('Field')).toBeVisible();

  await row.getByLabel('Query type').click();
  await row.page().getByRole('option', { name: 'Command' }).click();
  await expect(row.getByText('Command document (extended JSON)')).toBeVisible();
  await expect(row.getByLabel('Collection')).not.toBeVisible();

  await expect(row.page().getByText('An unexpected error happened')).not.toBeVisible();
});
