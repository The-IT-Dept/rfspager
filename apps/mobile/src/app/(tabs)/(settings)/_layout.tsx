import { ExperimentalStack } from "expo-router"

// See (pager)/_layout.tsx for why the experimental stack is used.
//
// delete-account, subscription, capcodes/suggest and systems-create used to be
// presented as modals from here. The experimental stack has no `presentation`
// option yet, so those four routes moved to the root stack (src/app/…) where
// the regular Stack still presents them as sheets. Their hrefs are unchanged.
export default function SettingsStackLayout() {
  return (
    <ExperimentalStack>
      <ExperimentalStack.Screen name="settings/index" options={{ title: "Settings" }} />
      <ExperimentalStack.Screen name="settings/account" options={{ title: "My Account" }} />
      <ExperimentalStack.Screen name="settings/edit-name" options={{ title: "Name" }} />
      <ExperimentalStack.Screen name="settings/edit-email" options={{ title: "Email" }} />
      <ExperimentalStack.Screen name="settings/change-password" options={{ title: "Change Password" }} />

      <ExperimentalStack.Screen name="capcodes/index" options={{ title: "Capcodes" }} />
      {/* Legacy settings/capcodes paths kept for back-compat */}
      <ExperimentalStack.Screen name="settings/capcodes" options={{ title: "Capcodes" }} />
      <ExperimentalStack.Screen name="settings/capcode-detail" options={{ title: "Agency Detail" }} />

      {/* The create action lives in the screen body: header items are not
          available on the experimental stack yet. */}
      <ExperimentalStack.Screen name="alertzones/index" options={{ title: "Alert Zones" }} />
      <ExperimentalStack.Screen name="alertzones/create" options={{ title: "Create Alert Zone" }} />
      {/* Legacy settings/alertzone* paths kept for back-compat */}
      <ExperimentalStack.Screen name="settings/alertzones" options={{ title: "Alert Zones" }} />
      <ExperimentalStack.Screen name="settings/alertzone-create" options={{ title: "Create Alert Zone" }} />

      <ExperimentalStack.Screen name="systems" options={{ title: "Systems" }} />
      <ExperimentalStack.Screen name="system/[uuid]" options={{ title: "System" }} />
    </ExperimentalStack>
  )
}
