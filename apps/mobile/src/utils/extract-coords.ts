import { Share } from "react-native"
import { getCrashlytics, log as crashlyticsLog } from "@react-native-firebase/crashlytics"

import { showLocation } from "react-native-map-link"

import { logEvent } from "./analytics"

export const extractLocation = (message: any) => {
  const re = /\[(.*?)\]/
  const res = re.exec(message.message)
  if (res === null) {
    return { longitude: 0, latitude: 0 }
  }

  const location = res[1].split(",")
  if (location.length === 2 && parseFloat(location[0]) && parseFloat(location[1])) {
    const result = {
      longitude: parseFloat(location[0]),
      latitude: parseFloat(location[1]),
    }
    return result
  }
  return { longitude: 0, latitude: 0 }
}

export const hasLocation = (message: any) => {
  const re = /\[(.*?)\]/
  const res = re.exec(message.message)
  if (res === null) {
    return false
  }

  const location = res[1].split(",")
  if (location.length === 2 && parseFloat(location[0]) && parseFloat(location[1])) {
    return message
  }

  return false
}

export const openMapWithNavigation = (message: any) => {
  logEvent("get_directions")
  const agency = message.capcode?.agency || "Address"
  const alias = message.capcode?.alias || message.address
  const latlng = extractLocation(message)

  showLocation({
    latitude: latlng.latitude,
    longitude: latlng.longitude,
    title: agency + " | " + alias,
    directionsMode: "car",
    googleForceLatLon: true,
    naverCallerName: "io.devlan.rfspager",
  })
}

export const shareMessage = async (message: any) => {
  const agency = message.capcode?.agency || "Address"
  const alias = message.capcode?.alias || message.address

  try {
    const result = await Share.share({
      message: agency + " | " + alias + ": " + message.message,
    })
    if (result.action === Share.sharedAction) {
      if (result.activityType) {
        // shared with activity type of result.activityType
        logEvent("share", [{ type: result.activityType }])
      } else {
        logEvent("share")
      }
    } else if (result.action === Share.dismissedAction) {
      logEvent("share_dismiss")
    }
  } catch (error) {
    crashlyticsLog(getCrashlytics(), String(error))
  }
}
