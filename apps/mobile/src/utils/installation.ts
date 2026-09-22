import { Platform } from "react-native"
import * as Application from "expo-application"
import * as Device from "expo-device"
import * as Localization from "expo-localization"
import { getMessaging, getToken, getAPNSToken } from "@react-native-firebase/messaging"
import AsyncStorage from "@react-native-async-storage/async-storage"
import uuid from "react-native-uuid"

// Installation IDs identify a registration; authorization is enforced by the
// server. Separate IDs per account preserve ownership when a device signs out
// and a different person signs in.
const pendingIds = new Map<string, Promise<string>>()
export const getInstallationId = (userId: string): Promise<string> => {
  if (!userId) return Promise.reject(new Error("A user is required for push registration"))
  const pending = pendingIds.get(userId)
  if (pending) return pending
  const request = (async () => {
    const key = `rfspager.installation.${userId}`
    const existing = await AsyncStorage.getItem(key)
    if (existing) return existing
    const id = String(uuid.v4()).toLowerCase()
    await AsyncStorage.setItem(key, id)
    return id
  })()
  pendingIds.set(userId, request)
  void request.finally(() => pendingIds.delete(userId)).catch(() => {})
  return request
}

export const getInstallationInput = async () => {
  // Now we can create / update the installation.
  const { token, expoToken } = await registerForPushNotificationsAsync()

  return {
    push_type: Device.manufacturer === "Apple" ? "APN" : "GCM",
    push_token: token,
    expo_token: expoToken,

    vendor: Device.manufacturer,
    version: Device.osVersion,
    model: Device.modelName,

    app_id: Application.applicationId,
    app_name: Application.applicationName,
    app_version: Application.nativeApplicationVersion,

    timezone: Localization.getCalendars()[0]?.timeZone || "UTC",
  }
}

async function registerForPushNotificationsAsync() {
  let token: string | undefined
  let expoToken: string | undefined

  if (Device.isDevice) {
    expoToken = await getToken(getMessaging())

    if (Platform.OS === "ios") {
      token = (await getAPNSToken(getMessaging())) ?? undefined
    } else {
      token = expoToken
    }
  }

  return { token, expoToken }
}
