import { Stack } from "expo-router"
import { useAppTheme } from "../../../theme"
import { stackScreenOptions } from "../../../navigation/stack-screen-options"

// See navigation/stack-screen-options.ts for why the regular Stack with a
// transparent header is the correct iOS 26 configuration.
export default function PagerStackLayout() {
  const theme = useAppTheme()
  return (
    <Stack screenOptions={stackScreenOptions(theme)}>
      <Stack.Screen name="index" options={{ title: "Pager" }} />
      <Stack.Screen name="message/[id]" options={{ title: "Message" }} />
    </Stack>
  )
}
