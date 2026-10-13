/**
 * The subscription screen and every capcode row's upsell modal call
 * setupPurchaseListeners on mount. The store delivers one purchase event to
 * every native subscription, so each extra registration used to mean another
 * server validation for the same purchase (one user hit Google's per-user rate
 * limit with 110 validations in five seconds).
 */
import { Platform } from "react-native"

const mockPurchaseListeners: Array<(purchase: any) => void> = []

jest.mock("expo-iap", () => ({
  initConnection: jest.fn(async () => true),
  endConnection: jest.fn(async () => {}),
  fetchProducts: jest.fn(async () => []),
  requestPurchase: jest.fn(),
  getAvailablePurchases: jest.fn(async () => []),
  finishTransaction: jest.fn(async () => {}),
  getTransactionJwsIOS: jest.fn(async () => ""),
  purchaseUpdatedListener: jest.fn((listener: (purchase: any) => void) => {
    mockPurchaseListeners.push(listener)
    return {
      remove: () => {
        const i = mockPurchaseListeners.indexOf(listener)
        if (i >= 0) mockPurchaseListeners.splice(i, 1)
      },
    }
  }),
  purchaseErrorListener: jest.fn(() => ({ remove: jest.fn() })),
}))

jest.mock("./api-client", () => ({ apiBaseUrl: () => "http://server.test" }))

type Service = typeof import("./purchase-service")
let service: Service

const purchase = {
  productId: "io.devlan.rfspager.monthly",
  purchaseToken: "play-token",
  transactionId: "GPA.1234",
}

// The store emits one event; the native bridge hands it to every subscription.
const emitPurchaseUpdated = () =>
  Promise.all([...mockPurchaseListeners].map((listener) => listener(purchase)))

beforeEach(() => {
  jest.isolateModules(() => {
    service = require("./purchase-service")
  })
  Platform.OS = "android"
  service.setIAPAuthToken("jwt")
  global.fetch = jest.fn(async () => ({
    ok: true,
    json: async () => ({
      valid: true,
      product_id: purchase.productId,
      expires_at: null,
      will_renew: true,
    }),
  })) as any
})

afterEach(() => {
  mockPurchaseListeners.length = 0
  jest.clearAllMocks()
})

test("one purchase event is validated with the server once, however many screens listen", async () => {
  await service.initIAP()
  const onSuccess = [jest.fn(), jest.fn(), jest.fn()]
  const cleanups = onSuccess.map((cb) => service.setupPurchaseListeners(cb, jest.fn()))

  await emitPurchaseUpdated()

  expect(global.fetch).toHaveBeenCalledTimes(1)
  // Every screen still hears about the purchase.
  for (const cb of onSuccess) expect(cb).toHaveBeenCalledTimes(1)
  cleanups.forEach((cleanup) => cleanup())
})

test("cleaning up every screen leaves no native listener behind", async () => {
  await service.initIAP()
  const cleanups = [1, 2, 3].map(() => service.setupPurchaseListeners(jest.fn(), jest.fn()))

  cleanups.forEach((cleanup) => cleanup())

  expect(mockPurchaseListeners).toHaveLength(0)
})

test("a screen that mounts before the store connection is ready still gets purchase events", async () => {
  const onSuccess = jest.fn()
  service.setupPurchaseListeners(onSuccess, jest.fn())
  await service.initIAP()

  await emitPurchaseUpdated()

  expect(onSuccess).toHaveBeenCalledTimes(1)
})
