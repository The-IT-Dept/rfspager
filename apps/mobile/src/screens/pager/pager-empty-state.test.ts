import { hideFeedForEmptyFilter, pagerEmptyState } from "./pager-empty-state"

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

// The filter list lives in a client store that is fetched after launch. Until
// it arrives, an empty list means "unknown", not "none selected": one user saw
// "No Capcodes Selected" on every cold start although 40 capcodes were saved.
test("before the filter list has loaded, an empty list is not reported as no capcodes selected", () => {
  const state = pagerEmptyState({ filterOn: true, filterCount: 0, filtersLoaded: false })
  expect(state.title).toBe("No Messages")
  expect(state.action).toBeUndefined()
})

test("the feed is only hidden for an empty filter once the filter list has loaded", () => {
  expect(hideFeedForEmptyFilter({ filterOn: true, filterCount: 0, filtersLoaded: false })).toBe(
    false,
  )
  expect(hideFeedForEmptyFilter({ filterOn: true, filterCount: 0, filtersLoaded: true })).toBe(true)
  expect(hideFeedForEmptyFilter({ filterOn: true, filterCount: 3, filtersLoaded: true })).toBe(
    false,
  )
  expect(hideFeedForEmptyFilter({ filterOn: false, filterCount: 0, filtersLoaded: true })).toBe(
    false,
  )
})
