import { BuilderFilter, QueryBuilderState } from '../../types';

/** Marks a value that must be embedded unquoted in the compiled JSON, e.g. a `$__timeFilter(...)` macro call. */
class Macro {
  constructor(public readonly text: string) {}
}

type Json = string | number | boolean | null | Macro | Json[] | { [key: string]: Json };

function parseValue(raw: string): Json {
  const trimmed = raw.trim();
  if (trimmed === '') {
    return '';
  }
  try {
    return JSON.parse(trimmed);
  } catch {
    return raw;
  }
}

function parseList(raw: string): Json[] {
  return raw
    .split(',')
    .map((v) => v.trim())
    .filter((v) => v.length > 0)
    .map(parseValue);
}

function filterClause(f: BuilderFilter): Record<string, Json> | undefined {
  if (!f.field) {
    return undefined;
  }
  switch (f.operator) {
    case 'eq':
      return { [f.field]: parseValue(f.value) };
    case 'ne':
      return { [f.field]: { $ne: parseValue(f.value) } };
    case 'gt':
      return { [f.field]: { $gt: parseValue(f.value) } };
    case 'gte':
      return { [f.field]: { $gte: parseValue(f.value) } };
    case 'lt':
      return { [f.field]: { $lt: parseValue(f.value) } };
    case 'lte':
      return { [f.field]: { $lte: parseValue(f.value) } };
    case 'in':
      return { [f.field]: { $in: parseList(f.value) } };
    case 'nin':
      return { [f.field]: { $nin: parseList(f.value) } };
    case 'exists':
      return { [f.field]: { $exists: f.value !== 'false' } };
    case 'regex':
      return { [f.field]: { $regex: f.value } };
    default:
      return undefined;
  }
}

/** Sanitizes a dotted/nested field name into a bare identifier usable as a $group._id sub-key. */
const asIdentifier = (field: string): string => field.replace(/\W/g, '_') || 'label';

function buildMatchStage(state: QueryBuilderState): Json | undefined {
  const parts: Json[] = [];
  if (state.timeFilterEnabled && state.timeField) {
    parts.push(new Macro(`$__timeFilter(${state.timeField})`));
  }
  for (const f of state.filters) {
    const clause = filterClause(f);
    if (clause) {
      parts.push(clause);
    }
  }
  if (parts.length === 0) {
    return undefined;
  }
  if (parts.length === 1) {
    return { $match: parts[0] };
  }
  return { $match: { $and: parts } };
}

function buildGroupStages(state: QueryBuilderState): Json[] {
  const { groupBy } = state;
  if (!groupBy.enabled || !groupBy.timeField) {
    return [];
  }

  const intervalArg = groupBy.interval ? `, "${groupBy.interval}"` : '';
  const bucket = new Macro(`$__timeGroup(${groupBy.timeField}${intervalArg})`);
  const labelKey = groupBy.labelField ? asIdentifier(groupBy.labelField) : undefined;

  const id: Record<string, Json> = { t: bucket };
  if (labelKey && groupBy.labelField) {
    id[labelKey] = `$${groupBy.labelField}`;
  }

  const group: Record<string, Json> = { _id: id };
  const project: Record<string, Json> = { _id: 0, time: '$_id.t' };
  if (labelKey) {
    project[labelKey] = `$_id.${labelKey}`;
  }

  for (const agg of groupBy.aggregations) {
    if (!agg.as || (agg.op !== 'count' && !agg.field)) {
      continue;
    }
    group[agg.as] = agg.op === 'count' ? { $sum: 1 } : { [`$${agg.op}`]: `$${agg.field}` };
    project[agg.as] = 1;
  }

  return [{ $group: group }, { $project: project }];
}

function buildSortStage(state: QueryBuilderState): Json | undefined {
  const sortDoc: Record<string, Json> = {};
  for (const s of state.sort) {
    if (s.field) {
      sortDoc[s.field] = s.direction === 'desc' ? -1 : 1;
    }
  }
  return Object.keys(sortDoc).length > 0 ? { $sort: sortDoc } : undefined;
}

/** Compiles builder state into a pretty-printed extended-JSON aggregation pipeline, the same text format the code editor accepts. */
export function compileBuilderPipeline(state: QueryBuilderState): string {
  const stages: Json[] = [];

  const match = buildMatchStage(state);
  if (match) {
    stages.push(match);
  }

  stages.push(...buildGroupStages(state));

  const sort = buildSortStage(state);
  if (sort) {
    stages.push(sort);
  }

  if (state.limit && state.limit > 0) {
    stages.push({ $limit: state.limit });
  }

  let counter = 0;
  const macros = new Map<string, string>();
  const json = JSON.stringify(
    stages,
    (_key, value) => {
      if (value instanceof Macro) {
        const token = `__QB_MACRO_${counter++}__`;
        macros.set(token, value.text);
        return token;
      }
      return value;
    },
    2
  );

  return Array.from(macros.entries()).reduce((text, [token, macro]) => text.replace(`"${token}"`, macro), json);
}
