import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"
import test from "node:test"
import { JSDOM } from "jsdom"

const source = await readFile(
  new URL("../internal/webassets/public/nav.js", import.meta.url),
  "utf8",
)

function page() {
  return new JSDOM(
    `<!doctype html><form data-contact-form action="/contatti" method="post">
      <input name="name" value="Mario Rossi"><input name="email" value="mario@example.test">
      <input name="phone" value=""><textarea name="message">Messaggio sufficientemente lungo</textarea>
      <input name="privacy" type="checkbox" value="accepted" checked>
      <input name="website" value=""><input name="started" value="signed-time">
      <button type="submit">Invia</button><div data-contact-feedback></div>
    </form>`,
    { url: "https://studio.example.test/contatti", runScripts: "outside-only" },
  )
}

test("contact enhancement retains DOM values on recoverable failure and prevents double submit", async () => {
  const dom = page()
  let calls = 0
  let resolveFetch
  dom.window.fetch = () => {
    calls++
    return new Promise((resolve) => {
      resolveFetch = resolve
    })
  }
  dom.window.eval(source)
  const form = dom.window.document.querySelector("form")
  form.dispatchEvent(
    new dom.window.Event("submit", { bubbles: true, cancelable: true }),
  )
  form.dispatchEvent(
    new dom.window.Event("submit", { bubbles: true, cancelable: true }),
  )
  assert.equal(calls, 1)
  assert.equal(form.querySelector("button").disabled, true)
  resolveFetch({
    ok: false,
    json: async () => ({
      error: {
        code: "contact_unavailable",
        message: "Servizio temporaneamente non disponibile.",
        requestId: "request-1",
        fields: {},
      },
    }),
  })
  await new Promise((resolve) => dom.window.setTimeout(resolve, 0))
  assert.equal(form.elements.name.value, "Mario Rossi")
  assert.equal(form.elements.email.value, "mario@example.test")
  assert.equal(form.querySelector("button").disabled, false)
  const feedback = form.querySelector("[data-contact-feedback]")
  assert.equal(feedback.getAttribute("role"), "alert")
  assert.match(feedback.textContent, /temporaneamente non disponibile/i)
  assert.equal(feedback.innerHTML.includes("<script"), false)
})

test("contact enhancement submits all protection fields and resets only after success", async () => {
  const dom = page()
  let submitted
  dom.window.fetch = async (url, options) => {
    submitted = { url, options }
    return {
      ok: true,
      json: async () => ({ redirect: "/contatti?esito=signed" }),
    }
  }
  let navigated = ""
  dom.window.history.pushState = (_, __, url) => {
    navigated = String(url)
  }
  dom.window.eval(
    source.replace(
      "window.location.assign(payload.redirect)",
      "window.history.pushState({}, '', payload.redirect)",
    ),
  )
  const form = dom.window.document.querySelector("form")
  form.dispatchEvent(
    new dom.window.Event("submit", { bubbles: true, cancelable: true }),
  )
  await new Promise((resolve) => dom.window.setTimeout(resolve, 0))
  assert.equal(submitted.url, "/contatti")
  assert.equal(submitted.options.credentials, "same-origin")
  assert.equal(submitted.options.headers.Accept, "application/json")
  const body = submitted.options.body
  for (const field of [
    "name",
    "email",
    "phone",
    "message",
    "privacy",
    "website",
    "started",
  ]) {
    assert.equal(body.has(field), true, field)
  }
  assert.equal(form.elements.name.value, "")
  assert.equal(navigated, "/contatti?esito=signed")
  assert.doesNotMatch(source, /localStorage|sessionStorage|document\.cookie/)
})

test("contact enhancement aborts an in-flight request when the page leaves", async () => {
  const dom = page()
  let signal
  dom.window.fetch = (_, options) => {
    signal = options.signal
    return new Promise(() => {})
  }
  dom.window.eval(source)
  dom.window.document
    .querySelector("form")
    .dispatchEvent(
      new dom.window.Event("submit", { bubbles: true, cancelable: true }),
    )
  assert.equal(signal?.aborted, false)
  dom.window.dispatchEvent(new dom.window.Event("pagehide"))
  assert.equal(signal?.aborted, true)
})

test("contact enhancement refuses a form endpoint outside the current origin", async () => {
  const dom = page()
  dom.window.document
    .querySelector("form")
    .setAttribute("action", "https://external.example/collect")
  let calls = 0
  const fetchMock = () => {
    calls++
    throw new Error("external fetch must not run")
  }
  dom.window.fetch = fetchMock
  dom.window.eval(source)
  const form = dom.window.document.querySelector("form")
  form.dispatchEvent(
    new dom.window.Event("submit", { bubbles: true, cancelable: true }),
  )
  await new Promise((resolve) => dom.window.setTimeout(resolve, 0))
  assert.equal(calls, 0)
  assert.match(
    form.querySelector("[data-contact-feedback]").textContent,
    /non è stato possibile inviare/i,
  )
})
