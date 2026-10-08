import React from "react"
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native"
import { Ionicons } from "@expo/vector-icons"

import { AppTheme, useAppTheme } from "../../theme"

const POINTS: { icon: keyof typeof Ionicons.glyphMap; title: string; body: string }[] = [
  {
    icon: "headset",
    title: "Listen live",
    body: "Hear NSW emergency services traffic as it's spoken, one call at a time like a scanner — even with your screen locked.",
  },
  {
    icon: "chatbox-ellipses",
    title: "Read every call",
    body: "Each transmission is transcribed, so you can catch up on what was said without listening back.",
  },
  {
    icon: "play-circle",
    title: "Replay recordings",
    body: "Play any recorded call again, and filter the timeline to the talkgroups you care about.",
  },
]

/** What a free account sees on the Radio tab. */
export function RadioPaywall({ onSubscribe }: { onSubscribe: () => void }) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  return (
    <ScrollView contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
      <View style={styles.badge}>
        <Ionicons name="radio" size={30} color={theme.colors.primary} />
      </View>
      <Text style={styles.title}>Hear the radio behind the pages</Text>
      <Text style={styles.body}>
        Radio lets you listen to NSW emergency services radio live and read every call as text, from
        the same receivers that decode the pager feed. It's part of an RFSPager subscription.
      </Text>
      <View style={styles.points}>
        {POINTS.map((p) => (
          <View key={p.title} style={styles.point}>
            <Ionicons
              name={p.icon}
              size={22}
              color={theme.colors.primary}
              style={styles.pointIcon}
            />
            <View style={{ flex: 1 }}>
              <Text style={styles.pointTitle}>{p.title}</Text>
              <Text style={styles.pointBody}>{p.body}</Text>
            </View>
          </View>
        ))}
      </View>
      <Pressable
        onPress={onSubscribe}
        accessibilityRole="button"
        style={({ pressed }) => [styles.button, pressed && { opacity: 0.8 }]}
      >
        <Text style={styles.buttonText}>Subscribe to unlock Radio</Text>
      </Pressable>
    </ScrollView>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    content: {
      paddingHorizontal: 24,
      paddingTop: 24,
      paddingBottom: 48,
    },
    badge: {
      width: 60,
      height: 60,
      borderRadius: 30,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: theme.dark ? "rgba(247, 36, 29, 0.18)" : "#FEF2F2",
      marginBottom: 18,
    },
    title: {
      fontSize: 24,
      fontWeight: "700",
      color: theme.colors.textPrimary,
      marginBottom: 10,
    },
    body: {
      fontSize: 16,
      lineHeight: 23,
      color: theme.colors.textSecondary,
    },
    points: {
      marginTop: 24,
      marginBottom: 28,
      gap: 18,
    },
    point: {
      flexDirection: "row",
      alignItems: "flex-start",
    },
    pointIcon: {
      marginRight: 14,
      marginTop: 1,
    },
    pointTitle: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 2,
    },
    pointBody: {
      fontSize: 14,
      lineHeight: 20,
      color: theme.colors.textTertiary,
    },
    button: {
      backgroundColor: theme.colors.primary,
      borderRadius: 10,
      paddingVertical: 14,
      alignItems: "center",
    },
    buttonText: {
      color: theme.colors.white,
      fontSize: 16,
      fontWeight: "600",
    },
  })
