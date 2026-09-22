import "react-native-gesture-handler"
import { GestureHandlerRootView } from "react-native-gesture-handler"
import { KeyboardProvider } from "react-native-keyboard-controller"
import "../i18n"
import "../utils/ignore-warnings"

import React, { useEffect } from "react"
import { Appearance } from "react-native"
import { Stack } from "expo-router"
import { StatusBar } from "expo-status-bar"
import { useFonts } from "expo-font"
import { initialWindowMetrics, SafeAreaProvider } from "react-native-safe-area-context"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { enableScreens } from "react-native-screens"
import mobileAds from "react-native-google-mobile-ads"
import { requestTrackingPermissionsAsync } from "expo-tracking-transparency"
import { Settings } from "react-native-fbsdk-next"
import { getMessaging, onMessage, setBackgroundMessageHandler } from "@react-native-firebase/messaging"
import type { FirebaseMessagingTypes } from "@react-native-firebase/messaging"
import notifee, { EventType } from "@notifee/react-native"

import { initIAP } from "../services/purchase-service"
import { StoreManager } from "../components"
import { useAuthStore, usePreferencesStore } from "../stores"

enableScreens(true)

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30 * 1000,
      retry: 2,
    },
  },
})

notifee.onBackgroundEvent(async ({ type, detail }) => {
  const { notification, pressAction } = detail
  if (type === EventType.ACTION_PRESS && pressAction?.id === "mark-as-read" && notification?.id) {
    await notifee.cancelNotification(notification.id)
  }
})

function RootLayoutInner() {
  const hasHydrated = useAuthStore((s) => s._hasHydrated)
  const user = useAuthStore((s) => s.user)
  const token = useAuthStore((s) => s.token)
  const onboarded = usePreferencesStore((s) => s.onboarded)
  const darkMode = usePreferencesStore((s) => s.darkMode)

  // The app draws its own theme from this preference, but the native chrome —
  // navigation title, tab bar labels, Liquid Glass tint — follows UIKit's trait
  // collection. Since the splash gained a dark variant, UIUserInterfaceStyle is
  // "Automatic", so that chrome tracked the *system* setting instead: white
  // title and tab labels over a light app when the phone is in dark mode.
  // Pin the native colour scheme to the app's theme so both layers agree.
  useEffect(() => {
    Appearance.setColorScheme(darkMode ? "dark" : "light")
  }, [darkMode])

  const isAuthed = !!user && !!token
  const showOnboarding = !onboarded
  const showAuth = onboarded && !isAuthed
  const showApp = onboarded && isAuthed

  const [fontsLoaded] = useFonts({
    "Lato-Black": require("../../assets/fonts/Lato-Black.ttf"),
    Lato: require("../../assets/fonts/Lato-Black.ttf"),
    "Lato-Bold": require("../../assets/fonts/Lato-Bold.ttf"),
    "Lato-Regular": require("../../assets/fonts/Lato-Regular.ttf"),
    "Poppins-Bold": require("../../assets/fonts/Poppins-Bold.ttf"),
    "Poppins-Regular": require("../../assets/fonts/Poppins-Regular.ttf"),
  })

  const onMessageReceived = (message: FirebaseMessagingTypes.RemoteMessage) => {
    return notifee.displayNotification({
      title: message.notification?.title,
      body: message.notification?.body,
      ios: {
        foregroundPresentationOptions: {
          alert: true,
          sound: true,
          badge: false,
        },
      },
      android: {
        channelId: "default",
      },
    })
  }

  useEffect(() => {
    initIAP().catch(console.error)
    mobileAds().initialize().catch(console.error)
    Settings.initializeSDK()

    requestTrackingPermissionsAsync().then((status) => {
      if (status.granted) {
        Settings.setAdvertiserTrackingEnabled(true)
      }
    })

    const unsubscribe = onMessage(getMessaging(), onMessageReceived)
    setBackgroundMessageHandler(getMessaging(), onMessageReceived)

    return () => {
      unsubscribe()
    }
  }, [])

  if (!hasHydrated || !fontsLoaded) return null

  return (
    <>
      <StoreManager>
        <Stack screenOptions={{ headerShown: false }}>
          <Stack.Protected guard={showOnboarding}>
            <Stack.Screen name="(onboarding)" />
          </Stack.Protected>
          <Stack.Protected guard={showAuth}>
            <Stack.Screen name="(auth)" />
          </Stack.Protected>
          <Stack.Protected guard={showApp}>
            <Stack.Screen name="(tabs)" />
            {/* Sheet-presented screens live on the root stack so they present
                over the tab bar rather than inside a tab. Group names never
                appear in hrefs, so moving the files here left every existing
                link intact. */}
            <Stack.Screen
              name="settings/delete-account"
              options={{ headerShown: true, title: "Delete Account", presentation: "modal" }}
            />
            <Stack.Screen
              name="settings/subscription"
              options={{ headerShown: true, title: "Subscription", presentation: "modal" }}
            />
            <Stack.Screen
              name="capcodes/suggest"
              options={{ headerShown: true, title: "Suggest Capcode", presentation: "modal" }}
            />
            <Stack.Screen
              name="systems-create"
              options={{ headerShown: true, title: "Register interest", presentation: "modal" }}
            />
          </Stack.Protected>
        </Stack>
      </StoreManager>
      <StatusBar style={darkMode ? "light" : "dark"} />
    </>
  )
}

export default function RootLayout() {
  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <QueryClientProvider client={queryClient}>
        <SafeAreaProvider initialMetrics={initialWindowMetrics}>
          {/* Drives KeyboardAwareScrollView on the input screens. The keyboard
              position is read on the native side, so the content follows it
              frame-for-frame instead of chasing it from JS. */}
          <KeyboardProvider>
            <RootLayoutInner />
          </KeyboardProvider>
        </SafeAreaProvider>
      </QueryClientProvider>
    </GestureHandlerRootView>
  )
}
