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
