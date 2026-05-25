import { Platform, StyleSheet, useWindowDimensions } from "react-native"
import { Drawer } from "expo-router/drawer"
import { useAppTheme } from "../../theme"
import { DrawerContent } from "../../components/drawer-content/drawer-content"

export default function AppDrawerLayout() {
  const theme = useAppTheme()
  const dims = useWindowDimensions()
  const isIPad =
    Platform.OS === "ios" &&
    ((Platform as any).isPad === true || Math.min(dims.width, dims.height) >= 768)
  const isTablet = isIPad || Math.min(dims.width, dims.height) >= 600

  return (
    <Drawer
      drawerContent={(props) => <DrawerContent {...props} />}
      screenOptions={{
        headerShown: false,
        drawerType: isTablet ? "permanent" : "slide",
        drawerStyle: {
          backgroundColor: theme.colors.cardBackground,
          width: isTablet ? 320 : Math.min(dims.width * 0.82, 320),
          borderRightWidth: isTablet ? StyleSheet.hairlineWidth : 0,
          borderRightColor: theme.colors.border,
        },
        overlayColor: "rgba(0,0,0,0.35)",
        sceneStyle: { backgroundColor: theme.colors.background },
      }}
    />
  )
}
