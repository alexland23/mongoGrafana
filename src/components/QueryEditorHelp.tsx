import React from 'react';
import { QueryEditorHelpProps } from '@grafana/data';
import { Button, Card } from '@grafana/ui';
import { MongoQuery } from '../types';

interface Example {
  title: string;
  description: string;
  target: Partial<MongoQuery>;
}

/**
 * Cheat sheet shown in Explore when the query editor is empty. Examples target the collections
 * `dev/mongo-seed.js` seeds into `sampledb` (metrics, orders, logs), so they run out of the box
 * against the bundled dev stack.
 */
const EXAMPLES: Example[] = [
  {
    title: 'Average CPU per host over time',
    description: 'Aggregate: buckets the metrics collection into a time series, one series per host.',
    target: {
      queryType: 'aggregate',
      collection: 'metrics',
      format: 'timeseries',
      editorMode: 'code',
      queryText: `[
  { "$match": $__timeFilter(time) },
  { "$group": {
      "_id": { "host": "$host", "t": $__timeGroup(time, "5m") },
      "avg_cpu": { "$avg": "$cpu" }
  } },
  { "$project": { "_id": 0, "time": "$_id.t", "host": "$_id.host", "avg_cpu": 1 } },
  { "$sort": { "time": 1 } }
]`,
    },
  },
  {
    title: 'Orders by status',
    description: 'Aggregate: counts orders in the current time range grouped by status, rendered as a table.',
    target: {
      queryType: 'aggregate',
      collection: 'orders',
      format: 'table',
      editorMode: 'code',
      queryText: `[
  { "$match": { "createdAt": $__timeFilter(createdAt) } },
  { "$group": { "_id": "$status", "orders": { "$sum": 1 }, "totalAmount": { "$sum": "$amount" } } },
  { "$sort": { "orders": -1 } }
]`,
    },
  },
  {
    title: 'Recent gold-tier orders',
    description: 'Find: filters orders with a nested-field match and sorts by newest first.',
    target: {
      queryType: 'find',
      collection: 'orders',
      format: 'table',
      editorMode: 'code',
      queryText: `{ "customer.tier": "gold" }`,
      sort: `{ "createdAt": -1 }`,
      limit: 50,
    },
  },
  {
    title: 'Error and warning logs',
    description: 'Find: filters the logs collection to warn/error events, rendered in the logs visualization.',
    target: {
      queryType: 'find',
      collection: 'logs',
      format: 'logs',
      editorMode: 'code',
      queryText: `{ "level": { "$in": ["warn", "error"] } }`,
      sort: `{ "time": -1 }`,
    },
  },
  {
    title: 'Distinct log services',
    description: 'Distinct: lists the unique values of a field -- handy for populating a dashboard variable.',
    target: {
      queryType: 'distinct',
      collection: 'logs',
      format: 'table',
      editorMode: 'code',
      field: 'service',
    },
  },
];

export function QueryEditorHelp({ query, onClickExample }: QueryEditorHelpProps<MongoQuery>) {
  return (
    <div>
      <p>
        Sample queries against the collections seeded into <code>sampledb</code> by the bundled dev stack
        (<code>metrics</code>, <code>orders</code>, <code>logs</code>). Click a card to load it into the editor, then
        adjust the collection/database if you&apos;re pointed at your own data.
      </p>
      {EXAMPLES.map((example) => (
        <Card key={example.title} onClick={() => onClickExample({ ...query, ...example.target })}>
          <Card.Heading>{example.title}</Card.Heading>
          <Card.Description>{example.description}</Card.Description>
          <Card.Actions>
            <Button
              size="sm"
              variant="secondary"
              icon="arrow-right"
              onClick={(e) => {
                e.stopPropagation();
                onClickExample({ ...query, ...example.target });
              }}
            >
              Use this query
            </Button>
          </Card.Actions>
        </Card>
      ))}
    </div>
  );
}
