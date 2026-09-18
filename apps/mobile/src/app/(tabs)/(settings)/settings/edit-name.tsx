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

export default function EditNameScreen() {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const router = useRouter()
  const user = useUser()
  const updateProfile = useAuthStore((s) => s.updateProfile)

  const [name, setName] = useState(user?.name ?? "")
  const [saving, setSaving] = useState(false)

  const canSave = name.trim().length > 0 && name.trim() !== (user?.name ?? "")

  const onSave = async () => {
    if (!canSave) return
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
    <KeyboardAwareScrollView
      contentInsetAdjustmentBehavior="automatic"
      automaticallyAdjustKeyboardInsets
      keyboardShouldPersistTaps="handled"
      style={styles.container}
      contentContainerStyle={{ paddingBottom: 48 }}
    >
      <Text style={styles.label}>Name</Text>
        <TextInput
          style={styles.input}
          value={name}
          onChangeText={setName}
          autoFocus
          autoCapitalize="words"
          returnKeyType="done"
          onSubmitEditing={onSave}
          placeholder="Your name"
          placeholderTextColor={theme.colors.textSecondary}
        />
        <Text style={styles.hint}>This is the name shown to others when you respond to a message.</Text>

        <TouchableOpacity
          style={[styles.button, !canSave && styles.buttonDisabled]}
          onPress={onSave}
          disabled={!canSave || saving}
        >
          {saving ? (
            <ActivityIndicator color="#fff" />
          ) : (
            <Text style={styles.buttonText}>Save</Text>
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
