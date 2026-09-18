import React, { FC, useState } from "react"
import {
  Alert,
  Image,
  View,
  TouchableOpacity,
  TextInput,
  SafeAreaView,
} from "react-native"
import { useAuthStore } from "../../stores"
import { ActivityIndicator, Text } from "react-native"
import { KeyboardAwareScrollView } from "react-native-keyboard-controller"

import { emailValidator } from "../../utils/validators"
import { useRouter } from "expo-router"
import { Images } from "../../constants"
import { useAppTheme } from "../../theme"
import { StyleSheet } from "react-native"
import { AppTheme } from "../../theme/rfspager"

export const AuthForgotPasswordScreen: FC = () => {
    const router = useRouter()
    const theme = useAppTheme()
    const styles = makeStyles(theme)

    const storeForgotPassword = useAuthStore((s) => s.forgotPassword)

    const [loading, setLoading] = useState(false)
    const [error, setError] = useState("")
    const [email, setEmail] = useState({ value: "", error: "" })

    const forgotPassword = async () => {
      setLoading(true)
      setError("") // Clear previous errors
      const emailError = emailValidator(email.value)

      if (emailError) {
        setEmail({ ...email, error: emailError })
        setLoading(false)
        return
      }

      const result = await storeForgotPassword(email.value.toLowerCase())

      if (result !== "OK") {
        setError("There has been an error sending password reset email. Please try again.")
        setLoading(false)
      } else {
        Alert.alert("Success", "Password reset email sent. Please see email to complete.", [
          {
            text: "Back to Login",
            onPress: () => router.push("/(auth)/login"),
          },
        ])
      }
    }

    return (
      <SafeAreaView style={styles.container}>
        <KeyboardAwareScrollView
          showsVerticalScrollIndicator={false}
          automaticallyAdjustKeyboardInsets
          keyboardShouldPersistTaps="handled"
          contentContainerStyle={{ paddingBottom: 48 }}
        >
          <View style={styles.headerSection}>
            <Image source={Images.forgetPassword} resizeMode="contain" style={styles.imageStyle} />
            <Text style={styles.forgotPassword}>Forgot Password?</Text>
          </View>

          <View style={styles.contentWrapper}>
            <View style={styles.formSection}>
              <Text style={styles.subTitle}>
                Don't worry! It happens. Please enter the address associated with your account.
              </Text>

              <View style={styles.inputContainer}>
                <Text style={styles.inputLabel}>Email</Text>
                <TextInput
                  placeholder="Enter your email"
                  placeholderTextColor={theme.colors.textTertiary}
                  returnKeyType="done"
                  value={email.value}
                  onChangeText={(text) => setEmail({ value: text, error: "" })}
                  autoCapitalize="none"
                  autoComplete="email"
                  textContentType="emailAddress"
                  keyboardType="email-address"
                  style={[styles.textInput, email.error ? styles.textInputError : {}]}
                />
                {!!email.error && <Text style={styles.errorText}>{email.error}</Text>}
              </View>

              {!!error && <Text style={styles.errorText}>{error}</Text>}

              <TouchableOpacity
                style={[styles.submitButton, loading ? styles.submitButtonDisabled : {}]}
                onPress={forgotPassword}
                disabled={loading}
              >
                {loading ? (
                  <ActivityIndicator size="small" color={theme.colors.white} />
                ) : (
                  <Text style={styles.submitLabel}>Submit</Text>
                )}
              </TouchableOpacity>
            </View>

            <View style={styles.bottomSection}>
              <View style={styles.bottomTextContainer}>
                <Text style={styles.rememberPassword}>Remember your password? </Text>
                <TouchableOpacity onPress={() => router.push("/(auth)/login")}>
                  <Text style={styles.loginLink}>Login</Text>
                </TouchableOpacity>
              </View>
            </View>
          </View>
        </KeyboardAwareScrollView>
      </SafeAreaView>
    )
  }

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    headerSection: {
      backgroundColor: theme.colors.cardBackground,
      alignItems: "center",
      paddingVertical: 40,
      paddingHorizontal: 20,
    },
    imageStyle: {
      width: 80,
      height: 80,
      marginBottom: 16,
    },
    contentWrapper: {
      flex: 1,
      paddingHorizontal: 16,
      paddingTop: 20,
    },
    forgotPassword: {
      fontSize: 28,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      textAlign: "center",
    },
    formSection: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 20,
      marginBottom: 20,
    },
    subTitle: {
      color: theme.colors.textTertiary,
      fontSize: 16,
      lineHeight: 22,
      textAlign: "center",
      marginBottom: 24,
    },
    inputContainer: {
      marginBottom: 16,
    },
    inputLabel: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 8,
    },
    textInput: {
      height: 50,
      backgroundColor: theme.colors.backgroundTertiary,
      borderRadius: 0,
      borderWidth: 1,
      borderColor: theme.colors.border,
      paddingHorizontal: 16,
      fontSize: 16,
      color: theme.colors.textPrimary,
    },
    textInputError: {
      borderColor: theme.colors.error,
    },
    errorText: {
      fontSize: 13,
      color: theme.colors.error,
      marginTop: 4,
    },
    submitButton: {
      backgroundColor: theme.colors.primary,
      height: 50,
      justifyContent: "center",
      alignItems: "center",
      marginTop: 20,
    },
    submitButtonDisabled: {
      backgroundColor: theme.colors.borderSecondary,
    },
    submitLabel: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.white,
    },
    bottomSection: {
      backgroundColor: theme.colors.cardBackground,
      paddingHorizontal: 16,
      paddingVertical: 20,
    },
    bottomTextContainer: {
      flexDirection: "row",
      justifyContent: "center",
      alignItems: "center",
    },
    rememberPassword: {
      color: theme.colors.textTertiary,
      fontSize: 15,
      fontWeight: "400",
    },
    loginLink: {
      color: theme.colors.interactive,
      fontSize: 15,
      fontWeight: "600",
    },
  })
