import { getAnalytics, logEvent as fbLogEvent, setUserId as fbSetUserId, setUserProperty } from "@react-native-firebase/analytics"
import { AppEventsLogger } from "react-native-fbsdk-next"

export const logEvent = async (event: string, params?: { [p: string]: any }) => {
  if (params) {
    await fbLogEvent(getAnalytics(), event, params)
  } else {
    await fbLogEvent(getAnalytics(), event)
  }
  if (params) {
    AppEventsLogger.logEvent(event, params)
  } else {
    AppEventsLogger.logEvent(event)
  }
}

export const setUserId = async (id: string | null) => {
  await fbSetUserId(getAnalytics(), id)
}

export const setUserEmail = async (email: string | null) => {
  await setUserProperty(getAnalytics(), "email", email)
}
