import React, { useState } from "react"
import {
  ActivityIndicator,
  Alert,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
} from "react-native"
import { useRouter } from "expo-router"
import { useAuthStore } from "../../../../stores"
import { useAppTheme, AppTheme } from "../../../../theme"

export default function ChangePasswordScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const updatePassword = useAuthStore((s) => s.updatePassword)

  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [saving, setSaving] = useState(false)

  const passwordsMatch = next.length >= 8 && next === confirm
  const canSave = current.length > 0 && passwordsMatch

  const onSave = async () => {
    if (!canSave) return
    setSaving(true)
    const result = await updatePassword(current, next, confirm)
    setSaving(false)
    if (result === "OK") {
      Alert.alert("Password updated", "Your password has been changed.")
      router.back()
    } else {
      Alert.alert("Couldn't change password", result)
    }
  }

  return (
    <KeyboardAvoidingView
      style={{ flex: 1, backgroundColor: theme.colors.backgroundTertiary }}
      behavior={Platform.OS === "ios" ? "padding" : undefined}
    >
      <ScrollView contentInsetAdjustmentBehavior="automatic" style={styles.container}>
        <Text style={styles.label}>Current password</Text>
        <TextInput
          style={styles.input}
          value={current}
          onChangeText={setCurrent}
          autoFocus
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          textContentType="password"
          returnKeyType="next"
        />

        <Text style={[styles.label, { marginTop: 16 }]}>New password</Text>
        <TextInput
          style={styles.input}
          value={next}
          onChangeText={setNext}
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          textContentType="newPassword"
          returnKeyType="next"
          placeholder="At least 8 characters"
          placeholderTextColor={theme.colors.textSecondary}
        />

        <Text style={[styles.label, { marginTop: 16 }]}>Confirm new password</Text>
        <TextInput
          style={styles.input}
          value={confirm}
          onChangeText={setConfirm}
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          textContentType="newPassword"
          returnKeyType="done"
          onSubmitEditing={onSave}
        />

        {confirm.length > 0 && next !== confirm && (
          <Text style={[styles.hint, { color: theme.colors.error }]}>Passwords don't match.</Text>
        )}

        <TouchableOpacity
          style={[styles.button, !canSave && styles.buttonDisabled]}
          onPress={onSave}
          disabled={!canSave || saving}
        >
          {saving ? (
            <ActivityIndicator color="#fff" />
          ) : (
            <Text style={styles.buttonText}>Change password</Text>
          )}
        </TouchableOpacity>
      </ScrollView>
    </KeyboardAvoidingView>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: { flex: 1, padding: 16 },
    label: {
      fontSize: 13,
      color: theme.colors.textSecondary,
      textTransform: "uppercase",
      letterSpacing: 0.5,
      marginBottom: 6,
      marginLeft: 4,
    },
    input: {
      backgroundColor: theme.colors.cardBackground,
      color: theme.colors.textPrimary,
      borderRadius: 10,
      paddingHorizontal: 12,
      paddingVertical: 14,
      fontSize: 17,
    },
    hint: {
      marginTop: 8,
      marginHorizontal: 4,
      fontSize: 13,
      color: theme.colors.textSecondary,
    },
    button: {
      marginTop: 24,
      backgroundColor: theme.colors.primary,
      paddingVertical: 14,
      borderRadius: 10,
      alignItems: "center",
    },
    buttonDisabled: { opacity: 0.4 },
    buttonText: { color: "#fff", fontSize: 17, fontWeight: "600" },
  })
