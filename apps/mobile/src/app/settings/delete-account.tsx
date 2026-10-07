import React, { useEffect, useState } from "react"
import { Alert, StyleSheet, Text, View } from "react-native"
import { useRouter } from "expo-router"
import { useAuthStore, useUser } from "../../stores"
import { useAppTheme, AppTheme } from "../../theme"
import { FormButton, FormField, FormScreen, FormSection } from "../../components/form/form"

/**
 * Apple requires every account to be deletable in the app. Accounts created
 * with Apple/Facebook sign-in have no password they know, so they confirm with
 * the alert alone; everyone else re-enters their password.
 */
export default function DeleteAccountScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const user = useUser()
  const deleteAccount = useAuthStore((s) => s.deleteAccount)
  const refreshUser = useAuthStore((s) => s.refreshUser)
  const hasPassword = user?.has_password !== false

  const [password, setPassword] = useState("")
  const [pending, setPending] = useState(false)

  useEffect(() => {
    void refreshUser()
  }, [refreshUser])

  const canSubmit = !hasPassword || password.length > 0

  const onSubmit = () => {
    if (!canSubmit || pending) return
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
            const result = await deleteAccount(hasPassword ? password : undefined)
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
    <FormScreen>
      <View style={styles.intro}>
        <Text style={styles.title}>This permanently deletes your account.</Text>
        <Text style={styles.body}>
          Your profile, capcode subscriptions, filters, alert zones and push registrations will be
          removed. Any systems you've registered will remain available to others but will no longer
          be linked to you.
        </Text>
        <Text style={styles.body}>
          An active App Store or Play Store subscription keeps billing until you cancel it in your
          store's subscription settings.
        </Text>
      </View>

      {hasPassword && (
        <FormSection title="Confirm it's you">
          <FormField
            label="Password"
            value={password}
            onChangeText={setPassword}
            secureTextEntry
            autoCapitalize="none"
            autoCorrect={false}
            textContentType="password"
            returnKeyType="done"
            onSubmitEditing={onSubmit}
            placeholder="Required"
            isLast
          />
        </FormSection>
      )}

      <FormButton
        title="Delete My Account"
        tone="destructive"
        onPress={onSubmit}
        disabled={!canSubmit}
        loading={pending}
      />
    </FormScreen>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    intro: { marginTop: 24, marginHorizontal: 32, gap: 10 },
    title: { fontSize: 20, fontWeight: "600", color: theme.colors.textPrimary },
    body: { fontSize: 15, lineHeight: 21, color: theme.colors.textSecondary },
  })
