import * as React from "react"
import { Text, View, StyleSheet, Animated, Pressable } from "react-native"
import { ActivityIndicator } from "react-native"
import moment from "moment"
import { FontAwesome5 } from "@expo/vector-icons"
import { useRouter } from "expo-router"
import { useQueryClient } from "@tanstack/react-query"
import { hasLocation, openMapWithNavigation, shareMessage } from "../../utils/extract-coords"
import {
  RespondButton,
  DetailsButton,
  ShareButton,
  DirectionsButton,
} from "../action-buttons/ActionButtons"
import { capcodeDisplay } from "../../utils/capcodeDisplay"
import { useAuthStore } from "../../stores"
import { api } from "../../services/api-client"
import { AppTheme, useAppTheme } from "../../theme"

export interface MessageProps {
  message: any
}

export function Message(props: MessageProps) {
  const { message } = props
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const queryClient = useQueryClient()
  const currentUser = useAuthStore((s) => s.user)
  const animatedHeight = React.useState(new Animated.Value(0))[0]
  const [respondLoading, setRespondLoading] = React.useState(false)
  const [localResponding, setLocalResponding] = React.useState<boolean | null>(null)

  const serverResponding =
    message.am_i_responding ??
    message.responders?.some(
      (r: any) =>
        r.user_id === currentUser?.id || String(r.user_id) === currentUser?.user_id,
    ) ??
    false
  const amIResponding = localResponding ?? serverResponding

  const handleRespond = React.useCallback(async () => {
    if (respondLoading) return
    const previous = amIResponding
    setRespondLoading(true)
    setLocalResponding(!previous) // optimistic flip
    try {
      const result = await api.messages.respond(message.id)
      setLocalResponding(result.responding)
      queryClient.invalidateQueries({ queryKey: ["messages"] })
    } catch (e) {
      console.error("Failed to respond:", e)
      setLocalResponding(previous) // revert
    } finally {
      setRespondLoading(false)
    }
  }, [message?.id, respondLoading, amIResponding, queryClient])

  if (typeof message === "undefined" || typeof message === "undefined") {
    return (
      <View style={{ justifyContent: "center", alignItems: "center", flex: 1 }}>
        <ActivityIndicator />
      </View>
    )
  }

  const messageDate = moment.utc(message.created_at).local()
  let displayDate = messageDate.format("YYYY-MM-DD HH:mm")
  if (messageDate.isSame(new Date(), "day")) {
    displayDate = messageDate.format("HH:mm")
  }

  const rawIcon = message.capcode_icon || message.capcode?.icon || "fas fa-question"
  const icon = rawIcon.replace("fas fa-", "")
  const iconColour = message.capcode_colour || message.capcode?.colour || theme.colors.accent
  const [isAnimated, setIsAnimated] = React.useState(false)

  const handleAnimation = () => {
    isAnimated
      ? Animated.timing(animatedHeight, {
          toValue: 0,
          duration: 300,
          useNativeDriver: false,
        }).start()
      : Animated.spring(animatedHeight, {
          toValue: 32,
          useNativeDriver: false,
        }).start()
  }

  return (
    <>
      <Pressable
        onPress={() => {
          setIsAnimated(!isAnimated)
          handleAnimation()
        }}
        style={styles.container}
      >
        <View style={[styles.iconContainer, { backgroundColor: iconColour }]}>
          <FontAwesome5 name={icon} size={10} color={"#FFFFFF"} />
        </View>
        <View style={styles.contentContainer}>
          <View style={styles.row}>
            <View style={styles.title}>
              <Text style={styles.bold}>{capcodeDisplay(message)}</Text>
            </View>
            <View style={styles.flexRow}>
              {(message.responder_count > 0 || message.responders?.length > 0) && (
                <View style={styles.responderCounter}>
                  <FontAwesome5 name="users" size={10} color={theme.colors.textTertiary} />
                  <Text style={styles.responderCount}>{message.responder_count || message.responders?.length || 0}</Text>
                </View>
              )}
              <Text style={styles.date}>{displayDate}</Text>
            </View>
          </View>
          <View style={styles.row}>
            <Text numberOfLines={3} style={styles.message}>
              {message.message}
            </Text>
          </View>
          <Animated.View
            style={[styles.expandedContainer, { height: animatedHeight, overflow: "hidden" }]}
          >
            <View style={styles.separator} />
            <View style={styles.row}>
              <Animated.View
                style={[
                  styles.leftButtons,
                  {
                    opacity: animatedHeight.interpolate({
                      inputRange: [0, 32],
                      outputRange: [0, 1],
                    }),
                  },
                ]}
              >
                <RespondButton onPress={handleRespond} isResponding={amIResponding} />

                <DetailsButton onPress={() => router.push(`/message/${message.id}`)} />
              </Animated.View>
              <View style={styles.actionsContainer}>
                {hasLocation(message) && (
                  <DirectionsButton onPress={() => openMapWithNavigation(message)} />
                )}
                <ShareButton onPress={() => shareMessage(message)} />
              </View>
            </View>
          </Animated.View>
        </View>
      </Pressable>
    </>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      backgroundColor: theme.colors.cardBackground,
      marginBottom: 8,
      paddingHorizontal: 16,
      paddingVertical: 12,
      flexDirection: "row",
    },
    iconContainer: {
      width: 20,
      height: 20,
      borderRadius: 3,
      justifyContent: "center",
      alignItems: "center",
      marginRight: 12,
    },
    contentContainer: {
      flex: 1,
    },
    row: {
      alignItems: "center",
      flexDirection: "row",
      justifyContent: "space-between",
    },
    title: {
      flex: 1,
    },
    bold: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 2,
    },
    date: {
      fontSize: 13,
      color: theme.colors.textTertiary,
      fontWeight: "400",
    },
    flexRow: {
      alignItems: "center",
      flexDirection: "row",
    },
    message: {
      fontSize: 14,
      lineHeight: 19,
      color: theme.colors.textSecondary,
      marginVertical: 3,
    },
    leftButtons: {
      flexDirection: "row",
      alignItems: "center",
    },
    responseButton: {
      backgroundColor: "#FF3B30",
      paddingHorizontal: 10,
      paddingVertical: 4,
      borderRadius: 4,
      marginRight: 6,
    },
    notRespondingButton: {
      backgroundColor: "transparent",
      borderWidth: 1,
      borderColor: "#FF3B30",
      paddingHorizontal: 10,
      paddingVertical: 4,
      borderRadius: 4,
      marginRight: 6,
    },
    detailsButton: {
      backgroundColor: "#007AFF",
      paddingHorizontal: 10,
      paddingVertical: 4,
      borderRadius: 4,
    },
    responseText: {
      fontSize: 11,
      fontWeight: "600",
      color: "#FF3B30",
    },
    responseActiveText: {
      color: "#FFFFFF",
    },
    detailsText: {
      fontSize: 11,
      fontWeight: "600",
      color: "#FFFFFF",
    },
    actionsContainer: {
      flexDirection: "row",
      alignItems: "center",
    },
    actionIcon: {
      padding: 4,
      marginLeft: 4,
    },
    avatarRow: {
      alignItems: "flex-start",
      flexDirection: "row",
      marginTop: 4,
    },
    responderCounter: {
      flexDirection: "row",
      alignItems: "center",
      backgroundColor: theme.colors.backgroundTertiary,
      paddingHorizontal: 6,
      paddingVertical: 2,
      borderRadius: 8,
      marginRight: 8,
    },
    responderCount: {
      fontSize: 11,
      color: theme.colors.textTertiary,
      fontWeight: "500",
      marginLeft: 3,
    },
    expandedContainer: {
      paddingTop: 0,
    },
    separator: {
      height: 0.5,
      backgroundColor: theme.colors.border,
      marginBottom: 12,
    },
  })
