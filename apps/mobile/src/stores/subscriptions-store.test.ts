jest.mock("../services/api-client", () => ({
  api: {
    subscriptions: { list: jest.fn(async () => [12, 34]), create: jest.fn(), delete: jest.fn() },
    filters: { list: jest.fn(async () => [56]), create: jest.fn(), delete: jest.fn() },
  },
}))

import { api } from "../services/api-client"
import { useSubscriptionsStore } from "./subscriptions-store"

beforeEach(() => {
  useSubscriptionsStore.setState({ subscriptions: [], filters: [], loaded: false })
  jest.clearAllMocks()
})

test("load() fetches the saved subscriptions and filters and marks the store loaded", async () => {
  await useSubscriptionsStore.getState().load()

  const state = useSubscriptionsStore.getState()
  expect(state.filters).toEqual(["56"])
  expect(state.subscriptions).toEqual(["12", "34"])
  expect(state.loaded).toBe(true)
})

test("concurrent load() calls from several screens fetch once", async () => {
  const store = useSubscriptionsStore.getState()
  await Promise.all([store.load(), store.load(), store.load()])

  expect(api.filters.list).toHaveBeenCalledTimes(1)
  expect(useSubscriptionsStore.getState().loaded).toBe(true)
})
