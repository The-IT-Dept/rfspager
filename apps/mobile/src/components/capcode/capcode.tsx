import * as React from "react"
import { View, StyleSheet, TouchableOpacity } from "react-native"
import { Text, ActivityIndicator } from "react-native"

import { useValidSubscription, useSubscriptionsStore } from "../../stores"
import { AppTheme, useAppTheme } from "../../theme"
import { SubscriptionModal } from "../subscription-modal"
import { capcodeActionLabel } from "./capcode-action-label"

export interface CapcodeProps {
  capcode: any
  editable: Boolean
  isLastItem?: boolean
}

export function Capcode(props: CapcodeProps) {
  const { capcode, isLastItem = false } = props
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const [subscriptionModalVisible, setSubscriptionModalVisible] = React.useState(false)

  const validSubscription = useValidSubscription()
  const subscriptions = useSubscriptionsStore((s) => s.subscriptions)
  const filters = useSubscriptionsStore((s) => s.filters)
  const createCapcodeSubscription = useSubscriptionsStore((s) => s.subscribe)
  const deleteCapcodeSubscription = useSubscriptionsStore((s) => s.unsubscribe)
  const createCapcodeFilter = useSubscriptionsStore((s) => s.addFilter)
  const deleteCapcodeFilter = useSubscriptionsStore((s) => s.removeFilter)

  // Handle undefined capcode case after all hooks
  if (!capcode || typeof capcode === "undefined") {
    return (
      <View style={styles.activityIndicatorWrapper}>
        <ActivityIndicator />
      </View>
    )
  }

  const capcodeId = String(capcode.id)
  const isFilter = filters.includes(capcodeId)
  const isSubscribe = subscriptions.includes(capcodeId)

  const alias = capcode?.alias || "Unknown"
  const agency = capcode?.agency || "Unknown"

  // Always show all capcodes - no filtering based on subscription status

  const renderAction = () => {
    return (
      <View style={styles.rightIconsContainer}>
        <TouchableOpacity
          style={[styles.actionButton, isFilter && styles.activeActionButton]}
          accessibilityRole="button"
          accessibilityState={{ selected: isFilter }}
          onPress={() => {
            if (!validSubscription) {
              setSubscriptionModalVisible(true)
            } else {
              if (isFilter) {
                deleteCapcodeFilter(capcodeId)
              } else {
                createCapcodeFilter(capcodeId)
              }
            }
          }}
        >
          <Text style={[styles.actionButtonText, isFilter && styles.activeActionButtonText]}>
            {capcodeActionLabel("filter", isFilter)}
          </Text>
        </TouchableOpacity>

        <TouchableOpacity
          style={[styles.actionButton, isSubscribe && styles.activeActionButton]}
          accessibilityRole="button"
          accessibilityState={{ selected: isSubscribe }}
          onPress={() => {
            if (!validSubscription) {
              setSubscriptionModalVisible(true)
            } else {
              if (isSubscribe) {
                deleteCapcodeSubscription(capcodeId)
              } else {
                createCapcodeSubscription(capcodeId)
              }
            }
          }}
        >
          <Text style={[styles.actionButtonText, isSubscribe && styles.activeActionButtonText]}>
            {capcodeActionLabel("notify", isSubscribe)}
          </Text>
        </TouchableOpacity>
      </View>
    )
  }

  return (
    <View>
      <View style={[styles.dataContainer, isLastItem && styles.lastCapcodeItem]}>
        <View style={styles.detailsContainer}>
          <Text style={styles.detailHeading}>{alias}</Text>
          <Text style={styles.detailSubheading}>{agency}</Text>
        </View>
        {renderAction()}
      </View>
      <SubscriptionModal
        visible={subscriptionModalVisible}
        onDismiss={() => setSubscriptionModalVisible(false)}
      />
    </View>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    activityIndicatorWrapper: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      backgroundColor: theme.colors.backgroundTertiary,
      paddingVertical: 20,
    },
    dots: {
      tintColor: theme.colors.grey70,
    },
    dataContainer: {
      flexDirection: "row",
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 11,
      alignItems: "center",
      justifyContent: "space-between",
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.borderSecondary,
    },
    lastCapcodeItem: {
      borderBottomWidth: 0,
    },
    detailsContainer: {
      flex: 1,
      justifyContent: "center",
    },
    detailHeading: {
      fontFamily: "Lato-Regular",
      fontWeight: "400",
      fontSize: 17,
      color: theme.colors.textPrimary, // iOS primary label color
      marginBottom: 2,
    },
    detailSubheading: {
      fontFamily: "Lato-Regular",
      fontWeight: "400",
      fontSize: 13,
      color: theme.colors.textTertiary, // iOS secondary label color
    },
    rightIconsContainer: {
      marginLeft: "auto",
      flexDirection: "row",
      alignItems: "center",
    },
    filterIcon: {
      height: 20,
      width: 20,
      marginHorizontal: 6,
      tintColor: theme.colors.primary, // RFS red for active
    },
    filterGrey: {
      height: 20,
      width: 20,
      marginHorizontal: 6,
      tintColor: theme.colors.textDisabled, // iOS tertiary label color
    },
    bellIcon: {
      height: 20,
      width: 20,
      marginHorizontal: 6,
      tintColor: theme.colors.primary, // RFS red for active
    },
    greyBell: {
      height: 20,
      width: 20,
      marginHorizontal: 6,
      tintColor: theme.colors.textDisabled, // iOS tertiary label color
    },
    botttomButtonContainer: {
      flexDirection: "row",
      alignItems: "center",
      marginTop: 13,
    },
    buttonView: {
      height: 32,
      justifyContent: "center",
      alignItems: "center",
      borderRadius: 8,
      paddingHorizontal: 12,
    },
    saveButton: {
      backgroundColor: theme.colors.primary, // RFS red
      marginRight: 12,
    },
    discardButton: {
      backgroundColor: theme.colors.backgroundTertiary, // iOS secondary background
      borderWidth: 0,
    },
    buttonText: {
      fontSize: 14,
      fontFamily: "Lato-Regular",
      fontWeight: "600",
    },
    saveText: {
      color: theme.colors.white,
    },
    discardText: {
      color: theme.colors.textTertiary, // iOS secondary label color
    },
    checkbox: {
      height: 18,
      width: 18,
      borderWidth: 1.5,
      marginRight: 8,
      borderRadius: 4,
      borderColor: theme.colors.textDisabled,
    },
    filter_NotificationText: {
      fontSize: 13,
      fontFamily: "Lato-Regular",
      fontWeight: "400",
      color: theme.colors.textPrimary,
    },
    actionButton: {
      minWidth: 76,
      alignItems: "center",
      paddingHorizontal: 12,
      paddingVertical: 6,
      borderRadius: 4,
      backgroundColor: theme.colors.backgroundTertiary,
      marginLeft: 8,
      borderWidth: 1,
      borderColor: theme.colors.border,
    },
    activeActionButton: {
      backgroundColor: theme.colors.primary,
      borderColor: theme.colors.primary,
    },
    actionButtonText: {
      fontSize: 12,
      fontWeight: "500",
      color: theme.colors.textTertiary,
    },
    activeActionButtonText: {
      color: theme.colors.white,
    },
  })
