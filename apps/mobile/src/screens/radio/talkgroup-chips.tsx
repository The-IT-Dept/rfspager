import React from "react"
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native"

import type { Talkgroup } from "../../services/api-client"
import { AppTheme, useAppTheme } from "../../theme"
import { agencyColour, agencyName } from "./radio-format"

/** "All" plus every talkgroup heard so far, most recently active first. */
export function TalkgroupChips({
  talkgroups,
  activeId,
  onSelect,
}: {
  talkgroups: Talkgroup[]
  activeId: number | null
  onSelect: (tgid: number | null) => void
}) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.content}
      style={styles.scroll}
    >
      <Chip
        label="All"
        selected={activeId === null}
        onPress={() => onSelect(null)}
        styles={styles}
      />
      {talkgroups.map((t) => (
        <Chip
          key={t.id}
          label={t.alpha_tag ?? `Talkgroup ${t.tgid}`}
          colour={agencyColour(t.agency)}
          accessibilityHint={
            [agencyName(t.agency), t.description].filter(Boolean).join(", ") || undefined
          }
          selected={activeId === t.tgid}
          onPress={() => onSelect(activeId === t.tgid ? null : t.tgid)}
          styles={styles}
        />
      ))}
    </ScrollView>
  )
}

function Chip({
  label,
  colour,
  selected,
  onPress,
  accessibilityHint,
  styles,
}: {
  label: string
  colour?: string
  selected: boolean
  onPress: () => void
  accessibilityHint?: string
  styles: ReturnType<typeof makeStyles>
}) {
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityState={{ selected }}
      accessibilityHint={accessibilityHint}
      style={({ pressed }) => [
        styles.chip,
        selected && styles.chipSelected,
        pressed && styles.pressed,
      ]}
    >
      {colour && <View style={[styles.stripe, { backgroundColor: colour }]} />}
      <Text style={[styles.label, selected && styles.labelSelected]} numberOfLines={1}>
        {label}
      </Text>
    </Pressable>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    scroll: {
      flexGrow: 0,
      backgroundColor: theme.colors.cardBackground,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: theme.colors.border,
    },
    content: {
      paddingHorizontal: 16,
      paddingVertical: 10,
      gap: 8,
    },
    chip: {
      flexDirection: "row",
      alignItems: "center",
      height: 32,
      paddingHorizontal: 12,
      borderRadius: 16,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    chipSelected: {
      backgroundColor: theme.colors.textPrimary,
    },
    pressed: {
      opacity: 0.7,
    },
    stripe: {
      width: 3,
      height: 14,
      borderRadius: 2,
      marginRight: 7,
    },
    label: {
      fontSize: 14,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      maxWidth: 200,
    },
    labelSelected: {
      color: theme.colors.background,
    },
  })
