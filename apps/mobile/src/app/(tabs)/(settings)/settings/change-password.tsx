import React, { useEffect, useState } from "react"
import { Alert } from "react-native"
import { Stack, useRouter } from "expo-router"
import { useAuthStore, useUser } from "../../../../stores"
import { FormButton, FormField, FormScreen, FormSection } from "../../../../components/form/form"

/**
 * Change, or for accounts created with Apple/Facebook sign-in, set a password.
 *
 * Those accounts never had a password they know (the server stored a random
 * one), so asking for the "current" password locked them out. /me reports
 * has_password; until it is known we assume a password exists, the safe side.
 */
export default function ChangePasswordScreen() {
  const router = useRouter()
  const user = useUser()
  const updatePassword = useAuthStore((s) => s.updatePassword)
  const refreshUser = useAuthStore((s) => s.refreshUser)
  const hasPassword = user?.has_password !== false

  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    void refreshUser()
  }, [refreshUser])

  const tooShort = next.length > 0 && next.length < 8
  const mismatch = confirm.length > 0 && next !== confirm
  const canSave = (!hasPassword || current.length > 0) && next.length >= 8 && next === confirm

  const onSave = async () => {
    if (!canSave || saving) return
    setSaving(true)
    const result = await updatePassword(hasPassword ? current : undefined, next, confirm)
    setSaving(false)
    if (result === "OK") {
      Alert.alert(
        hasPassword ? "Password changed" : "Password set",
        hasPassword
          ? "Your other devices have been signed out."
          : "You can now also sign in with your email address and this password.",
      )
      router.back()
    } else {
      Alert.alert(hasPassword ? "Couldn't change password" : "Couldn't set password", result)
    }
  }

  const footer = tooShort
    ? "Use at least 8 characters."
    : mismatch
      ? "The passwords don't match."
      : hasPassword
        ? "Changing your password signs out your other devices."
        : "You signed up with Apple or Facebook, so there is no password yet. Set one to also sign in with your email address."

  return (
    <FormScreen>
      <Stack.Screen options={{ title: hasPassword ? "Change Password" : "Set Password" }} />

      {hasPassword && (
        <FormSection title="Current">
          <FormField
            label="Password"
            value={current}
            onChangeText={setCurrent}
            autoFocus
            secureTextEntry
            autoCapitalize="none"
            autoCorrect={false}
            textContentType="password"
            returnKeyType="next"
            placeholder="Required"
            isLast
          />
        </FormSection>
      )}

      <FormSection
        title={hasPassword ? "New" : "Password"}
        footer={footer}
        footerTone={tooShort || mismatch ? "error" : "neutral"}
      >
        <FormField
          label="New"
          value={next}
          onChangeText={setNext}
          autoFocus={!hasPassword}
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          textContentType="newPassword"
          returnKeyType="next"
          placeholder="At least 8 characters"
        />
        <FormField
          label="Confirm"
          value={confirm}
          onChangeText={setConfirm}
          secureTextEntry
          autoCapitalize="none"
          autoCorrect={false}
          textContentType="newPassword"
          returnKeyType="done"
          onSubmitEditing={onSave}
          placeholder="Repeat it"
          isLast
        />
      </FormSection>

      <FormButton
        title={hasPassword ? "Change Password" : "Set Password"}
        onPress={onSave}
        disabled={!canSave}
        loading={saving}
      />
    </FormScreen>
  )
}
