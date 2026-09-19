import { ExperimentalStack } from "expo-router"

// See (pager)/_layout.tsx for why the experimental stack is used.
export default function IncidentsStackLayout() {
  return (
    <ExperimentalStack>
      <ExperimentalStack.Screen name="incidents" options={{ title: "Incidents" }} />
      <ExperimentalStack.Screen name="incident/[id]" options={{ title: "Incident" }} />
    </ExperimentalStack>
  )
}
