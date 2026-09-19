import { Stack } from "expo-router"
import { useAppTheme } from "../../../theme"
import { stackScreenOptions } from "../../../navigation/stack-screen-options"

// Sheet-presented screens (delete-account, subscription, capcodes/suggest,
// systems-create) live on the root stack; their hrefs are unchanged.
export default function SettingsStackLayout() {
  const theme = useAppTheme()
  return (
    <Stack screenOptions={stackScreenOptions(theme)}>
      <Stack.Screen name="settings/index" options={{ title: "Settings" }} />
      <Stack.Screen name="settings/account" options={{ title: "My Account" }} />
      <Stack.Screen name="settings/edit-name" options={{ title: "Name" }} />
      <Stack.Screen name="settings/edit-email" options={{ title: "Email" }} />
      <Stack.Screen name="settings/change-password" options={{ title: "Change Password" }} />

      <Stack.Screen name="capcodes/index" options={{ title: "Capcodes" }} />
      {/* Legacy settings/capcodes paths kept for back-compat */}
      <Stack.Screen name="settings/capcodes" options={{ title: "Capcodes" }} />
      <Stack.Screen name="settings/capcode-detail" options={{ title: "Agency Detail" }} />

      {/* The "+" is declared in the screen body via Stack.Toolbar so it renders
          as a native glass item. */}
      <Stack.Screen name="alertzones/index" options={{ title: "Alert Zones" }} />
      <Stack.Screen name="alertzones/create" options={{ title: "Create Alert Zone" }} />
      {/* Legacy settings/alertzone* paths kept for back-compat */}
      <Stack.Screen name="settings/alertzones" options={{ title: "Alert Zones" }} />
      <Stack.Screen name="settings/alertzone-create" options={{ title: "Create Alert Zone" }} />

      <Stack.Screen name="systems" options={{ title: "Systems" }} />
      <Stack.Screen name="system/[uuid]" options={{ title: "System" }} />
    </Stack>
  )
}
