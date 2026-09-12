import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, readdirSync, rmSync, chmodSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const script = resolve('scripts/deploy-container-app.sh');
const subscription = '00000000-0000-0000-0000-000000000000';
const rg = 'rg-callegarin';
const app = 'callegarin';
const prior = 'callegarin--prior';
const candidate = 'callegarin--deploy-test';
const oldDigest = `ghcr.io/francescostumpo/legal-callegarin@sha256:${'a'.repeat(64)}`;
const digest = `ghcr.io/francescostumpo/legal-callegarin@sha256:${'b'.repeat(64)}`;
const publicBase = 'https://www.studio-callegarin.it';
const baseArgs = ['--subscription-id', subscription, '--resource-group', rg, '--container-app-name', app, '--image-digest', digest, '--revision-suffix', 'deploy-test', '--public-base-url', publicBase];
const appQuery = '{id:id,name:name,location:location,provisioningState:properties.provisioningState,latestRevisionName:properties.latestRevisionName,latestReadyRevisionName:properties.latestReadyRevisionName,activeRevisionsMode:properties.configuration.activeRevisionsMode,external:properties.configuration.ingress.external,allowInsecure:properties.configuration.ingress.allowInsecure,fqdn:properties.configuration.ingress.fqdn,traffic:properties.configuration.ingress.traffic,template:properties.template}';
const revisionQuery = '{id:id,name:name,active:properties.active,healthState:properties.healthState,provisioningState:properties.provisioningState,runningState:properties.runningState,fqdn:properties.fqdn,trafficWeight:properties.trafficWeight,template:properties.template}';
const listQuery = '{items:value[].{id:id,name:name,active:properties.active,healthState:properties.healthState,provisioningState:properties.provisioningState,runningState:properties.runningState,fqdn:properties.fqdn,trafficWeight:properties.trafficWeight,template:properties.template},nextLink:nextLink}';

const fakeAz = String.raw`#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
const scenario = process.env.TEST_SCENARIO || 'success';
const log = process.env.TEST_LOG;
fs.appendFileSync(log, JSON.stringify(['az', ...args]) + '\n');
const sub = '00000000-0000-0000-0000-000000000000';
const rg = 'rg-callegarin'; const app = 'callegarin';
const appPath = '/subscriptions/' + sub + '/resourceGroups/' + rg + '/providers/Microsoft.App/containerApps/' + app;
const revisionsPath = appPath + '/revisions';
const prior = 'callegarin--prior'; const candidate = 'callegarin--deploy-test';
const repo = 'ghcr.io/francescostumpo/legal-callegarin@sha256:';
const oldDigest = repo + 'a'.repeat(64); const digest = repo + 'b'.repeat(64);
const stateFile = process.env.TEST_STATE;
let state = { copied: false, promoted: false, rolledBack: false, rollbackAttempted: false };
try { state = JSON.parse(fs.readFileSync(stateFile, 'utf8')); } catch {}
const save = () => fs.writeFileSync(stateFile, JSON.stringify(state));
const option = name => args[args.indexOf(name) + 1];
const has = name => args.includes(name);
const output = value => process.stdout.write(typeof value === 'string' ? value : JSON.stringify(value));
const template = (image, suffix, overrides = {}) => ({
  revisionSuffix: suffix,
  terminationGracePeriodSeconds: 30,
  containers: [{
    name: 'callegarin', image,
    env: [{name:'APP_ENV',value:'production'},{name:'ARTICLE_STORAGE_SCHEMA_MODE',value:'migrate'}],
    resources: {cpu:0.25,memory:'0.5Gi'}, probes:[{type:'Liveness'}], ...overrides
  }],
  scale: {minReplicas:0,maxReplicas:1}
});
const traffic = () => {
  if (scenario === 'rollback-state' && state.rollbackAttempted) return [{revisionName:candidate,weight:100}];
  if (state.rolledBack) return [{revisionName:prior,weight:100}];
  if (state.promoted) {
    if (scenario === 'posttraffic-extra') return [{revisionName:candidate,weight:100},{revisionName:'other',weight:1}];
    return [{revisionName:candidate,weight:100}];
  }
  if (scenario === 'latest-traffic') return [{latestRevision:true,weight:100}];
  if (scenario === 'labeled-traffic') return [{revisionName:prior,weight:100,label:'stable'}];
  if (scenario === 'multiple-traffic') return [{revisionName:prior,weight:90},{revisionName:'other',weight:10}];
  return [{revisionName:prior,weight:100}];
};
const appTemplate = () => {
  const value = template(state.copied ? digest : oldDigest, state.copied ? 'deploy-test' : 'prior');
  if (scenario === 'app-containers') value.containers.push({...value.containers[0],name:'sidecar'});
  if (scenario === 'template-drift') value.scale.maxReplicas = 2;
  return value;
};
const appDoc = () => ({
  id: scenario === 'app-id' ? appPath + '-wrong' : appPath, name:app, location:'italynorth',
  provisioningState: scenario === 'app-provisioning' ? 'Failed' : 'Succeeded',
  latestRevisionName: state.copied ? candidate : prior, latestReadyRevisionName: state.copied ? candidate : prior,
  activeRevisionsMode: scenario === 'app-mode' ? 'Single' : 'Multiple',
  external: scenario === 'app-ingress' ? false : true, allowInsecure:false,
  fqdn:'callegarin.example.azurecontainerapps.io', traffic:traffic(), template:appTemplate()
});
const revision = name => {
  const isCandidate = name === candidate;
  let image = isCandidate ? digest : oldDigest;
  let suffix = isCandidate ? 'deploy-test' : 'prior';
  let mode = 'migrate';
  let fqdn = name + '.example.azurecontainerapps.io';
  let healthState = 'Healthy';
  let runningState = isCandidate ? 'Running' : 'ScaledToZero';
  let extraEnv = [];
  let overrides = {};
  if (!isCandidate && scenario === 'prior-unhealthy') healthState = 'Unhealthy';
  if (!isCandidate && scenario === 'prior-compat') mode = 'compat';
  if (!isCandidate && scenario === 'prior-tagged') image = 'ghcr.io/francescostumpo/legal-callegarin:latest';
  if (!isCandidate && scenario === 'prior-duplicate-mode') extraEnv = [{name:'ARTICLE_STORAGE_SCHEMA_MODE',value:'migrate'}];
  if (!isCandidate && scenario === 'prior-missing-fqdn') fqdn = '';
  if (isCandidate && scenario === 'candidate-unhealthy') healthState = 'Unhealthy';
  if (isCandidate && scenario === 'candidate-image') image = oldDigest;
  if (isCandidate && scenario === 'candidate-env') mode = 'compat';
  if (isCandidate && scenario === 'candidate-template') overrides = {command:['unexpected']};
  if (!isCandidate && state.rolledBack && scenario === 'rollback-health') healthState = 'Unhealthy';
  const doc = {
    id: revisionsPath + '/' + name, name, active:true, healthState, provisioningState:'Provisioned', runningState, fqdn,
    trafficWeight: state.promoted ? (isCandidate ? 100 : null) : (isCandidate ? null : 100),
    template:template(image,suffix,{env:[{name:'APP_ENV',value:'production'},{name:'ARTICLE_STORAGE_SCHEMA_MODE',value:mode},...extraEnv],...overrides})
  };
  return doc;
};
const listItems = () => {
  const items = [revision(prior)];
  if (scenario === 'suffix-collision' && !state.copied) items.push({...revision(candidate),active:false});
  if (state.copied && scenario !== 'candidate-timeout') items.push(revision(candidate));
  return items;
};
if (args[0] === 'account' && args[1] === 'show') output({id: scenario === 'wrong-account' ? '11111111-1111-1111-1111-111111111111' : sub,state:'Enabled'});
else if (args[0] === 'group' && args[1] === 'show') output(scenario === 'wrong-rg' ? appPath : '/subscriptions/' + sub + '/resourceGroups/' + rg);
else if (args[0] === 'rest') {
  const url = new URL(option('--url'));
  const query = option('--query');
  const page2 = url.searchParams.has('$skiptoken');
  if (url.pathname.toLowerCase() === appPath.toLowerCase()) output(appDoc());
  else if (url.pathname.toLowerCase() === revisionsPath.toLowerCase()) {
    const all = listItems();
    if (scenario === 'pagination') {
      output(page2 ? {items:all.slice(1),nextLink:null} : {items:all.slice(0,1),nextLink:'https://management.azure.com' + revisionsPath + '?api-version=2026-01-01&$skiptoken=two'});
    } else if (scenario === 'pagination-duplicate') {
      output(page2 ? {items:[revision(prior)],nextLink:null} : {items:[revision(prior)],nextLink:'https://management.azure.com' + revisionsPath + '?api-version=2026-01-01&$skiptoken=two'});
    } else if (scenario === 'pagination-offhost') {
      output({items:all.slice(0,1),nextLink:'https://evil.invalid' + revisionsPath + '?api-version=2026-01-01&$skiptoken=two'});
    } else if (scenario === 'pagination-overflow') {
      const page = Number(url.searchParams.get('$skiptoken') || '0');
      output({items:page === 0 ? [revision(prior)] : [],nextLink:'https://management.azure.com' + revisionsPath + '?api-version=2026-01-01&$skiptoken=' + (page + 1)});
    } else if (scenario === 'pagination-cycle') {
      output({items:all.slice(0,1),nextLink:'https://management.azure.com' + revisionsPath + '?api-version=2026-01-01&$skiptoken=cycle'});
    } else output({items:all,nextLink:null});
  } else if (url.pathname.toLowerCase().startsWith((revisionsPath + '/').toLowerCase())) output(revision(decodeURIComponent(url.pathname.split('/').at(-1))));
  else process.exit(21);
  if (!has('--output') || option('--output') !== 'json' || !query) process.exit(22);
} else if (args[0] === 'containerapp' && args[1] === 'revision' && args[2] === 'copy') {
  state.copied = true; save();
  if (scenario === 'copy-error') process.exit(31);
} else if (args[0] === 'containerapp' && args[1] === 'ingress' && args[2] === 'traffic' && args[3] === 'set') {
  const weights = args.slice(args.indexOf('--revision-weight') + 1, args.indexOf('--output'));
  const promoting = weights.some(value => value.startsWith(candidate + '=100'));
  if (promoting) {
    state.promoted = true; state.rolledBack = false; save();
    if (scenario === 'promotion-error') process.exit(32);
  } else {
    if (scenario === 'rollback-command') process.exit(33);
    state.rollbackAttempted = true;
    state.promoted = false; state.rolledBack = scenario !== 'rollback-state'; save();
  }
} else process.exit(99);
`;

const fakeCurl = String.raw`#!/usr/bin/env node
const fs = require('node:fs'); const args = process.argv.slice(2);
const scenario = process.env.TEST_SCENARIO || 'success';
fs.appendFileSync(process.env.TEST_LOG, JSON.stringify(['curl', ...args]) + '\n');
const option = name => args[args.indexOf(name) + 1];
const url = option('--write-out') === '%{http_code}' ? args.at(-1) : '';
const headersFile = option('--dump-header'); const bodyFile = option('--output');
const state = JSON.parse(fs.readFileSync(process.env.TEST_STATE, 'utf8'));
const candidate = url.includes('callegarin--deploy-test.');
const health = url.endsWith('/health/live') || url.endsWith('/health/ready');
const rollback = state.rolledBack;
let status = '200'; let body = health ? 'ok\n' : '<html>Alessandro Callegarin</html>';
let headers = 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n';
if (candidate && !health) { status='308'; body=''; headers='HTTP/1.1 308 Permanent Redirect\r\nLocation: https://www.studio-callegarin.it/\r\n'; }
if ((scenario === 'candidate-cookie' || scenario.startsWith('rollback-')) && candidate) headers += 'Set-Cookie: bad=1\r\n';
if (scenario === 'candidate-health' && candidate && health) { status='503'; body='no\n'; }
if (scenario === 'candidate-redirect' && candidate && !health) headers=headers.replace('https://www.studio-callegarin.it/','https://evil.invalid/');
if (scenario === 'canonical-cookie' && !candidate && !rollback) headers += 'set-cookie: bad=1\r\n';
if (scenario === 'canonical-root' && !candidate && !health && !rollback) body='<html>wrong</html>';
if (scenario === 'canonical-health' && !candidate && health && !rollback) { status='503'; body='no\n'; }
if (scenario === 'rollback-smoke' && rollback) { status='503'; body='no\n'; }
headers += '\r\n'; fs.writeFileSync(headersFile,headers); fs.writeFileSync(bodyFile,body); process.stdout.write(status);
`;

const fakeSleep = String.raw`#!/usr/bin/env node
require('node:fs').appendFileSync(process.env.TEST_LOG, JSON.stringify(['sleep', ...process.argv.slice(2)]) + '\n');
`;

function runCase(scenario = 'success', args = baseArgs) {
  const root = mkdtempSync(join(tmpdir(), 'deploy-test-'));
  const bin = join(root, 'bin'); const responses = join(root, 'responses');
  mkdirSync(bin); mkdirSync(responses);
  const log = join(root, 'calls.log'); const state = join(root, 'state.json');
  writeFileSync(log, ''); writeFileSync(state, JSON.stringify({copied:false,promoted:false,rolledBack:false,rollbackAttempted:false}));
  for (const [name, source] of [['az',fakeAz],['curl',fakeCurl],['sleep',fakeSleep]]) {
    const path = join(bin,name); writeFileSync(path,source); chmodSync(path,0o755);
  }
  const result = spawnSync('bash',[script,...args],{encoding:'utf8',env:{...process.env,PATH:`${bin}:${process.env.PATH}`,TMPDIR:responses,TEST_SCENARIO:scenario,TEST_LOG:log,TEST_STATE:state}});
  const calls = readFileSync(log,'utf8').trim().split('\n').filter(Boolean).map(line=>JSON.parse(line));
  const finalState = JSON.parse(readFileSync(state,'utf8'));
  const tempEntries = readdirSync(responses);
  rmSync(root,{recursive:true,force:true});
  return {...result,calls,finalState,tempEntries};
}

const mutations = result => result.calls.filter(call => call[0] === 'az' && call.includes('containerapp'));
const curls = result => result.calls.filter(call => call[0] === 'curl');

test('help and invalid input make no az, curl, or sleep calls', () => {
  for (const args of [['--help'], baseArgs.slice(0,-2), [...baseArgs,'positional'], [...baseArgs,'--image-digest',digest], baseArgs.map(v=>v===digest?'ghcr.io/francescostumpo/legal-callegarin:latest':v), baseArgs.map(v=>v===publicBase?`${publicBase}/path`:v)]) {
    const result = runCase('success',args);
    if (args[0] === '--help') assert.equal(result.status,0); else assert.notEqual(result.status,0);
    assert.deepEqual(result.calls,[]);
    assert.deepEqual(result.tempEntries,[]);
  }
});

test('successful rollout paginates, smokes, promotes exact digest, and keeps prior active at zero', () => {
  const result = runCase('pagination');
  assert.equal(result.status,0,result.stderr);
  assert.match(result.stdout,/deployment_status=succeeded/);
  assert.deepEqual(result.tempEntries,[]);
  assert.equal(result.finalState.promoted,true);
  const mutationCalls = mutations(result);
  assert.equal(mutationCalls.length,2);
  assert.deepEqual(mutationCalls[0],['az','containerapp','revision','copy','--subscription',subscription,'--resource-group',rg,'--name',app,'--from-revision',prior,'--container-name',app,'--image',digest,'--revision-suffix','deploy-test','--set-env-vars','ARTICLE_STORAGE_SCHEMA_MODE=migrate','--output','none']);
  assert.deepEqual(mutationCalls[1],['az','containerapp','ingress','traffic','set','--subscription',subscription,'--resource-group',rg,'--name',app,'--revision-weight',`${candidate}=100`,`${prior}=0`,'--output','none']);
  assert.equal(curls(result).length,6);
  assert.ok(curls(result).every(call=>!call.includes('--location')));
  const copyIndex = result.calls.indexOf(mutationCalls[0]);
  const promoteIndex = result.calls.indexOf(mutationCalls[1]);
  const candidateCurlIndexes = curls(result).slice(0,3).map(call=>result.calls.indexOf(call));
  const canonicalCurlIndexes = curls(result).slice(3).map(call=>result.calls.indexOf(call));
  assert.ok(candidateCurlIndexes.every(index=>index > copyIndex && index < promoteIndex));
  assert.ok(canonicalCurlIndexes.every(index=>index > promoteIndex));
  assert.ok(result.calls.some(call=>call[0]==='az' && String(call[call.indexOf('--url')+1]).includes('$skiptoken=two')));
  const restQueries = new Set(result.calls.filter(call=>call[0]==='az' && call[1]==='rest').map(call=>call[call.indexOf('--query')+1]));
  assert.deepEqual(restQueries,new Set([appQuery,revisionQuery,listQuery]));
  const forbiddenTokens = new Set(['login','get-access-token','secret','registry','bicep','update','delete','deactivate']);
  for (const call of result.calls.filter(call=>call[0]==='az')) {
    assert.ok(!call.some(value=>forbiddenTokens.has(String(value).toLowerCase())),JSON.stringify(call));
  }
});

test('unsafe preflight states fail before mutation and before curl', () => {
  const scenarios = ['wrong-account','wrong-rg','app-id','app-mode','app-provisioning','app-ingress','app-containers','latest-traffic','labeled-traffic','multiple-traffic','prior-unhealthy','prior-compat','prior-tagged','prior-duplicate-mode','prior-missing-fqdn','suffix-collision','template-drift','pagination-cycle','pagination-duplicate','pagination-offhost','pagination-overflow'];
  for (const scenario of scenarios) {
    const result = runCase(scenario);
    assert.notEqual(result.status,0,scenario);
    assert.equal(mutations(result).length,0,scenario);
    assert.equal(curls(result).length,0,scenario);
    assert.deepEqual(result.tempEntries,[],scenario);
  }
});

test('copy/candidate failures restore prior traffic and never promote', () => {
  for (const scenario of ['copy-error','candidate-unhealthy','candidate-image','candidate-env','candidate-template','candidate-cookie','candidate-health','candidate-redirect']) {
    const result = runCase(scenario);
    assert.notEqual(result.status,0,scenario);
    assert.equal(result.finalState.rolledBack,true,scenario);
    assert.ok(mutations(result).some(call=>call.includes(`${prior}=100`)),scenario);
    assert.ok(!mutations(result).some(call=>call.includes(`${candidate}=100`)),scenario);
    assert.deepEqual(result.tempEntries,[],scenario);
  }
});

test('candidate visibility timeout is bounded and rolls back', () => {
  const result = runCase('candidate-timeout');
  assert.notEqual(result.status,0);
  assert.equal(result.finalState.rolledBack,true);
  assert.equal(result.calls.filter(call=>call[0]==='sleep').length,23);
  assert.deepEqual(result.tempEntries,[]);
});

test('promotion and post-promotion failures always roll back', () => {
  for (const scenario of ['promotion-error','posttraffic-extra','canonical-cookie','canonical-root','canonical-health']) {
    const result = runCase(scenario);
    assert.notEqual(result.status,0,scenario);
    assert.equal(result.finalState.rolledBack,true,scenario);
    const mutationCalls = mutations(result);
    assert.ok(mutationCalls.some(call=>call.includes(`${candidate}=100`)),scenario);
    assert.ok(mutationCalls.some(call=>call.includes(`${prior}=100`)),scenario);
    assert.deepEqual(result.tempEntries,[],scenario);
  }
});

test('rollback failures report ambiguous deployment state', () => {
  for (const scenario of ['rollback-command','rollback-state','rollback-health','rollback-smoke']) {
    const result = runCase(scenario);
    assert.notEqual(result.status,0,scenario);
    assert.match(result.stderr,/deployment state ambiguous; manual intervention required/,scenario);
    assert.deepEqual(result.tempEntries,[],scenario);
  }
});
