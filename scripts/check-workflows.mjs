import { lstat, readFile, readdir } from "node:fs/promises"
import { extname, resolve } from "node:path"

// This is a bounded, defense-in-depth source policy. Full YAML and GitHub
// Actions schema validation with actionlint remains mandatory once workflows
// exist; this dependency-free check does not replace it.

const yamlExtensions = new Set([".yml", ".yaml"])
const eventExpression = /\$\{\{\s*github\.event\./
const latestReference = /(?:@|:)latest(?=$|[\s'"},])/i
const releaseComment = /^v\d+(?:\.\d+){0,2}(?:[-+][0-9A-Za-z.-]+)?(?:\s|$)/
const actionReference =
  /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+(?:\/[A-Za-z0-9_./-]+)?@[0-9a-f]{40}$/
const dockerReference = /^docker:\/\/[^@\s]+@sha256:[0-9a-f]{64}$/
const diagnostics = []

function addDiagnostic(path, line, rule, message) {
  diagnostics.push({ path, line, rule, message })
}

function indentation(line) {
  return line.match(/^ */)[0].length
}

function splitComment(line) {
  let quote = ""
  let escaped = false

  for (let index = 0; index < line.length; index++) {
    const character = line[index]
    if (escaped) {
      escaped = false
      continue
    }
    if (quote === '"' && character === "\\") {
      escaped = true
      continue
    }
    if (quote) {
      if (character === quote) quote = ""
      continue
    }
    if (character === '"' || character === "'") {
      quote = character
      continue
    }
    if (character === "#" && (index === 0 || /\s/.test(line[index - 1]))) {
      return {
        code: line.slice(0, index).trimEnd(),
        comment: line.slice(index + 1).trim(),
      }
    }
  }

  return { code: line.trimEnd(), comment: "" }
}

function unquote(value) {
  const trimmed = value.trim()
  if (
    trimmed.length >= 2 &&
    ((trimmed.startsWith('"') && trimmed.endsWith('"')) ||
      (trimmed.startsWith("'") && trimmed.endsWith("'")))
  ) {
    return trimmed.slice(1, -1)
  }
  return trimmed
}

function usesValue(code) {
  const match = code.match(
    /^\s*(?:-\s*)?(?:uses|"uses"|'uses')\s*:\s*(.*?)\s*$/,
  )
  return match ? unquote(match[1]) : null
}

function hasFlowStyleUses(code) {
  return (
    /^\s*-\s*\{/.test(code) &&
    /(?:\{|,)\s*(?:uses|"uses"|'uses')\s*:/.test(code)
  )
}

function hasPullRequestTarget(record) {
  if (
    /^\s*(?:pull_request_target|"pull_request_target"|'pull_request_target')\s*:/i.test(
      record.code,
    )
  ) {
    return true
  }

  const flow = record.code.match(/^\s*(?:on|"on"|'on')\s*:\s*\[(.*?)\]\s*$/i)
  return (
    flow !== null &&
    /(?:^|,)\s*(?:pull_request_target|"pull_request_target"|'pull_request_target')\s*(?=,|$)/i.test(
      flow[1],
    )
  )
}

function recordsFor(contents) {
  return contents
    .replaceAll("\r\n", "\n")
    .split("\n")
    .map((raw, index) => {
      const { code, comment } = splitComment(raw)
      return {
        raw,
        code,
        comment,
        indent: indentation(raw),
        line: index + 1,
        trimmed: code.trim(),
      }
    })
}

function workflowPermissions(path, records) {
  for (let index = 0; index < records.length; index++) {
    const record = records[index]
    if (/^\s*permissions\s*:\s*write-all\s*$/i.test(record.code)) {
      addDiagnostic(
        path,
        record.line,
        "workflow-permissions",
        "permissions: write-all is forbidden; grant minimum job-scoped permissions",
      )
    }
    if (record.indent !== 0) continue

    const match = record.code.match(/^permissions\s*:\s*(.*?)\s*$/)
    if (!match) continue
    const scalar = match[1]
    if (scalar) {
      if (/\bwrite(?:-all)?\b/i.test(scalar)) {
        addDiagnostic(
          path,
          record.line,
          "workflow-permissions",
          "workflow-level write permissions are forbidden; default to read or none",
        )
      }
      continue
    }

    for (
      let childIndex = index + 1;
      childIndex < records.length;
      childIndex++
    ) {
      const child = records[childIndex]
      if (child.trimmed && child.indent <= record.indent) break
      const childPermission = child.trimmed.match(
        /^[A-Za-z0-9_-]+\s*:\s*(.*?)\s*$/,
      )
      if (
        childPermission !== null &&
        unquote(childPermission[1]).toLowerCase() === "write"
      ) {
        addDiagnostic(
          path,
          child.line,
          "workflow-permissions",
          "workflow-level write permission is forbidden; move minimum access to the job",
        )
      }
    }
  }
}

function jobsFrom(records) {
  const jobs = []
  const jobsIndex = records.findIndex(
    (record) => record.indent === 0 && record.trimmed === "jobs:",
  )
  if (jobsIndex < 0) return jobs

  let jobIndent = null
  for (let index = jobsIndex + 1; index < records.length; index++) {
    const record = records[index]
    if (!record.trimmed) continue
    if (record.indent === 0) break
    if (jobIndent === null) jobIndent = record.indent
    if (record.indent !== jobIndent) continue
    const match = record.trimmed.match(/^([A-Za-z0-9_-]+)\s*:\s*$/)
    if (!match) continue
    jobs.push({ id: match[1], start: index, end: records.length })
  }

  for (let index = 0; index < jobs.length; index++) {
    jobs[index].end = jobs[index + 1]?.start ?? records.length
  }
  return jobs
}

function concurrencyAt(records, index, end) {
  const record = records[index]
  const match = record.code.match(/^\s*concurrency\s*:\s*(.*?)\s*$/)
  if (!match) return null

  let group = unquote(match[1])
  let groupLine = record.line
  let cancellation = ""
  let cancellationLine = record.line
  if (!group) {
    for (let childIndex = index + 1; childIndex < end; childIndex++) {
      const child = records[childIndex]
      if (child.trimmed && child.indent <= record.indent) break
      const groupMatch = child.code.match(/^\s*group\s*:\s*(.*?)\s*$/)
      if (groupMatch) {
        group = unquote(groupMatch[1])
        groupLine = child.line
      }
      const cancellationMatch = child.code.match(
        /^\s*cancel-in-progress\s*:\s*(.*?)\s*$/,
      )
      if (cancellationMatch) {
        cancellation = unquote(cancellationMatch[1])
        cancellationLine = child.line
      }
    }
  }

  return {
    stable: /^[A-Za-z0-9._/-]+$/.test(group) && !group.includes("${{"),
    cancelFalse: cancellation === "false",
    groupLine,
    cancellationLine,
    line: record.line,
  }
}

function workflowConcurrency(records) {
  const candidates = []
  for (let index = 0; index < records.length; index++) {
    if (records[index].indent !== 0) continue
    const candidate = concurrencyAt(records, index, records.length)
    if (candidate) candidates.push(candidate)
  }
  return candidates
}

function jobConcurrency(records, job, directIndent) {
  const candidates = []
  for (let index = job.start + 1; index < job.end; index++) {
    if (records[index].indent !== directIndent) continue
    const candidate = concurrencyAt(records, index, job.end)
    if (candidate) candidates.push(candidate)
  }
  return candidates
}

function productionJobs(path, records, azureLoginLines) {
  const globalConcurrency = workflowConcurrency(records)
  for (const job of jobsFrom(records)) {
    const body = records.slice(job.start + 1, job.end)
    const directIndent = Math.min(
      ...body.filter((record) => record.trimmed).map((record) => record.indent),
    )
    const azureLogin = body.some((record) => {
      const value = usesValue(record.code)
      return value !== null && /^azure\/login@/i.test(value)
    })
    const name = body.find(
      (record) =>
        record.indent === directIndent && /^name\s*:/i.test(record.trimmed),
    )
    const productionDeploy =
      /deploy/i.test(job.id) && /prod(?:uction)?/i.test(job.id)
        ? true
        : name
          ? /deploy/i.test(name.trimmed) && /production/i.test(name.trimmed)
          : false
    if (!azureLogin && !productionDeploy) continue

    const environment = body.some(
      (record) =>
        record.indent === directIndent &&
        record.trimmed === "environment: production",
    )
    if (!environment) {
      addDiagnostic(
        path,
        records[job.start].line,
        "production-environment",
        "Azure/production deploy jobs require exact environment: production",
      )
    }

    const candidates = [
      ...globalConcurrency,
      ...jobConcurrency(records, job, directIndent),
    ]
    if (
      !candidates.some((candidate) => candidate.stable && candidate.cancelFalse)
    ) {
      const candidate = candidates[0]
      let line = records[job.start].line
      let message =
        "Azure/production deploy jobs require stable concurrency with cancel-in-progress: false"
      if (candidate && !candidate.stable) {
        line = candidate.groupLine
        message =
          "production concurrency group must be a fixed, non-expression value"
      } else if (candidate && !candidate.cancelFalse) {
        line = candidate.cancellationLine
        message = "production concurrency requires cancel-in-progress: false"
      }
      addDiagnostic(path, line, "production-concurrency", message)
    }
  }

  if (azureLoginLines.length > 0) {
    for (const record of records) {
      if (/^\s*creds\s*:/i.test(record.code)) {
        addDiagnostic(
          path,
          record.line,
          "azure-credentials",
          "azure/login creds is forbidden; use OIDC without a client secret",
        )
      }
    }
  }
}

function scanWorkflow(path, contents) {
  const records = recordsFor(contents)
  const azureLoginLines = []
  let runBlockIndent = null

  for (const record of records) {
    if (/^\t/.test(record.raw)) {
      addDiagnostic(
        path,
        record.line,
        "source-policy",
        "tab-indented YAML cannot be checked safely; use spaces and actionlint",
      )
    }

    if (runBlockIndent !== null) {
      if (record.raw.trim() && record.indent <= runBlockIndent) {
        runBlockIndent = null
      } else {
        if (eventExpression.test(record.raw)) {
          addDiagnostic(
            path,
            record.line,
            "event-in-run",
            "github.event.* must not be interpolated directly in shell content; use a validated env value",
          )
        }
        if (/\bAZURE_CLIENT_SECRET\b/.test(record.raw)) {
          addDiagnostic(
            path,
            record.line,
            "azure-credentials",
            "AZURE_CLIENT_SECRET references are forbidden; use OIDC",
          )
        }
        if (latestReference.test(record.raw)) {
          addDiagnostic(
            path,
            record.line,
            "mutable-latest",
            "mutable latest image/action references are forbidden; pin an immutable digest or SHA",
          )
        }
        continue
      }
    }

    if (!record.trimmed) continue
    if (hasPullRequestTarget(record)) {
      addDiagnostic(
        path,
        record.line,
        "pull-request-target",
        "pull_request_target is forbidden; use pull_request with least privilege",
      )
    }
    if (/\bAZURE_CLIENT_SECRET\b/.test(record.code)) {
      addDiagnostic(
        path,
        record.line,
        "azure-credentials",
        "AZURE_CLIENT_SECRET references are forbidden; use OIDC",
      )
    }

    const run = record.code.match(/^\s*(?:-\s*)?run\s*:\s*(.*?)\s*$/)
    if (run) {
      if (eventExpression.test(run[1])) {
        addDiagnostic(
          path,
          record.line,
          "event-in-run",
          "github.event.* must not be interpolated directly in run; use a validated env value",
        )
      }
      if (/^[|>](?:[+-]|[1-9]|[+-][1-9]|[1-9][+-])?$/.test(run[1])) {
        runBlockIndent = record.indent
      }
    }

    if (hasFlowStyleUses(record.code)) {
      addDiagnostic(
        path,
        record.line,
        "unsupported-uses-syntax",
        "flow-style uses cannot be checked safely; use a block-style uses key pinned to an immutable SHA or digest",
      )
    }

    const value = usesValue(record.code)
    if (value !== null) {
      if (/^azure\/login@/i.test(value)) azureLoginLines.push(record.line)
      if (value.startsWith("./")) continue
      if (latestReference.test(value)) {
        addDiagnostic(
          path,
          record.line,
          "mutable-latest",
          "mutable latest action/image references are forbidden; pin an immutable SHA or digest",
        )
      }
      if (!actionReference.test(value) && !dockerReference.test(value)) {
        addDiagnostic(
          path,
          record.line,
          "pinned-uses",
          "remote uses must pin an exact 40-character lowercase commit SHA or docker sha256 digest",
        )
      }
      if (!releaseComment.test(record.comment)) {
        addDiagnostic(
          path,
          record.line,
          "release-comment",
          "remote uses requires a same-line release comment such as # v7.0.1",
        )
      }
    }

    if (
      latestReference.test(record.code) &&
      (/^\s*(?:image|container)\s*:/i.test(record.code) || run !== null)
    ) {
      addDiagnostic(
        path,
        record.line,
        "mutable-latest",
        "mutable latest image/action references are forbidden; pin an immutable digest or SHA",
      )
    }
  }

  workflowPermissions(path, records)
  productionJobs(path, records, azureLoginLines)
}

function readable(mode, mask) {
  return (mode & mask) !== 0
}

async function explicitCandidates(arguments_) {
  const candidates = []
  for (const argument of arguments_) {
    const path = resolve(argument)
    if (!yamlExtensions.has(extname(path).toLowerCase())) {
      addDiagnostic(path, 0, "path", "expected an explicit .yml or .yaml file")
      continue
    }
    let metadata
    try {
      metadata = await lstat(path)
    } catch {
      addDiagnostic(path, 0, "path", "expected a readable YAML file")
      continue
    }
    if (!metadata.isFile()) {
      addDiagnostic(path, 0, "path", "expected a regular YAML file")
      continue
    }
    if (!readable(metadata.mode, 0o444)) {
      addDiagnostic(path, 0, "path", "expected a readable YAML file")
      continue
    }
    candidates.push(path)
  }
  return [...new Set(candidates)].sort()
}

async function defaultCandidates() {
  const directory = resolve(".github/workflows")
  let metadata
  try {
    metadata = await lstat(directory)
  } catch (error) {
    if (error.code === "ENOENT") return []
    addDiagnostic(
      directory,
      0,
      "path",
      "default workflow directory is unreadable",
    )
    return []
  }
  if (!metadata.isDirectory()) {
    addDiagnostic(
      directory,
      0,
      "path",
      "default workflow path must be a directory",
    )
    return []
  }
  if (!readable(metadata.mode, 0o555)) {
    addDiagnostic(
      directory,
      0,
      "path",
      "default workflow directory is unreadable",
    )
    return []
  }

  try {
    return (await readdir(directory, { withFileTypes: true }))
      .filter(
        (entry) =>
          entry.isFile() &&
          yamlExtensions.has(extname(entry.name).toLowerCase()),
      )
      .map((entry) => resolve(directory, entry.name))
      .sort()
  } catch {
    addDiagnostic(
      directory,
      0,
      "path",
      "default workflow directory is unreadable",
    )
    return []
  }
}

const candidates =
  process.argv.length > 2
    ? await explicitCandidates(process.argv.slice(2))
    : await defaultCandidates()

for (const path of candidates) {
  try {
    scanWorkflow(path, await readFile(path, "utf8"))
  } catch {
    addDiagnostic(path, 0, "path", "expected a readable YAML file")
  }
}

diagnostics.sort(
  (left, right) =>
    left.path.localeCompare(right.path) ||
    left.line - right.line ||
    left.rule.localeCompare(right.rule) ||
    left.message.localeCompare(right.message),
)

if (diagnostics.length > 0) {
  process.stderr.write(
    diagnostics
      .map(
        ({ path, line, rule, message }) =>
          `${path}:${line}: [${rule}] ${message}`,
      )
      .join("\n") + "\n",
  )
  process.exitCode = 1
}
