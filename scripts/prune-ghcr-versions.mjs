#!/usr/bin/env node

import { spawnSync } from 'node:child_process';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const OWNER = 'francescostumpo';
const PACKAGE = 'legal-callegarin';
const IMAGE_REPOSITORY = `ghcr.io/${OWNER}/${PACKAGE}`;
const ARM_ORIGIN = 'https://management.azure.com';
const ARM_API_VERSION = '2026-01-01';
const GITHUB_API_VERSION = '2026-03-10';
const ACCEPT_HEADER = 'Accept: application/vnd.github+json';
const API_VERSION_HEADER = `X-GitHub-Api-Version: ${GITHUB_API_VERSION}`;
const RETAIN_NEWEST = 10;
const RETAIN_AGE_MS = 30 * 24 * 60 * 60 * 1000;
const MAX_PAGES = 100;
const MAX_VERSIONS = 10_000;
const MAX_DELETIONS = 100;
const MAX_OUTPUT_BYTES = 1024 * 1024;
const APP_QUERY = '{id:id,name:name,location:location,provisioningState:properties.provisioningState,latestRevisionName:properties.latestRevisionName,latestReadyRevisionName:properties.latestReadyRevisionName,activeRevisionsMode:properties.configuration.activeRevisionsMode,external:properties.configuration.ingress.external,allowInsecure:properties.configuration.ingress.allowInsecure,fqdn:properties.configuration.ingress.fqdn,traffic:properties.configuration.ingress.traffic,template:properties.template}';
const REVISION_QUERY = '{id:id,name:name,active:properties.active,healthState:properties.healthState,provisioningState:properties.provisioningState,runningState:properties.runningState,fqdn:properties.fqdn,trafficWeight:properties.trafficWeight,template:properties.template}';
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const tagPattern = /^sha-[0-9a-f]{40}$/;
const safeRevisionPattern = /^[a-z0-9][a-z0-9-]{0,63}$/;
const fqdnPattern = /^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$/;

class SafeError extends Error {}

const own = (object, key) => Object.prototype.hasOwnProperty.call(object, key);
const fail = (message) => { throw new SafeError(message); };
const object = (value, label) => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) fail(`${label} is malformed`);
  return value;
};
const exactKeys = (value, keys, label) => {
  const actual = Object.keys(object(value, label)).sort();
  const expected = [...keys].sort();
  if (JSON.stringify(actual) !== JSON.stringify(expected)) fail(`${label} has unexpected or missing fields`);
};

function parseRfc3339Nanos(value, label = 'timestamp', nowNanos) {
  if (typeof value !== 'string') fail(`${label} is invalid`);
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) fail(`${label} is invalid`);
  const [, year, month, day, hour, minute, second, fraction = '', zone] = match;
  const [y, mo, d, h, mi, s] = [year, month, day, hour, minute, second].map(Number);
  if (mo < 1 || mo > 12 || h > 23 || mi > 59 || s > 59) fail(`${label} is invalid`);
  const localDate = new Date(0);
  localDate.setUTCFullYear(y, mo - 1, d);
  localDate.setUTCHours(h, mi, s, 0);
  if (localDate.getUTCFullYear() !== y || localDate.getUTCMonth() !== mo - 1 || localDate.getUTCDate() !== d || localDate.getUTCHours() !== h || localDate.getUTCMinutes() !== mi || localDate.getUTCSeconds() !== s) fail(`${label} is invalid`);
  let offsetMinutes = 0;
  if (zone !== 'Z') {
    const offsetHours = Number(zone.slice(1, 3));
    const offsetRemainder = Number(zone.slice(4, 6));
    if (offsetHours > 23 || offsetRemainder > 59) fail(`${label} is invalid`);
    offsetMinutes = (zone[0] === '+' ? 1 : -1) * (offsetHours * 60 + offsetRemainder);
  }
  const fractionNanos = BigInt((fraction + '000000000').slice(0, 9));
  const instant = BigInt(localDate.getTime()) * 1_000_000n + fractionNanos - BigInt(offsetMinutes) * 60_000_000_000n;
  if (nowNanos !== undefined && instant > nowNanos) fail(`${label} is invalid or in the future`);
  return instant;
}

const compareDescending = (left, right) => left === right ? 0 : left > right ? -1 : 1;
const compareNewest = (left, right) =>
  compareDescending(parseRfc3339Nanos(left.created_at), parseRfc3339Nanos(right.created_at)) ||
  compareDescending(parseRfc3339Nanos(left.updated_at), parseRfc3339Nanos(right.updated_at)) ||
  right.id - left.id;

export function selectRetentionCandidates(versions, routedDigest, now = new Date()) {
  const ordered = [...versions].sort(compareNewest);
  const newestIds = new Set(ordered.slice(0, RETAIN_NEWEST).map((version) => version.id));
  const cutoff = BigInt(now.getTime() - RETAIN_AGE_MS) * 1_000_000n;
  return ordered.filter((version) =>
    version.name !== routedDigest &&
    !newestIds.has(version.id) &&
    parseRfc3339Nanos(version.created_at) < cutoff);
}

function parseArguments(argv, environment) {
  const values = new Map();
  let mode = '';
  for (let index = 0; index < argv.length; index += 1) {
    const key = argv[index];
    if (key === '--dry-run' || key === '--apply') {
      if (mode || values.has(key)) fail('exactly one mode is required');
      values.set(key, true);
      mode = key.slice(2);
      continue;
    }
    if (!['--subscription-id', '--resource-group', '--container-app-name'].includes(key)) {
      if (typeof key === 'string' && key.startsWith('--')) fail('unknown argument');
      fail('unexpected positional argument');
    }
    if (values.has(key)) fail('argument repeated');
    const value = argv[index + 1];
    if (typeof value !== 'string' || value.length === 0 || value.startsWith('--')) fail('argument value is missing');
    values.set(key, value);
    index += 1;
  }
  const subscriptionId = values.get('--subscription-id');
  const resourceGroup = values.get('--resource-group');
  const containerAppName = values.get('--container-app-name');
  if (!subscriptionId || !resourceGroup || !containerAppName || !mode) fail('required arguments are missing');
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(subscriptionId)) fail('subscription ID must be a canonical lowercase UUID');
  if (resourceGroup.length > 90 || !(/^[A-Za-z0-9]$/.test(resourceGroup) || /^[A-Za-z0-9][A-Za-z0-9._-]*[A-Za-z0-9_]$/.test(resourceGroup))) fail('invalid resource group name');
  if (containerAppName.length < 2 || containerAppName.length > 31 || !/^[a-z][a-z0-9-]*[a-z0-9]$/.test(containerAppName) || containerAppName.includes('--')) fail('invalid Container App name');
  if (typeof environment.GH_TOKEN !== 'string' || environment.GH_TOKEN.length === 0) fail('GH_TOKEN is required');
  return { subscriptionId, resourceGroup, containerAppName, mode };
}

function defaultRun(command, args, label, environment = process.env) {
  const childEnvironment = { ...environment, AZURE_CORE_OUTPUT: 'none' };
  if (command === 'az') delete childEnvironment.GH_TOKEN;
  const result = spawnSync(command, args, {
    encoding: 'utf8',
    env: childEnvironment,
    maxBuffer: MAX_OUTPUT_BYTES,
    shell: false,
  });
  if (result.error?.code === 'ENOENT') fail(`required tool not found: ${command}`);
  if (result.error || result.signal || result.status !== 0) fail(`${label} failed`);
  if (typeof result.stdout !== 'string' || Buffer.byteLength(result.stdout) > MAX_OUTPUT_BYTES) fail(`${label} returned an oversized response`);
  return result.stdout;
}

let executeCommand = defaultRun;
let writeOutput = (value) => process.stdout.write(value);

function parseJson(source, label) {
  try {
    return JSON.parse(source);
  } catch {
    fail(`${label} returned malformed JSON`);
  }
}

function validateTools(environment) {
  executeCommand('az', ['--version'], 'Azure CLI availability check', environment);
  executeCommand('gh', ['--version'], 'GitHub CLI availability check', environment);
}

function armGet(url, query, environment) {
  return parseJson(executeCommand('az', ['rest', '--method', 'get', '--url', url, '--query', query, '--output', 'json'], 'Azure ARM read', environment), 'Azure ARM read');
}

function validateAccountAndGroup(config, environment) {
  const account = parseJson(executeCommand('az', ['account', 'show', '--query', '{id:id,state:state}', '--output', 'json'], 'Azure account read', environment), 'Azure account read');
  exactKeys(account, ['id', 'state'], 'Azure account projection');
  if (typeof account.id !== 'string' || account.id.toLowerCase() !== config.subscriptionId || account.state !== 'Enabled') fail('Azure account is not the requested enabled subscription');
  const expected = `/subscriptions/${config.subscriptionId}/resourceGroups/${config.resourceGroup}`;
  const actual = executeCommand('az', ['group', 'show', '--subscription', config.subscriptionId, '--name', config.resourceGroup, '--query', 'id', '--output', 'tsv'], 'Azure resource group read', environment).trim();
  if (!actual || actual.toLowerCase() !== expected.toLowerCase()) fail('Azure resource group identity mismatch');
}

function validateTraffic(traffic) {
  if (!Array.isArray(traffic) || traffic.length === 0) fail('Container App traffic is malformed');
  let total = 0;
  const positive = [];
  const revisions = new Set();
  for (const rawRule of traffic) {
    const rule = object(rawRule, 'traffic rule');
    if (own(rule, 'latestRevision') || own(rule, 'label')) fail('traffic must not use latestRevision or labels');
    if (typeof rule.revisionName !== 'string' || !safeRevisionPattern.test(rule.revisionName)) fail('traffic revision name is missing or unsafe');
    if (revisions.has(rule.revisionName)) fail('traffic contains duplicate revision names');
    revisions.add(rule.revisionName);
    if (!Number.isInteger(rule.weight) || rule.weight < 0 || rule.weight > 100) fail('traffic weight is invalid');
    total += rule.weight;
    if (rule.weight > 0) positive.push(rule);
  }
  if (total !== 100 || positive.length !== 1 || positive[0].weight !== 100) fail('traffic must route exactly 100 percent to one named revision');
  return positive[0].revisionName;
}

function revisionDigest(revision, expectedId, revisionName, appName) {
  exactKeys(revision, ['id','name','active','healthState','provisioningState','runningState','fqdn','trafficWeight','template'], 'revision projection');
  if (typeof revision.id !== 'string' || revision.id.toLowerCase() !== expectedId.toLowerCase() || revision.name !== revisionName) fail('revision identity mismatch');
  if (revision.active !== true || revision.healthState !== 'Healthy' || revision.provisioningState !== 'Provisioned' || !['Running','ScaledToZero'].includes(revision.runningState)) fail('routed revision is not active, healthy, provisioned, and viable');
  if (revision.trafficWeight !== 100 || typeof revision.fqdn !== 'string' || !fqdnPattern.test(revision.fqdn) || revision.fqdn.includes('..')) fail('routed revision traffic weight or FQDN is invalid');
  const template = object(revision.template, 'revision template');
  if (!Array.isArray(template.containers) || template.containers.length !== 1) fail('revision must contain exactly one container');
  const container = object(template.containers[0], 'revision container');
  if (container.name !== appName) fail('revision does not contain the expected container');
  const imagePrefix = `${IMAGE_REPOSITORY}@`;
  if (typeof container.image !== 'string' || !container.image.startsWith(imagePrefix) || !digestPattern.test(container.image.slice(imagePrefix.length))) fail('routed image is not the expected immutable digest');
  if (!Array.isArray(container.env)) fail('revision environment is malformed');
  const modes = container.env.filter((entry) => entry && typeof entry === 'object' && !Array.isArray(entry) && entry.name === 'ARTICLE_STORAGE_SCHEMA_MODE');
  if (modes.length !== 1 || modes[0].value !== 'migrate' || own(modes[0], 'secretRef')) fail('schema mode must be one literal migrate value');
  return container.image.slice(imagePrefix.length);
}

function discoverAzureDigest(config, environment) {
  validateAccountAndGroup(config, environment);
  const appPath = `/subscriptions/${config.subscriptionId}/resourceGroups/${config.resourceGroup}/providers/Microsoft.App/containerApps/${config.containerAppName}`;
  const appUrl = `${ARM_ORIGIN}${appPath}?api-version=${ARM_API_VERSION}`;
  const app = armGet(appUrl, APP_QUERY, environment);
  exactKeys(app, ['id','name','location','provisioningState','latestRevisionName','latestReadyRevisionName','activeRevisionsMode','external','allowInsecure','fqdn','traffic','template'], 'Container App projection');
  if (typeof app.id !== 'string' || app.id.toLowerCase() !== appPath.toLowerCase() || app.name !== config.containerAppName) fail('Container App identity mismatch');
  if (app.provisioningState !== 'Succeeded' || app.activeRevisionsMode !== 'Multiple' || app.external !== true || app.allowInsecure !== false) fail('Container App mode, provisioning, or ingress is unsafe');
  if (typeof app.location !== 'string' || !app.location || typeof app.fqdn !== 'string' || !fqdnPattern.test(app.fqdn) || app.fqdn.includes('..')) fail('Container App location or FQDN is malformed');
  const appTemplate = object(app.template, 'Container App template');
  if (!Array.isArray(appTemplate.containers) || appTemplate.containers.length !== 1) fail('Container App template must contain exactly one container');
  const appContainer = object(appTemplate.containers[0], 'Container App container');
  if (typeof appContainer.name !== 'string' || !safeRevisionPattern.test(appContainer.name)) fail('Container App container name is missing or unsafe');
  const revisionName = validateTraffic(app.traffic);
  const revisionPath = `${appPath}/revisions/${revisionName}`;
  const revisionUrl = `${ARM_ORIGIN}${revisionPath}?api-version=${ARM_API_VERSION}`;
  const revision = armGet(revisionUrl, REVISION_QUERY, environment);
  return revisionDigest(revision, revisionPath, revisionName, appContainer.name);
}

function ghArgs(endpoint, method, silent = false) {
  const args = ['api', endpoint, '--method', method, '-H', ACCEPT_HEADER, '-H', API_VERSION_HEADER];
  if (silent) args.push('--silent');
  return args;
}

function ghGet(endpoint, label, environment) {
  return parseJson(executeCommand('gh', ghArgs(endpoint, 'GET'), label, environment), label);
}

function validatePackage(value) {
  const pkg = object(value, 'GitHub package');
  const owner = object(pkg.owner, 'GitHub package owner');
  if (owner.login !== OWNER || pkg.name !== PACKAGE || pkg.package_type !== 'container' || pkg.visibility !== 'private') fail('GitHub package identity, type, or visibility is unsafe');
}

function parseRfc3339(value, label, nowMs) {
  return parseRfc3339Nanos(value, label, BigInt(nowMs) * 1_000_000n);
}

function validateVersions(rawVersions, routedDigest, now) {
  if (!Array.isArray(rawVersions)) fail('GitHub versions page is not an array');
  const ids = new Set();
  const digests = new Set();
  const nowMs = now.getTime();
  const versions = [];
  for (const raw of rawVersions) {
    const version = object(raw, 'GitHub package version');
    if (!Number.isSafeInteger(version.id) || version.id <= 0 || ids.has(version.id)) fail('GitHub package version ID is invalid or duplicated');
    if (typeof version.name !== 'string' || !digestPattern.test(version.name) || digests.has(version.name)) fail('GitHub package version digest is invalid or duplicated');
    const created = parseRfc3339(version.created_at, 'created_at', nowMs);
    const updated = parseRfc3339(version.updated_at, 'updated_at', nowMs);
    if (updated < created) fail('updated_at precedes created_at');
    const metadata = object(version.metadata, 'GitHub package version metadata');
    const container = object(metadata.container, 'GitHub container metadata');
    if (metadata.package_type !== 'container' || !Array.isArray(container.tags)) fail('GitHub container metadata is malformed');
    const tags = new Set();
    for (const tag of container.tags) {
      if (typeof tag !== 'string' || !tagPattern.test(tag) || tags.has(tag)) fail('GitHub package version tag is mutable, unknown, or duplicated');
      tags.add(tag);
    }
    ids.add(version.id);
    digests.add(version.name);
    versions.push({
      id: version.id,
      name: version.name,
      created_at: version.created_at,
      updated_at: version.updated_at,
      metadata: { package_type: 'container', container: { tags: [...container.tags] } },
    });
  }
  if (versions.filter((version) => version.name === routedDigest).length !== 1) fail('routed digest is not present exactly once in GHCR');
  return versions;
}

function readGithubSnapshot(routedDigest, now, environment) {
  validatePackage(ghGet(`/user/packages/container/${PACKAGE}`, 'GitHub package read', environment));
  const rawVersions = [];
  for (let page = 1; page <= MAX_PAGES; page += 1) {
    const endpoint = `/user/packages/container/${PACKAGE}/versions?state=active&per_page=100&page=${page}`;
    const values = ghGet(endpoint, 'GitHub package versions read', environment);
    if (!Array.isArray(values)) fail('GitHub versions page is not an array');
    if (values.length > 100) fail('GitHub versions page exceeds 100 records');
    rawVersions.push(...values);
    if (rawVersions.length > MAX_VERSIONS) fail('GitHub version count exceeds 10000');
    if (values.length < 100) return validateVersions(rawVersions, routedDigest, now);
    if (page === MAX_PAGES) fail('GitHub version pagination exceeds 100 pages');
  }
  fail('GitHub version pagination did not terminate');
}

function snapshotFingerprint(versions) {
  const canonical = versions.map((version) => ({
    id: version.id,
    name: version.name,
    created_at: version.created_at,
    updated_at: version.updated_at,
    metadata: { package_type: version.metadata.package_type, container: { tags: [...version.metadata.container.tags].sort() } },
  })).sort((left, right) => left.id - right.id);
  return JSON.stringify(canonical);
}

function printPlan(mode, versions, candidates) {
  writeOutput(`mode=${mode}\n`);
  writeOutput(`versions=${versions.length}\n`);
  writeOutput(`retained=${versions.length - candidates.length}\n`);
  writeOutput(`candidates=${candidates.length}\n`);
  for (const candidate of candidates) writeOutput(`candidate id=${candidate.id} digest=${candidate.name} created_at=${candidate.created_at}\n`);
}

function deleteVersion(id, environment) {
  executeCommand('gh', ghArgs(`/users/${OWNER}/packages/container/${PACKAGE}/versions/${id}`, 'DELETE', true), 'GitHub package version delete', environment);
}

export function main(argv = process.argv.slice(2), environment = process.env, runner = defaultRun, writer = (value) => process.stdout.write(value)) {
  const previousRunner = executeCommand;
  const previousWriter = writeOutput;
  executeCommand = runner;
  writeOutput = writer;
  try {
    const config = parseArguments(argv, environment);
    const now = new Date();
    validateTools(environment);
    const routedDigest = discoverAzureDigest(config, environment);
    const initial = readGithubSnapshot(routedDigest, now, environment);
    const candidates = selectRetentionCandidates(initial, routedDigest, now);
    if (candidates.length > MAX_DELETIONS) fail('planned deletion count exceeds 100');
    printPlan(config.mode, initial, candidates);
    if (config.mode === 'dry-run') return;
    if (candidates.length === 0) {
      writeOutput('deleted=0\ncleanup_status=succeeded\n');
      return;
    }
    const preDeleteDigest = discoverAzureDigest(config, environment);
    const preDelete = readGithubSnapshot(preDeleteDigest, now, environment);
    if (preDeleteDigest !== routedDigest || snapshotFingerprint(preDelete) !== snapshotFingerprint(initial)) fail('Azure or GHCR state changed before deletion; zero versions deleted');
    let deleted = 0;
    for (const candidate of candidates) {
      try {
        deleteVersion(candidate.id, environment);
        deleted += 1;
      } catch {
        fail(`delete failed for failed ID ${candidate.id}; ${deleted} already deleted; cleanup needs operator review`);
      }
    }
    try {
      const finalDigest = discoverAzureDigest(config, environment);
      const finalSnapshot = readGithubSnapshot(finalDigest, now, environment);
      if (finalDigest !== routedDigest) fail('routed digest changed');
      const deletedIds = new Set(candidates.map((candidate) => candidate.id));
      const expected = initial.filter((version) => !deletedIds.has(version.id));
      if (snapshotFingerprint(finalSnapshot) !== snapshotFingerprint(expected)) fail('final GHCR snapshot differs from the planned result');
    } catch {
      fail('final cleanup verification failed; cleanup needs operator review');
    }
    writeOutput(`deleted=${deleted}\ncleanup_status=succeeded\n`);
  } finally {
    executeCommand = previousRunner;
    writeOutput = previousWriter;
  }
}

const invokedPath = process.argv[1] ? pathToFileURL(resolve(process.argv[1])).href : '';
if (invokedPath === import.meta.url) {
  try {
    main();
  } catch (error) {
    const message = error instanceof SafeError ? error.message : 'unexpected internal error';
    process.stderr.write(`retention failed: ${message}\n`);
    process.exitCode = 1;
  }
}
