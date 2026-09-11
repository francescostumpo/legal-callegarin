import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import App from "./App"

describe("App", () => {
  it("renders the administration console heading", () => {
    render(<App />)

    expect(
      screen.getByRole("heading", { name: "Console di amministrazione" }),
    ).toBeInTheDocument()
  })
})
