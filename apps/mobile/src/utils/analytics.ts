import { getAnalytics, logEvent as fbLogEvent, setUserId as fbSetUserId, setUserProperty } from "@react-native-firebase/analytics"
import { AppEventsLogger } from "react-native-fbsdk-next"

export const logEvent = async (event: string, params?: { [p: string]: any }) => {
  await fbLogEvent(getAnalytics(), event, params)
  AppEventsLogger.logEvent(event, params)
}

export const setUserId = async (id: string | null) => {
  await fbSetUserId(getAnalytics(), id)
}

export const setUserEmail = async (email: string | null) => {
  await setUserProperty(getAnalytics(), "email", email)
}
