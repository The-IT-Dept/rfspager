import { Platform, TouchableOpacity } from "react-native"
import { Stack, useRouter } from "expo-router"
import { Ionicons } from "@expo/vector-icons"
import { useAppTheme } from "../../../theme"
import { DrawerToggle } from "../../../components/drawer-toggle/drawer-toggle"

export default function AppStackLayout() {
  const theme = useAppTheme()
  const router = useRouter()

  return (
    <Stack
      screenOptions={{
        headerTransparent: Platform.OS === "ios",
        headerStyle:
          Platform.OS === "ios"
            ? { backgroundColor: "transparent" }
            : { backgroundColor: theme.colors.background },
        headerTintColor: theme.colors.textPrimary,
        headerTitleStyle: { color: theme.colors.textPrimary },
        headerShadowVisible: false,
        headerLargeStyle: { backgroundColor: "transparent" },
        contentStyle: { backgroundColor: theme.colors.background },
        // iOS 26 Liquid Glass: scrollEdgeEffects.top renders the proper
        // translucent header that doesn't pick up the colour of content
        // scrolling under it. Combining this with headerBlurEffect causes
        // overlapping effects (per react-native-screens docs) so we use this
        // alone.
        scrollEdgeEffects: { top: "soft" },
      }}
    >
      <Stack.Screen
        name="index"
        options={{
          title: "Pager",
          headerLeft: () => <DrawerToggle />,
        }}
      />
      <Stack.Screen
        name="incidents"
        options={{
          title: "Incidents",
          headerLeft: () => <DrawerToggle />,
        }}
      />
      <Stack.Screen
        name="map"
        options={{
          title: "",
          headerLeft: () => <DrawerToggle />,
        }}
      />
      <Stack.Screen
        name="systems"
        options={{
          title: "Systems",
          headerLeft: () => <DrawerToggle />,
        }}
      />
      <Stack.Screen
        name="systems-create"
        options={{ title: "Register interest", presentation: "modal" }}
      />
      <Stack.Screen name="my-system/[uuid]" options={{ title: "My system" }} />
      <Stack.Screen
        name="settings/index"
        options={{
          title: "Settings",
          headerLeft: () => <DrawerToggle />,
        }}
      />
      <Stack.Screen name="settings/account" options={{ title: "My Account" }} />
      <Stack.Screen
        name="settings/subscription"
        options={{ title: "Subscription", presentation: "modal" }}
      />
      <Stack.Screen name="settings/capcodes" options={{ title: "Capcodes" }} />
      <Stack.Screen name="settings/capcode-detail" options={{ title: "Agency Detail" }} />
      <Stack.Screen
        name="settings/alertzones"
        options={{
          title: "Alert Zones",
          headerRight: () => (
            <TouchableOpacity
              onPress={() => router.push("/(tabs)/(stack)/settings/alertzone-create" as any)}
            >
              <Ionicons name="add" size={24} color={theme.colors.primary} />
            </TouchableOpacity>
          ),
        }}
      />
      <Stack.Screen name="settings/alertzone-create" options={{ title: "Create Alert Zone" }} />
      <Stack.Screen name="message/[id]" options={{ title: "Message" }} />
      <Stack.Screen name="incident/[id]" options={{ title: "Incident" }} />
    </Stack>
  )
}
