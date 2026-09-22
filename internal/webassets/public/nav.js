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
  const generatedFieldState = new Map()
  let submitting = false
  let activeRequest = null

  window.addEventListener("pagehide", () => activeRequest?.abort())

  form.addEventListener("submit", async (event) => {
    event.preventDefault()
    if (submitting) return

    submitting = true
    if (submitButton) submitButton.disabled = true
    clearGeneratedValidation()
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

      if (payload?.error?.fields) {
        showContactValidation(payload.error.message, payload.error.fields)
      } else {
        showContactError(payload?.error?.message)
      }
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

  function showContactValidation(message, fields) {
    if (!feedback) return
    const errors = []
    for (const fieldName of ["name", "email", "phone", "message", "privacy"]) {
      const fieldMessage = fields[fieldName]
      const control = form.elements.namedItem(fieldName)
      if (typeof fieldMessage !== "string" || !fieldMessage || !control) {
        continue
      }

      generatedFieldState.set(control, {
        invalid: control.getAttribute("aria-invalid"),
        describedBy: control.getAttribute("aria-describedby"),
      })
      const errorID = `contact-${fieldName}-error`
      const descriptionIDs = new Set(
        (control.getAttribute("aria-describedby") || "")
          .split(/\s+/)
          .filter(Boolean),
      )
      descriptionIDs.add(errorID)
      control.setAttribute("aria-invalid", "true")
      control.setAttribute("aria-describedby", [...descriptionIDs].join(" "))

      const fieldError = document.createElement("p")
      fieldError.id = errorID
      fieldError.className = "field-error"
      fieldError.dataset.contactGeneratedError = ""
      fieldError.textContent = fieldMessage
      control.closest(".form-field, .form-checkbox")?.append(fieldError)
      errors.push({ fieldName, fieldMessage })
    }

    feedback.replaceChildren()
    const heading = document.createElement("h3")
    heading.textContent = message || "Controlla i dati inseriti."
    feedback.append(heading)
    if (errors.length > 0) {
      const list = document.createElement("ul")
      for (const error of errors) {
        const item = document.createElement("li")
        const link = document.createElement("a")
        link.href = `#${error.fieldName}`
        link.textContent = error.fieldMessage
        item.append(link)
        list.append(item)
      }
      feedback.append(list)
    }
    feedback.hidden = false
    feedback.setAttribute("role", "alert")
    feedback.focus()
  }

  function clearGeneratedValidation() {
    form
      .querySelectorAll("[data-contact-generated-error]")
      .forEach((error) => error.remove())
    for (const [control, state] of generatedFieldState) {
      if (state.invalid === null) control.removeAttribute("aria-invalid")
      else control.setAttribute("aria-invalid", state.invalid)
      if (state.describedBy === null)
        control.removeAttribute("aria-describedby")
      else control.setAttribute("aria-describedby", state.describedBy)
    }
    generatedFieldState.clear()
  }
}
