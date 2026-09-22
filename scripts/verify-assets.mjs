import { readdir, readFile, stat } from "node:fs/promises"
import { fileURLToPath } from "node:url"

import sharp from "sharp"

const coversDirectory = fileURLToPath(
  new URL("../internal/webassets/covers/", import.meta.url),
)
const manifest = JSON.parse(
  await readFile(
    new URL("../internal/webassets/covers/manifest.json", import.meta.url),
  ),
)
const maximumBytes = 300 * 1024
const requiredPairs = new Set([
  "landscape:avif",
  "landscape:webp",
  "card:avif",
  "card:webp",
])

assert(manifest.version === 1, "manifest version must be 1")
assert(manifest.assets.length === 12, "manifest must contain exactly 12 assets")

const ids = new Set()
const listedFiles = new Set()
for (const asset of manifest.assets) {
  assert(!ids.has(asset.id), `duplicate asset ID: ${asset.id}`)
  assert(asset.alt?.trim(), `asset ${asset.id} lacks alt text`)
  assert(asset.usageRole?.trim(), `asset ${asset.id} lacks a usage role`)
  ids.add(asset.id)

  const pairs = new Set()
  for (const derivative of asset.derivatives) {
    const pair = `${derivative.variant}:${derivative.format}`
    assert(requiredPairs.has(pair), `unexpected derivative ${asset.id}/${pair}`)
    assert(!pairs.has(pair), `duplicate derivative ${asset.id}/${pair}`)
    pairs.add(pair)
    assert(
      derivative.filename ===
        `${asset.id}-${derivative.variant}.${derivative.format}`,
      `unstable filename for ${asset.id}/${pair}`,
    )
    assert(
      !listedFiles.has(derivative.filename),
      `duplicate file ${derivative.filename}`,
    )
    listedFiles.add(derivative.filename)

    const pathname = `${coversDirectory}/${derivative.filename}`
    const [metadata, file] = await Promise.all([
      sharp(pathname).metadata(),
      stat(pathname),
    ])
    const expectedSharpFormat =
      derivative.format === "avif" ? "heif" : derivative.format
    assert(
      metadata.format === expectedSharpFormat,
      `wrong format for ${derivative.filename}`,
    )
    assert(
      metadata.width === derivative.width &&
        metadata.height === derivative.height,
      `wrong dimensions for ${derivative.filename}`,
    )
    assert(
      file.size > 0 && file.size <= maximumBytes,
      `oversized ${derivative.filename}: ${file.size}`,
    )
    assert(
      !metadata.exif && !metadata.iptc && !metadata.xmp,
      `metadata remains in ${derivative.filename}`,
    )
  }
  assert(
    pairs.size === requiredPairs.size &&
      [...requiredPairs].every((pair) => pairs.has(pair)),
    `asset ${asset.id} lacks a required derivative`,
  )
}

const directoryFiles = await readdir(coversDirectory)
const allowedFiles = new Set(["manifest.json", ...listedFiles])
assert(
  directoryFiles.length === allowedFiles.size,
  "covers directory contains missing or unlisted files",
)
for (const filename of directoryFiles) {
  assert(allowedFiles.has(filename), `unlisted cover file: ${filename}`)
}

console.log(
  `Verified ${ids.size} editorial assets and ${listedFiles.size} derivatives.`,
)

function assert(condition, message) {
  if (!condition) {
    throw new Error(message)
  }
}
