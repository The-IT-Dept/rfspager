import { pagerEmptyState } from "./pager-empty-state"

test("filtering with no capcodes selected points at the capcode picker", () => {
  const state = pagerEmptyState({ filterOn: true, filterCount: 0 })
  expect(state.title).toBe("No Capcodes Selected")
  expect(state.action).toBe("select")
})

test("filtering on capcodes that have had no pages says so, with the count", () => {
  const state = pagerEmptyState({ filterOn: true, filterCount: 47 })
  expect(state.title).toBe("No Pages Yet")
  expect(state.body).toContain("47 filtered capcodes")
  expect(state.action).toBe("adjust")
})

test("one filtered capcode reads naturally", () => {
  expect(pagerEmptyState({ filterOn: true, filterCount: 1 }).body).toContain(
    "Your filtered capcode hasn't",
  )
})

test("an unfiltered empty feed is just empty", () => {
  const state = pagerEmptyState({ filterOn: false, filterCount: 47 })
  expect(state.title).toBe("No Messages")
  expect(state.action).toBeUndefined()
})
