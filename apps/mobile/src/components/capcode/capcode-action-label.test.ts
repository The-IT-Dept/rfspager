import { capcodeActionLabel } from "./capcode-action-label"

// Colour alone did not tell a user whether the red button meant on or off.
test("an active filter button says it is filtering", () => {
  expect(capcodeActionLabel("filter", true)).toBe("Filtering")
  expect(capcodeActionLabel("filter", false)).toBe("Filter")
})

test("an active notify button says it is notifying", () => {
  expect(capcodeActionLabel("notify", true)).toBe("Notifying")
  expect(capcodeActionLabel("notify", false)).toBe("Notify")
})
