import { Stack } from "expo-router"
import { useAppTheme } from "../../../theme"
import { stackScreenOptions } from "../../../navigation/stack-screen-options"

// See navigation/stack-screen-options.ts for why the regular Stack with a
// transparent header is the correct iOS 26 configuration.
export default function RadioStackLayout() {
  const theme = useAppTheme()
  return (
    <Stack screenOptions={stackScreenOptions(theme)}>
      <Stack.Screen name="radio" options={{ title: "Radio", headerLargeTitle: true }} />
    </Stack>
  )
}
