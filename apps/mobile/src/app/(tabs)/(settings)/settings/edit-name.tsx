import React, { useState } from "react"
import { Alert } from "react-native"
import { useRouter } from "expo-router"
import { useAuthStore, useUser } from "../../../../stores"
import { FormButton, FormField, FormScreen, FormSection } from "../../../../components/form/form"

export default function EditNameScreen() {
  const router = useRouter()
  const user = useUser()
  const updateProfile = useAuthStore((s) => s.updateProfile)

  const [name, setName] = useState(user?.name ?? "")
  const [saving, setSaving] = useState(false)

  const canSave = name.trim().length > 0 && name.trim() !== (user?.name ?? "")

  const onSave = async () => {
    if (!canSave || saving) return
    setSaving(true)
    const result = await updateProfile({ name: name.trim() })
    setSaving(false)
    if (result === "OK") {
      router.back()
    } else {
      Alert.alert("Couldn't save", result)
    }
  }

  return (
    <FormScreen>
      <FormSection title="Name" footer="Shown to others when you respond to a message.">
        <FormField
          label="Name"
          value={name}
          onChangeText={setName}
          autoCapitalize="words"
          textContentType="name"
          returnKeyType="done"
          onSubmitEditing={onSave}
          placeholder="Your name"
          isLast
        />
      </FormSection>

      <FormButton title="Save" onPress={onSave} disabled={!canSave} loading={saving} />
    </FormScreen>
  )
}
