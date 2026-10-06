import React from "react"
import { ScrollView, View, StyleSheet, TouchableOpacity, Alert } from "react-native"
import { Text } from "react-native"
import { Gravatar } from "react-native-gravatar"
import { AppIcon } from "../../../../components/app-icon/app-icon"
import { useRouter } from "expo-router"
import { useUser, useAuthStore, useValidSubscription, useProductName } from "../../../../stores"
import { useAppTheme, AppTheme } from "../../../../theme"

export default function AccountScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const user = useUser()
  const logout = useAuthStore((s) => s.logout)
  const validSubscription = useValidSubscription()
  const productName = useProductName()

  const handleLogout = () => {
    Alert.alert("Log Out", "Are you sure you want to log out?", [
      { text: "Cancel", style: "cancel" },
      { text: "Log Out", style: "destructive", onPress: () => logout() },
    ])
  }

  return (
    <ScrollView
      style={styles.container}
      contentInsetAdjustmentBehavior="automatic"
      showsVerticalScrollIndicator={false}
    >
      <View style={styles.avatarSection}>
        <Gravatar
          style={styles.avatar}
          options={{
            email: user?.email || "",
            parameters: { size: "200", d: "robohash" },
            secure: true,
          }}
        />
      </View>

      <SectionHeader theme={theme}>Profile</SectionHeader>
      <View style={styles.card}>
        <EditableRow
          theme={theme}
          label="Name"
          value={user?.name || "—"}
          onPress={() => router.push("/settings/edit-name")}
        />
        <EditableRow
          theme={theme}
          label="Email"
          value={user?.email || "—"}
          onPress={() => router.push("/settings/edit-email")}
          isLast
        />
      </View>

      <SectionHeader theme={theme}>Security</SectionHeader>
      <View style={styles.card}>
        <EditableRow
          theme={theme}
          label={user?.has_password === false ? "Set Password" : "Change Password"}
          onPress={() => router.push("/settings/change-password")}
          isLast
        />
      </View>

      <SectionHeader theme={theme}>Subscription</SectionHeader>
      <View style={styles.card}>
        <EditableRow
          theme={theme}
          label="Plan"
          value={productName}
          valueColor={validSubscription ? theme.colors.success : undefined}
          onPress={() => router.push("/settings/subscription")}
          isLast
        />
      </View>

      <TouchableOpacity style={styles.actionButton} onPress={handleLogout}>
        <Text style={[styles.actionText, { color: theme.colors.error }]}>Log Out</Text>
      </TouchableOpacity>

      <TouchableOpacity
        style={styles.actionButton}
        onPress={() => router.push("/settings/delete-account")}
      >
        <Text style={[styles.actionText, { color: theme.colors.error }]}>Delete Account</Text>
      </TouchableOpacity>
    </ScrollView>
  )
}

const SectionHeader = ({ theme, children }: { theme: AppTheme; children: React.ReactNode }) => (
  <Text
    style={{
      marginTop: 24,
      marginBottom: 6,
      marginHorizontal: 16,
      fontSize: 13,
      color: theme.colors.textSecondary,
      textTransform: "uppercase",
      letterSpacing: 0.5,
    }}
  >
    {children}
  </Text>
)

const EditableRow = ({
  theme,
  label,
  value,
  valueColor,
  onPress,
  isLast,
}: {
  theme: AppTheme
  label: string
  value?: string
  valueColor?: string
  onPress?: () => void
  isLast?: boolean
}) => {
  const styles = makeStyles(theme)
  return (
    <TouchableOpacity
      style={[styles.row, isLast && styles.lastRow]}
      onPress={onPress}
      disabled={!onPress}
      activeOpacity={onPress ? 0.6 : 1}
    >
      <Text style={styles.label}>{label}</Text>
      <View style={styles.valueWrap}>
        {value !== undefined && (
          <Text
            style={[styles.value, valueColor ? { color: valueColor } : null]}
            numberOfLines={1}
            ellipsizeMode="tail"
          >
            {value}
          </Text>
        )}
        {onPress && (
          <AppIcon
            sf="chevron.right" ion="chevron-forward"
            size={18}
            color={theme.colors.textSecondary}
            style={{ marginLeft: 6 }}
          />
        )}
      </View>
    </TouchableOpacity>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    avatarSection: {
      alignItems: "center",
      paddingVertical: 24,
    },
    avatar: {
      width: 80,
      height: 80,
      borderRadius: 40,
    },
    card: {
      marginHorizontal: 16,
      backgroundColor: theme.colors.cardBackground,
      overflow: "hidden",
      borderRadius: 10,
    },
    row: {
      flexDirection: "row",
      justifyContent: "space-between",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 11,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    lastRow: {
      borderBottomWidth: 0,
    },
    label: {
      fontSize: 17,
      color: theme.colors.textPrimary,
    },
    valueWrap: {
      flexDirection: "row",
      alignItems: "center",
      flexShrink: 1,
      marginLeft: 12,
    },
    value: {
      fontSize: 17,
      color: theme.colors.textSecondary,
      maxWidth: 200,
    },
    actionButton: {
      marginHorizontal: 16,
      marginTop: 16,
      backgroundColor: theme.colors.cardBackground,
      paddingVertical: 11,
      alignItems: "center",
      borderRadius: 10,
    },
    actionText: {
      fontSize: 17,
    },
  })
