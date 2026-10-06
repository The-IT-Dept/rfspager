import React, { useEffect, useState } from "react"
import { Alert } from "react-native"
import { useRouter } from "expo-router"
import { useAuthStore, useUser } from "../../../../stores"
import { FormButton, FormField, FormScreen, FormSection } from "../../../../components/form/form"

/**
 * The email is the sign-in identifier, so changing it asks for the current
 * password, unless the account has none (created with Apple/Facebook sign-in,
 * often with an Apple private-relay address).
 */
export default function EditEmailScreen() {
  const router = useRouter()
  const user = useUser()
  const updateProfile = useAuthStore((s) => s.updateProfile)
  const refreshUser = useAuthStore((s) => s.refreshUser)
  const hasPassword = user?.has_password !== false

  const [email, setEmail] = useState(user?.email ?? "")
  const [password, setPassword] = useState("")
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    void refreshUser()
  }, [refreshUser])

  const trimmed = email.trim()
  const emailLooksValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)
  const changed = trimmed.toLowerCase() !== (user?.email ?? "").toLowerCase()
  const canSave = emailLooksValid && changed && (!hasPassword || password.length > 0)

  const onSave = async () => {
    if (!canSave || saving) return
    setSaving(true)
    const result = await updateProfile({
      email: trimmed,
      ...(hasPassword ? { current_password: password } : {}),
    })
    setSaving(false)
    if (result === "OK") {
      Alert.alert("Email updated", "Your other devices have been signed out. Use the new address next time you sign in.")
      router.back()
    } else {
      Alert.alert("Couldn't change email", result)
    }
  }

  return (
    <FormScreen>
      <FormSection
        title="Email"
        footer={
          trimmed.length > 0 && !emailLooksValid
            ? "That doesn't look like an email address."
            : "Used to sign in and for account messages."
        }
        footerTone={trimmed.length > 0 && !emailLooksValid ? "error" : "neutral"}
      >
        <FormField
          label="Email"
          value={email}
          onChangeText={setEmail}
          autoFocus
          keyboardType="email-address"
          autoCapitalize="none"
          autoCorrect={false}
          textContentType="emailAddress"
          returnKeyType={hasPassword ? "next" : "done"}
          onSubmitEditing={hasPassword ? undefined : onSave}
          placeholder="name@example.com"
          isLast
        />
      </FormSection>

      {hasPassword && (
        <FormSection title="Confirm it's you" footer="Enter your current password to change your sign-in email.">
          <FormField
            label="Password"
            value={password}
            onChangeText={setPassword}
            secureTextEntry
            autoCapitalize="none"
            autoCorrect={false}
            textContentType="password"
            returnKeyType="done"
            onSubmitEditing={onSave}
            placeholder="Required"
            isLast
          />
        </FormSection>
      )}

      <FormButton title="Save" onPress={onSave} disabled={!canSave} loading={saving} />
    </FormScreen>
  )
}
