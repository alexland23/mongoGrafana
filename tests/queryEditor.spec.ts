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
  await row.getByLabel('Collection').fill('metrics');
  const editor = panelEditPage.getByGrafanaSelector(selectors.components.CodeEditor.container);
  await setPipelineText(editor, '[{"$out": "pwned"}]');
  await panelEditPage.setVisualization('Table');
  await expect(panelEditPage.refreshPanel()).not.toBeOK();
  await expect(row.page().getByText('is not permitted by this datasource\'s safety settings')).toBeVisible();
});

test('explain shows a query plan for an aggregate query', async ({ panelEditPage, readProvisionedDataSource, selectors }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');
  await row.getByLabel('Collection').fill('metrics');
  const editor = panelEditPage.getByGrafanaSelector(selectors.components.CodeEditor.container);
  await setPipelineText(editor, '[{"$match": {}}]');
  await row.getByRole('button', { name: 'Explain' }).click();
  await expect(row.page().getByRole('dialog', { name: 'Query plan' })).toBeVisible();
  await expect(row.page().getByText('"queryPlanner"')).toBeVisible();
});
