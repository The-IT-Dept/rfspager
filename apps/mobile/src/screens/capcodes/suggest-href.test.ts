import { suggestCapcodeHref } from "./suggest-href"

const params = (href: string) => new URLSearchParams(href.split("?")[1])

// Ticket 302: users could only label unknown capcodes, not correct known ones.
test("a known capcode opens the form as an amendment with its current label", () => {
  const href = suggestCapcodeHref({
    capcode_id: 572,
    address: "0789000",
    alias: "Illawarra Broadcast Message",
    agency: "Illawarra",
  })
  expect(href.startsWith("/capcodes/suggest?")).toBe(true)
  const p = params(href)
  expect(p.get("capcode_id")).toBe("572")
  expect(p.get("address")).toBe("0789000")
  expect(p.get("alias")).toBe("Illawarra Broadcast Message")
  expect(p.get("agency")).toBe("Illawarra")
})

test("an unknown address opens the form for a new capcode", () => {
  const p = params(suggestCapcodeHref({ capcode_id: null, address: "1234567", alias: null }))
  expect(p.get("address")).toBe("1234567")
  expect(p.has("capcode_id")).toBe(false)
  expect(p.has("alias")).toBe(false)
  expect(p.has("agency")).toBe(false)
})

test("aliases with reserved characters survive the round trip", () => {
  const p = params(suggestCapcodeHref({ capcode_id: 1, address: "0000001", alias: "A&B #2 / 50%" }))
  expect(p.get("alias")).toBe("A&B #2 / 50%")
})
