import { KeyboardAwareScrollView } from "react-native-keyboard-controller"
import React, { FC, useState } from "react"
import {
  Alert,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native"
import { Ionicons } from "@expo/vector-icons"
import { useRouter } from "expo-router"
import { useCreateSystem } from "../../hooks"
import { AppTheme, useAppTheme } from "../../theme"
import { HOSTING_TERMS_SECTIONS } from "./hosting-terms"

type Step = "hardware" | "terms" | "details"

export const CreateSystemScreen: FC = function CreateSystemScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const create = useCreateSystem()

  const [step, setStep] = useState<Step>("hardware")
  const [hasPi, setHasPi] = useState(false)
  const [hasSDR, setHasSDR] = useState(false)
  const [acceptedTerms, setAcceptedTerms] = useState(false)
  const [name, setName] = useState("")
  const [suburb, setSuburb] = useState("")
  const [notes, setNotes] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const hardwareReady = hasPi && hasSDR

  const submit = async () => {
    if (submitting) return
    if (!name.trim() || !suburb.trim()) {
      Alert.alert("Missing details", "Name and suburb are required.")
      return
    }
    setSubmitting(true)
    try {
      await create.mutateAsync({
        name: name.trim(),
        suburb: suburb.trim(),
        notes: notes.trim() || undefined,
        accepted_terms: true,
      })
      Alert.alert(
        "Request submitted",
        "An admin will review your system. We'll send a push notification when it's approved.",
        [{ text: "OK", onPress: () => router.back() }],
      )
    } catch (e: any) {
      Alert.alert("Couldn't submit request", e?.message || "Please try again.")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <KeyboardAwareScrollView
      style={{ flex: 1, backgroundColor: theme.colors.background }}
      contentInsetAdjustmentBehavior="automatic"
      automaticallyAdjustKeyboardInsets
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={[styles.scroll, { paddingBottom: 48 }]}
    >
      <Stepper step={step} theme={theme} />

        {step === "hardware" && (
          <View>
            <Text style={styles.heading}>Do you have the hardware?</Text>
            <Text style={styles.body}>
              An RFSPager receiver needs a Raspberry Pi 4 or 5 and an RTL-SDR USB dongle with a VHF
              antenna. We don't provide hardware — you'll host the gear at home or wherever you
              want coverage.
            </Text>

            <CheckRow
              label="I have a Raspberry Pi 4 or 5"
              checked={hasPi}
              onToggle={() => setHasPi((v) => !v)}
              theme={theme}
            />
            <CheckRow
              label="I have an RTL-SDR (or compatible) dongle + antenna"
              checked={hasSDR}
              onToggle={() => setHasSDR((v) => !v)}
              theme={theme}
            />

            <NextButton
              label="Continue"
              disabled={!hardwareReady}
              onPress={() => setStep("terms")}
              theme={theme}
            />
            {!hardwareReady && (
              <Text style={styles.hint}>
                You'll need both before we can approve your system. Pop back when you've got them.
              </Text>
            )}
          </View>
        )}

        {step === "terms" && (
          <View>
            <Text style={styles.heading}>Hosting terms</Text>
            <Text style={styles.body}>
              These are the rules every hosted system runs under. They're short, so please read.
            </Text>

            {HOSTING_TERMS_SECTIONS.map((s) => (
              <View key={s.title} style={styles.termsBlock}>
                <Text style={styles.termsTitle}>{s.title}</Text>
                <Text style={styles.termsBody}>{s.body}</Text>
              </View>
            ))}

            <CheckRow
              label="I agree to these hosting terms"
              checked={acceptedTerms}
              onToggle={() => setAcceptedTerms((v) => !v)}
              theme={theme}
            />

            <NextButton
              label="Continue"
              disabled={!acceptedTerms}
              onPress={() => setStep("details")}
              theme={theme}
            />
          </View>
        )}

        {step === "details" && (
          <View>
            <Text style={styles.heading}>System details</Text>
            <Text style={styles.body}>
              An admin will see this when reviewing your request.
            </Text>

            <Field
              label="Name"
              value={name}
              onChangeText={setName}
              placeholder="e.g. Central Coast 03"
              theme={theme}
              autoCapitalize="words"
            />
            <Field
              label="Suburb"
              value={suburb}
              onChangeText={setSuburb}
              placeholder="e.g. Gosford NSW"
              theme={theme}
              autoCapitalize="words"
            />
            <Field
              label="Notes to admin (optional)"
              value={notes}
              onChangeText={setNotes}
              placeholder="Why you want to host, coverage gaps, anything we should know."
              theme={theme}
              multiline
            />

            <NextButton
              label={submitting ? "Submitting…" : "Submit for approval"}
              disabled={submitting || !name.trim() || !suburb.trim()}
              onPress={submit}
              theme={theme}
            />
          </View>
        )}
    </KeyboardAwareScrollView>
  )
}

function Stepper({ step, theme }: { step: Step; theme: AppTheme }) {
  const styles = makeStyles(theme)
  const idx = step === "hardware" ? 0 : step === "terms" ? 1 : 2
  return (
    <View style={styles.stepperRow}>
      {["Hardware", "Terms", "Details"].map((label, i) => (
        <View key={label} style={styles.stepperItem}>
          <View
            style={[
              styles.stepperDot,
              {
                backgroundColor:
                  i <= idx ? theme.colors.primary : theme.colors.borderSecondary,
              },
            ]}
          />
          <Text
            style={[
              styles.stepperLabel,
              { color: i === idx ? theme.colors.textPrimary : theme.colors.textTertiary },
            ]}
          >
            {label}
          </Text>
        </View>
      ))}
    </View>
  )
}

function CheckRow({
  label,
  checked,
  onToggle,
  theme,
}: {
  label: string
  checked: boolean
  onToggle: () => void
  theme: AppTheme
}) {
  const styles = makeStyles(theme)
  return (
    <Pressable onPress={onToggle} style={styles.checkRow} hitSlop={4}>
      <Ionicons
        name={checked ? "checkbox" : "square-outline"}
        size={24}
        color={checked ? theme.colors.primary : theme.colors.textTertiary}
      />
      <Text style={styles.checkLabel}>{label}</Text>
    </Pressable>
  )
}

function NextButton({
  label,
  disabled,
  onPress,
  theme,
}: {
  label: string
  disabled?: boolean
  onPress: () => void
  theme: AppTheme
}) {
  const styles = makeStyles(theme)
  return (
    <TouchableOpacity
      style={[styles.nextButton, disabled && styles.nextButtonDisabled]}
      onPress={onPress}
      disabled={disabled}
    >
      <Text style={styles.nextButtonText}>{label}</Text>
    </TouchableOpacity>
  )
}

function Field({
  label,
  value,
  onChangeText,
  placeholder,
  multiline,
  autoCapitalize,
  theme,
}: {
  label: string
  value: string
  onChangeText: (s: string) => void
  placeholder?: string
  multiline?: boolean
  autoCapitalize?: "none" | "sentences" | "words" | "characters"
  theme: AppTheme
}) {
  const styles = makeStyles(theme)
  return (
    <View style={styles.field}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        style={[styles.fieldInput, multiline && styles.fieldInputMulti]}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={theme.colors.textTertiary}
        multiline={multiline}
        autoCapitalize={autoCapitalize ?? "sentences"}
      />
    </View>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    scroll: {
      padding: 16,
      paddingBottom: 48,
    },
    stepperRow: {
      flexDirection: "row",
      justifyContent: "space-between",
      marginBottom: 24,
      paddingHorizontal: 8,
    },
    stepperItem: {
      alignItems: "center",
      flex: 1,
    },
    stepperDot: {
      width: 10,
      height: 10,
      borderRadius: 5,
      marginBottom: 6,
    },
    stepperLabel: {
      fontSize: 12,
      fontWeight: "600",
    },
    heading: {
      fontSize: 22,
      fontWeight: "700",
      color: theme.colors.textPrimary,
      marginBottom: 8,
    },
    body: {
      fontSize: 15,
      color: theme.colors.textSecondary,
      lineHeight: 21,
      marginBottom: 20,
    },
    checkRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingVertical: 10,
    },
    checkLabel: {
      fontSize: 15,
      color: theme.colors.textPrimary,
      flex: 1,
    },
    nextButton: {
      backgroundColor: theme.colors.primary,
      borderRadius: 12,
      paddingVertical: 14,
      alignItems: "center",
      marginTop: 24,
    },
    nextButtonDisabled: {
      opacity: 0.4,
    },
    nextButtonText: {
      color: "#FFFFFF",
      fontWeight: "700",
      fontSize: 16,
    },
    hint: {
      marginTop: 12,
      fontSize: 13,
      color: theme.colors.textTertiary,
      textAlign: "center",
    },
    termsBlock: {
      marginBottom: 16,
    },
    termsTitle: {
      fontSize: 14,
      fontWeight: "700",
      color: theme.colors.textPrimary,
      marginBottom: 4,
    },
    termsBody: {
      fontSize: 14,
      color: theme.colors.textSecondary,
      lineHeight: 20,
    },
    field: {
      marginBottom: 16,
    },
    fieldLabel: {
      fontSize: 13,
      fontWeight: "600",
      color: theme.colors.textSecondary,
      marginBottom: 6,
    },
    fieldInput: {
      backgroundColor: theme.colors.cardBackground,
      borderRadius: 10,
      paddingHorizontal: 12,
      paddingVertical: 12,
      fontSize: 16,
      color: theme.colors.textPrimary,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: theme.colors.border,
    },
    fieldInputMulti: {
      minHeight: 90,
      textAlignVertical: "top",
    },
  })
