import { ExperimentalStack } from "expo-router"

/**
 * Tab stacks use expo-router's ExperimentalStack, which renders
 * react-native-screens' gamma RNSStackHost.
 *
 * The regular <Stack /> cannot render iOS 26 Liquid Glass headers. Its native
 * side (RNSScreenStackHeaderConfig.buildAppearance) always installs a custom
 * UINavigationBarAppearance — configureWithOpaqueBackground, blurEffect None —
 * and any custom appearance replaces the system material outright. Its
 * "SystemDefault" blur explicitly errors. That is why the header read as flat
 * white no matter what options were set. The gamma stack installs no
 * appearance and inherits the system glass.
 *
 * Only `title`, `headerShown`, `headerTransparent` and `headerBackVisible` are
 * supported here; anything else is ignored with a dev warning. Screens that
 * need modal presentation live on the root stack instead.
 */
export default function PagerStackLayout() {
  return (
    <ExperimentalStack>
      <ExperimentalStack.Screen name="index" options={{ title: "Pager" }} />
      <ExperimentalStack.Screen name="message/[id]" options={{ title: "Message" }} />
    </ExperimentalStack>
  )
}
