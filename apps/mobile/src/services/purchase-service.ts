/**
 * Purchase Service - IAP using expo-iap
 * Uses StoreKit 2 JWS for server-side validation (Apple)
 */
import { Platform } from "react-native"
import { apiBaseUrl } from "./api-client"
import {
  initConnection,
  endConnection,
  fetchProducts,
  requestPurchase,
  getAvailablePurchases,
  finishTransaction,
  purchaseUpdatedListener,
  purchaseErrorListener,
  getTransactionJwsIOS,
  type Purchase,
  type ProductSubscription,
} from "expo-iap"

// Store error codes changed from SCREAMING_CASE to kebab-case in expo-iap v4.
const isUserCancelled = (code: string | undefined) =>
  code === "E_USER_CANCELLED" || code === "user-cancelled"

const PRODUCT_SKUS = ["io.devlan.rfspager.monthly", "io.devlan.rfspager.yearly"]

const API_BASE = apiBaseUrl()
const IAP_API = `${API_BASE}/api/v2/mobile`

let authToken: string | null = null
let onSubscriptionSynced: (() => void) | null = null

export function setIAPAuthToken(token: string | null) {
  authToken = token
}

export function setOnSubscriptionSynced(callback: () => void) {
  onSubscriptionSynced = callback
}

async function apiPost(path: string, body: any) {
  const res = await fetch(`${IAP_API}${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(authToken ? { Authorization: `Bearer ${authToken}` } : {}),
    },
    body: JSON.stringify(body),
  })
  const data = await res.json()
  if (!res.ok) throw new Error(data.error || `Purchase verification failed (${res.status})`)
  return data
}

export interface PurchaseProduct {
  productId: string
  title: string
  description: string
  localizedPrice: string
  price: number
  currency: string
}

export interface ValidationResult {
  valid: boolean
  product_id: string
  expires_at: string | null
  will_renew: boolean
}

let purchaseUpdateSubscription: any = null
let purchaseErrorSubscription: any = null
let connectionReady = false

/**
 * Initialize IAP connection. Call once on app startup.
 */
export async function initIAP(): Promise<void> {
  try {
    await initConnection()
    connectionReady = true
    // Sync subscription status on startup
    syncSubscriptionOnStartup().catch((err) =>
      console.warn("[IAP] startup sync failed:", err),
    )
  } catch (err) {
    console.warn("[IAP] initConnection failed:", err)
  }
}

/**
 * On startup, check for active purchases and validate with server.
 * This ensures the server stays in sync if a renewal happened while offline.
 */
async function syncSubscriptionOnStartup(): Promise<void> {
  if (!authToken) return // Not logged in yet

  try {
    const purchases = await getAvailablePurchases()
    if (purchases.length === 0) return

    if (Platform.OS === "ios") {
      const jwsList: string[] = []
      for (const p of purchases) {
        const jws = await getJWSForProduct(p.productId)
        if (jws) jwsList.push(jws)
      }

      if (jwsList.length > 0) {
        const result = await apiPost("/iap/restore", {
          platform: "ios",
          jws_transactions: jwsList,
        })
        if (result?.restored) onSubscriptionSynced?.()
        return
      }
    }
  } catch (err) {
    console.warn("[IAP] syncSubscriptionOnStartup error:", err)
  }
}

/**
 * Set up purchase listeners. Returns cleanup function.
 * Safe to call before initConnection — listeners are deferred until ready.
 */
export function setupPurchaseListeners(
  onPurchaseSuccess: (result: ValidationResult) => void,
  onPurchaseError: (error: any) => void,
): () => void {
  if (!connectionReady) {
    return () => {}
  }

  try {
  purchaseUpdateSubscription = purchaseUpdatedListener(async (purchase: Purchase) => {
    try {
      const result = await validatePurchase(purchase)
      if (result.valid) {
        await finishTransaction({ purchase, isConsumable: false })
        onPurchaseSuccess(result)
      }
    } catch (err) {
      onPurchaseError(err)
    }
  })

  purchaseErrorSubscription = purchaseErrorListener((error: any) => {
    if (!isUserCancelled(error?.code)) {
      onPurchaseError(error)
    }
  })
  } catch (err) {
    console.warn("[IAP] Failed to set up listeners:", err)
  }

  return () => {
    purchaseUpdateSubscription?.remove()
    purchaseErrorSubscription?.remove()
  }
}

/**
 * Fetch available subscription products from the store.
 */
export async function getProducts(): Promise<PurchaseProduct[]> {
  try {
    const products = await fetchProducts({ skus: PRODUCT_SKUS, type: "subs" })
    if (!products) return []

    return (products as ProductSubscription[]).map((sub) => ({
      productId: sub.id,
      title: sub.title || sub.id,
      description: sub.description || "",
      localizedPrice: sub.displayPrice || `${sub.price ?? 0}`,
      price: sub.price ?? 0,
      currency: sub.currency || "AUD",
    }))
  } catch (err) {
    console.error("[IAP] getProducts failed:", err)
    return []
  }
}

/**
 * Initiate a subscription purchase.
 */
export async function purchaseSubscription(productId: string): Promise<void> {
  if (Platform.OS === "ios") {
    await requestPurchase({
      type: "subs",
      request: { apple: { sku: productId } },
    })
  } else {
    await requestPurchase({
      type: "subs",
      request: { google: { skus: [productId] } },
    })
  }
}

/**
 * Get the JWS signed transaction for a product (iOS only).
 */
async function getJWSForProduct(productId: string): Promise<string> {
  if (Platform.OS !== "ios") return ""
  try {
    return (await getTransactionJwsIOS(productId)) ?? ""
  } catch (err) {
    console.warn("[IAP] getTransactionJwsIOS failed:", err)
    return ""
  }
}

/**
 * Validate a purchase with our Go server.
 */
async function validatePurchase(purchase: Purchase): Promise<ValidationResult> {
  if (Platform.OS === "ios") {
    const jws = await getJWSForProduct(purchase.productId)
    return apiPost("/iap/apple/validate", {
      transaction_id: purchase.transactionId,
      jws_transaction: jws,
    })
  } else {
    return apiPost("/iap/google/validate", {
      purchase_token: purchase.purchaseToken,
      product_id: purchase.productId,
    })
  }
}

/**
 * The restore endpoint answers {restored, ...}, not the validate shape. The
 * screens read `.valid`, so normalise it here.
 */
function toValidationResult(data: any): ValidationResult {
  return {
    valid: Boolean(data?.restored ?? data?.valid),
    product_id: data?.product_id ?? "",
    expires_at: data?.expires_at ?? null,
    will_renew: data?.will_renew ?? false,
  }
}

/**
 * Restore previous purchases.
 */
export async function restorePurchases(): Promise<ValidationResult | null> {
  const purchases = await getAvailablePurchases()

  if (purchases.length === 0) {
    return null
  }

  if (Platform.OS === "ios") {
    // Get JWS for each purchase and send to server
    const jwsList: string[] = []
    for (const p of purchases) {
      const jws = await getJWSForProduct(p.productId)
      if (jws) jwsList.push(jws)
    }

    if (jwsList.length > 0) {
      return toValidationResult(
        await apiPost("/iap/restore", {
          platform: "ios",
          jws_transactions: jwsList,
        }),
      )
    }

    // Fallback to transaction IDs
    const transactionIds = purchases.map((p) => p.transactionId).filter(Boolean)
    if (transactionIds.length === 0) return null

    return toValidationResult(
      await apiPost("/iap/restore", {
        platform: "ios",
        transaction_ids: transactionIds,
      }),
    )
  } else {
    const purchaseData = purchases.map((p) => ({
      purchase_token: p.purchaseToken,
      product_id: p.productId,
    }))

    return toValidationResult(
      await apiPost("/iap/restore", {
        platform: "android",
        purchases: purchaseData,
      }),
    )
  }
}

/**
 * Clean up IAP connection.
 */
export async function endIAP(): Promise<void> {
  purchaseUpdateSubscription?.remove()
  purchaseErrorSubscription?.remove()
  await endConnection()
}
