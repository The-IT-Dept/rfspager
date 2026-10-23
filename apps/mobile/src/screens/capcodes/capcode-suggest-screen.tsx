import { KeyboardAwareScrollView } from "react-native-keyboard-controller"
/* eslint-disable react-native/no-inline-styles */
import React, { FC, useMemo, useState } from "react"
import {
  Alert,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native"
import { Stack, useLocalSearchParams, useRouter } from "expo-router"

import { useAppTheme, AppTheme } from "../../theme"
import {
  useCreateCapcodeSuggestion,
  useCapcodeMessages,
  useAgencies,
} from "../../hooks"

export const CapcodeSuggestScreen: FC = function CapcodeSuggestScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const params = useLocalSearchParams<{
    address?: string
    alias?: string
    agency?: string
    capcode_id?: string
  }>()

  const isAmendment = !!params.capcode_id
  const address = params.address ?? ""

  const [alias, setAlias] = useState(params.alias ?? "")
  const [agency, setAgency] = useState(params.agency ?? "")
  const [note, setNote] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [agencyFocused, setAgencyFocused] = useState(false)
  const mutation = useCreateCapcodeSuggestion()
  const recentMessages = useCapcodeMessages(address || null)
  const recent = recentMessages.data?.data ?? []
  const agenciesQuery = useAgencies()
  const allAgencies = agenciesQuery.data ?? []

  // Filter the agency suggestion list by what the user is typing. Empty
  // input shows the full sorted list so they can browse if they don't
  // know what to type yet.
  const filteredAgencies = useMemo(() => {
    const q = agency.trim().toLowerCase()
    const sorted = [...allAgencies].sort((a, b) => a.name.localeCompare(b.name))
    if (!q) return sorted.slice(0, 50)
    return sorted
      .filter((a) => a.name.toLowerCase().includes(q))
      .slice(0, 50)
  }, [allAgencies, agency])

  // Hide the suggestion list once the typed value exactly matches a known
  // agency name — they've made a choice, no need to keep showing options.
  const showSuggestions =
    agencyFocused &&
    filteredAgencies.length > 0 &&
    !allAgencies.some((a) => a.name.toLowerCase() === agency.trim().toLowerCase())

  const submit = async () => {
    if (!address) {
      Alert.alert("Missing address", "Cannot submit without a capcode address.")
      return
    }
    if (!alias.trim() && !agency.trim() && !note.trim()) {
      Alert.alert("Nothing to submit", "Provide an alias, an agency, or a note.")
      return
    }
    if (
      isAmendment &&
      alias.trim() === (params.alias ?? "").trim() &&
      agency.trim() === (params.agency ?? "").trim() &&
      !note.trim()
    ) {
      Alert.alert("Nothing changed", "Change the name or agency, or add a note for the reviewer.")
      return
    }
    setSubmitting(true)
    try {
      await mutation.mutateAsync({
        capcode_id: params.capcode_id ? Number(params.capcode_id) : null,
        address,
        alias: alias.trim(),
        agency: agency.trim(),
        note: note.trim(),
      })
      Alert.alert(
        "Thanks!",
        "Your suggestion is in the queue. An admin will review it soon.",
        [{ text: "OK", onPress: () => router.back() }],
      )
    } catch (e: any) {
      Alert.alert("Couldn't submit", e?.message ?? "Please try again later.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <View style={styles.container}>
      <Stack.Screen
        options={{
          title: isAmendment ? "Suggest Edit" : "Suggest Capcode",
          headerRight: () => (
            <TouchableOpacity
              onPress={submit}
              disabled={submitting}
              style={{ opacity: submitting ? 0.5 : 1 }}
            >
              <Text style={{ color: theme.colors.primary, fontSize: 17, fontWeight: "600" }}>
                Submit
              </Text>
            </TouchableOpacity>
          ),
        }}
      />

      <KeyboardAwareScrollView
        style={styles.scrollView}
        contentInsetAdjustmentBehavior="automatic"
        automaticallyAdjustKeyboardInsets
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={{ paddingBottom: 48 }}
      >
        <View style={styles.targetCard}>
          <Text style={styles.targetLabel}>
            {isAmendment ? "AMENDING CAPCODE" : "NEW CAPCODE"}
          </Text>
          <Text style={styles.targetAddress}>{address || "—"}</Text>
          {params.alias ? (
            <Text style={styles.targetCurrent}>
              Currently: <Text style={styles.targetCurrentValue}>{params.alias}</Text>
              {params.agency ? ` · ${params.agency}` : ""}
            </Text>
          ) : null}
        </View>

        <View style={styles.sectionHeader}>
          <Text style={styles.sectionTitle}>RECENT PAGES</Text>
        </View>
        <View style={styles.formCard}>
          {recentMessages.isLoading ? (
            <Text style={styles.recentEmpty}>Loading recent pages…</Text>
          ) : recent.length === 0 ? (
            <Text style={styles.recentEmpty}>
              No recent pages on file for this address.
            </Text>
          ) : (
            recent.map((m, i) => (
              <View
                key={m.id}
                style={[
                  styles.recentRow,
                  i === recent.length - 1 && styles.recentRowLast,
                ]}
              >
                <View style={styles.recentMeta}>
                  <Text style={styles.recentTime}>{relTime(m.created_at)}</Text>
                  {m.system_name && (
                    <Text style={styles.recentSystem} numberOfLines={1}>
                      via {m.system_name}
                    </Text>
                  )}
                </View>
                <Text style={styles.recentBody} numberOfLines={3}>
                  {m.message}
                </Text>
              </View>
            ))
          )}
        </View>

        <View style={styles.sectionHeader}>
          <Text style={styles.sectionTitle}>SUGGESTION</Text>
        </View>

        <View style={styles.formCard}>
          {/* Agency first — having the right agency matters more than the
              alias and the autocomplete keeps names consistent. */}
          <Field label="Agency" theme={theme}>
            <TextInput
              value={agency}
              onChangeText={setAgency}
              placeholder="Start typing — e.g. NSW Rural…"
              placeholderTextColor={theme.colors.textTertiary}
              style={styles.input}
              autoCapitalize="words"
              onFocus={() => setAgencyFocused(true)}
              onBlur={() => setTimeout(() => setAgencyFocused(false), 150)}
            />
          </Field>
          {showSuggestions && (
            <View style={styles.suggestionList}>
              {filteredAgencies.map((a) => (
                <Pressable
                  key={a.id}
                  onPress={() => {
                    setAgency(a.name)
                    setAgencyFocused(false)
                  }}
                  style={({ pressed }) => [
                    styles.suggestionItem,
                    pressed && styles.suggestionItemPressed,
                  ]}
                >
                  <Text style={styles.suggestionText} numberOfLines={1}>
                    {a.name}
                  </Text>
                  <Text style={styles.suggestionCount}>
                    {a.capcode_count}
                  </Text>
                </Pressable>
              ))}
            </View>
          )}
          <Field label="Alias" theme={theme} isLast>
            <TextInput
              value={alias}
              onChangeText={setAlias}
              placeholder="e.g. Cooma Fire Brigade"
              placeholderTextColor={theme.colors.textTertiary}
              style={styles.input}
              autoCapitalize="words"
            />
          </Field>
        </View>

        <View style={styles.sectionHeader}>
          <Text style={styles.sectionTitle}>NOTE FOR THE REVIEWER</Text>
        </View>
        <View style={styles.formCard}>
          <View style={styles.noteRow}>
            <TextInput
              value={note}
              onChangeText={setNote}
              placeholder="How do you know? Source, station, brigade…"
              placeholderTextColor={theme.colors.textTertiary}
              style={[styles.input, { minHeight: 80 }]}
              multiline
              textAlignVertical="top"
            />
          </View>
        </View>

        <Text style={styles.disclaimer}>
          Admins review every suggestion before changes go live.
        </Text>

        <TouchableOpacity
          style={[styles.submitButton, submitting && { opacity: 0.6 }]}
          onPress={submit}
          disabled={submitting}
          activeOpacity={0.8}
        >
          <Text style={styles.submitButtonText}>
            {submitting ? "Submitting…" : "Submit suggestion"}
          </Text>
        </TouchableOpacity>
      </KeyboardAwareScrollView>
    </View>
  )
}

function Field({
  label,
  children,
  theme,
  isLast,
}: {
  label: string
  children: React.ReactNode
  theme: AppTheme
  isLast?: boolean
}) {
  return (
    <View
      style={{
        paddingHorizontal: 16,
        paddingVertical: 11,
        borderBottomWidth: isLast ? 0 : 0.5,
        borderBottomColor: theme.colors.border,
      }}
    >
      <Text
        style={{
          fontSize: 12,
          fontWeight: "600",
          color: theme.colors.textSecondary,
          letterSpacing: 0.5,
          marginBottom: 4,
        }}
      >
        {label.toUpperCase()}
      </Text>
      {children}
    </View>
  )
}

function relTime(s: string): string {
  const d = new Date(s)
  const mins = Math.floor((Date.now() - d.getTime()) / 60000)
  if (mins < 1) return "now"
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return d.toLocaleDateString("en-AU", { day: "2-digit", month: "short" })
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    scrollView: {
      flex: 1,
    },
    targetCard: {
      marginHorizontal: 16,
      marginTop: 16,
      paddingHorizontal: 16,
      paddingVertical: 14,
      backgroundColor: theme.colors.cardBackground,
      borderRadius: 10,
    },
    targetLabel: {
      fontSize: 11,
      fontWeight: "700",
      color: theme.colors.primary,
      letterSpacing: 0.8,
      marginBottom: 6,
    },
    targetAddress: {
      fontSize: 24,
      fontWeight: "700",
      color: theme.colors.textPrimary,
      fontVariant: ["tabular-nums"],
    },
    targetCurrent: {
      marginTop: 6,
      fontSize: 13,
      color: theme.colors.textSecondary,
    },
    targetCurrentValue: {
      color: theme.colors.textPrimary,
      fontWeight: "600",
    },
    sectionHeader: {
      paddingHorizontal: 16,
      paddingTop: 20,
      paddingBottom: 6,
    },
    sectionTitle: {
      fontSize: 13,
      fontWeight: "400",
      color: theme.colors.textSecondary,
      letterSpacing: 0.4,
    },
    formCard: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      borderRadius: 10,
      overflow: "hidden",
    },
    noteRow: {
      paddingHorizontal: 16,
      paddingVertical: 11,
    },
    input: {
      fontSize: 17,
      color: theme.colors.textPrimary,
      paddingVertical: 0,
    },
    disclaimer: {
      paddingHorizontal: 16,
      paddingTop: 10,
      fontSize: 12,
      color: theme.colors.textTertiary,
    },
    submitButton: {
      marginHorizontal: 16,
      marginTop: 24,
      backgroundColor: theme.colors.primary,
      borderRadius: 10,
      paddingVertical: 14,
      alignItems: "center",
    },
    submitButtonText: {
      color: "#FFFFFF",
      fontSize: 17,
      fontWeight: "600",
    },
    // Agency suggestions
    suggestionList: {
      backgroundColor: theme.colors.cardBackground,
      maxHeight: 220,
      borderTopWidth: 0.5,
      borderTopColor: theme.colors.border,
    },
    suggestionItem: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 10,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    suggestionItemPressed: {
      backgroundColor: theme.colors.backgroundTertiary,
    },
    suggestionText: {
      flex: 1,
      fontSize: 15,
      color: theme.colors.textPrimary,
    },
    suggestionCount: {
      fontSize: 12,
      color: theme.colors.textTertiary,
      marginLeft: 12,
      fontVariant: ["tabular-nums"],
    },
    // Recent pages
    recentEmpty: {
      paddingHorizontal: 16,
      paddingVertical: 14,
      fontSize: 13,
      color: theme.colors.textTertiary,
      fontStyle: "italic",
    },
    recentRow: {
      paddingHorizontal: 16,
      paddingVertical: 10,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    recentRowLast: {
      borderBottomWidth: 0,
    },
    recentMeta: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      marginBottom: 2,
    },
    recentTime: {
      fontSize: 11,
      color: theme.colors.textTertiary,
      fontVariant: ["tabular-nums"],
    },
    recentSystem: {
      fontSize: 11,
      color: theme.colors.textTertiary,
      flex: 1,
      textAlign: "right",
      marginLeft: 8,
    },
    recentBody: {
      fontSize: 13,
      color: theme.colors.textPrimary,
      lineHeight: 17,
    },
  })
