import * as React from "react"
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native"
import { SafeAreaView } from "react-native-safe-area-context"
import { Ionicons } from "@expo/vector-icons"
import { useRouter, usePathname } from "expo-router"
import { AppTheme, useAppTheme } from "../../theme"
import { useUser, useValidSubscription } from "../../stores"

type IconName = React.ComponentProps<typeof Ionicons>["name"]

type NavItem = {
  label: string
  icon: IconName
  path: string
  match: (pathname: string) => boolean
  pro?: boolean
}

// Top group: operational tabs everyone uses every session.
const PRIMARY_NAV: NavItem[] = [
  {
    label: "Pager",
    icon: "radio",
    path: "/(tabs)/(stack)/",
    match: (p) => p === "/" || p === "",
  },
  {
    label: "Incidents",
    icon: "flame",
    path: "/(tabs)/(stack)/incidents",
    match: (p) => p.startsWith("/incidents"),
  },
  {
    label: "Map",
    icon: "map",
    path: "/(tabs)/(stack)/map",
    match: (p) => p.startsWith("/map"),
  },
]

// Middle group: configuration / personalisation. Alert Zones is Pro.
const SECONDARY_NAV: NavItem[] = [
  {
    label: "Capcodes",
    icon: "list",
    path: "/(tabs)/(stack)/capcodes",
    match: (p) => p.startsWith("/capcodes"),
  },
  {
    label: "Alert Zones",
    icon: "location",
    path: "/(tabs)/(stack)/alertzones",
    match: (p) => p.startsWith("/alertzones"),
    pro: true,
  },
  {
    label: "Systems",
    icon: "hardware-chip",
    path: "/(tabs)/(stack)/systems",
    match: (p) => p.startsWith("/systems"),
  },
]

export function DrawerContent(props: any) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const pathname = usePathname()
  const user = useUser()
  const hasPro = useValidSubscription()

  const go = (path: string) => {
    props.navigation?.closeDrawer?.()
    router.replace(path as any)
  }

  const renderItem = (item: NavItem) => {
    const focused = item.match(pathname)
    return (
      <Pressable
        key={item.path}
        onPress={() => go(item.path)}
        style={({ pressed }) => [
          styles.item,
          focused && styles.itemFocused,
          pressed && !focused && styles.itemPressed,
        ]}
      >
        <Ionicons
          name={item.icon}
          size={20}
          color={focused ? theme.colors.primary : theme.colors.textSecondary}
        />
        <Text style={[styles.itemLabel, focused && styles.itemLabelFocused]}>
          {item.label}
        </Text>
        {item.pro && !hasPro && (
          <View style={styles.proBadge}>
            <Text style={styles.proBadgeText}>PRO</Text>
          </View>
        )}
      </Pressable>
    )
  }

  return (
    <SafeAreaView style={styles.root} edges={["top", "bottom"]}>
      <View style={styles.header}>
        <Text style={styles.appName}>RFSPager</Text>
      </View>

      <ScrollView
        contentContainerStyle={styles.scroll}
        showsVerticalScrollIndicator={false}
      >
        {PRIMARY_NAV.map(renderItem)}
        <View style={styles.divider} />
        {SECONDARY_NAV.map(renderItem)}
      </ScrollView>

      {/* Pinned footer — user + Settings */}
      <View style={styles.footer}>
        <Pressable
          onPress={() => go("/(tabs)/(stack)/settings")}
          style={({ pressed }) => [
            styles.userRow,
            pressed && styles.itemPressed,
          ]}
        >
          <View style={styles.avatar}>
            <Ionicons name="person" size={18} color={theme.colors.textSecondary} />
          </View>
          <View style={styles.userText}>
            {user?.name ? (
              <Text style={styles.userName} numberOfLines={1}>
                {user.name}
              </Text>
            ) : (
              <Text style={styles.userName} numberOfLines={1}>
                Signed in
              </Text>
            )}
            {user?.email ? (
              <Text style={styles.userEmail} numberOfLines={1}>
                {user.email}
              </Text>
            ) : null}
          </View>
          {hasPro && (
            <View style={styles.proPill}>
              <Text style={styles.proPillText}>PRO</Text>
            </View>
          )}
          <Ionicons
            name="settings-outline"
            size={20}
            color={theme.colors.textSecondary}
          />
        </Pressable>
      </View>
    </SafeAreaView>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    root: {
      flex: 1,
      backgroundColor: theme.colors.cardBackground,
    },
    header: {
      paddingHorizontal: 20,
      paddingTop: 12,
      paddingBottom: 14,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
    },
    appName: {
      fontSize: 22,
      fontWeight: "700",
      color: theme.colors.primary,
    },
    scroll: {
      paddingTop: 8,
      paddingHorizontal: 8,
      paddingBottom: 16,
    },
    item: {
      flexDirection: "row",
      alignItems: "center",
      gap: 14,
      paddingHorizontal: 12,
      paddingVertical: 11,
      borderRadius: 10,
      marginBottom: 2,
    },
    itemFocused: {
      backgroundColor: theme.colors.primary + "15",
    },
    itemPressed: {
      backgroundColor: theme.colors.backgroundTertiary,
    },
    itemLabel: {
      flex: 1,
      fontSize: 16,
      fontWeight: "500",
      color: theme.colors.textPrimary,
    },
    itemLabelFocused: {
      color: theme.colors.primary,
      fontWeight: "600",
    },
    divider: {
      height: StyleSheet.hairlineWidth,
      backgroundColor: theme.colors.border,
      marginVertical: 10,
      marginHorizontal: 12,
    },
    proBadge: {
      paddingHorizontal: 6,
      paddingVertical: 2,
      borderRadius: 4,
      backgroundColor: theme.colors.primary,
    },
    proBadgeText: {
      fontSize: 10,
      fontWeight: "700",
      color: "#FFFFFF",
      letterSpacing: 0.5,
    },
    // Footer
    footer: {
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: theme.colors.border,
      paddingHorizontal: 8,
      paddingTop: 8,
      paddingBottom: 4,
    },
    userRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingHorizontal: 8,
      paddingVertical: 10,
      borderRadius: 10,
    },
    avatar: {
      width: 36,
      height: 36,
      borderRadius: 18,
      backgroundColor: theme.colors.backgroundTertiary,
      alignItems: "center",
      justifyContent: "center",
    },
    userText: {
      flex: 1,
      minWidth: 0,
    },
    userName: {
      fontSize: 14,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    userEmail: {
      fontSize: 12,
      color: theme.colors.textSecondary,
      marginTop: 1,
    },
    proPill: {
      paddingHorizontal: 6,
      paddingVertical: 2,
      borderRadius: 4,
      backgroundColor: theme.colors.primary,
    },
    proPillText: {
      fontSize: 10,
      fontWeight: "700",
      color: "#FFFFFF",
      letterSpacing: 0.5,
    },
  })
