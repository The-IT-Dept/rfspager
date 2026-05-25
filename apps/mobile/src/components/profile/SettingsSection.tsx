import React from "react"
import { View, StyleSheet, TouchableOpacity, Text, Switch } from "react-native"
import { Ionicons } from "@expo/vector-icons"
import { useAppTheme, AppTheme } from "../../theme"

export interface SettingItem {
  id: string
  title: string
  type: "toggle" | "navigation" | "dropdown" | "info"
  value?: boolean | string | number
  onPress?: () => void
  onToggle?: (value: boolean) => void
  icon?: string
  disabled?: boolean
  isPremium?: boolean
}

interface SettingsSectionProps {
  title: string
  icon: any
  items: SettingItem[]
  isLoading?: boolean
}

export const SettingsSection: React.FC<SettingsSectionProps> = (
  { title, icon, items, isLoading = false }) => {
    const theme = useAppTheme()
    const styles = makeStyles(theme)

    if (isLoading) {
      return (
        <View style={styles.container}>
          <View style={styles.loadingHeader} />
          {[1, 2, 3].map((i) => (
            <View key={i} style={styles.loadingItem} />
          ))}
        </View>
      )
    }

    const renderSettingItem = (item: SettingItem, index: number) => {
      const isDisabled = item.disabled && !item.isPremium // Don't disable premium features
      const isLastItem = index === items.length - 1

      return (
        <TouchableOpacity
          key={item.id}
          style={[
            styles.settingItem,
            isDisabled && styles.disabledItem,
            isLastItem && styles.lastSettingItem,
          ]}
          onPress={item.onPress}
          disabled={isDisabled || item.type === "toggle" || item.type === "info"}
        >
          <View style={styles.settingLeft}>
            <Text style={[styles.settingTitle, isDisabled && styles.disabledText]}>
              {item.title}
            </Text>
            {item.isPremium && <Text style={styles.premiumBadge}>PRO</Text>}
          </View>

          <View style={styles.settingRight}>
            {item.type === "toggle" && (
              <Switch
                value={item.value as boolean}
                onValueChange={item.onToggle}
                disabled={isDisabled}
                thumbColor={theme.colors.white}
                trackColor={{ false: theme.colors.borderSecondary, true: theme.colors.success }}
              />
            )}

            {item.type === "navigation" && (
              <Ionicons name="chevron-forward" size={16} color={theme.colors.textTertiary} />
            )}

            {item.type === "info" && typeof item.value === "string" && (
              <Text style={styles.infoValue}>{item.value}</Text>
            )}
          </View>
        </TouchableOpacity>
      )
    }

    return (
      <View style={styles.container}>
        <View style={styles.header}>
          <Text style={styles.sectionTitle}>{title}</Text>
        </View>

        <View style={styles.itemsContainer}>
          {items.map((item, index) => renderSettingItem(item, index))}
        </View>
      </View>
    )
  }

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      marginHorizontal: 16,
      marginTop: 16,
      marginBottom: 4,
    },
    header: {
      paddingHorizontal: 16,
      paddingBottom: 6,
      backgroundColor: "transparent",
    },
    sectionIcon: {
      display: "none", // Hide icons for cleaner iOS look
    },
    sectionTitle: {
      fontSize: 13,
      fontWeight: "400",
      color: theme.colors.textSecondary,
      textTransform: "uppercase",
      letterSpacing: 0.4,
    },
    itemsContainer: {
      backgroundColor: theme.colors.cardBackground,
      borderRadius: 0,
      overflow: "hidden",
    },
    settingItem: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      paddingHorizontal: 16,
      paddingVertical: 11,
      backgroundColor: theme.colors.cardBackground,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    settingLeft: {
      flex: 1,
      flexDirection: "row",
      alignItems: "center",
    },
    settingTitle: {
      fontSize: 17,
      color: theme.colors.textPrimary,
      marginRight: 8,
    },
    settingRight: {
      flexDirection: "row",
      alignItems: "center",
    },
    premiumBadge: {
      fontSize: 11,
      fontWeight: "600",
      color: theme.colors.primary,
      backgroundColor: theme.colors.primary + "15",
      paddingHorizontal: 6,
      paddingVertical: 2,
      borderRadius: 2,
      overflow: "hidden",
    },
    navIcon: {
      width: 13,
      height: 13,
      tintColor: theme.colors.textTertiary,
    },
    infoValue: {
      fontSize: 17,
      color: theme.colors.textSecondary,
    },
    disabledItem: {
      opacity: 0.3,
    },
    disabledText: {
      color: theme.colors.textSecondary,
    },
    lastSettingItem: {
      borderBottomWidth: 0,
    },
    // Loading states
    loadingHeader: {
      height: 32,
      backgroundColor: theme.colors.backgroundTertiary,
      marginBottom: 6,
    },
    loadingItem: {
      height: 44,
      backgroundColor: theme.colors.backgroundTertiary,
      marginBottom: 0.5,
    },
  })
