import { KeyboardAwareScrollView } from "react-native-keyboard-controller"
import React, { useState } from "react"
import {
  ActivityIndicator,
  Alert,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
} from "react-native"
import { useRouter } from "expo-router"
import { useAuthStore, useUser } from "../../../../stores"
import { useAppTheme, AppTheme } from "../../../../theme"

export default function EditEmailScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const user = useUser()
  const updateProfile = useAuthStore((s) => s.updateProfile)

  const [email, setEmail] = useState(user?.email ?? "")
  const [password, setPassword] = useState("")
  const [saving, setSaving] = useState(false)

  const emailLooksValid = /.+@.+\..+/.test(email.trim())
  const canSave =
    emailLooksValid &&
    email.trim().toLowerCase() !== (user?.email ?? "").toLowerCase() &&
    password.length > 0

  const onSave = async () => {
    if (!canSave) return
    setSaving(true)
    const result = await updateProfile({
      email: email.trim(),
      current_password: password,
    })
    setSaving(false)
    if (result === "OK") {
      Alert.alert("Email updated", "You'll need to verify the new address on your next sign-in.")
      router.back()
    } else {
      Alert.alert("Couldn't change email", result)
    }
  }

  return (
    <KeyboardAwareScrollView
      contentInsetAdjustmentBehavior="automatic"
      automaticallyAdjustKeyboardInsets
      keyboardShouldPersistTaps="handled"
      style={styles.container}
      contentContainerStyle={{ paddingBottom: 48 }}
    >
      <Text style={styles.label}>New email</Text>
        <TextInput
          style={styles.input}
          value={email}
          onChangeText={setEmail}
          autoFocus
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="email-address"
          returnKeyType="next"
          placeholder="you@example.com"
          placeholderTextColor={theme.colors.textSecondary}
        />

        <Text style={[styles.label, { marginTop: 16 }]}>Current password</Text>
        <TextInput
          style={styles.input}
          value={password}
          onChangeText={setPassword}
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          returnKeyType="done"
          onSubmitEditing={onSave}
          placeholder="Required to change your email"
          placeholderTextColor={theme.colors.textSecondary}
        />

        <Text style={styles.hint}>
          We require your current password before changing the email used to sign in.
        </Text>

        <TouchableOpacity
          style={[styles.button, !canSave && styles.buttonDisabled]}
          onPress={onSave}
          disabled={!canSave || saving}
        >
          {saving ? (
            <ActivityIndicator color="#fff" />
          ) : (
            <Text style={styles.buttonText}>Change email</Text>
          )}
        </TouchableOpacity>
    </KeyboardAwareScrollView>
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
