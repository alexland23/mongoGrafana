// Seeds sampledb with development data. Runs once on first container start
// (delete the container to re-seed). Timestamps are generated relative to
// "now" so dashboard time ranges like "Last 6 hours" show data.
/* global db */

db = db.getSiblingDB('sampledb');

const now = Date.now();
const hosts = ['web-01', 'web-02', 'db-01'];
const metrics = [];

// 6 hours of per-minute CPU/memory metrics per host.
for (let i = 0; i < 360; i++) {
  const ts = new Date(now - i * 60 * 1000);
  for (const host of hosts) {
    metrics.push({
      time: ts,
      host: host,
      cpu: Math.round((30 + 25 * Math.sin(i / 20) + Math.random() * 15) * 100) / 100,
      memory: Math.round((50 + 10 * Math.cos(i / 35) + Math.random() * 8) * 100) / 100,
    });
  }
}
db.metrics.insertMany(metrics);
db.metrics.createIndex({ time: 1 });

// A small orders collection for table/aggregation demos.
const statuses = ['pending', 'shipped', 'delivered', 'cancelled'];
const regions = ['us-east', 'us-west', 'eu-central'];
const orders = [];
for (let i = 0; i < 500; i++) {
  orders.push({
    orderId: 'ORD-' + String(1000 + i),
    createdAt: new Date(now - Math.floor(Math.random() * 6 * 60 * 60 * 1000)),
    status: statuses[Math.floor(Math.random() * statuses.length)],
    region: regions[Math.floor(Math.random() * regions.length)],
    amount: Math.round(Math.random() * 50000) / 100,
    items: Math.floor(Math.random() * 5) + 1,
    customer: {
      name: 'Customer ' + (i % 50),
      tier: i % 10 === 0 ? 'gold' : 'standard',
    },
  });
}
db.orders.insertMany(orders);
db.orders.createIndex({ createdAt: 1 });

// Log-style events for the logs format.
const levels = ['info', 'info', 'info', 'warn', 'error'];
const logs = [];
for (let i = 0; i < 300; i++) {
  const level = levels[Math.floor(Math.random() * levels.length)];
  logs.push({
    time: new Date(now - Math.floor(Math.random() * 6 * 60 * 60 * 1000)),
    level: level,
    service: hosts[Math.floor(Math.random() * hosts.length)],
    message: level === 'error' ? 'request failed with status 500' : 'request handled in ' + Math.floor(Math.random() * 400) + 'ms',
  });
}
db.logs.insertMany(logs);
db.logs.createIndex({ time: 1 });
