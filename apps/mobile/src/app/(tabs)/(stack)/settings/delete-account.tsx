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

export default function DeleteAccountScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const deleteAccount = useAuthStore((s) => s.deleteAccount)

  const [password, setPassword] = useState("")
  const [pending, setPending] = useState(false)

  const onSubmit = () => {
    if (password.length === 0) return
    Alert.alert(
      "Delete account?",
      "This permanently removes your account, capcode subscriptions, alert zones and push registrations. It cannot be undone.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: async () => {
            setPending(true)
            const result = await deleteAccount(password)
            setPending(false)
            if (result === "OK") {
              // The store has already cleared the session; root layout will
              // redirect to the auth flow on the next render.
              router.replace("/(auth)/login" as any)
            } else {
              Alert.alert("Couldn't delete account", result)
            }
          },
        },
      ],
    )
  }

  return (
    <KeyboardAvoidingView
      style={{ flex: 1, backgroundColor: theme.colors.backgroundTertiary }}
      behavior={Platform.OS === "ios" ? "padding" : undefined}
    >
      <ScrollView contentInsetAdjustmentBehavior="automatic" style={styles.container}>
        <Text style={styles.title}>This will permanently delete your account.</Text>
        <Text style={styles.body}>
          Your profile, capcode subscriptions, filters, alert zones and push registrations will be
          removed. Any systems you've registered will remain available to others but will no longer
          be linked to you.{"\n\n"}
          An active App Store or Play Store subscription will continue to bill until you cancel it
          in your store's subscription settings.
        </Text>

        <Text style={styles.label}>Confirm with your password</Text>
        <TextInput
          style={styles.input}
          value={password}
          onChangeText={setPassword}
          autoFocus
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          returnKeyType="done"
          onSubmitEditing={onSubmit}
        />

        <TouchableOpacity
          style={[styles.button, password.length === 0 && styles.buttonDisabled]}
          onPress={onSubmit}
          disabled={password.length === 0 || pending}
        >
          {pending ? (
            <ActivityIndicator color="#fff" />
          ) : (
            <Text style={styles.buttonText}>Delete my account</Text>
          )}
        </TouchableOpacity>
      </ScrollView>
    </KeyboardAvoidingView>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: { flex: 1, padding: 16 },
    title: {
      fontSize: 17,
      fontWeight: "600",
      color: theme.colors.error,
      marginBottom: 8,
    },
    body: {
      fontSize: 15,
      color: theme.colors.textPrimary,
      marginBottom: 24,
      lineHeight: 21,
    },
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
    button: {
      marginTop: 24,
      backgroundColor: theme.colors.error,
      paddingVertical: 14,
      borderRadius: 10,
      alignItems: "center",
    },
    buttonDisabled: { opacity: 0.4 },
    buttonText: { color: "#fff", fontSize: 17, fontWeight: "600" },
  })
