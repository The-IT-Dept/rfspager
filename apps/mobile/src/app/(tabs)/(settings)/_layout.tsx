import { TouchableOpacity } from "react-native"
import { Stack, useRouter } from "expo-router"
import { SymbolView } from "expo-symbols"
import { useAppTheme } from "../../../theme"
import { stackScreenOptions } from "../../../navigation/stack-screen-options"

export default function SettingsStackLayout() {
  const theme = useAppTheme()
  const router = useRouter()

  return (
    <Stack screenOptions={stackScreenOptions(theme)}>
      <Stack.Screen name="settings/index" options={{ title: "Settings" }} />
      <Stack.Screen name="settings/account" options={{ title: "My Account" }} />
      <Stack.Screen name="settings/edit-name" options={{ title: "Name" }} />
      <Stack.Screen name="settings/edit-email" options={{ title: "Email" }} />
      <Stack.Screen name="settings/change-password" options={{ title: "Change Password" }} />
      <Stack.Screen
        name="settings/delete-account"
        options={{ title: "Delete Account", presentation: "modal" }}
      />
      <Stack.Screen
        name="settings/subscription"
        options={{ title: "Subscription", presentation: "modal" }}
      />

      <Stack.Screen name="capcodes/index" options={{ title: "Capcodes" }} />
      <Stack.Screen
        name="capcodes/suggest"
        options={{ title: "Suggest Capcode", presentation: "modal" }}
      />
      {/* Legacy settings/capcodes paths kept for back-compat */}
      <Stack.Screen name="settings/capcodes" options={{ title: "Capcodes" }} />
      <Stack.Screen name="settings/capcode-detail" options={{ title: "Agency Detail" }} />

      <Stack.Screen
        name="alertzones/index"
        options={{
          title: "Alert Zones",
          headerRight: () => (
            <TouchableOpacity
              accessibilityRole="button"
              accessibilityLabel="Create alert zone"
              onPress={() => router.push("/alertzones/create")}
            >
              <SymbolView name="plus" size={22} tintColor={theme.colors.primary} />
            </TouchableOpacity>
          ),
        }}
      />
      <Stack.Screen name="alertzones/create" options={{ title: "Create Alert Zone" }} />
      {/* Legacy settings/alertzone* paths kept for back-compat */}
      <Stack.Screen name="settings/alertzones" options={{ title: "Alert Zones" }} />
      <Stack.Screen name="settings/alertzone-create" options={{ title: "Create Alert Zone" }} />

      <Stack.Screen name="systems" options={{ title: "Systems" }} />
      <Stack.Screen
        name="systems-create"
        options={{ title: "Register interest", presentation: "modal" }}
      />
      <Stack.Screen name="system/[uuid]" options={{ title: "System" }} />
    </Stack>
  )
}
