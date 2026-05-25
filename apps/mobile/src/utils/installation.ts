import { Platform } from "react-native"
import * as Application from "expo-application"
import * as Device from "expo-device"
import * as Localization from "expo-localization"
import { getMessaging, getToken, getAPNSToken } from "@react-native-firebase/messaging"
import { getUniqueId } from "react-native-device-info"

const stringToUuid = (str: string) => {
  // Ensure str is a string and handle edge cases
  const stringValue = str ? String(str) : "unknown-device"

  const cleanStr = stringValue.replace(/-/g, "")
  const uuid = "xxxxxxxx-xxxx-4xxx-xxxx-xxxxxxxxxxxx".replace(/[x]/g, function (_, p) {
    return cleanStr[p % cleanStr.length]
  })

  return uuid
}

export const getInstallationId = async () => {
  try {
    const uniqueId = await getUniqueId()
    return stringToUuid(uniqueId)
  } catch (error) {
    // Fallback to a timestamp-based ID
    const fallbackId = `fallback-${Date.now()}`
    return stringToUuid(fallbackId)
  }
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
      token = await getAPNSToken(getMessaging())
    } else {
      token = expoToken
    }
  }

  return { token, expoToken }
}
