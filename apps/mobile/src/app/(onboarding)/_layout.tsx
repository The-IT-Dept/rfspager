import { Stack } from "expo-router"

export default function OnboardingLayout() {
  return (
    <Stack screenOptions={{ headerShown: false, animation: "none" }}>
      <Stack.Screen name="first" />
      <Stack.Screen name="second" />
      <Stack.Screen name="third" />
      <Stack.Screen name="fourth" />
      <Stack.Screen name="fifth" />
    </Stack>
  )
}
