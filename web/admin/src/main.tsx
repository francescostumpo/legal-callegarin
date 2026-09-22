import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import "@fontsource-variable/fraunces"
import "@fontsource-variable/source-sans-3"

import App from "./App"

const root = document.getElementById("root")

if (root === null) {
  throw new Error("Missing admin root element")
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
