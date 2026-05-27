/* eslint-disable react-native/no-inline-styles */
import React, { FC, useState } from "react"
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native"
import { Stack, useLocalSearchParams, useRouter } from "expo-router"

import { useAppTheme, AppTheme } from "../../theme"
import { useCreateCapcodeSuggestion } from "../../hooks"

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
  const mutation = useCreateCapcodeSuggestion()

  const submit = async () => {
    if (!address) {
      Alert.alert("Missing address", "Cannot submit without a capcode address.")
      return
    }
    if (!alias.trim() && !agency.trim() && !note.trim()) {
      Alert.alert("Nothing to submit", "Provide an alias, an agency, or a note.")
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
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : undefined}
      style={styles.container}
    >
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

      <ScrollView
        style={styles.scrollView}
        contentInsetAdjustmentBehavior="automatic"
        keyboardShouldPersistTaps="handled"
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
          <Text style={styles.sectionTitle}>SUGGESTION</Text>
        </View>

        <View style={styles.formCard}>
          <Field label="Alias" theme={theme}>
            <TextInput
              value={alias}
              onChangeText={setAlias}
              placeholder="e.g. Cooma Fire Brigade"
              placeholderTextColor={theme.colors.textTertiary}
              style={styles.input}
              autoCapitalize="words"
            />
          </Field>
          <Field label="Agency" theme={theme} isLast>
            <TextInput
              value={agency}
              onChangeText={setAgency}
              placeholder="e.g. NSW Rural Fire Service"
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
      </ScrollView>
    </KeyboardAvoidingView>
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
  })
