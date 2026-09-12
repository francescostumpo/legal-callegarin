document.documentElement.classList.add("js-enabled")

const navigations = document.querySelectorAll("[data-navigation]")

for (const navigation of navigations) {
  const summary = navigation.querySelector("summary")
  const closeButton = navigation.querySelector("[data-navigation-close]")
  const firstLink = navigation.querySelector("nav a")

  navigation.addEventListener("toggle", () => {
    if (navigation.open) {
      firstLink?.focus()
    }
  })

  closeButton?.addEventListener("click", () => closeNavigation(true))

  navigation.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && navigation.open) {
      event.preventDefault()
      closeNavigation(true)
    }
  })

  navigation.querySelectorAll("nav a").forEach((link) => {
    link.addEventListener("click", () => closeNavigation(false))
  })

  document.addEventListener("pointerdown", (event) => {
    if (navigation.open && !navigation.contains(event.target)) {
      closeNavigation(false)
    }
  })

  function closeNavigation(returnFocus) {
    navigation.open = false
    if (returnFocus) {
      summary?.focus()
    }
  }
}

for (const form of document.querySelectorAll("[data-contact-form]")) {
  const feedback = form.querySelector("[data-contact-feedback]")
  const submitButton = form.querySelector('[type="submit"]')
  let submitting = false
  let activeRequest = null

  window.addEventListener("pagehide", () => activeRequest?.abort())

  form.addEventListener("submit", async (event) => {
    event.preventDefault()
    if (submitting) return

    submitting = true
    if (submitButton) submitButton.disabled = true
    if (feedback) {
      feedback.hidden = true
      feedback.removeAttribute("role")
      feedback.textContent = ""
    }

    try {
      const endpoint = new URL(
        form.getAttribute("action"),
        window.location.href,
      )
      if (endpoint.origin !== window.location.origin) {
        showContactError("Non è stato possibile inviare la richiesta. Riprova.")
        return
      }
      const controller = new AbortController()
      activeRequest = controller
      const payloadBody = new URLSearchParams(new FormData(form))
      const response = await fetch(endpoint.pathname + endpoint.search, {
        method: "POST",
        body: payloadBody,
        headers: { Accept: "application/json" },
        credentials: "same-origin",
        signal: controller.signal,
      })
      const payload = await response.json()
      if (response.ok && payload.redirect) {
        form.reset()
        for (const field of form.querySelectorAll(
          "input:not([type='hidden']), textarea",
        )) {
          if (field.type === "checkbox" || field.type === "radio") {
            field.checked = false
          } else {
            field.value = ""
          }
        }
        window.location.assign(payload.redirect)
        return
      }

      showContactError(payload?.error?.message)
    } catch (error) {
      if (error?.name === "AbortError") return
      showContactError(
        "Servizio temporaneamente non disponibile. Riprova tra poco.",
      )
    } finally {
      activeRequest = null
      submitting = false
      if (submitButton) submitButton.disabled = false
    }
  })

  function showContactError(message) {
    if (!feedback) return
    feedback.textContent =
      message || "Non è stato possibile inviare la richiesta. Riprova."
    feedback.hidden = false
    feedback.setAttribute("role", "alert")
    feedback.focus()
  }
}
